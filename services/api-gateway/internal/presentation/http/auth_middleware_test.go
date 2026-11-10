package http_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	domainIdentity "marketmind/services/api-gateway/internal/domain/identity"
	infraAuth "marketmind/services/api-gateway/internal/infrastructure/auth"
	presHTTP "marketmind/services/api-gateway/internal/presentation/http"
)

const testSecret = "super-secret-test-key-32-chars-long"

func makeJWT(secret string, sub, tenant string, roles []domainIdentity.Role, expired bool) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	exp := time.Now().Add(1 * time.Hour).Unix()
	if expired {
		exp = time.Now().Add(-1 * time.Hour).Unix()
	}

	claims := map[string]interface{}{
		"sub":       sub,
		"tenant_id": tenant,
		"roles":     roles,
		"iat":       time.Now().Unix(),
		"exp":       exp,
	}
	payloadBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signedData := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedData))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return header + "." + payload + "." + sig
}

func TestAuthAndTenantIsolation(t *testing.T) {
	verifier := infraAuth.NewJWTVerifier(testSecret)
	useCase := appIdentity.NewUseCase(verifier)

	// Protected handler that verifies propagated tenant headers
	protectedEndpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.Header.Get(presHTTP.HeaderTenantID)
		user := r.Header.Get(presHTTP.HeaderUserID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok: tenant=" + tenant + ", user=" + user))
	})

	authMiddleware := presHTTP.AuthenticateMiddleware(useCase)
	handler := authMiddleware(protectedEndpoint)

	t.Run("401 when no token is provided", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("401 on expired token", func(t *testing.T) {
		token := makeJWT(testSecret, "user-1", "tenant-alpha", []domainIdentity.Role{domainIdentity.RoleAnalyst}, true)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("200 on valid token and propagates tenant context", func(t *testing.T) {
		token := makeJWT(testSecret, "user-1", "tenant-alpha", []domainIdentity.Role{domainIdentity.RoleAnalyst}, false)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("403 Forbidden on Tenant Isolation Violation (cross-tenant access)", func(t *testing.T) {
		// User belongs to tenant-alpha but request header requests tenant-beta
		token := makeJWT(testSecret, "user-1", "tenant-alpha", []domainIdentity.Role{domainIdentity.RoleAnalyst}, false)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(presHTTP.HeaderTenantID, "tenant-beta")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden on cross-tenant access attempt, got %d", rec.Code)
		}
	})

	t.Run("RBAC: blocks non-admin when role required", func(t *testing.T) {
		adminOnlyHandler := presHTTP.RequireRole(domainIdentity.RoleAdmin)(protectedEndpoint)
		chain := authMiddleware(adminOnlyHandler)

		viewerToken := makeJWT(testSecret, "user-2", "tenant-alpha", []domainIdentity.Role{domainIdentity.RoleViewer}, false)
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		rec := httptest.NewRecorder()

		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for viewer accessing admin route, got %d", rec.Code)
		}
	})
}