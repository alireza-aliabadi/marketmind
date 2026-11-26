// Package oidctest is a disposable test issuer. Only test targets import it;
// it is not an authentication provider or token endpoint for application use.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

type Provider struct {
	Server            *httptest.Server
	DiscoveryRequests atomic.Int64
	JWKSRequests      atomic.Int64
	FailDiscovery     atomic.Bool
	FailJWKS          atomic.Bool
	mu                sync.RWMutex
	key               *rsa.PrivateKey
	keyID             string
	version           int
	issuerOverride    string
	jwksOverride      string
}

func New(t testing.TB) *Provider {
	t.Helper()
	provider := &Provider{}
	provider.Rotate(t)
	// Publish the server pointer before starting its request goroutines. This
	// matters when discovery comes from the gateway in a separate OS process.
	provider.Server = httptest.NewUnstartedServer(http.HandlerFunc(provider.serveHTTP))
	provider.Server.Start()
	t.Cleanup(provider.Server.Close)
	return provider
}

func (p *Provider) URL() string { return p.Server.URL }

func (p *Provider) Rotate(t testing.TB) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.version++
	p.key, p.keyID = key, fmt.Sprintf("fixture-key-%d", p.version)
}

func (p *Provider) SetMetadata(issuer, jwks string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.issuerOverride, p.jwksOverride = issuer, jwks
}

func (p *Provider) Token(t testing.TB, overrides map[string]any) string {
	t.Helper()
	claims := map[string]any{
		"iss": p.URL(), "aud": "marketmind-api", "sub": "user-1",
		"tenant_id": "org-alpha", "roles": []string{"analyst"},
		"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	for key, value := range overrides {
		if value == nil {
			delete(claims, key)
		} else {
			claims[key] = value
		}
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	key := jose.JSONWebKey{Key: p.key, KeyID: p.keyID, Algorithm: "RS256", Use: "sig"}
	p.mu.RUnlock()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("at+jwt"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(body)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (p *Provider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		p.DiscoveryRequests.Add(1)
		if p.FailDiscovery.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		p.mu.RLock()
		issuer, jwks := p.issuerOverride, p.jwksOverride
		p.mu.RUnlock()
		if issuer == "" {
			issuer = p.URL()
		}
		if jwks == "" {
			jwks = p.URL() + "/jwks"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "jwks_uri": jwks,
			"authorization_endpoint": p.URL() + "/authorize", "token_endpoint": p.URL() + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/jwks":
		p.JWKSRequests.Add(1)
		if p.FailJWKS.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		p.mu.RLock()
		key := jose.JSONWebKey{Key: &p.key.PublicKey, KeyID: p.keyID, Algorithm: "RS256", Use: "sig"}
		p.mu.RUnlock()
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
