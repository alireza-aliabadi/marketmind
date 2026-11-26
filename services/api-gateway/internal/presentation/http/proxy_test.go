package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	domainIdentity "marketmind/services/api-gateway/internal/domain/identity"
	"marketmind/services/api-gateway/internal/infrastructure/auth"
	presHTTP "marketmind/services/api-gateway/internal/presentation/http"
	"marketmind/services/api-gateway/internal/support/oidctest"
)

func gatewayProxy(t *testing.T, provider *oidctest.Provider, target string) *httptest.Server {
	t.Helper()
	verifier, err := auth.NewOIDCVerifier(context.Background(), auth.OIDCConfig{
		IssuerURL: provider.URL(), Audience: "marketmind-api", AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	presHTTP.RegisterProxyRoutes(mux, appIdentity.NewUseCase(verifier), targetURL, logger)
	server := httptest.NewServer(presHTTP.TracingMiddleware(logger)(mux))
	t.Cleanup(server.Close)
	return server
}

func TestRegisteredProxyRoutesEnforceTenantPermissions(t *testing.T) {
	provider := oidctest.New(t)
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path": r.URL.RequestURI(), "tenant": r.Header.Get(presHTTP.HeaderTenantID),
			"user": r.Header.Get(presHTTP.HeaderUserID), "roles": r.Header.Get(presHTTP.HeaderUserRoles),
			"permissions": r.Header.Get(presHTTP.HeaderUserPermissions),
			"request_id":  r.Header.Get(presHTTP.RequestIDHeader), "correlation_id": r.Header.Get(presHTTP.CorrelationIDHeader),
			"authorization": r.Header.Get("Authorization"), "forwarded": r.Header.Get("Forwarded"),
			"forwarded_for": r.Header.Get("X-Forwarded-For"),
			"organization":  r.Header.Get("X-Organization-ID"), "email": r.Header.Get("X-User-Email"),
		})
	}))
	t.Cleanup(upstream.Close)
	gateway := gatewayProxy(t, provider, upstream.URL)
	cases := []struct {
		name    string
		method  string
		path    string
		claims  map[string]any
		headers http.Header
		noToken bool
		status  int
	}{
		{"sample viewer", "GET", "/api/v1/sample/health/live", map[string]any{"roles": []string{"viewer"}}, nil, false, 200},
		{"sample scope only", "GET", "/api/v1/sample/health", map[string]any{"roles": nil, "scope": "sample:read"}, nil, false, 200},
		{"market viewer", "GET", "/api/v1/market/prices", map[string]any{"roles": []string{"viewer"}}, nil, false, 200},
		{"market root", "GET", "/api/v1/market", nil, nil, false, 200},
		{"research viewer read", "GET", "/api/v1/research/run-1", map[string]any{"roles": []string{"viewer"}}, nil, false, 200},
		{"research viewer write", "POST", "/api/v1/research/", map[string]any{"roles": []string{"viewer"}}, nil, false, 403},
		{"research viewer write root", "POST", "/api/v1/research", map[string]any{"roles": []string{"viewer"}}, nil, false, 403},
		{"research analyst write", "POST", "/api/v1/research", nil, nil, false, 200},
		{"research admin write", "POST", "/api/v1/research/", map[string]any{"roles": []string{"admin"}}, nil, false, 200},
		{"research explicit permission", "POST", "/api/v1/research", map[string]any{"roles": nil, "permissions": []string{"research:write"}}, nil, false, 200},
		{"no role or permission", "GET", "/api/v1/sample/health", map[string]any{"roles": nil}, nil, false, 403},
		{"unknown role", "GET", "/api/v1/market/", map[string]any{"roles": []string{"unrecognized"}}, nil, false, 403},
		{"cross tenant", "GET", "/api/v1/sample/health", nil, http.Header{presHTTP.HeaderTenantID: {"org-beta"}}, false, 403},
		{"duplicate tenant", "GET", "/api/v1/sample/health", nil, http.Header{presHTTP.HeaderTenantID: {"org-alpha", "org-beta"}}, false, 403},
		{"missing tenant claim", "GET", "/api/v1/market/", map[string]any{"tenant_id": nil}, nil, false, 401},
		{"expired token", "GET", "/api/v1/market/", map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}, nil, false, 401},
		{"missing token", "GET", "/api/v1/sample/health", nil, nil, true, 401},
		{"market write denied", "POST", "/api/v1/market/", nil, nil, false, 405},
		{"research unknown method denied", "DELETE", "/api/v1/research/", nil, nil, false, 405},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := upstreamCalls.Load()
			request, err := http.NewRequest(test.method, gateway.URL+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.headers != nil {
				request.Header = test.headers.Clone()
			}
			if !test.noToken {
				request.Header.Set("Authorization", "Bearer "+provider.Token(t, test.claims))
			}
			response, err := gateway.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("expected %d, got %d", test.status, response.StatusCode)
			}
			if response.Header.Get("Content-Type") != "application/json; charset=utf-8" && test.status != 200 {
				t.Fatal("authorization failures must have a JSON wire response")
			}
			if test.status == 401 && response.Header.Get("WWW-Authenticate") == "" {
				t.Fatal("OAuth2 bearer challenge missing")
			}
			if test.status == 405 && response.Header.Get("Allow") == "" {
				t.Fatal("allowed methods missing")
			}
			if test.status != 200 && upstreamCalls.Load() != before {
				t.Fatal("rejected request reached the downstream service")
			}
		})
	}
}

