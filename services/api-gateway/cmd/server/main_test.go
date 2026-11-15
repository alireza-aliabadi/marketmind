package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"marketmind/services/api-gateway/internal/support/oidctest"
)

type childProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error // Read only after done is closed.
	logPath string
	once    sync.Once
}

func processEnvironment(values map[string]string) []string {
	environment := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[name]; !replaced {
			environment = append(environment, entry)
		}
	}
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func startProcess(t *testing.T, command *exec.Cmd, logPath string) *childProcess {
	t.Helper()
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout, command.Stderr = log, log
	command.WaitDelay = 5 * time.Second
	if err := command.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	process := &childProcess{command: command, done: make(chan struct{}), logPath: logPath}
	go func() {
		process.err = command.Wait()
		_ = log.Close()
		close(process.done)
	}()
	t.Cleanup(func() {
		process.stop(t)
		if t.Failed() {
			data, _ := os.ReadFile(process.logPath)
			t.Logf("%s:\n%s", filepath.Base(process.logPath), data)
		}
	})
	return process
}

func (p *childProcess) stop(t *testing.T) {
	t.Helper()
	p.once.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = p.command.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = p.command.Process.Kill()
			<-p.done
			t.Error("child process did not shut down gracefully")
		}
	})
}

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()
	return port
}

func waitForHealth(t *testing.T, client *http.Client, process *childProcess, baseURL string) {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-process.done:
			t.Fatalf("service exited before readiness: %v", process.err)
		case <-deadline.C:
			t.Fatal("service did not become ready within 15 seconds")
		case <-ticker.C:
			response, err := client.Get(baseURL + "/health/ready")
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return
				}
			}
		}
	}
}

func request(t *testing.T, client *http.Client, baseURL, method, path, token string, headers http.Header, expected int) (*http.Response, any) {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if headers != nil {
		req.Header = headers.Clone()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 65536))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != expected {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, expected, response.StatusCode, body)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("non-JSON response for %s: %s", path, body)
	}
	return response, value
}

func validateResponseContract(t *testing.T, root, path string, response *http.Response, value any) {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "contracts/openapi/api-gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var document map[string]any
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	operation := document["paths"].(map[string]any)[path].(map[string]any)["get"].(map[string]any)
	declared, ok := operation["responses"].(map[string]any)[strconv.Itoa(response.StatusCode)]
	if !ok {
		t.Fatalf("missing contract response: %s/%d", path, response.StatusCode)
	}
	contract := declared.(map[string]any)
	for contract["$ref"] != nil {
		var target any = document
		for _, part := range strings.Split(strings.TrimPrefix(contract["$ref"].(string), "#/"), "/") {
			target = target.(map[string]any)[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
		}
		contract = target.(map[string]any)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		t.Fatal("response content type differs from contract")
	}
	for header := range contract["headers"].(map[string]any) {
		if response.Header.Get(header) == "" {
			t.Fatalf("missing contract response header: %s", header)
		}
	}
	schemaRef := contract["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"].(string)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	const location = "urn:marketmind:gateway:e2e"
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location + schemaRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatalf("actual gateway response violates contract: %v", err)
	}
}

