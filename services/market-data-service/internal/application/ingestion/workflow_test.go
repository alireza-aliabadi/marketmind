package ingestion_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	domain "marketmind/services/market-data-service/internal/domain/market"
	"marketmind/services/market-data-service/internal/infra/events"
)

type repository struct {
	applies           int
	failures          []app.Failure
	applyErr, parkErr error
}

func (*repository) Enqueue(context.Context, app.Envelope, app.RawData) (bool, error) {
	return true, nil
}
func (r *repository) Apply(context.Context, app.Envelope, app.RawData, app.Record) (bool, error) {
	r.applies++
	return true, r.applyErr
}
func (r *repository) Quarantine(_ context.Context, failure app.Failure) error {
	r.failures = append(r.failures, failure)
	return r.parkErr
}
func (*repository) DispatchNext(context.Context, func(app.Delivery) app.DispatchResult) (bool, error) {
	return false, nil
}
func (*repository) Replay(context.Context, string) error { return nil }

func delivery(t *testing.T) app.Delivery {
	t.Helper()
	source := domain.Provenance{Provider: "sec-edgar", ExternalID: "1", URL: "https://source.example/metadata.json",
		RetrievedAt: time.Now().UTC().Add(-time.Hour), PayloadSHA256: strings.Repeat("a", 64),
		License: domain.LicensePolicy{Classification: "synthetic-test-only", PermissionReference: "synthetic", RetentionAllowed: true}}
	record := app.Record{Kind: "filings", Filings: &domain.Snapshot{Company: domain.Company{CIK: "0000000001", Name: "SYNTHETIC", MetadataSource: source}, Filings: []domain.Filing{}}}
	event, data, err := events.New(record, "unit-test")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(event)
	return app.Delivery{Topic: event.Type, Key: data.IdempotencyKey, Value: body}
}

func TestProcessingRetriesAndDurableParking(t *testing.T) {
	store := &repository{applyErr: errors.New("temporary database outage")}
	processor := app.Processor{Repository: store, Decoder: events.Codec{}, Retry: app.RetryPolicy{Attempts: 3}}
	if err := processor.Handle(context.Background(), delivery(t)); err != nil {
		t.Fatal(err)
	}
	if store.applies != 3 || len(store.failures) != 1 || store.failures[0].Attempts != 3 {
		t.Fatal(store)
	}
	store.parkErr = errors.New("DLQ database unavailable")
	if err := processor.Handle(context.Background(), delivery(t)); !errors.Is(err, store.parkErr) {
		t.Fatal("failure must prevent offset acknowledgement", err)
	}
}

func TestPoisonKeyAndCancellationDoNotApply(t *testing.T) {
	store := &repository{}
	processor := app.Processor{Repository: store, Decoder: events.Codec{}, Retry: app.RetryPolicy{Attempts: 3}}
	bad := delivery(t)
	bad.Key = "wrong-key"
	if err := processor.Handle(context.Background(), bad); err != nil || store.applies != 0 || len(store.failures) != 1 {
		t.Fatal(err, store)
	}
	bad.Value = []byte("invalid JSON")
	if err := processor.Handle(context.Background(), bad); err != nil || store.applies != 0 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := processor.Handle(ctx, delivery(t)); !errors.Is(err, context.Canceled) || store.applies != 0 || len(store.failures) != 2 {
		t.Fatal(err, store)
	}
}

func TestBoundedRetryPolicy(t *testing.T) {
	for _, policy := range []app.RetryPolicy{{}, {Attempts: 11}, {Attempts: 2, BaseDelay: time.Second, MaxDelay: time.Millisecond}} {
		if err := policy.Run(context.Background(), func() error { t.Fatal("invalid policy ran"); return nil }); err == nil {
			t.Fatal("invalid retry accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	err := (app.RetryPolicy{Attempts: 3, BaseDelay: time.Second, MaxDelay: time.Second}).Run(ctx, func() error { count++; cancel(); return errors.New("temporary") })
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatal(err, count)
	}
}
