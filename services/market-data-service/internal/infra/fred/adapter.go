package fred

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	app "marketmind/services/market-data-service/internal/application/market"
	domain "marketmind/services/market-data-service/internal/domain/market"
	"marketmind/services/market-data-service/internal/infra/provider"
)

const baseURL = "https://api.stlouisfed.org/fred"

var requestGate = make(chan struct{}, 1)

type Config struct {
	APIKey              string
	PermissionReference string
	ReviewedSeries      []string
	FixtureEndpoint     string // Loopback only; never reads a live provider.
	Limiter             app.RequestLimiter
}

type Adapter struct {
	config   Config
	endpoint string
	client   *http.Client
	interval time.Duration
	policy   domain.LicensePolicy
}

var _ app.EconomicProvider = (*Adapter)(nil)

func New(config Config) (*Adapter, error) {
	if len(config.APIKey) != 32 || strings.Trim(config.APIKey, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
		return nil, app.ERRAccessDenied
	}
	endpoint, err := provider.Endpoint(config.FixtureEndpoint, baseURL)
	if err != nil {
		return nil, err
	}
	policy := domain.LicensePolicy{Classification: "fred-written-permission", PermissionReference: config.PermissionReference, RetentionAllowed: true}
	interval := 500 * time.Millisecond
	if config.FixtureEndpoint != "" {
		endpoint += "/fred"
		policy.Classification, policy.PermissionReference = "synthetic-test-only", "synthetic-fixture"
		interval = 0
	} else if config.PermissionReference == "" || len(config.ReviewedSeries) == 0 {
		// Current FRED terms prohibit retention and AI development without permission.
		return nil, app.ERRLicenseRestricted
	}
	return &Adapter{config: config, endpoint: endpoint, client: provider.Client(), interval: interval, policy: policy}, nil
}

