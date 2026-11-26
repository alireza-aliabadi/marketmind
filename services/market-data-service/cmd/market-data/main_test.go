package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCLIFixtureAndConfigurationFailure(t *testing.T) {
	body, err := os.ReadFile("../../testdata/sec/submissions.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	t.Setenv("SEC_EDGAR_USER_AGENT", "")
	var output bytes.Buffer
	if err := run(context.Background(), []string{"sec", "--cik", "1045810", "--fixture-endpoint", server.URL}, &output, &output); err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if json.Unmarshal(output.Bytes(), &snapshot) != nil || !strings.Contains(output.String(), "SYNTHETIC") {
		t.Fatal(output.String())
	}
	for _, args := range [][]string{{}, {"unknown"}, {"consume", "--limit", "0"}, {"sec", "--timeout", "11m"}, {"sec", "--cik", "bad"}} {
		if err := run(context.Background(), args, &output, &output); err == nil {
			t.Fatal("invalid CLI accepted", args)
		}
	}
}

func TestCLISetupRequiresExplicitOperatorCredentials(t *testing.T) {
	t.Setenv("MARKET_ENV", "local")
	t.Setenv("MARKET_MIGRATION_DATABASE_URL", "")
	t.Setenv("MARKET_MIGRATION_DATABASE_URL_FILE", "")
	t.Setenv("MARKET_DATABASE_URL", "postgresql://runtime:synthetic-sensitive-password@localhost/marketmind")
	var output bytes.Buffer
	if err := run(context.Background(), []string{"setup"}, &output, &output); err == nil {
		t.Fatal("setup used runtime credentials or an administrator fallback")
	}
	if strings.Contains(output.String(), "synthetic-sensitive-password") {
		t.Fatal("setup leaked credentials")
	}
	if err := run(context.Background(), []string{"setup", "--help"}, &output, &output); err != nil {
		t.Fatal("setup help unexpectedly required a database", err)
	}
	t.Setenv("MARKET_ENV", "production")
	t.Setenv("MARKET_MIGRATION_DATABASE_URL", "postgresql://operator:synthetic@localhost/marketmind?sslmode=disable")
	if err := run(context.Background(), []string{"setup"}, &output, &output); err == nil {
		t.Fatal("setup allowed a production TLS downgrade")
	}
}
