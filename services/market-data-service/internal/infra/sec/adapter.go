package sec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	app "marketmind/services/market-data-service/internal/application/market"
	domain "marketmind/services/market-data-service/internal/domain/market"
	"marketmind/services/market-data-service/internal/infra/provider"
)

const (
	maxHistoryFiles = 64
	baseURL         = "https://data.sec.gov/submissions"
)

var (
	accessionPattern = regexp.MustCompile(
		`^[0-9]{10}-[0-9]{2}-[0-9]{6}$`,
	)

	historyPattern = regexp.MustCompile(
		`^CIK[0-9]{10}-submissions-[0-9]+\.json$`,
	)
	// All SEC adapter instances in this process share the same fair-access gate.
	requestGate = make(chan struct{}, 1)
)

type Adapter struct {
	client              *http.Client
	userAgent           string
	submissionsUrl      string
	interval            time.Duration
	gate                chan struct{}
	fixture             bool
	limiter             app.RequestLimiter
	permissionReference string
}

var _ app.MarketDataPort = (*Adapter)(nil)

func New(userAgent string) (*Adapter, error) {
	if strings.ContainsAny(userAgent, "\r\n") {
		return nil, fmt.Errorf(
			"!!!SEC user agent must not contain line breaks!!!",
		)
	}

	userAgent = strings.TrimSpace(userAgent)
	parts := strings.Fields(userAgent)
	if len(parts) < 2 {
		return nil, fmt.Errorf("SEC User-Agent requires an application name and contact email")
	}
	contact := strings.Trim(parts[len(parts)-1], "<>")
	address, err := mail.ParseAddress(contact)
	if err != nil || address.Address != contact || !strings.Contains(contact, "@") {
		return nil, fmt.Errorf("SEC User-Agent requires a valid contact email")
	}
	return &Adapter{
		client: provider.Client(), userAgent: userAgent, submissionsUrl: baseURL,
		interval: 125 * time.Millisecond, gate: requestGate,
	}, nil
}

// WithFixtureEndpoint changes transport only; production provenance remains canonical.
func (a *Adapter) WithFixtureEndpoint(raw string) error {
	endpoint, err := provider.Endpoint(raw, "https://data.sec.gov")
	if err == nil {
		a.submissionsUrl = endpoint + "/submissions"
		a.fixture = raw != ""
	}
	return err
}

type filingColumns struct {
	AccessionNumber       []string `json:"accessionNumber"`
	Form                  []string `json:"form"`
	FilingDate            []string `json:"filingDate"`
	ReportDate            []string `json:"reportDate"`
	AcceptanceDateTime    []string `json:"acceptanceDateTime"`
	PrimaryDocument       []string `json:"primaryDocument"`
	PrimaryDocDescription []string `json:"primaryDocDescription"`
}

type submissions struct {
	CIK       json.RawMessage `json:"cik"`
	Name      string          `json:"name"`
	Tickers   []string        `json:"tickers"`
	Exchanges []string        `json:"exchanges"`
	EIN       string          `json:"ein"`
	SIC       string          `json:"sic"`
	Filings   struct {
		Recent filingColumns `json:"recent"`
		Files  []struct {
			Name string `json:"name"`
		} `json:"files"`
	} `json:"filings"`
}

func (a *Adapter) FetchCompanyFilings(ctx context.Context, query app.Query) (domain.Snapshot, error) {
	cik, err := domain.ParseCIK(string(query.CIK))
	if err != nil {
		return domain.Snapshot{}, err
	}
	file := "CIK" + string(cik) + ".json"
	body, err := a.fetch(ctx, file)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var response submissions
	if json.Unmarshal(body, &response) != nil || strings.TrimSpace(response.Name) == "" {
		return domain.Snapshot{}, app.ERRInvalidResponse
	}
	responseCIK, err := domain.ParseCIK(strings.Trim(string(response.CIK), `"`))
	if err != nil || responseCIK != cik || len(response.Tickers) != len(response.Exchanges) {
		return domain.Snapshot{}, app.ERRInvalidResponse
	}
	source := a.source(string(cik), file, body)
	filings, err := normalize(cik, response.Filings.Recent, source)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if query.IncludeHistory {
		if len(response.Filings.Files) > maxHistoryFiles {
			return domain.Snapshot{}, app.ERRHistorylimit
		}
		seenFiles := map[string]bool{}
		for _, history := range response.Filings.Files {
			if !historyPattern.MatchString(history.Name) ||
				!strings.HasPrefix(history.Name, "CIK"+string(cik)+"-") || seenFiles[history.Name] {
				return domain.Snapshot{}, app.ERRInvalidResponse
			}
			seenFiles[history.Name] = true
			body, err = a.fetch(ctx, history.Name)
			if err != nil {
				return domain.Snapshot{}, err // Never return an apparently complete partial history.
			}
			var columns filingColumns
			if json.Unmarshal(body, &columns) != nil {
				return domain.Snapshot{}, app.ERRInvalidResponse
			}
			extra, err := normalize(cik, columns, a.source(string(cik), history.Name, body))
			if err != nil {
				return domain.Snapshot{}, err
			}
			filings = append(filings, extra...)
		}
	}
	unique := make([]domain.Filing, 0, len(filings))
	seen := map[string]bool{}
	for _, filing := range filings {
		if !seen[filing.AccessionNumber] {
			seen[filing.AccessionNumber] = true
			unique = append(unique, filing)
		}
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].FilingDate != unique[j].FilingDate {
			return unique[i].FilingDate > unique[j].FilingDate
		}
		return unique[i].AccessionNumber > unique[j].AccessionNumber
	})
	return domain.Snapshot{
		Company: domain.Company{CIK: cik, Name: response.Name, Tickers: response.Tickers,
			Exchanges: response.Exchanges, EIN: response.EIN, SIC: response.SIC, MetadataSource: source},
		Filings: unique, HistoryComplete: query.IncludeHistory || len(response.Filings.Files) == 0,
	}, nil
}

