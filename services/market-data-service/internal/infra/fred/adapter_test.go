package fred

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	app "marketmind/services/market-data-service/internal/application/market"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("../../../testdata/fred/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func adapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, err := New(Config{APIKey: strings.Repeat("a", 32), FixtureEndpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func query() app.EconomicQuery {
	return app.EconomicQuery{SeriesID: "TESTSERIES", StartDate: "2024-01-01", EndDate: "2024-03-01", VintageDate: "2024-03-01"}
}

func TestSeriesPaginationVintageAndExactMissingValues(t *testing.T) {
	var calls int
	a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("api_key") != strings.Repeat("a", 32) || r.URL.Query().Get("realtime_start") != "2024-03-01" {
			t.Error("missing key or vintage")
		}
		if r.URL.Path == "/fred/series" {
			_, _ = w.Write(fixture(t, "series"))
			return
		}
		var payload map[string]any
		_ = json.Unmarshal(fixture(t, "observations"), &payload)
		rows := payload["observations"].([]any)
		if r.URL.Query().Get("offset") == "0" {
			payload["observations"] = rows[:2]
		} else {
			payload["offset"], payload["observations"] = 2, rows[2:]
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	result, err := a.FetchSeries(context.Background(), query())
	if err != nil || calls != 3 || len(result.Observations) != 3 || result.Series.ID != "fred:TESTSERIES" {
		t.Fatal(result, calls, err)
	}
	if *result.Observations[0].Value != "100.123456789012345678" || result.Observations[1].Value != nil || *result.Observations[2].Value != "-1.25" {
		t.Fatal("precision or missing data corrupted", result)
	}
	if strings.Contains(result.Series.MetadataSource.URL, "api_key") || !strings.Contains(result.Series.Notes, "copyright") || result.Series.MetadataSource.License.AIAllowed {
		t.Fatal("secret/rights metadata not preserved")
	}
	if result.Observations[0].RealtimeStart != "2024-03-01" || result.VintageDate != "2024-03-01" {
		t.Fatal("lost vintage")
	}
}

func TestRestrictedLiveFREDAndReviewedSeries(t *testing.T) {
	if _, err := New(Config{APIKey: strings.Repeat("a", 32)}); !errors.Is(err, app.ERRLicenseRestricted) {
		t.Fatal(err)
	}
	if _, err := New(Config{APIKey: "bad"}); !errors.Is(err, app.ERRAccessDenied) {
		t.Fatal(err)
	}
	a, err := New(Config{APIKey: strings.Repeat("a", 32), PermissionReference: "reviewed-permission", ReviewedSeries: []string{"OTHER"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.FetchSeries(context.Background(), query()); !errors.Is(err, app.ERRLicenseRestricted) {
		t.Fatal("unreviewed series admitted", err)
	}
}

func TestMalformedOrIncompleteEconomicData(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"nonfinite value":   func(v map[string]any) { v["observations"].([]any)[0].(map[string]any)["value"] = "NaN" },
		"wrong offset":      func(v map[string]any) { v["offset"] = 1 },
		"invalid date":      func(v map[string]any) { v["observations"].([]any)[0].(map[string]any)["date"] = "invalid" },
		"invalid vintage":   func(v map[string]any) { v["observations"].([]any)[0].(map[string]any)["realtime_start"] = "2024-04-01" },
		"duplicate":         func(v map[string]any) { rows := v["observations"].([]any); rows[1] = rows[0] },
		"pagination stalls": func(v map[string]any) { v["observations"] = []any{} },
	} {
		t.Run(name, func(t *testing.T) {
			a := adapter(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/fred/series" {
					_, _ = w.Write(fixture(t, "series"))
					return
				}
				var v map[string]any
				_ = json.Unmarshal(fixture(t, "observations"), &v)
				mutate(v)
				_ = json.NewEncoder(w).Encode(v)
			})
			if _, err := a.FetchSeries(context.Background(), query()); !errors.Is(err, app.ERRInvalidResponse) {
				t.Fatal(err)
			}
		})
	}
	a := adapter(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403); _, _ = w.Write([]byte("SECRET")) })
	if _, err := a.FetchSeries(context.Background(), query()); !errors.Is(err, app.ERRAccessDenied) || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}
