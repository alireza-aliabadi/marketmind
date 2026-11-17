package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "marketmind/services/market-data-service/internal/domain/market"
)

const (
	FilingsTopic    = "market.filings.raw.v1"
	EconomicsTopic  = "market.economics.raw.v1"
	PricesTopic     = "market.prices.raw.v1"
	DLQTopic        = "market.ingestion.dlq.v1"
	MaxMessageBytes = 2 * 1024 * 1024
)

var ErrInvalidData = errors.New("invalid ingestion data")

// Record is internal native data, not a Kafka/DB/framework object.
type Record struct {
	Kind      string
	Filings   *domain.Snapshot
	Economics *domain.EconomicSnapshot
	Prices    *domain.PriceSnapshot
}

type Envelope struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	DataSchema      string          `json:"dataschema"`
	CorrelationID   string          `json:"correlationid"`
	Data            json.RawMessage `json:"data"`
}

type RawData struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Kind           string          `json:"kind"`
	Snapshot       json.RawMessage `json:"snapshot"`
}

type Delivery struct {
	Topic, Key string
	Value      []byte
}
type DispatchResult struct {
	Attempts int
	Err      error
}
type Failure struct {
	ID                                string
	Topic, MessageKey, EventKey, Code string
	Value                             []byte
	Attempts                          int
	FailedAt                          time.Time
}

type Decoder interface {
	Decode(topic string, body []byte) (Envelope, RawData, Record, error)
}
type SourcePolicy interface{ Check(Record) error }
type Publisher interface {
	Publish(context.Context, Delivery) error
}
type Repository interface {
	Enqueue(context.Context, Envelope, RawData) (bool, error)
	Apply(context.Context, Envelope, RawData, Record) (bool, error)
	Quarantine(context.Context, Failure) error
	// Implementations hold a row lock while the handler publishes; successful
	// publication and delivery state are not claimed to be an atomic Kafka commit.
	DispatchNext(context.Context, func(Delivery) DispatchResult) (bool, error)
	Replay(context.Context, string) error
}

func (r Record) Topic() string {
	switch r.Kind {
	case "filings":
		return FilingsTopic
	case "economics":
		return EconomicsTopic
	case "prices":
		return PricesTopic
	default:
		return ""
	}
}

func Schema(topic string) string {
	return "urn:marketmind:events:" + strings.TrimSuffix(topic, ".v1") + ":1.0.0"
}

func (r Record) Snapshot() any {
	switch r.Kind {
	case "filings":
		return r.Filings
	case "economics":
		return r.Economics
	case "prices":
		return r.Prices
	default:
		return nil
	}
}

func (r Record) Sources() []domain.Provenance {
	var sources []domain.Provenance
	if r.Filings != nil {
		sources = append(sources, r.Filings.Company.MetadataSource)
		for _, filing := range r.Filings.Filings {
			sources = append(sources, filing.MetadataSource)
		}
	}
	if r.Economics != nil {
		sources = append(sources, r.Economics.Series.MetadataSource)
		for _, observation := range r.Economics.Observations {
			sources = append(sources, observation.MetadataSource)
		}
	}
	if r.Prices != nil {
		sources = append(sources, r.Prices.Instrument.MetadataSource)
		for _, bar := range r.Prices.Bars {
			sources = append(sources, bar.MetadataSource)
		}
	}
	return sources
}

