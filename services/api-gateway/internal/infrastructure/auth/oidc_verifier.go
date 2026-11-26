package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	domain "marketmind/services/api-gateway/internal/domain/identity"
)

type OIDCConfig struct {
	IssuerURL         string
	Audience          string
	AllowInsecureHTTP bool // Explicitly permitted only for loopback development issuers.
}

type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
	issuer   string
}

func (c OIDCConfig) Validate() error {
	if c.Audience == "" || strings.TrimSpace(c.Audience) != c.Audience {
		return errors.New("OIDC_AUDIENCE must identify the API resource, not the browser client")
	}
	if err := validateOIDCEndpoint(c.IssuerURL, c.AllowInsecureHTTP); err != nil {
		return fmt.Errorf("OIDC_ISSUER_URL: %w", err)
	}
	issuer, _ := url.Parse(c.IssuerURL)
	if issuer.RawQuery != "" {
		return errors.New("OIDC_ISSUER_URL must not contain a query")
	}
	return nil
}

func validateOIDCEndpoint(value string, allowHTTP bool) error {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return errors.New("an absolute HTTPS URL without credentials or fragments is required")
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(endpoint.Hostname())
	loopback := endpoint.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if endpoint.Scheme == "http" && allowHTTP && loopback {
		return nil
	}
	return errors.New("HTTPS is required; explicitly enabled HTTP is restricted to loopback")
}

// NewOIDCVerifier discovers the configured provider and uses its rotating public
// JWKS. It never accepts a caller-supplied issuer/key URL or falls back to HS256.
func NewOIDCVerifier(ctx context.Context, config OIDCConfig) (*OIDCVerifier, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	providerContext := oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(providerContext, config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}
	var metadata struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("invalid OIDC metadata: %w", err)
	}
	if err := validateOIDCEndpoint(metadata.JWKSURL, config.AllowInsecureHTTP); err != nil {
		return nil, fmt.Errorf("invalid OIDC jwks_uri: %w", err)
	}
	return &OIDCVerifier{
		issuer: config.IssuerURL,
		verifier: provider.VerifierContext(providerContext, &oidc.Config{
			ClientID:             config.Audience,
			SupportedSigningAlgs: []string{oidc.RS256},
		}),
	}, nil
}

func (v *OIDCVerifier) VerifyToken(ctx context.Context, tokenString string) (*domain.UserClaims, error) {
	if len(tokenString) > 16384 || strings.Count(tokenString, ".") != 2 {
		return nil, domain.ErrInvalidToken
	}
	token, err := v.verifier.Verify(ctx, tokenString)
	if err != nil {
		var expired *oidc.TokenExpiredError
		if errors.As(err, &expired) {
			return nil, domain.ErrExpiredToken
		}
		return nil, domain.ErrInvalidToken
	}
	var raw struct {
		TenantID    string        `json:"tenant_id"`
		Email       string        `json:"email"`
		Roles       []domain.Role `json:"roles"`
		Permissions []string      `json:"permissions"`
		Scope       string        `json:"scope"`
		IssuedAt    int64         `json:"iat"`
		ExpiresAt   int64         `json:"exp"`
		NotBefore   int64         `json:"nbf"`
	}
	if err := token.Claims(&raw); err != nil || token.Issuer != v.issuer || raw.NotBefore > time.Now().Unix() {
		return nil, domain.ErrInvalidToken
	}
	claims := &domain.UserClaims{
		Issuer:      token.Issuer,
		Subject:     token.Subject,
		TenantID:    raw.TenantID,
		Email:       raw.Email,
		Roles:       raw.Roles,
		Permissions: append(raw.Permissions, strings.Fields(raw.Scope)...),
		IssuedAt:    time.Unix(raw.IssuedAt, 0),
		ExpiresAt:   time.Unix(raw.ExpiresAt, 0),
	}
	if err := claims.Validate(time.Now()); err != nil {
		return nil, err
	}
	return claims, nil
}
