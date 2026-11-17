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
