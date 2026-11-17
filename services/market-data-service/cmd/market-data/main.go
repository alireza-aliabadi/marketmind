// market-data is a bounded local ingestion/operations CLI, not a public business API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ingestion "marketmind/services/market-data-service/internal/application/ingestion"
	market "marketmind/services/market-data-service/internal/application/market"
	settings "marketmind/services/market-data-service/internal/infra/config"
	"marketmind/services/market-data-service/internal/infra/events"
	"marketmind/services/market-data-service/internal/infra/fred"
	"marketmind/services/market-data-service/internal/infra/massive"
	"marketmind/services/market-data-service/internal/infra/postgres"
	"marketmind/services/market-data-service/internal/infra/sec"
	"marketmind/services/market-data-service/internal/infra/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		// Do not echo driver errors, credentials, provider payloads or source text.
		slog.Error("market-data command failed", "error_class", errorClass(err))
		os.Exit(1)
	}
}

func errorClass(err error) string {
	switch {
	case errors.Is(err, market.ERRLicenseRestricted):
		return "provider_permission_required"
	case errors.Is(err, market.ERRAccessDenied):
		return "provider_access_denied"
	case errors.Is(err, market.ERRRateLimited):
		return "provider_rate_limited"
	default:
		return "configuration_or_operation_failed"
	}
}

