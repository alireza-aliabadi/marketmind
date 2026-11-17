// Package massive implements Polygon's successor REST API, without vendor types inward.
package massive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	app "marketmind/services/market-data-service/internal/application/market"
	domain "marketmind/services/market-data-service/internal/domain/market"
	"marketmind/services/market-data-service/internal/infra/provider"
)

const baseURL = "https://api.massive.com"

var requestGate = make(chan struct{}, 1)
var figiPattern = regexp.MustCompile(`^BBG[A-Z0-9]{9}$`)
var micPattern = regexp.MustCompile(`^[A-Z0-9]{4}$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type Config struct {
	APIKey, Plan, PermissionReference, FixtureEndpoint string
	Limiter                                            app.RequestLimiter
}
type Adapter struct {
	config   Config
	endpoint string
	client   *http.Client
	interval time.Duration
	delay    domain.Delay
	policy   domain.LicensePolicy
}

var _ app.PriceProvider = (*Adapter)(nil)

func New(config Config) (*Adapter, error) {
	if config.APIKey == "" || strings.ContainsAny(config.APIKey, "\r\n ") {
		return nil, app.ERRAccessDenied
	}
	endpoint, err := provider.Endpoint(config.FixtureEndpoint, baseURL)
	if err != nil {
		return nil, err
	}
	delay := domain.Delay{}
	switch config.Plan {
	case "stocks-basic":
		delay.Mode = "end_of_day"
	case "stocks-starter", "stocks-developer":
		seconds := 900
		delay.Mode, delay.Seconds = "delayed", &seconds
	case "stocks-advanced", "stocks-business":
		seconds := 0
		delay.Mode, delay.Seconds = "realtime", &seconds
	default:
		return nil, fmt.Errorf("a documented Massive Stocks plan is required")
	}
	policy := domain.LicensePolicy{Classification: "massive-contract-restricted", PermissionReference: config.PermissionReference, RetentionAllowed: true}
	interval := 12 * time.Second // Conservative five requests/minute; shared across instances.
	if config.FixtureEndpoint != "" {
		policy.Classification, policy.PermissionReference = "synthetic-test-only", "synthetic-fixture"
		interval = 0
	} else if config.PermissionReference == "" {
		return nil, app.ERRLicenseRestricted
	}
	return &Adapter{config: config, endpoint: endpoint, client: provider.Client(), interval: interval, delay: delay, policy: policy}, nil
}

func (a *Adapter) FetchPrices(ctx context.Context, query app.PriceQuery) (domain.PriceSnapshot, error) {
	query.Ticker = strings.ToUpper(strings.TrimSpace(query.Ticker))
	if !app.ValidSymbol(query.Ticker) || app.ValidateRange(query.StartDate, query.EndDate) != nil {
		return domain.PriceSnapshot{}, app.ERRInvalidResponse
	}
	instrument, err := a.instrument(ctx, query.Ticker, query.StartDate)
	if err != nil {
		return domain.PriceSnapshot{}, err
	}
	if query.StartDate != query.EndDate {
		endInstrument, err := a.instrument(ctx, query.Ticker, query.EndDate)
		if err != nil {
			return domain.PriceSnapshot{}, err
		}
		if endInstrument.ID != instrument.ID || endInstrument.Exchange != instrument.Exchange || endInstrument.Currency != instrument.Currency {
			return domain.PriceSnapshot{}, fmt.Errorf("%w: ticker range changes instrument/listing", app.ERRInvalidResponse)
		}
	}
	adjustment := "unadjusted"
	if query.Adjusted {
		adjustment = "split_adjusted"
	}
	path := "/v2/aggs/ticker/" + url.PathEscape(query.Ticker) + "/range/1/day/" + query.StartDate + "/" + query.EndDate
	params := url.Values{"adjusted": {fmt.Sprint(query.Adjusted)}, "sort": {"asc"}, "limit": {"50000"}}
	canonical := baseURL + path + "?" + params.Encode()
	result := domain.PriceSnapshot{Instrument: instrument, Bars: []domain.PriceBar{}}
	seenPages, seenBars := map[string]bool{}, map[int64]bool{}
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return domain.PriceSnapshot{}, err
	}
	for page := 0; page < 32; page++ {
		if seenPages[canonical] {
			return domain.PriceSnapshot{}, app.ERRInvalidResponse
		}
		seenPages[canonical] = true
		body, source, err := a.fetch(ctx, canonical, query.Ticker+":"+query.StartDate+":"+query.EndDate+":"+adjustment)
		if err != nil {
			return domain.PriceSnapshot{}, err
		}
		var response struct {
			Status   string `json:"status"`
			Ticker   string `json:"ticker"`
			Adjusted *bool  `json:"adjusted"`
			Count    *int   `json:"resultsCount"`
			NextURL  string `json:"next_url"`
			Results  []struct {
				Time   int64       `json:"t"`
				Open   json.Number `json:"o"`
				High   json.Number `json:"h"`
				Low    json.Number `json:"l"`
				Close  json.Number `json:"c"`
				Volume json.Number `json:"v"`
			} `json:"results"`
		}
		if json.Unmarshal(body, &response) != nil || response.Status != "OK" || response.Ticker != query.Ticker ||
			response.Adjusted == nil || *response.Adjusted != query.Adjusted || response.Count == nil || *response.Count != len(response.Results) {
			return domain.PriceSnapshot{}, app.ERRInvalidResponse
		}
		for _, raw := range response.Results {
			instant := time.UnixMilli(raw.Time).UTC()
			date := instant.In(location).Format(time.DateOnly)
			if date < query.StartDate || date > query.EndDate || seenBars[raw.Time] {
				return domain.PriceSnapshot{}, app.ERRInvalidResponse
			}
			seenBars[raw.Time] = true
			bar := domain.PriceBar{Time: instant, Open: domain.Decimal(raw.Open), High: domain.Decimal(raw.High),
				Low: domain.Decimal(raw.Low), Close: domain.Decimal(raw.Close), Volume: domain.Decimal(raw.Volume),
				Adjustment: adjustment, Delay: a.delay, MetadataSource: source}
			if err := bar.Validate(); err != nil {
				return domain.PriceSnapshot{}, fmt.Errorf("%w: %v", app.ERRInvalidResponse, err)
			}
			result.Bars = append(result.Bars, bar)
		}
		if response.NextURL == "" {
			sort.Slice(result.Bars, func(i, j int) bool { return result.Bars[i].Time.Before(result.Bars[j].Time) })
			return result, nil
		}
		next, err := url.Parse(response.NextURL)
		if err != nil || next.Scheme != "https" || next.Host != "api.massive.com" || next.User != nil || next.Fragment != "" || next.Path != path {
			return domain.PriceSnapshot{}, app.ERRInvalidResponse
		}
		query := next.Query()
		query.Del("apiKey") // Use the configured header; never trust/persist URL credentials.
		next.RawQuery = query.Encode()
		canonical = next.String()
	}
	return domain.PriceSnapshot{}, app.ERRHistorylimit
}

func (a *Adapter) instrument(ctx context.Context, ticker, date string) (domain.Instrument, error) {
	canonical := baseURL + "/v3/reference/tickers/" + url.PathEscape(ticker) + "?date=" + date
	body, source, err := a.fetch(ctx, canonical, ticker+":"+date)
	if err != nil {
		return domain.Instrument{}, err
	}
	var response struct {
		Status  string `json:"status"`
		Results struct {
			Ticker   string `json:"ticker"`
			Name     string `json:"name"`
			Currency string `json:"currency_name"`
			Exchange string `json:"primary_exchange"`
			FIGI     string `json:"share_class_figi"`
			CIK      string `json:"cik"`
			ListDate string `json:"list_date"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &response) != nil || response.Status != "OK" {
		return domain.Instrument{}, app.ERRInvalidResponse
	}
	metadata := response.Results
	currency := strings.ToUpper(metadata.Currency)
	if metadata.Ticker != ticker || metadata.Name == "" || !figiPattern.MatchString(metadata.FIGI) ||
		!micPattern.MatchString(metadata.Exchange) || !currencyPattern.MatchString(currency) {
		return domain.Instrument{}, app.ERRInvalidResponse
	}
	companyID := ""
	if metadata.CIK != "" {
		cik, err := domain.ParseCIK(metadata.CIK)
		if err != nil {
			return domain.Instrument{}, app.ERRInvalidResponse
		}
		companyID = "cik:" + string(cik)
	}
	if metadata.ListDate != "" {
		if _, err := time.Parse(time.DateOnly, metadata.ListDate); err != nil || metadata.ListDate > date {
			return domain.Instrument{}, app.ERRInvalidResponse
		}
	}
	return domain.Instrument{ID: "figi:" + metadata.FIGI, CompanyID: companyID, Ticker: ticker,
		Exchange: metadata.Exchange, Currency: currency, Name: metadata.Name, ListDate: metadata.ListDate,
		AsOfDate: date, MetadataSource: source}, nil
}

func (a *Adapter) fetch(ctx context.Context, canonical, externalID string) ([]byte, domain.Provenance, error) {
	transport := strings.Replace(canonical, baseURL, a.endpoint, 1)
	body, err := provider.GetLimited(ctx, a.client, transport,
		http.Header{"Authorization": {"Bearer " + a.config.APIKey}, "User-Agent": {"MarketMindResearch/0.1"}}, requestGate, a.interval, "massive", a.config.Limiter)
	if err != nil {
		return nil, domain.Provenance{}, err
	}
	source := provider.Source("massive", externalID, canonical, body)
	source.License = a.policy
	return body, source, nil
}
