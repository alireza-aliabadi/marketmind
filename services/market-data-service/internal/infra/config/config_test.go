package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func productionEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"MARKET_PROVIDER_GRANTS_FILE", "MARKET_DATABASE_URL_FILE", "KAFKA_SASL_PASSWORD_FILE", "FRED_API_KEY", "FRED_API_KEY_FILE", "MASSIVE_API_KEY", "MASSIVE_API_KEY_FILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("MARKET_ENV", "production")
	t.Setenv("MARKET_DATABASE_URL", "postgresql://ingestor:private@db.example:5432/marketmind?sslmode=verify-full")
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "broker.example:9093")
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-512")
	t.Setenv("KAFKA_SASL_USERNAME", "ingestor")
	t.Setenv("KAFKA_SASL_PASSWORD", "private-not-logged")
}
func TestProductionSettingsFailClosedAndSourcesDisabled(t *testing.T) {
	productionEnvironment(t)
	c, err := Load()
	if err != nil || !c.Production {
		t.Fatal(err)
	}
	if _, err := c.RequireProvider("fred"); err == nil {
		t.Fatal("live source enabled without a grant")
	}
	for _, mode := range []string{"disable", "prefer", "require", "verify-ca", ""} {
		t.Setenv("MARKET_DATABASE_URL", "postgresql://ingestor:private@db.example/db?sslmode="+mode)
		if _, err := Load(); err == nil {
			t.Fatal("accepted insecure DB mode", mode)
		}
	}
	t.Setenv("MARKET_DATABASE_URL", "postgresql://ingestor:private@db.example/db?sslmode=verify-full")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("unauthenticated production broker accepted")
	}
}
func TestSecretFilesAndNoSecretErrors(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "secret")
	if err := os.WriteFile(path, []byte("secret-data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYNTHETIC_SECRET", "")
	t.Setenv("SYNTHETIC_SECRET_FILE", path)
	value, err := Secret("SYNTHETIC_SECRET")
	if err != nil || value != "secret-data" {
		t.Fatal(err)
	}
	t.Setenv("SYNTHETIC_SECRET", "secret-data")
	_, err = Secret("SYNTHETIC_SECRET")
	if err == nil || strings.Contains(err.Error(), "secret-data") {
		t.Fatal("ambiguous secret config or leaked value", err)
	}
	t.Setenv("SYNTHETIC_SECRET", "")
	_ = os.WriteFile(path, []byte(strings.Repeat("x", 16385)), 0600)
	if _, err := Secret("SYNTHETIC_SECRET"); err == nil {
		t.Fatal("oversized secret accepted")
	}
}
func TestGrantExpiryAndRetention(t *testing.T) {
	productionEnvironment(t)
	grant := Grant{Reference: "operator-reviewed-grant", ExpiresAt: time.Now().UTC().Add(time.Hour), ReviewedOn: time.Now().UTC().Format(time.DateOnly), RetentionDays: 14, PrivateIngestion: true, Series: []string{"TEST"}}
	file := filepath.Join(t.TempDir(), "grants.json")
	body, _ := json.Marshal(GrantFile{Version: 1, Providers: map[string]Grant{"fred": grant}})
	_ = os.WriteFile(file, body, 0600)
	t.Setenv("MARKET_PROVIDER_GRANTS_FILE", file)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.RequireProvider("fred"); err != nil {
		t.Fatal(err)
	}
	grant.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	if validateGrant("fred", grant) == nil {
		t.Fatal("expired grant accepted")
	}
	grant.ExpiresAt = time.Now().UTC().Add(time.Hour)
	grant.RetentionDays = 7
	if validateGrant("fred", grant) == nil {
		t.Fatal("grant shorter than default DLQ retention accepted")
	}
}
