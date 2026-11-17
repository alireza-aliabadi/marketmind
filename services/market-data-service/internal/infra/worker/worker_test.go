package worker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationalReadinessAndRedactedMetrics(t *testing.T) {
	state := &State{}
	handler := state.Handler()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatal("ready before dependency acceptance")
	}
	state.Ready.Store(true)
	state.Pending.Store(3)
	state.Failed.Store(2)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(response.Body.String(), "market_outbox_pending 3") || strings.Contains(response.Body.String(), "source_url") {
		t.Fatal(response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/health/ready", nil))
	if response.Code != 405 {
		t.Fatal("invalid method accepted")
	}
}
