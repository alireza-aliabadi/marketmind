package market

import (
	"context"
	"errors"
	"testing"

	domain "marketmind/services/market-data-service/internal/domain/market"
)

type stubProvider struct {
	query  Query
	called bool
}

func (p *stubProvider) FetchCompanyFilings(_ context.Context, q Query) (domain.Snapshot, error) {
	p.query, p.called = q, true
	return domain.Snapshot{Company: domain.Company{CIK: q.CIK}}, nil
}

func TestUsecaseValidatesAndDelegates(t *testing.T) {
	p := &stubProvider{}
	usecase := NewUsecase(p)
	if _, err := usecase.Execute(context.Background(), "bad", false); err == nil || p.called {
		t.Fatal("invalid CIK reached provider")
	}
	result, err := usecase.Execute(context.Background(), "1045810", true)
	if err != nil || result.Company.CIK != "0001045810" || !p.query.IncludeHistory {
		t.Fatal(result, err)
	}
	if _, err := NewUsecase(nil).Execute(context.Background(), "1045810", false); !errors.Is(err, ERRUnavailable) {
		t.Fatal("nil provider did not fail safely", err)
	}
}
