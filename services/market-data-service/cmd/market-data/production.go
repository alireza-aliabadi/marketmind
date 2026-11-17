package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	_ "time/tzdata" // Minimal non-root image still preserves provider ET boundaries.

	app "marketmind/services/market-data-service/internal/application/ingestion"
	market "marketmind/services/market-data-service/internal/application/market"
	settings "marketmind/services/market-data-service/internal/infra/config"
	"marketmind/services/market-data-service/internal/infra/events"
	"marketmind/services/market-data-service/internal/infra/fred"
	"marketmind/services/market-data-service/internal/infra/kafka"
	"marketmind/services/market-data-service/internal/infra/massive"
	"marketmind/services/market-data-service/internal/infra/postgres"
	"marketmind/services/market-data-service/internal/infra/sec"
)

func configuredClient(c settings.Config) (*kafka.Client, error) {
	return kafka.NewConfigured(kafka.Config{Brokers: c.Brokers, Production: c.Production,
		TLS:      c.Production || c.KafkaTLS.CA != "" || c.KafkaTLS.Certificate != "" || c.KafkaMechanism != "",
		TLSFiles: c.KafkaTLS, Mechanism: c.KafkaMechanism, Username: c.KafkaUsername, Password: c.KafkaPassword})
}
func configuredRepository(ctx context.Context, c settings.Config) (*postgres.Repository, error) {
	return postgres.NewConfigured(ctx, postgres.Config{URL: c.DatabaseURL, Production: c.Production, Policy: c})
}

type providerRequest struct {
	CIK, Series, Ticker, Start, End, Vintage, Correlation string
	History, Adjusted, Enqueue                            bool
}

func productionFetch(ctx context.Context, command string, request providerRequest, c settings.Config, out io.Writer) (err error) {
	name := map[string]string{"sec": "sec-edgar", "fred": "fred", "prices": "massive"}[command]
	grant, err := c.RequireProvider(name)
	if err != nil {
		return err
	}
	repository, err := configuredRepository(ctx, c)
	if err != nil {
		return err
	}
	defer repository.Close()
	runID, err := repository.BeginRun(ctx, name, request.Correlation)
	if err != nil {
		return err
	}
	eventKey := ""
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		class := ""
		if err != nil {
			class = errorClass(err)
		}
		if finishErr := repository.FinishRun(cleanup, runID, eventKey, class); err == nil && finishErr != nil {
			err = fmt.Errorf("provider run journal unavailable")
		}
	}()
	var record app.Record
	switch command {
	case "sec":
		adapter, createErr := sec.New(c.SECUserAgent)
		if createErr != nil {
			return createErr
		}
		if err := adapter.ConfigureProduction(repository, grant.Reference); err != nil {
			return err
		}
		snapshot, fetchErr := market.NewUsecase(adapter).Execute(ctx, request.CIK, request.History)
		if fetchErr != nil {
			return fetchErr
		}
		record = app.Record{Kind: "filings", Filings: &snapshot}
	case "fred":
		adapter, createErr := fred.New(fred.Config{APIKey: c.FREDKey, PermissionReference: grant.Reference, ReviewedSeries: grant.Series, Limiter: repository})
		if createErr != nil {
			return createErr
		}
		snapshot, fetchErr := adapter.FetchSeries(ctx, market.EconomicQuery{SeriesID: request.Series, StartDate: request.Start, EndDate: request.End, VintageDate: request.Vintage})
		if fetchErr != nil {
			return fetchErr
		}
		record = app.Record{Kind: "economics", Economics: &snapshot}
	case "prices":
		adapter, createErr := massive.New(massive.Config{APIKey: c.MassiveKey, Plan: c.MassivePlan, PermissionReference: grant.Reference, Limiter: repository})
		if createErr != nil {
			return createErr
		}
		snapshot, fetchErr := adapter.FetchPrices(ctx, market.PriceQuery{Ticker: request.Ticker, StartDate: request.Start, EndDate: request.End, Adjusted: request.Adjusted})
		if fetchErr != nil {
			return fetchErr
		}
		record = app.Record{Kind: "prices", Prices: &snapshot}
	default:
		return fmt.Errorf("unknown provider operation")
	}
	if err := c.Check(record); err != nil {
		return err
	}
	if !request.Enqueue {
		return json.NewEncoder(out).Encode(record.Snapshot())
	}
	event, data, err := events.New(record, request.Correlation)
	if err != nil {
		return err
	}
	queued, err := repository.Enqueue(ctx, event, data)
	if err != nil {
		return err
	}
	eventKey = data.IdempotencyKey
	return json.NewEncoder(out).Encode(map[string]any{"run_id": runID, "queued": queued, "idempotency_key": eventKey, "event_id": event.ID})
}