func (a *Adapter) fetch(ctx context.Context, file string) ([]byte, error) {
	return provider.GetLimited(ctx, a.client, a.submissionsUrl+"/"+file,
		http.Header{"User-Agent": {a.userAgent}}, a.gate, a.interval, "sec-edgar", a.limiter)
}

func (a *Adapter) ConfigureProduction(limiter app.RequestLimiter, permissionReference string) error {
	if a.fixture || limiter == nil || strings.TrimSpace(permissionReference) == "" {
		return fmt.Errorf("production SEC requires shared pacing and reviewed metadata permission")
	}
	parts := strings.Fields(a.userAgent)
	contact := strings.Trim(parts[len(parts)-1], "<>")
	domain := strings.ToLower(strings.SplitN(contact, "@", 2)[1])
	if strings.HasSuffix(domain, ".local") || strings.HasSuffix(domain, ".example") || domain == "example.com" || domain == "example.org" || domain == "example.net" || domain == "localhost" {
		return fmt.Errorf("production SEC contact must be a real operational address, not a placeholder")
	}
	a.limiter, a.permissionReference = limiter, permissionReference
	return nil
}

func (a *Adapter) source(cik, file string, body []byte) domain.Provenance {
	source := provider.Source("sec-edgar", cik, baseURL+"/"+file, body)
	if a.permissionReference != "" {
		source.License.PermissionReference = a.permissionReference
	}
	if a.fixture {
		source.License.Classification, source.License.PermissionReference = "synthetic-test-only", "synthetic-fixture"
	}
	return source
}

func normalize(cik domain.CIK, columns filingColumns, source domain.Provenance) ([]domain.Filing, error) {
	count := len(columns.AccessionNumber)
	if columns.AccessionNumber == nil || len(columns.Form) != count || len(columns.FilingDate) != count ||
		len(columns.PrimaryDocument) != count {
		return nil, app.ERRInvalidResponse
	}
	for _, optional := range [][]string{columns.ReportDate, columns.AcceptanceDateTime, columns.PrimaryDocDescription} {
		if len(optional) != 0 && len(optional) != count {
			return nil, app.ERRInvalidResponse
		}
	}
	result := make([]domain.Filing, 0, count)
	for i, accession := range columns.AccessionNumber {
		if !accessionPattern.MatchString(accession) || strings.TrimSpace(columns.Form[i]) == "" {
			return nil, app.ERRInvalidResponse
		}
		if _, err := time.Parse(time.DateOnly, columns.FilingDate[i]); err != nil {
			return nil, app.ERRInvalidResponse
		}
		base := "https://www.sec.gov/Archives/edgar/data/" + strings.TrimLeft(string(cik), "0") +
			"/" + strings.ReplaceAll(accession, "-", "") + "/"
		document := columns.PrimaryDocument[i]
		if strings.ContainsAny(document, `/\\?#`) || document == "." || document == ".." {
			return nil, app.ERRInvalidResponse
		}
		documentURL := ""
		if document != "" {
			documentURL = base + url.PathEscape(document)
		}
		acceptance := valueAt(columns.AcceptanceDateTime, i)
		var published *time.Time
		if acceptance != "" {
			instant, err := time.Parse(time.RFC3339Nano, acceptance)
			if err == nil {
				instant = instant.UTC()
				if instant.After(source.RetrievedAt) {
					return nil, app.ERRInvalidResponse
				}
				published = &instant
			} // Zone-less legacy acceptance strings are retained, never invented as UTC.
		}
		result = append(result, domain.Filing{
			CIK: cik, AccessionNumber: accession, Form: columns.Form[i], FilingDate: columns.FilingDate[i],
			ReportDate: valueAt(columns.ReportDate, i), AcceptanceDateTime: acceptance,
			PrimaryDoc: document, PrimaryDocDescription: valueAt(columns.PrimaryDocDescription, i),
			DocumentURL: documentURL, IndexURL: base + accession + "-index.html", PublishedAt: published,
			MetadataSource: source,
		})
	}
	return result, nil
}

func valueAt(values []string, index int) string {
	if len(values) == 0 {
		return ""
	}
	return values[index]
}