// TestGatewayEndToEnd runs the actual gateway executable against the actual
// Python service. It needs no database, Docker, external IdP or private .env.
func TestGatewayEndToEnd(t *testing.T) {
	if os.Getenv("RUN_GATEWAY_INTEGRATION_TESTS") != "1" {
		t.Skip("run make gateway-test for the Go -> FastAPI runtime scenario")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "gateway")
	buildContext, cancelBuild := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gateway build failed: %v\n%s", err, output)
	}
	python := filepath.Join(root, ".venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Fatal("Python environment missing; run uv sync --extra dev --frozen")
	}
	client := &http.Client{Timeout: 7 * time.Second}
	samplePort := unusedPort(t)
	sampleURL := "http://127.0.0.1:" + samplePort
	sampleCommand := exec.Command(python, "-m", "uvicorn", "src.presentation.http.main:app", "--app-dir", root, "--host", "127.0.0.1", "--port", samplePort)
	sampleCommand.Dir = directory // Settings cannot read the development .env here.
	sampleCommand.Env = processEnvironment(map[string]string{"APP_NAME": "phase1-sample", "APP_ENV": "test", "APP_VERSION": "0.1.0"})
	sample := startProcess(t, sampleCommand, filepath.Join(directory, "sample.log"))
	waitForHealth(t, client, sample, sampleURL)
	provider := oidctest.New(t)
	gatewayPort := unusedPort(t)
	gatewayURL := "http://127.0.0.1:" + gatewayPort
	gatewayCommand := exec.Command(binary)
	gatewayCommand.Dir = directory
	gatewayCommand.Env = processEnvironment(map[string]string{
		"APP_NAME": "phase1-gateway", "APP_ENV": "test", "GATEWAY_HOST": "127.0.0.1", "GATEWAY_PORT": gatewayPort,
		"DOWNSTREAM_URL": sampleURL, "OIDC_ISSUER_URL": provider.URL(), "OIDC_AUDIENCE": "marketmind-api", "OIDC_ALLOW_INSECURE_HTTP": "true",
	})
	gateway := startProcess(t, gatewayCommand, filepath.Join(directory, "gateway.log"))
	waitForHealth(t, client, gateway, gatewayURL)
	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		response, value := request(t, client, gatewayURL, "GET", path, "", nil, 200)
		validateResponseContract(t, root, path, response, value)
	}
	viewer := provider.Token(t, map[string]any{"roles": []string{"viewer"}})
	for _, path := range []string{"/api/v1/sample/health", "/api/v1/sample/health/live", "/api/v1/sample/health/ready"} {
		response, value := request(t, client, gatewayURL, "GET", path, viewer, http.Header{
			"X-Request-Id": {"phase1-request"}, "X-Correlation-Id": {"phase1-correlation"},
		}, 200)
		validateResponseContract(t, root, path, response, value)
		if response.Header.Get("X-Request-ID") != "phase1-request" || response.Header.Get("X-Correlation-ID") != "phase1-correlation" ||
			value.(map[string]any)["status"] != "healthy" {
			t.Fatal("gateway did not reach the healthy sample with propagated tracing")
		}
	}
	cases := []struct {
		name    string
		token   string
		headers http.Header
		status  int
	}{
		{"missing bearer", "", nil, 401},
		{"expired", provider.Token(t, map[string]any{"exp": time.Now().Add(-time.Second).Unix()}), nil, 401},
		{"wrong API audience", provider.Token(t, map[string]any{"aud": "browser-client"}), nil, 401},
		{"wrong issuer", provider.Token(t, map[string]any{"iss": "https://untrusted.example"}), nil, 401},
		{"missing organization", provider.Token(t, map[string]any{"tenant_id": nil}), nil, 401},
		{"future not-before", provider.Token(t, map[string]any{"nbf": time.Now().Add(time.Minute).Unix()}), nil, 401},
		{"no permission", provider.Token(t, map[string]any{"roles": nil}), nil, 403},
		{"cross organization", viewer, http.Header{"X-Tenant-Id": {"org-beta"}}, 403},
		{"ambiguous organization", viewer, http.Header{"X-Tenant-Id": {"org-alpha", "org-beta"}}, 403},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := "/api/v1/sample/health/live"
			response, value := request(t, client, gatewayURL, "GET", path, test.token, test.headers, test.status)
			validateResponseContract(t, root, path, response, value)
		})
	}
	request(t, client, gatewayURL, "POST", "/api/v1/research", viewer, nil, 403)
	methodResponse, methodValue := request(t, client, gatewayURL, "POST", "/api/v1/sample/health", viewer, nil, 405)
	validateResponseContract(t, root, "/api/v1/sample/health", methodResponse, methodValue)
	provider.Rotate(t)
	request(t, client, gatewayURL, "GET", "/api/v1/sample/health/live", provider.Token(t, nil), nil, 200)
	sample.stop(t)
	sampleLog, err := os.ReadFile(sample.logPath)
	if err != nil || !strings.Contains(string(sampleLog), "Application shutdown complete.") ||
		!strings.Contains(string(sampleLog), "Finished server process") {
		t.Fatalf("sample lifecycle shutdown did not complete: %v", err)
	}
	if sample.err != nil {
		// Uvicorn re-raises the captured signal after graceful ASGI shutdown.
		// Accept only that exact signal, backed by the completed lifecycle log.
		var exit *exec.ExitError
		if !errors.As(sample.err, &exit) {
			t.Fatalf("unexpected sample shutdown error: %v", sample.err)
		}
		status, ok := exit.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
			t.Fatalf("unexpected sample shutdown status: %v", sample.err)
		}
	}
	response, value := request(t, client, gatewayURL, "GET", "/api/v1/sample/health/ready", provider.Token(t, nil), nil, 502)
	validateResponseContract(t, root, "/api/v1/sample/health/ready", response, value)
	request(t, client, gatewayURL, "GET", "/health/live", "", nil, 200)
	gateway.stop(t)
	if gateway.err != nil {
		t.Fatalf("gateway did not shut down cleanly: %v", gateway.err)
	}
	badContext, cancelBad := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelBad()
	badGateway := exec.CommandContext(badContext, binary)
	badGateway.Env = processEnvironment(map[string]string{"OIDC_ISSUER_URL": "", "OIDC_AUDIENCE": "", "OIDC_ALLOW_INSECURE_HTTP": "false"})
	if output, err := badGateway.CombinedOutput(); err == nil || !strings.Contains(string(output), "invalid gateway configuration") {
		t.Fatalf("unconfigured gateway did not fail closed: %v %s", err, output)
	}
	t.Log("PASS: real Go -> FastAPI proxy; OIDC discovery/JWKS rotation; 200/401/403/405/502 contracts; tenant/RBAC denial; graceful shutdown; missing-config startup denial")
}