func ValidHash(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
func ValidToken(value string) bool {
	return value != "" && len(value) <= 128 && strings.Trim(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._:-") == ""
}

func (r Record) Validate() error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidData, reason) }
	count := 0
	if r.Filings != nil {
		count++
	}
	if r.Economics != nil {
		count++
	}
	if r.Prices != nil {
		count++
	}
	if count != 1 || r.Topic() == "" ||
		(r.Kind == "filings" && r.Filings == nil) || (r.Kind == "economics" && r.Economics == nil) || (r.Kind == "prices" && r.Prices == nil) {
		return invalid("exactly one matching snapshot required")
	}
	provider := map[string]string{"filings": "sec-edgar", "economics": "fred", "prices": "massive"}[r.Kind]
	for _, source := range r.Sources() {
		if source.Provider != provider || source.ExternalID == "" || len(source.ExternalID) > 1024 ||
			!strings.HasPrefix(source.URL, "https://") || len(source.URL) > 2048 ||
			strings.Contains(strings.ToLower(source.URL), "api_key") || strings.Contains(strings.ToLower(source.URL), "apikey") ||
			!ValidHash(source.PayloadSHA256) || source.RetrievedAt.IsZero() ||
			!source.License.RetentionAllowed || source.License.PermissionReference == "" || source.License.Classification == "" {
			return invalid("invalid or unlicensed source provenance")
		}
	}
	if r.Filings != nil {
		company := r.Filings.Company
		cik, err := domain.ParseCIK(string(company.CIK))
		if err != nil || cik != company.CIK || company.Name == "" || len(company.Tickers) != len(company.Exchanges) {
			return invalid("invalid company")
		}
		for _, filing := range r.Filings.Filings {
			accession := filing.AccessionNumber
			if filing.CIK != cik || len(accession) != 20 || accession[10] != '-' || accession[13] != '-' ||
				strings.Trim(strings.ReplaceAll(accession, "-", ""), "0123456789") != "" || filing.Form == "" {
				return invalid("invalid filing identity")
			}
			if _, err := time.Parse(time.DateOnly, filing.FilingDate); err != nil {
				return invalid("invalid filing date")
			}
			if filing.ReportDate != "" {
				if _, err := time.Parse(time.DateOnly, filing.ReportDate); err != nil {
					return invalid("invalid report date")
				}
			}
			if !strings.HasPrefix(filing.IndexURL, "https://") ||
				(filing.DocumentURL != "" && !strings.HasPrefix(filing.DocumentURL, "https://")) {
				return invalid("invalid filing source URL")
			}
			if filing.PublishedAt != nil && filing.PublishedAt.After(filing.MetadataSource.RetrievedAt) {
				return invalid("future publication")
			}
		}
	}
	if r.Economics != nil {
		series := r.Economics.Series
		if !strings.HasPrefix(series.ID, "fred:") || series.Name == "" || series.Units == "" {
			return invalid("invalid series")
		}
		vintage, err := time.Parse(time.DateOnly, r.Economics.VintageDate)
		if err != nil {
			return invalid("invalid vintage")
		}
		for _, observation := range r.Economics.Observations {
			_, dateErr := time.Parse(time.DateOnly, observation.Date)
			start, e1 := time.Parse(time.DateOnly, observation.RealtimeStart)
			end, e2 := time.Parse(time.DateOnly, observation.RealtimeEnd)
			if dateErr != nil || e1 != nil || e2 != nil || start.After(vintage) || end.Before(vintage) {
				return invalid("invalid observation/vintage")
			}
			if observation.Value != nil {
				if _, err := domain.ParseDecimal(string(*observation.Value)); err != nil {
					return invalid("invalid economic decimal")
				}
			}
		}
	}
	if r.Prices != nil {
		instrument := r.Prices.Instrument
		if !strings.HasPrefix(instrument.ID, "figi:BBG") || len(instrument.ID) != 17 || instrument.Ticker == "" ||
			len(instrument.Exchange) != 4 || len(instrument.Currency) != 3 || strings.Trim(instrument.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return invalid("invalid canonical instrument/currency/exchange")
		}
		if _, err := time.Parse(time.DateOnly, instrument.AsOfDate); err != nil {
			return invalid("invalid instrument date")
		}
		for _, bar := range r.Prices.Bars {
			if err := bar.Validate(); err != nil {
				return invalid(err.Error())
			}
		}
	}
	return nil
}
