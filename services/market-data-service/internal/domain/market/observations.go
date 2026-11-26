package market

import (
	"fmt"
	"math/big"
	"strings"
	"time"
)

// PermissionReference identifies reviewed rights, not a boolean assertion of ownership.
// AI use is separate from local retention and defaults to denied.
type LicensePolicy struct {
	Classification      string `json:"classification"`
	PermissionReference string `json:"permission_reference"`
	RetentionAllowed    bool   `json:"retention_allowed"`
	AIAllowed           bool   `json:"ai_allowed"`
}

// Decimal retains exact provider precision; no float64 rounding in ingestion.
type Decimal string

func ParseDecimal(raw string) (Decimal, error) {
	if raw == "" || len(raw) > 128 || strings.Trim(raw, "+-0123456789.eE") != "" {
		return "", fmt.Errorf("invalid finite decimal")
	}
	// Bound exponents before allocating a potentially enormous rational.
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		exponent := strings.TrimLeft(raw[index+1:], "+-0")
		if len(exponent) > 2 {
			return "", fmt.Errorf("decimal exponent is too large")
		}
	}
	if _, ok := new(big.Rat).SetString(raw); !ok {
		return "", fmt.Errorf("invalid finite decimal")
	}
	return Decimal(raw), nil
}

func (d Decimal) Nonnegative() bool {
	if _, err := ParseDecimal(string(d)); err != nil {
		return false
	}
	value, ok := new(big.Rat).SetString(string(d))
	return ok && value.Sign() >= 0
}

func (d Decimal) Compare(other Decimal) int {
	a, _ := new(big.Rat).SetString(string(d))
	b, _ := new(big.Rat).SetString(string(other))
	return a.Cmp(b) // Call only after validation.
}

type EconomicSeries struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Units              string     `json:"units"`
	Frequency          string     `json:"frequency"`
	SeasonalAdjustment string     `json:"seasonal_adjustment"`
	Notes              string     `json:"notes"`
	MetadataSource     Provenance `json:"metadata_source"`
}

type EconomicObservation struct {
	Date           string     `json:"date"`
	Value          *Decimal   `json:"value"`
	RealtimeStart  string     `json:"realtime_start"`
	RealtimeEnd    string     `json:"realtime_end"`
	MetadataSource Provenance `json:"metadata_source"`
}

type EconomicSnapshot struct {
	Series       EconomicSeries        `json:"series"`
	Observations []EconomicObservation `json:"observations"`
	VintageDate  string                `json:"vintage_date"`
}

type Instrument struct {
	ID             string     `json:"id"` // Share-class FIGI, never a ticker-only identity.
	CompanyID      string     `json:"company_id"`
	Ticker         string     `json:"ticker"`
	Exchange       string     `json:"exchange"` // ISO 10383 MIC.
	Currency       string     `json:"currency"` // ISO 4217.
	Name           string     `json:"name"`
	ListDate       string     `json:"list_date"`
	AsOfDate       string     `json:"as_of_date"`
	MetadataSource Provenance `json:"metadata_source"`
}

type Delay struct {
	Mode    string `json:"mode"` // end_of_day, delayed, realtime, unknown
	Seconds *int   `json:"seconds"`
}

func (d Delay) Validate() error {
	switch d.Mode {
	case "end_of_day", "unknown":
		if d.Seconds != nil {
			return fmt.Errorf("delay seconds must be unknown")
		}
	case "delayed":
		if d.Seconds == nil || *d.Seconds <= 0 {
			return fmt.Errorf("positive delay required")
		}
	case "realtime":
		if d.Seconds == nil || *d.Seconds != 0 {
			return fmt.Errorf("realtime delay must be zero")
		}
	default:
		return fmt.Errorf("unknown delay mode")
	}
	return nil
}

type PriceBar struct {
	Time           time.Time  `json:"time"`
	Open           Decimal    `json:"open"`
	High           Decimal    `json:"high"`
	Low            Decimal    `json:"low"`
	Close          Decimal    `json:"close"`
	Volume         Decimal    `json:"volume"`     // Provider volumes can be fractional.
	Adjustment     string     `json:"adjustment"` // unadjusted or split_adjusted; not total return.
	Delay          Delay      `json:"delay"`
	MetadataSource Provenance `json:"metadata_source"`
}

func (p PriceBar) Validate() error {
	for _, value := range []Decimal{p.Open, p.High, p.Low, p.Close, p.Volume} {
		if !value.Nonnegative() {
			return fmt.Errorf("prices and volume must be nonnegative finite decimals")
		}
	}
	if p.High.Compare(p.Low) < 0 || p.High.Compare(p.Open) < 0 || p.High.Compare(p.Close) < 0 ||
		p.Low.Compare(p.Open) > 0 || p.Low.Compare(p.Close) > 0 {
		return fmt.Errorf("invalid OHLC ordering")
	}
	if p.Adjustment != "split_adjusted" && p.Adjustment != "unadjusted" {
		return fmt.Errorf("unknown adjustment")
	}
	if p.Time.IsZero() || p.Time.After(p.MetadataSource.RetrievedAt) {
		return fmt.Errorf("invalid observation time")
	}
	return p.Delay.Validate()
}

type PriceSnapshot struct {
	Instrument Instrument `json:"instrument"`
	Bars       []PriceBar `json:"bars"`
}

// Corporate actions are not automatically applied to raw prices. Cash dividends
// and split ratios have distinct semantics; split-adjusted bars exclude dividends.
type CorporateAction struct {
	ID             string     `json:"id"`
	InstrumentID   string     `json:"instrument_id"`
	Kind           string     `json:"kind"`
	EffectiveDate  string     `json:"effective_date"`
	SplitFrom      *Decimal   `json:"split_from"`
	SplitTo        *Decimal   `json:"split_to"`
	CashAmount     *Decimal   `json:"cash_amount"`
	Currency       string     `json:"currency"`
	MetadataSource Provenance `json:"metadata_source"`
}
