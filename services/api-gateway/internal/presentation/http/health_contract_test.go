package http_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	appHealth "marketmind/services/api-gateway/internal/application/health"
	domainHealth "marketmind/services/api-gateway/internal/domain/health"
	presHTTP "marketmind/services/api-gateway/internal/presentation/http"
)

type contractChecker struct {
	status domainHealth.HealthStatus
}

func (c contractChecker) CheckLiveness() domainHealth.HealthStatus {
	return domainHealth.StatusHealthy
}

func (c contractChecker) CheckReadiness() domainHealth.SystemHealth {
	return domainHealth.NewSystemHealth(c.status, "0.1.0", []domainHealth.ComponentHealth{
		{Name: "fixture", Status: c.status},
	})
}

func TestGatewayHealthProducerContract(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "contracts", "openapi", "api-gateway.json")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var document interface{}
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	const location = "urn:marketmind:api:gateway:0.1.0"
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	states := []domainHealth.HealthStatus{domainHealth.StatusHealthy, domainHealth.StatusDegraded, domainHealth.StatusUnhealthy}
	for _, state := range states {
		for _, route := range []string{"/health", "/health/live", "/health/ready"} {
			t.Run(string(state)+route, func(t *testing.T) {
				mux := http.NewServeMux()
				presHTTP.NewHandler(appHealth.NewUseCase(contractChecker{state})).RegisterRoutes(mux)
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				handler := presHTTP.TracingMiddleware(logger)(mux)
				request := httptest.NewRequest(http.MethodGet, route, nil)
				request.Header.Set(presHTTP.RequestIDHeader, "contract-test")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				status := http.StatusOK
				if route == "/health/ready" && state != domainHealth.StatusHealthy {
					status = http.StatusServiceUnavailable
				}
				if response.Code != status {
					t.Fatalf("status %d, expected %d", response.Code, status)
				}
				mediaType, _, err := mime.ParseMediaType(response.Header().Get("Content-Type"))
				if err != nil || mediaType != "application/json" {
					t.Fatalf("unexpected Content-Type: %s", response.Header().Get("Content-Type"))
				}
				var body interface{}
				bodyDecoder := json.NewDecoder(response.Body)
				bodyDecoder.UseNumber()
				if err := bodyDecoder.Decode(&body); err != nil {
					t.Fatal(err)
				}
				doc := document.(map[string]interface{})
				operation := doc["paths"].(map[string]interface{})[route].(map[string]interface{})["get"].(map[string]interface{})
				declared, ok := operation["responses"].(map[string]interface{})[strconv.Itoa(status)]
				if !ok {
					t.Fatalf("contract does not declare %s status %d", route, status)
				}
				responseContract := declared.(map[string]interface{})
				for responseContract["$ref"] != nil {
					var target interface{} = doc
					for _, part := range strings.Split(strings.TrimPrefix(responseContract["$ref"].(string), "#/"), "/") {
						target = target.(map[string]interface{})[part]
					}
					responseContract = target.(map[string]interface{})
				}
				content := responseContract["content"].(map[string]interface{})["application/json"].(map[string]interface{})
				ref := content["schema"].(map[string]interface{})["$ref"].(string)
				schema, err := compiler.Compile(location + ref)
				if err != nil {
					t.Fatal(err)
				}
				if err := schema.Validate(body); err != nil {
					t.Fatalf("producer response violates contract: %v", err)
				}
				for _, header := range []string{presHTTP.RequestIDHeader, presHTTP.CorrelationIDHeader} {
					if response.Header().Get(header) != "contract-test" {
						t.Fatalf("missing or changed tracing header %s", header)
					}
				}
			})
		}
	}
}
