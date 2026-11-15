package auth_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	domain "marketmind/services/api-gateway/internal/domain/identity"
	"marketmind/services/api-gateway/internal/infrastructure/auth"
	"marketmind/services/api-gateway/internal/support/oidctest"
)

func verifier(t *testing.T, provider *oidctest.Provider) *auth.OIDCVerifier {
	t.Helper()
	result, err := auth.NewOIDCVerifier(context.Background(), auth.OIDCConfig{
		IssuerURL: provider.URL(), Audience: "marketmind-api", AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOIDCDiscoveryAndVerifiedClaims(t *testing.T) {
	provider := oidctest.New(t)
	result := verifier(t, provider)
	claims, err := result.VerifyToken(context.Background(), provider.Token(t, map[string]any{
		"aud": []string{"another-resource", "marketmind-api"}, "email": "analyst@example.test",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != provider.URL() || claims.Subject != "user-1" || claims.TenantID != "org-alpha" ||
		claims.Email != "analyst@example.test" || !claims.HasPermission(domain.PermissionResearchWrite) {
		t.Fatalf("incorrect verified identity projection: %+v", claims)
	}
	if provider.DiscoveryRequests.Load() != 1 || provider.JWKSRequests.Load() != 1 {
		t.Fatal("discovery and public JWKS were not used")
	}
}

func TestOIDCRejectsInvalidTokens(t *testing.T) {
	provider := oidctest.New(t)
	result := verifier(t, provider)
	cases := []struct {
		name   string
		claims map[string]any
	}{
		{"wrong issuer", map[string]any{"iss": "https://untrusted.example"}},
		{"issuer trailing slash", map[string]any{"iss": provider.URL() + "/"}},
		{"wrong audience", map[string]any{"aud": "browser-client"}},
		{"missing audience", map[string]any{"aud": nil}},
		{"expired", map[string]any{"exp": time.Now().Add(-time.Second).Unix()}},
		{"expiry boundary", map[string]any{"exp": time.Now().Unix()}},
		{"missing expiry", map[string]any{"exp": nil}},
		{"missing issued-at", map[string]any{"iat": nil}},
		{"future issued-at", map[string]any{"iat": time.Now().Add(time.Minute).Unix()}},
		{"future not-before within SDK leeway", map[string]any{"nbf": time.Now().Add(time.Minute).Unix()}},
		{"missing subject", map[string]any{"sub": nil}},
		{"missing tenant", map[string]any{"tenant_id": nil}},
		{"blank tenant", map[string]any{"tenant_id": " "}},
		{"unsafe subject", map[string]any{"sub": "user\nadmin"}},
		{"malformed roles", map[string]any{"roles": "admin"}},
		{"unsafe roles", map[string]any{"roles": []string{"viewer,admin"}}},
		{"malformed permissions", map[string]any{"permissions": 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			claims, err := result.VerifyToken(context.Background(), provider.Token(t, test.claims))
			if claims != nil || (!errors.Is(err, domain.ErrInvalidToken) && !errors.Is(err, domain.ErrExpiredToken)) {
				t.Fatalf("invalid token accepted: claims=%+v err=%v", claims, err)
			}
		})
	}
	t.Run("tampered payload", func(t *testing.T) {
		parts := strings.Split(provider.Token(t, nil), ".")
		parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker"}`))
		if _, err := result.VerifyToken(context.Background(), strings.Join(parts, ".")); !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatal("tampered token accepted")
		}
	})
	t.Run("wrong key", func(t *testing.T) {
		other := oidctest.New(t)
		if _, err := result.VerifyToken(context.Background(), other.Token(t, map[string]any{"iss": provider.URL()})); !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatal("token signed by an untrusted key accepted")
		}
	})
	for _, token := range []string{"not-a-jwt", "a.b.c.d", strings.Repeat("a", 16385),
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + ".e30.",
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`)) + ".e30.signature"} {
		if _, err := result.VerifyToken(context.Background(), token); !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatal("malformed/unsigned/HMAC token accepted")
		}
	}
}

func TestOIDCOAuthScopesGrantOnlyExplicitActions(t *testing.T) {
	provider := oidctest.New(t)
	result := verifier(t, provider)
	claims, err := result.VerifyToken(context.Background(), provider.Token(t, map[string]any{
		"roles": nil, "scope": "openid profile sample:read", "permissions": []string{"market:read"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !claims.HasPermission(domain.PermissionSampleRead) || !claims.HasPermission(domain.PermissionMarketRead) ||
		claims.HasPermission(domain.PermissionResearchWrite) || claims.HasPermission("openid") {
		t.Fatal("OAuth2 scopes/permissions were not action-scoped")
	}
}

func TestOIDCJWKSCacheRotationAndFailure(t *testing.T) {
	provider := oidctest.New(t)
	result := verifier(t, provider)
	token := provider.Token(t, nil)
	for range 2 {
		if _, err := result.VerifyToken(context.Background(), token); err != nil {
			t.Fatal(err)
		}
	}
	if provider.JWKSRequests.Load() != 1 {
		t.Fatal("known signing key was not cached")
	}
	provider.FailJWKS.Store(true)
	if _, err := result.VerifyToken(context.Background(), token); err != nil {
		t.Fatal("cached trusted key should remain usable during a JWKS outage")
	}
	provider.Rotate(t)
	rotated := provider.Token(t, nil)
	if _, err := result.VerifyToken(context.Background(), rotated); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatal("unknown key accepted during JWKS outage")
	}
	provider.FailJWKS.Store(false)
	if _, err := result.VerifyToken(context.Background(), rotated); err != nil {
		t.Fatalf("rotated key was not refreshed after recovery: %v", err)
	}
}

func TestOIDCDiscoveryFailsClosed(t *testing.T) {
	for _, name := range []string{"unavailable", "issuer-mismatch", "unsafe-jwks"} {
		t.Run(name, func(t *testing.T) {
			provider := oidctest.New(t)
			switch name {
			case "unavailable":
				provider.FailDiscovery.Store(true)
			case "issuer-mismatch":
				provider.SetMetadata("https://untrusted.example", "")
			case "unsafe-jwks":
				provider.SetMetadata("", "http://untrusted.example/keys")
			}
			if _, err := auth.NewOIDCVerifier(context.Background(), auth.OIDCConfig{
				IssuerURL: provider.URL(), Audience: "marketmind-api", AllowInsecureHTTP: true,
			}); err == nil {
				t.Fatal("invalid/unavailable discovery accepted")
			}
		})
	}
}