func (a *Adapter) FetchSeries(ctx context.Context, query app.EconomicQuery) (domain.EconomicSnapshot, error) {
	if !app.ValidSymbol(query.SeriesID) || app.ValidateRange(query.StartDate, query.EndDate) != nil {
		return domain.EconomicSnapshot{}, app.ERRInvalidResponse
	}
	if a.config.FixtureEndpoint == "" {
		allowed := false
		for _, id := range a.config.ReviewedSeries {
			allowed = allowed || id == query.SeriesID
		}
		if !allowed {
			return domain.EconomicSnapshot{}, app.ERRLicenseRestricted
		}
	}
	if query.VintageDate == "" {
		query.VintageDate = time.Now().UTC().Format(time.DateOnly)
	}
	vintage, err := time.Parse(time.DateOnly, query.VintageDate)
	if err != nil || vintage.After(time.Now().UTC()) {
		return domain.EconomicSnapshot{}, app.ERRInvalidResponse
	}
	params := url.Values{"series_id": {query.SeriesID}, "file_type": {"json"},
		"realtime_start": {query.VintageDate}, "realtime_end": {query.VintageDate}}
	body, source, err := a.fetch(ctx, "series", params, query.SeriesID)
	if err != nil {
		return domain.EconomicSnapshot{}, err
	}
	var metadata struct {
		Series []struct {
			ID                 string `json:"id"`
			Title              string `json:"title"`
			Units              string `json:"units"`
			Frequency          string `json:"frequency"`
			SeasonalAdjustment string `json:"seasonal_adjustment"`
			Notes              string `json:"notes"`
		} `json:"seriess"`
	}
	if json.Unmarshal(body, &metadata) != nil || len(metadata.Series) != 1 ||
		metadata.Series[0].ID != query.SeriesID || metadata.Series[0].Title == "" || metadata.Series[0].Units == "" {
		return domain.EconomicSnapshot{}, app.ERRInvalidResponse
	}
	series := metadata.Series[0]
	result := domain.EconomicSnapshot{Series: domain.EconomicSeries{
		ID: "fred:" + query.SeriesID, Name: series.Title, Units: series.Units,
		Frequency: series.Frequency, SeasonalAdjustment: series.SeasonalAdjustment,
		Notes: series.Notes, MetadataSource: source,
	}, VintageDate: query.VintageDate, Observations: []domain.EconomicObservation{}}
	params.Set("observation_start", query.StartDate)
	params.Set("observation_end", query.EndDate)
	params.Set("units", "lin") // Retain native units; do not calculate growth in ingestion.
	params.Set("limit", "10000")
	params.Set("sort_order", "asc")
	count, offset := -1, 0
	seen := map[string]bool{}
	for page := 0; page < 64; page++ {
		params.Set("offset", strconv.Itoa(offset))
		body, source, err = a.fetch(ctx, "series/observations", params, query.SeriesID+":"+query.VintageDate)
		if err != nil {
			return domain.EconomicSnapshot{}, err
		}
		var response struct {
			Count        *int `json:"count"`
			Offset       *int `json:"offset"`
			Observations []struct {
				Date          string `json:"date"`
				Value         string `json:"value"`
				RealtimeStart string `json:"realtime_start"`
				RealtimeEnd   string `json:"realtime_end"`
			} `json:"observations"`
		}
		if json.Unmarshal(body, &response) != nil || response.Count == nil || response.Offset == nil ||
			*response.Offset != offset || *response.Count < 0 || *response.Count > 100000 ||
			(count != -1 && count != *response.Count) || offset+len(response.Observations) > *response.Count {
			return domain.EconomicSnapshot{}, app.ERRInvalidResponse
		}
		count = *response.Count
		for _, observation := range response.Observations {
			if _, err = time.Parse(time.DateOnly, observation.Date); err != nil ||
				observation.Date < query.StartDate || observation.Date > query.EndDate {
				return domain.EconomicSnapshot{}, app.ERRInvalidResponse
			}
			start, e1 := time.Parse(time.DateOnly, observation.RealtimeStart)
			end, e2 := time.Parse(time.DateOnly, observation.RealtimeEnd)
			key := observation.Date + ":" + observation.RealtimeStart + ":" + observation.RealtimeEnd
			if e1 != nil || e2 != nil || end.Before(start) || start.After(vintage) || end.Before(vintage) || seen[key] {
				return domain.EconomicSnapshot{}, app.ERRInvalidResponse
			}
			seen[key] = true
			var value *domain.Decimal
			if observation.Value != "." {
				decimal, err := domain.ParseDecimal(observation.Value)
				if err != nil {
					return domain.EconomicSnapshot{}, app.ERRInvalidResponse
				}
				value = &decimal
			}
			result.Observations = append(result.Observations, domain.EconomicObservation{
				Date: observation.Date, Value: value, RealtimeStart: observation.RealtimeStart,
				RealtimeEnd: observation.RealtimeEnd, MetadataSource: source,
			})
		}
		offset += len(response.Observations)
		if offset == count {
			sort.Slice(result.Observations, func(i, j int) bool { return result.Observations[i].Date < result.Observations[j].Date })
			return result, nil
		}
		if len(response.Observations) == 0 {
			return domain.EconomicSnapshot{}, app.ERRInvalidResponse
		}
	}
	return domain.EconomicSnapshot{}, app.ERRHistorylimit
}

func (a *Adapter) fetch(ctx context.Context, path string, params url.Values, externalID string) ([]byte, domain.Provenance, error) {
	canonical := baseURL + "/" + path + "?" + params.Encode()
	transportParams := url.Values{}
	for key, values := range params {
		transportParams[key] = append([]string(nil), values...)
	}
	transportParams.Set("api_key", a.config.APIKey)
	body, err := provider.GetLimited(ctx, a.client, a.endpoint+"/"+path+"?"+transportParams.Encode(),
		http.Header{"User-Agent": {"MarketMindResearch/0.1"}}, requestGate, a.interval, "fred", a.config.Limiter)
	if err != nil {
		return nil, domain.Provenance{}, err
	}
	source := provider.Source("fred", externalID, canonical, body)
	source.License = a.policy
	return body, source, nil
}
