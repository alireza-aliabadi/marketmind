package http_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	appHealth "marketmind/services/api-gateway/internal/application/health"
	domainHealth "marketmind/services/api-gateway/internal/domain/health"
	infraHealth "marketmind/services/api-gateway/internal/infrastructure/health"
	presHTTP "marketmind/services/api-gateway/internal/presentation/http"
)

func setupTestServer() http.Handler {
	checker := infraHealth.NewSystemChecker("test-gateway", "0.1.0", "test")
	useCase := appHealth.NewUseCase(checker)
	handler := presHTTP.NewHandler(useCase)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return presHTTP.TracingMiddleware(logger)(mux)
}

func TestHealthEndpoints(t *testing.T) {
	server := setupTestServer()

	t.Run("GET /health returns 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var body domainHealth.SystemHealth
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if body.Status != domainHealth.StatusHealthy {
			t.Errorf("expected status 'healthy', got '%s'", body.Status)
		}
		if len(body.Components) == 0 {
			t.Errorf("expected components to be populated")
		}
		if rec.Header().Get(presHTTP.RequestIDHeader) == "" {
			t.Errorf("expected X-Request-ID header to be present")
		}
	})

	t.Run("GET /health/live returns 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if body["status"] != "healthy" {
			t.Errorf("expected status healthy, got %s", body["status"])
		}
	})

	t.Run("GET /health/ready returns 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("preserves custom X-Request-ID", func(t *testing.T) {
		customID := "test-request-id-999"
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		req.Header.Set(presHTTP.RequestIDHeader, customID)
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Header().Get(presHTTP.RequestIDHeader) != customID {
			t.Errorf("expected %s, got %s", customID, rec.Header().Get(presHTTP.RequestIDHeader))
		}
		if rec.Header().Get(presHTTP.CorrelationIDHeader) != customID {
			t.Errorf("expected %s, got %s", customID, rec.Header().Get(presHTTP.CorrelationIDHeader))
		}
	})
}