func run(parent context.Context, args []string, output, errorOutput io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("choose sec, fred, prices, migrate, topics, relay, consume, status, inspect or replay")
	}
	command := args[0]
	flags := flag.NewFlagSet("market-data "+command, flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	cik := flags.String("cik", "", "SEC company CIK")
	history := flags.Bool("history", false, "include bounded historical submissions files")
	series := flags.String("series", "", "reviewed FRED series ID")
	ticker := flags.String("ticker", "", "market ticker")
	start := flags.String("start", "", "inclusive date YYYY-MM-DD")
	end := flags.String("end", "", "inclusive date YYYY-MM-DD")
	vintage := flags.String("vintage", "", "FRED vintage date; default current UTC date")
	adjusted := flags.Bool("adjusted", false, "split-adjusted, not dividend-adjusted/total-return")
	enqueue := flags.Bool("enqueue", false, "atomically queue the provider snapshot and raw outbox")
	fixture := flags.String("fixture-endpoint", "", "loopback synthetic HTTP fixture endpoint only")
	correlation := flags.String("correlation-id", "ingestion:"+time.Now().UTC().Format("20060102T150405.000000000"), "trace correlation ID")
	limit := flags.Int("limit", 1, "bounded messages/rows; 1..10000")
	group := flags.String("group", "market-data-normalizer-v1", "Kafka consumer group")
	kind := flags.String("kind", "", "inspect company, filing, economic or prices")
	id := flags.String("id", "", "canonical record or failure ID")
	role := flags.String("role", "", "supervised worker role: relay or consumer")
	apply := flags.Bool("apply", false, "explicitly apply eligible completed-payload redaction; default dry run")
	retention := flags.Int("retention-days", 14, "completed payload retention, minimum 14 days")
	timeout := flags.Duration("timeout", 90*time.Second, "overall command deadline; at most 10m")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *timeout <= 0 || *timeout > 10*time.Minute || *limit < 1 || *limit > 10000 {
		return fmt.Errorf("invalid bounded command arguments")
	}
	mode := os.Getenv("MARKET_ENV")
	if mode != "" && mode != "local" && mode != "production" {
		return fmt.Errorf("invalid MARKET_ENV")
	}
	if mode == "production" && *fixture != "" {
		return fmt.Errorf("production forbids synthetic/fixture endpoints")
	}
	if command == "migrate" || command == "runtime-init" {
		dsn, err := settings.Secret("MARKET_MIGRATION_DATABASE_URL")
		if err != nil {
			return err
		}
		ctx, stop := context.WithTimeout(parent, *timeout)
		defer stop()
		connection := postgres.Config{URL: dsn, Production: mode == "production"}
		if command == "runtime-init" {
			return postgres.ProvisionRuntimeRole(ctx, connection)
		}
		return postgres.MigrateConfigured(ctx, connection)
	}
	cfg, err := settings.Load()
	if err != nil {
		return err
	}
	if command == "worker" {
		if cfg.Production && *role == "consumer" && len(cfg.Grants) == 0 {
			return market.ERRLicenseRestricted
		}
		startup, stop := context.WithTimeout(parent, 20*time.Second)
		repository, err := configuredRepository(startup, cfg)
		stop()
		if err != nil {
			return err
		}
		defer repository.Close()
		client, err := configuredClient(cfg)
		if err != nil {
			return err
		}
		defer client.Close()
		return worker.Run(parent, worker.Config{Role: *role, Group: *group, Address: cfg.OperationalAddress,
			OperationTimeout: min(*timeout, 30*time.Second), Policy: cfg}, repository, client)
	}
	ctx, cancel := context.WithTimeout(parent, *timeout)
	defer cancel()
	write := func(value any) error {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	if command == "sec" || command == "fred" || command == "prices" {
		if cfg.Production {
			return productionFetch(ctx, command, providerRequest{CIK: *cik, Series: *series, Ticker: *ticker, Start: *start, End: *end, Vintage: *vintage, Correlation: *correlation, History: *history, Adjusted: *adjusted, Enqueue: *enqueue}, cfg, output)
		}
		var record ingestion.Record
		var err error
		switch command {
		case "sec":
			userAgent := os.Getenv("SEC_EDGAR_USER_AGENT")
			if *fixture != "" && userAgent == "" {
				userAgent = "MarketMindTest researcher@example.com"
			}
			adapter, createErr := sec.New(userAgent)
			if createErr != nil {
				return createErr
			}
			if *fixture != "" {
				if err := adapter.WithFixtureEndpoint(*fixture); err != nil {
					return err
				}
			}
			snapshot, fetchErr := market.NewUsecase(adapter).Execute(ctx, *cik, *history)
			err, record = fetchErr, ingestion.Record{Kind: "filings", Filings: &snapshot}
		case "fred":
			key := os.Getenv("FRED_API_KEY")
			if *fixture != "" && key == "" {
				key = strings.Repeat("a", 32)
			}
			adapter, createErr := fred.New(fred.Config{APIKey: key, FixtureEndpoint: *fixture,
				PermissionReference: os.Getenv("FRED_PERMISSION_REFERENCE"), ReviewedSeries: strings.Split(os.Getenv("FRED_REVIEWED_SERIES"), ",")})
			if createErr != nil {
				return createErr
			}
			snapshot, fetchErr := adapter.FetchSeries(ctx, market.EconomicQuery{SeriesID: *series, StartDate: *start, EndDate: *end, VintageDate: *vintage})
			err, record = fetchErr, ingestion.Record{Kind: "economics", Economics: &snapshot}
		case "prices":
			key, plan := os.Getenv("MASSIVE_API_KEY"), os.Getenv("MASSIVE_STOCKS_PLAN")
			if *fixture != "" {
				if key == "" {
					key = "synthetic-fixture-key"
				}
				if plan == "" {
					plan = "stocks-developer"
				}
			}
			adapter, createErr := massive.New(massive.Config{APIKey: key, Plan: plan, FixtureEndpoint: *fixture, PermissionReference: os.Getenv("MASSIVE_PERMISSION_REFERENCE")})
			if createErr != nil {
				return createErr
			}
			snapshot, fetchErr := adapter.FetchPrices(ctx, market.PriceQuery{Ticker: *ticker, StartDate: *start, EndDate: *end, Adjusted: *adjusted})
			err, record = fetchErr, ingestion.Record{Kind: "prices", Prices: &snapshot}
		}
		if err != nil {
			return err
		}
		if !*enqueue {
			return write(record.Snapshot())
		}
		event, data, err := events.New(record, *correlation)
		if err != nil {
			return err
		}
		repository, err := configuredRepository(ctx, cfg)
		if err != nil {
			return err
		}
		defer repository.Close()
		queued, err := repository.Enqueue(ctx, event, data)
		if err != nil {
			return err
		}
		slog.Info("provider snapshot queued", "topic", event.Type, "event_id", event.ID,
			"idempotency_key", data.IdempotencyKey, "correlation_id", event.CorrelationID, "new_record", queued)
		return write(map[string]any{"queued": queued, "idempotency_key": data.IdempotencyKey, "event_id": event.ID})
	}
	if command == "topics" {
		client, err := configuredClient(cfg)
		if err != nil {
			return err
		}
		defer client.Close()
		return client.EnsureTopics(ctx)
	}
	if command != "relay" && command != "consume" && command != "status" && command != "inspect" && command != "replay" && command != "retention" && command != "check" {
		return fmt.Errorf("unknown command")
	}
	repository, err := configuredRepository(ctx, cfg)
	if err != nil {
		return err
	}
	defer repository.Close()
	switch command {
	case "status":
		value, err := repository.Status(ctx)
		if err != nil {
			return err
		}
		return write(value)
	case "inspect":
		value, err := repository.Inspect(ctx, *kind, *id)
		if err != nil {
			return err
		}
		return write(value)
	case "replay":
		return repository.Replay(ctx, *id)
	case "retention":
		count, err := repository.RedactCompletedPayloads(ctx, time.Now().UTC().Add(-time.Duration(*retention)*24*time.Hour), *apply)
		if err != nil {
			return err
		}
		return write(map[string]any{"eligible": count, "applied": *apply})
	}
	client, err := configuredClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if command == "check" {
		if err := client.Check(ctx); err != nil {
			return err
		}
		return repository.Ping(ctx)
	}
	if command == "relay" {
		count, err := (ingestion.Relay{Repository: repository, Publisher: client, Retry: ingestion.DefaultRetryPolicy()}).Drain(ctx, *limit)
		if err != nil {
			return err
		}
		return write(map[string]int{"delivered": count})
	}
	if cfg.Production && len(cfg.Grants) == 0 {
		return market.ERRLicenseRestricted
	}
	consumer, err := client.Consumer(*group, ingestion.Processor{Repository: repository, Decoder: events.Codec{}, Retry: ingestion.DefaultRetryPolicy(), Policy: cfg})
	if err != nil {
		return err
	}
	defer consumer.Close()
	for range *limit {
		if err := consumer.Next(ctx); err != nil {
			return err
		}
	}
	return write(map[string]int{"processed_or_parked": *limit})
}
