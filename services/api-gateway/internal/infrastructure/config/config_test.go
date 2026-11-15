package config_test

import (
	"testing"

	"marketmind/services/api-gateway/internal/infrastructure/config"
)

func environment(overrides map[string]string) func(string) string {
	values := map[string]string{"OIDC_ISSUER_URL": "https://identity.example/realm", "OIDC_AUDIENCE": "marketmind-api"}
	for key, value := range overrides {
		values[key] = value
	}
	return func(key string) string { return values[key] }
}

func TestGatewayConfiguration(t *testing.T) {
	result, err := config.Load(environment(map[string]string{"PORT": "8000"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != "127.0.0.1:8080" || result.DownstreamURL.String() != "http://127.0.0.1:8000" || result.OIDC.AllowInsecureHTTP {
		t.Fatal("gateway defaults must be distinct from the Python sample's PORT")
	}
	local, err := config.Load(environment(map[string]string{
		"OIDC_ISSUER_URL": "http://127.0.0.1:9005", "OIDC_ALLOW_INSECURE_HTTP": "true",
		"GATEWAY_PORT": "8090", "GATEWAY_HOST": "::1",
	}))
	if err != nil || local.Address != "[::1]:8090" || !local.OIDC.AllowInsecureHTTP {
		t.Fatalf("explicit loopback development configuration failed: %v", err)
	}
}

func TestInvalidGatewayConfigurationFailsClosed(t *testing.T) {
	cases := []map[string]string{
		{"OIDC_ISSUER_URL": ""}, {"OIDC_AUDIENCE": ""}, {"OIDC_AUDIENCE": " "},
		{"OIDC_ISSUER_URL": "relative/path"}, {"OIDC_ISSUER_URL": "http://localhost:9005"},
		{"OIDC_ISSUER_URL": "http://identity.example", "OIDC_ALLOW_INSECURE_HTTP": "true"},
		{"OIDC_ISSUER_URL": "https://user:password@identity.example"},
		{"OIDC_ISSUER_URL": "https://identity.example?issuer=other"},
		{"OIDC_ISSUER_URL": "https://identity.example#fragment"},
		{"OIDC_ALLOW_INSECURE_HTTP": "not-a-bool"},
		{"OIDC_ALLOW_INSECURE_HTTP": "true", "APP_ENV": "production"},
		{"OIDC_ALLOW_INSECURE_HTTP": "true", "APP_ENV": "staging"},
		{"GATEWAY_PORT": "0"}, {"GATEWAY_PORT": "65536"}, {"GATEWAY_PORT": "abc"},
		{"DOWNSTREAM_URL": "relative/path"}, {"DOWNSTREAM_URL": "ftp://sample.example"},
		{"DOWNSTREAM_URL": "http://user:password@sample.example"},
		{"DOWNSTREAM_URL": "http://sample.example?tenant=other"},
		{"DOWNSTREAM_URL": "http://sample.example#fragment"},
	}
	for _, values := range cases {
		t.Run("invalid", func(t *testing.T) {
			if _, err := config.Load(environment(values)); err == nil {
				t.Fatalf("invalid configuration accepted: %v", values)
			}
		})
	}
}
