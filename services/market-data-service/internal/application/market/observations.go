package market

import (
	"context"
	"fmt"
	"strings"
	"time"

	domain "marketmind/services/market-data-service/internal/domain/market"
)

type EconomicQuery struct{ SeriesID, StartDate, EndDate, VintageDate string }
type EconomicProvider interface {
	FetchSeries(context.Context, EconomicQuery) (domain.EconomicSnapshot, error)
}

type PriceQuery struct {
	Ticker, StartDate, EndDate string
	Adjusted                   bool
}
type PriceProvider interface {
	FetchPrices(context.Context, PriceQuery) (domain.PriceSnapshot, error)
}

func ValidateRange(start, end string) error {
	from, err := time.Parse(time.DateOnly, start)
	if err != nil {
		return fmt.Errorf("invalid start date")
	}
	to, err := time.Parse(time.DateOnly, end)
	if err != nil || to.Before(from) || to.Sub(from) > 3660*24*time.Hour {
		return fmt.Errorf("invalid or unbounded date range")
	}
	return nil
}

func ValidSymbol(raw string) bool {
	return raw != "" && len(raw) <= 32 && strings.Trim(raw, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-") == ""
}
