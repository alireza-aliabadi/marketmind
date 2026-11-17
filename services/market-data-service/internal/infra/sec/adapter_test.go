package sec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	app "marketmind/services/market-data-service/internal/application/market"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/sec/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func adapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, err := New("MarketMindTest researcher@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.WithFixtureEndpoint(server.URL); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSubmissionsAndBoundedHistory(t *testing.T) {
	metadata, history := fixture(t, "submissions"), fixture(t, "history")
	var requests []string
	a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("User-Agent") != "MarketMindTest researcher@example.com" {
			t.Error("missing SEC contact")
		}
		if strings.Contains(r.URL.Path, "-submissions-") {
			_, _ = w.Write(history)
		} else {
			_, _ = w.Write(metadata)
		}
	})
	usecase := app.NewUsecase(a)
	result, err := usecase.Execute(context.Background(), "1045810", false)
	if err != nil || len(result.Filings) != 1 || result.HistoryComplete {
		t.Fatal(result, err)
	}
	result, err = usecase.Execute(context.Background(), "1045810", true)
	if err != nil || len(result.Filings) != 2 || !result.HistoryComplete {
		t.Fatal(result, err)
	}
	hash := sha256.Sum256(metadata)
	if result.Company.MetadataSource.PayloadSHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("incorrect payload hash")
	}
	filing := result.Filings[0]
	if filing.CIK != "0001045810" || filing.AccessionNumber != "0001045810-24-000001" ||
		filing.PublishedAt.Format(time.RFC3339) != "2024-02-21T21:06:00Z" ||
		filing.DocumentURL != "https://www.sec.gov/Archives/edgar/data/1045810/000104581024000001/synthetic-10k.htm" {
		t.Fatal("lost provider IDs, UTC time or canonical URL", filing)
	}
	if result.Filings[1].PublishedAt != nil || result.Filings[1].MetadataSource.URL != baseURL+"/CIK0001045810-submissions-001.json" {
		t.Fatal("unknown publication time or historical provenance lost")
	}
	if len(requests) != 3 {
		t.Fatal(requests)
	}
}

func TestMalformedSubmissionsAreRejected(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"wrong CIK":          func(v map[string]any) { v["cik"] = "1" },
		"missing name":       func(v map[string]any) { delete(v, "name") },
		"ticker mismatch":    func(v map[string]any) { v["exchanges"] = []string{} },
		"ragged arrays":      func(v map[string]any) { recent(v)["form"] = []string{} },
		"bad accession":      func(v map[string]any) { recent(v)["accessionNumber"] = []string{"../bad"} },
		"path traversal":     func(v map[string]any) { recent(v)["primaryDocument"] = []string{"../secret"} },
		"bad date":           func(v map[string]any) { recent(v)["filingDate"] = []string{"2024-13-01"} },
		"future publication": func(v map[string]any) { recent(v)["acceptanceDateTime"] = []string{"2999-01-01T00:00:00Z"} },
		"history traversal": func(v map[string]any) {
			v["filings"].(map[string]any)["files"] = []map[string]string{{"name": "../bad.json"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(fixture(t, "submissions"), &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			body, _ := json.Marshal(value)
			a := adapter(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
			if _, err := app.NewUsecase(a).Execute(context.Background(), "1045810", true); !errors.Is(err, app.ERRInvalidResponse) {
				t.Fatalf("expected invalid response, got %v", err)
			}
		})
	}
}

func recent(v map[string]any) map[string]any {
	return v["filings"].(map[string]any)["recent"].(map[string]any)
}

func TestHistoryFailureIsNotPartialSuccess(t *testing.T) {
	a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "-submissions-") {
			w.WriteHeader(404)
		} else {
			_, _ = w.Write(fixture(t, "submissions"))
		}
	})
	if _, err := app.NewUsecase(a).Execute(context.Background(), "1045810", true); !errors.Is(err, app.ERRNotFound) {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(fixture(t, "submissions"), &value)
	files := make([]map[string]string, maxHistoryFiles+1)
	value["filings"].(map[string]any)["files"] = files
	body, _ := json.Marshal(value)
	a = adapter(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	if _, err := app.NewUsecase(a).Execute(context.Background(), "1045810", true); !errors.Is(err, app.ERRHistorylimit) {
		t.Fatal(err)
	}
}

func TestProviderStatusAndBoundedRetries(t *testing.T) {
	for _, tc := range []struct {
		status, attempts int
		expected         error
	}{
		{404, 1, app.ERRNotFound}, {403, 1, app.ERRAccessDenied},
		{429, 3, app.ERRRateLimited}, {503, 3, app.ERRUnavailable}, {400, 1, app.ERRInvalidResponse},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var calls atomic.Int32
			a := adapter(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(tc.status) })
			_, err := app.NewUsecase(a).Execute(context.Background(), "1045810", false)
			if !errors.Is(err, tc.expected) || calls.Load() != int32(tc.attempts) {
				t.Fatal(err, calls.Load())
			}
		})
	}
}

func TestLongRetryAfterDoesNotRetryEarly(t *testing.T) {
	var calls atomic.Int32
	a := adapter(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	_, err := app.NewUsecase(a).Execute(context.Background(), "1045810", false)
	if !errors.Is(err, app.ERRRateLimited) || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestUserAgentEndpointAndCancellation(t *testing.T) {
	for _, ua := range []string{"", "bot", "bot no-email", "bot contact@example.com\r\nInjected: yes"} {
		if _, err := New(ua); err == nil {
			t.Fatalf("accepted %q", ua)
		}
	}
	a := adapter(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) })
	if err := a.WithFixtureEndpoint("https://attacker.example"); err == nil {
		t.Fatal("untrusted override accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.NewUsecase(a).Execute(ctx, "1045810", false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentRequestsRespectFairAccess(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	body := fixture(t, "submissions")
	a := adapter(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		_, _ = w.Write(body)
	})
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if _, err := app.NewUsecase(a).Execute(context.Background(), "1045810", false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 100*time.Millisecond {
			t.Fatal("SEC fair-access limit exceeded", starts)
		}
	}
}