func TestProxyReplacesSpoofedIdentityAndForwardsTracing(t *testing.T) {
	provider := oidctest.New(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.RequestURI(), "headers": r.Header})
	}))
	t.Cleanup(upstream.Close)
	gateway := gatewayProxy(t, provider, upstream.URL)
	for _, suppliedTracing := range []bool{false, true} {
		t.Run("trusted headers", func(t *testing.T) {
			request, _ := http.NewRequest("GET", gateway.URL+"/api/v1/sample/health/live?probe=1", nil)
			request.Header.Set("Authorization", "Bearer "+provider.Token(t, map[string]any{"roles": []string{"viewer"}}))
			request.Header.Set(presHTTP.HeaderTenantID, "org-alpha")
			request.Header.Set(presHTTP.HeaderUserID, "spoofed-user")
			request.Header.Set(presHTTP.HeaderUserRoles, "admin")
			request.Header.Set(presHTTP.HeaderUserPermissions, "research:write")
			request.Header.Set("X-Organization-ID", "spoofed-org")
			request.Header.Set("X-User-Email", "spoofed@example.test")
			request.Header.Set("X-Forwarded-For", "spoofed-client")
			request.Header.Set("Forwarded", "for=spoofed-client")
			request.Header.Set("Connection", "X-Tenant-ID, X-User-ID, X-User-Roles, X-Request-ID, X-Correlation-ID")
			if suppliedTracing {
				request.Header.Set(presHTTP.RequestIDHeader, "request-123")
				request.Header.Set(presHTTP.CorrelationIDHeader, "correlation-123")
			}
			response, err := gateway.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("proxy failed: %d", response.StatusCode)
			}
			var body struct {
				Path    string      `json:"path"`
				Headers http.Header `json:"headers"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Path != "/health/live?probe=1" || body.Headers.Get(presHTTP.HeaderTenantID) != "org-alpha" ||
				body.Headers.Get(presHTTP.HeaderUserID) != "user-1" || body.Headers.Get(presHTTP.HeaderUserRoles) != "viewer" ||
				strings.Contains(body.Headers.Get(presHTTP.HeaderUserPermissions), "research:write") {
				t.Fatalf("incorrect trusted identity/routing: %+v", body)
			}
			for _, name := range []string{"Authorization", "Forwarded", "X-Organization-ID", "X-User-Email"} {
				if body.Headers.Get(name) != "" {
					t.Fatalf("untrusted/sensitive header forwarded: %s", name)
				}
			}
			if strings.Contains(body.Headers.Get("X-Forwarded-For"), "spoofed") {
				t.Fatal("caller-supplied forwarding identity was trusted")
			}
			for _, name := range []string{presHTTP.RequestIDHeader, presHTTP.CorrelationIDHeader} {
				if body.Headers.Get(name) == "" || body.Headers.Get(name) != response.Header.Get(name) {
					t.Fatal("generated/supplied tracing was not propagated")
				}
			}
		})
	}
}

func TestProxyDownstreamFailureIsBoundedJSON(t *testing.T) {
	provider := oidctest.New(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := upstream.URL
	upstream.Close()
	gateway := gatewayProxy(t, provider, address)
	request, _ := http.NewRequest("GET", gateway.URL+"/api/v1/sample/health", nil)
	request.Header.Set("Authorization", "Bearer "+provider.Token(t, nil))
	response, err := gateway.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadGateway || body["error"] != "downstream service unavailable" {
		t.Fatalf("unexpected proxy failure response: %d %v", response.StatusCode, body)
	}
}

func TestPermissionGuardWithoutAuthentication(t *testing.T) {
	handler := presHTTP.RequirePermission(domainIdentity.PermissionSampleRead)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("permission guard admitted an unauthenticated request")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("missing principal must produce 401")
	}
}
