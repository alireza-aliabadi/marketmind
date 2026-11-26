package massive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	app "marketmind/services/market-data-service/internal/application/market"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("../../../testdata/massive/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func adapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, err := New(Config{APIKey: "fixture-secret", Plan: "stocks-developer", FixtureEndpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func query() app.PriceQuery {
	return app.PriceQuery{Ticker: "test", StartDate: "2024-01-02", EndDate: "2024-01-03", Adjusted: true}
}

func TestPricePaginationCanonicalIdentityAndSemantics(t *testing.T) {
	var calls int
	a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-secret" || r.URL.Query().Get("apiKey") != "" {
			t.Error("wrong credential transport")
		}
		if strings.HasPrefix(r.URL.Path, "/v3/reference/") {
			_, _ = w.Write(fixture(t, "instrument"))
			return
		}
		var payload map[string]any
		_ = json.Unmarshal(fixture(t, "bars"), &payload)
		rows := payload["results"].([]any)
		payload["resultsCount"] = 1
		if r.URL.Query().Get("cursor") == "next" {
			payload["results"] = rows[1:]
		} else {
			payload["results"] = rows[:1]
			payload["next_url"] = baseURL + r.URL.Path + "?cursor=next&apiKey=untrusted-key"
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	result, err := a.FetchPrices(context.Background(), query())
	if err != nil || calls != 4 || len(result.Bars) != 2 {
		t.Fatal(result, calls, err)
	}
	if result.Instrument.ID != "figi:BBG000000001" || result.Instrument.CompanyID != "cik:0001045810" || result.Instrument.Currency != "USD" || result.Instrument.Exchange != "XNAS" {
		t.Fatal(result.Instrument)
	}
	bar := result.Bars[0]
	if bar.Time.Format(time.RFC3339) != "2024-01-02T05:00:00Z" || bar.Open != "100.12345678" || bar.Volume != "1000.25" || bar.Adjustment != "split_adjusted" || *bar.Delay.Seconds != 900 {
		t.Fatal("lost time, precision, delay or split semantics", bar)
	}
	for _, row := range result.Bars {
		if strings.Contains(row.MetadataSource.URL, "apiKey") || strings.Contains(row.MetadataSource.URL, "secret") {
			t.Fatal("secret in provenance")
		}
	}
}

func TestLiveMassiveRequiresPlanAndPermission(t *testing.T) {
	if _, err := New(Config{APIKey: "key", Plan: "stocks-developer"}); !errors.Is(err, app.ERRLicenseRestricted) {
		t.Fatal(err)
	}
	if _, err := New(Config{APIKey: "key", Plan: "imaginary", PermissionReference: "reviewed"}); err == nil {
		t.Fatal("unknown plan")
	}
	for _, plan := range []string{"stocks-basic", "stocks-starter", "stocks-developer", "stocks-advanced", "stocks-business"} {
		a, err := New(Config{APIKey: "key", Plan: plan, PermissionReference: "reviewed"})
		if err != nil || a.delay.Validate() != nil || a.policy.AIAllowed {
			t.Fatal(plan, err)
		}
	}
}

func TestPriceFailuresAndMaliciousPagination(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"wrong adjustment": func(v map[string]any) { v["adjusted"] = false },
		"missing price":    func(v map[string]any) { delete(v["results"].([]any)[0].(map[string]any), "o") },
		"bad OHLC":         func(v map[string]any) { v["results"].([]any)[0].(map[string]any)["h"] = 50 },
		"negative volume":  func(v map[string]any) { v["results"].([]any)[0].(map[string]any)["v"] = -1 },
		"wrong ticker":     func(v map[string]any) { v["ticker"] = "OTHER" },
		"foreign next URL": func(v map[string]any) { v["next_url"] = "https://attacker.example/steal" },
		"duplicate bars":   func(v map[string]any) { rows := v["results"].([]any); rows[1] = rows[0] },
		"future time":      func(v map[string]any) { v["results"].([]any)[0].(map[string]any)["t"] = 9999999999999 },
	} {
		t.Run(name, func(t *testing.T) {
			a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/v3/reference/") {
					_, _ = w.Write(fixture(t, "instrument"))
					return
				}
				var v map[string]any
				_ = json.Unmarshal(fixture(t, "bars"), &v)
				mutate(v)
				_ = json.NewEncoder(w).Encode(v)
			})
			if _, err := a.FetchPrices(context.Background(), query()); !errors.Is(err, app.ERRInvalidResponse) {
				t.Fatal(err)
			}
		})
	}
}

func TestTickerReuseIsNotOneInstrument(t *testing.T) {
	a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.Unmarshal(fixture(t, "instrument"), &v)
		if r.URL.Query().Get("date") == "2024-01-03" {
			v["results"].(map[string]any)["share_class_figi"] = "BBG000000002"
		}
		_ = json.NewEncoder(w).Encode(v)
	})
	if _, err := a.FetchPrices(context.Background(), query()); !errors.Is(err, app.ERRInvalidResponse) {
		t.Fatal(err)
	}
}
