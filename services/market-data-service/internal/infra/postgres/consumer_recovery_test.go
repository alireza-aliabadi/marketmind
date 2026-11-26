package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	"marketmind/services/market-data-service/internal/infra/events"
)

type unavailableStore struct {
	*Repository
	unavailable bool
	seen        []string
}

func (s *unavailableStore) Apply(ctx context.Context, event app.Envelope, data app.RawData, record app.Record) (bool, error) {
	s.seen = append(s.seen, event.ID)
	if s.unavailable {
		return false, errors.New("synthetic database outage")
	}
	return s.Repository.Apply(ctx, event, data, record)
}
func (s *unavailableStore) Quarantine(ctx context.Context, failure app.Failure) error {
	if s.unavailable {
		return errors.New("synthetic parking outage")
	}
	return s.Repository.Quarantine(ctx, failure)
}

func TestConsumerRetryDoesNotSkipFailedMessage(t *testing.T) {
	ctx, repository, client, _, _ := stack(t)
	record := filings(t, ctx, fixtureServer(t), false)
	first, data := queue(t, ctx, repository, record)
	second, _, err := events.New(record, "second-event")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []app.Envelope{first, second} {
		body, _ := json.Marshal(event)
		if err := client.Publish(ctx, app.Delivery{Topic: event.Type, Key: data.IdempotencyKey, Value: body}); err != nil {
			t.Fatal(err)
		}
	}
	store := &unavailableStore{Repository: repository, unavailable: true}
	worker, err := client.Consumer("failed-message-recovery", app.Processor{Repository: store, Decoder: events.Codec{}, Retry: app.DefaultRetryPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if err := worker.Next(ctx); err == nil {
		t.Fatal("outage must prevent acknowledgement")
	}
	store.unavailable = false
	if err := worker.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.seen[len(store.seen)-1]; got != first.ID {
		t.Fatalf("consumer skipped unacknowledged message: retried %s, expected first %s", got, first.ID)
	}
	if err := worker.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.seen[len(store.seen)-1]; got != second.ID {
		t.Fatalf("second message not processed after first: %s", got)
	}
	t.Log("failed message retained through database/parking outage; no later offset skipped it")
}
