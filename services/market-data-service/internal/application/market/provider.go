package market

import (
	"context"
	"errors"
	"time"

	domain "marketmind/services/market-data-service/internal/domain/market"
)

// Errors definition
var (
	ERRNotFound = errors.New(
		"!!!Company not found!!!",
	)
	ERRAccessDenied = errors.New(
		"!!!Provider access denied!!!",
	)
	ERRRateLimited = errors.New(
		"!!!Provider rate limit!!!",
	)
	ERRUnavailable = errors.New(
		"!!!Provider unavailable!!!",
	)
	ERRInvalidResponse = errors.New(
		"!!!Invalid provider response!!!",
	)
	ERRHistorylimit = errors.New(
		"!!!history file request rate limit!!!",
	)
	ERRLicenseRestricted = errors.New("provider permission is required for this use")
)

type Query struct {
	CIK            domain.CIK
	IncludeHistory bool
}

type MarketDataPort interface {
	FetchCompanyFilings(context.Context, Query) (domain.Snapshot, error)
}

// RequestLimiter coordinates provider access across worker processes. Transport
// stays in adapters; implementations release their lease even after cancellation.
type RequestLimiter interface {
	Acquire(context.Context, string, time.Duration) (func() error, error)
}

type Usecase struct {
	provider MarketDataPort
}

func NewUsecase(provider MarketDataPort) *Usecase {
	return &Usecase{
		provider: provider,
	}
}

func (u *Usecase) Execute(ctx context.Context, rawCIK string, includeHistory bool) (domain.Snapshot, error) {
	cik, err := domain.ParseCIK(rawCIK)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if u.provider == nil {
		return domain.Snapshot{}, ERRUnavailable
	}

	return u.provider.FetchCompanyFilings(ctx, Query{
		CIK:            cik,
		IncludeHistory: includeHistory,
	})
}
