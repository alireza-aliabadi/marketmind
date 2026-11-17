package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"
	app "marketmind/services/market-data-service/internal/application/ingestion"
	market "marketmind/services/market-data-service/internal/application/market"
	"marketmind/services/market-data-service/internal/infra/events"
	"marketmind/services/market-data-service/internal/infra/fred"
	"marketmind/services/market-data-service/internal/infra/kafka"
	"marketmind/services/market-data-service/internal/infra/massive"
	"marketmind/services/market-data-service/internal/infra/sec"
)

func stack(t *testing.T) (context.Context, *Repository, *kafka.Client, string, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("RUN_MARKET_RUNTIME_TESTS") != "1" {
		t.Skip("make market-data-test uses isolated Testcontainers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	adminDSN := os.Getenv("MARKET_TEST_ADMIN_DSN")
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if err := Migrate(ctx, adminDSN); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, adminDSN); err != nil {
		t.Fatal("migration replay is not idempotent", err)
	}
	_, err = admin.Exec(ctx, `CREATE ROLE market_test LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE
        NOREPLICATION NOBYPASSRLS PASSWORD 'synthetic-test-only-password' IN ROLE mm_market_data`)
	if err != nil {
		t.Fatal(err)
	}
	dsn := strings.Replace(adminDSN, "postgresql://postgres:", "postgresql://market_test:", 1)
	if repository, err := New(ctx, adminDSN); err == nil {
		repository.Close()
		t.Fatal("administrator accepted as runtime")
	}
	repository, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repository.Close)
	client, err := kafka.NewSingleBrokerFixture(os.Getenv("MARKET_TEST_KAFKA"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.EnsureTopics(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, repository, client, dsn, admin
}

func fixtureServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := ""
		switch {
		case strings.Contains(r.URL.Path, "-submissions-"):
			file = "sec/history.json"
		case strings.HasPrefix(r.URL.Path, "/submissions/"):
			file = "sec/submissions.json"
		case r.URL.Path == "/fred/series":
			file = "fred/series.json"
		case r.URL.Path == "/fred/series/observations":
			file = "fred/observations.json"
		case strings.HasPrefix(r.URL.Path, "/v3/reference/"):
			file = "massive/instrument.json"
		case strings.HasPrefix(r.URL.Path, "/v2/aggs/"):
			file = "massive/bars.json"
		default:
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile("../../../testdata/" + file)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		if file == "massive/bars.json" {
			var value map[string]any
			_ = json.Unmarshal(body, &value)
			value["adjusted"] = r.URL.Query().Get("adjusted") == "true"
			_ = json.NewEncoder(w).Encode(value)
		} else {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func filings(t *testing.T, ctx context.Context, endpoint string, history bool) app.Record {
	t.Helper()
	adapter, err := sec.New("MarketMindTest researcher@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.WithFixtureEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	snapshot, err := market.NewUsecase(adapter).Execute(ctx, "1045810", history)
	if err != nil {
		t.Fatal(err)
	}
	return app.Record{Kind: "filings", Filings: &snapshot}
}

func queue(t *testing.T, ctx context.Context, repository *Repository, record app.Record) (app.Envelope, app.RawData) {
	t.Helper()
	event, data, err := events.New(record, "runtime-milestone")
	if err != nil {
		t.Fatal(err)
	}
	if inserted, err := repository.Enqueue(ctx, event, data); err != nil || !inserted {
		t.Fatal("queue failed", inserted, err)
	}
	writeEvidence(t, event.Type+"."+data.IdempotencyKey[:8], event)
	return event, data
}

func writeEvidence(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("MARKET_TEST_EVIDENCE_DIR"), name+".event.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}

func scalar(t *testing.T, ctx context.Context, repository *Repository, query string, args ...any) int {
	t.Helper()
	var count int
	if err := repository.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func consumer(t *testing.T, client *kafka.Client, repository *Repository, group string) *kafka.Consumer {
	t.Helper()
	result, err := client.Consumer(group, app.Processor{Repository: repository, Decoder: events.Codec{}, Retry: app.DefaultRetryPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Close() })
	return result
}

type tracedRepository struct {
	*Repository
	t *testing.T
}

func (r tracedRepository) Apply(ctx context.Context, event app.Envelope, data app.RawData, record app.Record) (bool, error) {
	r.t.Log("consumer applying", event.Type)
	result, err := r.Repository.Apply(ctx, event, data, record)
	r.t.Log("consumer apply returned", err)
	return result, err
}
func (r tracedRepository) Quarantine(ctx context.Context, failure app.Failure) error {
	r.t.Log("consumer parking", failure.Code)
	err := r.Repository.Quarantine(ctx, failure)
	r.t.Log("consumer parking returned", err)
	return err
}

func TestKafkaDeliveryMilestone(t *testing.T) {
	ctx, repository, client, _, _ := stack(t)
	endpoint := fixtureServer(t)
	record := filings(t, ctx, endpoint, false)
	event, data := queue(t, ctx, repository, record)
	refetched := filings(t, ctx, endpoint, false)
	duplicate, duplicateData, err := events.New(refetched, "duplicate-refetch")
	if err != nil {
		t.Fatal(err)
	}
	if duplicateData.IdempotencyKey != data.IdempotencyKey {
		t.Fatal("refetch identity changed")
	}
	if inserted, err := repository.Enqueue(ctx, duplicate, duplicateData); err != nil || inserted {
		t.Fatal("duplicate created outbox", inserted, err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.outbox") != 1 {
		t.Fatal("duplicate outbox")
	}

	// Real connection failure exhausts the bounded relay policy and parks original
	// bytes. Recovery is explicit replay, not mistaking the pending DLQ for raw data.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddress := listener.Addr().String()
	_ = listener.Close()
	broken, _ := kafka.NewSingleBrokerFixture(closedAddress)
	defer broken.Close()
	if _, err := (app.Relay{Repository: repository, Publisher: broken, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 10); err == nil {
		t.Fatal("unreachable broker accepted publication")
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.outbox WHERE idempotency_key=$1 AND failed_at IS NOT NULL", data.IdempotencyKey) != 1 {
		t.Fatal("transport failure was not parked")
	}
	var transportFailureID string
	if err := repository.pool.QueryRow(ctx, "SELECT failure_id FROM market_data.ingestion_errors WHERE code='delivery_exhausted' AND attempts=3").Scan(&transportFailureID); err != nil {
		t.Fatal("bounded original-byte failure missing", err)
	}
	if count, err := (app.Relay{Repository: repository, Publisher: client, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 10); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	transportPartition := (&kafkago.Hash{}).Balance(kafkago.Message{Key: []byte(transportFailureID)}, 0, 1, 2)
	transportDLQ, err := client.ReadPartition(ctx, app.DLQTopic, transportPartition, transportFailureID)
	if err != nil {
		t.Fatal(err)
	}
	var transportEnvelope app.Envelope
	if err := json.Unmarshal(transportDLQ.Value, &transportEnvelope); err != nil {
		t.Fatal(err)
	}
	writeEvidence(t, app.DLQTopic+".transport", transportEnvelope)
	if err := repository.Replay(ctx, transportFailureID); err != nil {
		t.Fatal(err)
	}
	if count, err := (app.Relay{Repository: repository, Publisher: client, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 10); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	probeContext, stopProbe := context.WithTimeout(ctx, 15*time.Second)
	partitionProbe := (&kafkago.Hash{}).Balance(kafkago.Message{Key: []byte(data.IdempotencyKey)}, 0, 1, 2)
	probe, err := client.ReadPartition(probeContext, app.FilingsTopic, partitionProbe, data.IdempotencyKey)
	stopProbe()
	if err != nil || probe.Key != data.IdempotencyKey {
		t.Fatal("raw publication probe failed", err)
	}
	t.Log("raw Kafka message received on expected partition", partitionProbe)

	// Persistent DB failure injects three application attempts, each rolled back.
	_, err = repository.pool.Exec(ctx, `CREATE FUNCTION market_data.m23_failure() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'synthetic persistence outage'; END $$;
        CREATE TRIGGER m23_failure BEFORE INSERT ON market_data.filings FOR EACH ROW EXECUTE FUNCTION market_data.m23_failure()`)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := client.Consumer("m23-acceptance", app.Processor{Repository: tracedRepository{repository, t}, Decoder: events.Codec{}, Retry: app.DefaultRetryPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	consumeContext, stopConsume := context.WithTimeout(ctx, 20*time.Second)
	defer stopConsume()
	if err := worker.Next(consumeContext); err != nil {
		t.Fatal(err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.companies") != 0 {
		t.Fatal("partial writes escaped transaction")
	}
	var failureID string
	var attempts int
	if err := repository.pool.QueryRow(ctx, `SELECT failure_id,attempts FROM market_data.ingestion_errors WHERE code='processing_failed'`).Scan(&failureID, &attempts); err != nil || attempts != 3 {
		t.Fatal(attempts, err)
	}
	if count, err := (app.Relay{Repository: repository, Publisher: client, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 10); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	partition := (&kafkago.Hash{}).Balance(kafkago.Message{Key: []byte(failureID)}, 0, 1, 2)
	deadLetter, err := client.ReadPartition(ctx, app.DLQTopic, partition, failureID)
	if err != nil {
		t.Fatal(err)
	}
	var dlq app.Envelope
	if err := json.Unmarshal(deadLetter.Value, &dlq); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Original string `json:"original_base64"`
		Attempts int    `json:"attempts"`
	}
	_ = json.Unmarshal(dlq.Data, &payload)
	original, err := base64.StdEncoding.DecodeString(payload.Original)
	var originalEnvelope app.Envelope
	_ = json.Unmarshal(original, &originalEnvelope)
	if err != nil || originalEnvelope.ID != event.ID || payload.Attempts != 3 {
		t.Fatal("DLQ lost original record", err)
	}
	writeEvidence(t, app.DLQTopic, dlq)

	if _, err := repository.pool.Exec(ctx, `DROP TRIGGER m23_failure ON market_data.filings; DROP FUNCTION market_data.m23_failure()`); err != nil {
		t.Fatal(err)
	}
	if err := repository.Replay(ctx, failureID); err != nil {
		t.Fatal(err)
	}
	if count, err := (app.Relay{Repository: repository, Publisher: client, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 10); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := worker.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.filings") != 1 {
		t.Fatal("replay did not persist filing")
	}
	if scalar(t, ctx, repository, "SELECT replay_count FROM market_data.ingestion_errors WHERE failure_id=$1", failureID) != 1 {
		t.Fatal("replay not audited")
	}
	body, _ := json.Marshal(event)
	if err := client.Publish(ctx, app.Delivery{Topic: event.Type, Key: data.IdempotencyKey, Value: body}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.filings") != 1 {
		t.Fatal("duplicate Kafka replay was not idempotent")
	}
	if err := repository.Quarantine(ctx, app.Failure{Topic: app.FilingsTopic, MessageKey: "invalid", Value: []byte("broken"), Code: "invalid_event", Attempts: 1, FailedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	invalidID := events.FailureID(app.Failure{Topic: app.FilingsTopic, MessageKey: "invalid", Value: []byte("broken"), Code: "invalid_event"})
	if err := repository.Replay(ctx, invalidID); err == nil {
		t.Fatal("invalid bytes replayed")
	}
	t.Log("M2.3: real Kafka raw/DLQ, bounded outage, transactional retries, original-byte replay, audit and duplicate idempotency passed")
}

func TestPersistenceMilestone(t *testing.T) {
	ctx, repository, client, dsn, admin := stack(t)
	endpoint := fixtureServer(t)
	record := filings(t, ctx, endpoint, true)
	// Deliberately fail outbox insertion: the associated source snapshot must roll back.
	_, err := repository.pool.Exec(ctx, `CREATE FUNCTION market_data.m24_failure() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'synthetic outbox failure'; END $$;
        CREATE TRIGGER m24_failure BEFORE INSERT ON market_data.outbox FOR EACH ROW EXECUTE FUNCTION market_data.m24_failure()`)
	if err != nil {
		t.Fatal(err)
	}
	event, data, err := events.New(record, "outbox-atomicity")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Enqueue(ctx, event, data); err == nil {
		t.Fatal("injected outbox failure ignored")
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.ingestion_events") != 0 {
		t.Fatal("snapshot committed without outbox")
	}
	if _, err := repository.pool.Exec(ctx, `DROP TRIGGER m24_failure ON market_data.outbox; DROP FUNCTION market_data.m24_failure()`); err != nil {
		t.Fatal(err)
	}

	// Exercise the actual compiled CLI against the fixture provider and runtime DB.
	command := exec.CommandContext(ctx, os.Getenv("MARKET_TEST_BINARY"), "sec", "--cik", "1045810", "--history", "--fixture-endpoint", endpoint, "--enqueue")
	command.Env = append(os.Environ(), "MARKET_DATABASE_URL="+dsn, "SEC_EDGAR_USER_AGENT=MarketMindTest researcher@example.com")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatal("compiled CLI ingestion failed", err, string(output))
	}
	var actualEnvelope app.Envelope
	var actualBytes []byte
	if err := repository.pool.QueryRow(ctx, "SELECT envelope FROM market_data.outbox WHERE idempotency_key=$1", data.IdempotencyKey).Scan(&actualBytes); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(actualBytes, &actualEnvelope); err != nil {
		t.Fatal(err)
	}
	writeEvidence(t, app.FilingsTopic, actualEnvelope)
	// FRED and both price adjustments run through the real provider adapters.
	fredAdapter, err := fred.New(fred.Config{APIKey: strings.Repeat("a", 32), FixtureEndpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	economic, err := fredAdapter.FetchSeries(ctx, market.EconomicQuery{SeriesID: "TESTSERIES", StartDate: "2024-01-01", EndDate: "2024-03-01", VintageDate: "2024-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	queue(t, ctx, repository, app.Record{Kind: "economics", Economics: &economic})
	pricesAdapter, err := massive.New(massive.Config{APIKey: "synthetic", Plan: "stocks-developer", FixtureEndpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	for _, adjusted := range []bool{true, false} {
		prices, err := pricesAdapter.FetchPrices(ctx, market.PriceQuery{Ticker: "TEST", StartDate: "2024-01-02", EndDate: "2024-01-03", Adjusted: adjusted})
		if err != nil {
			t.Fatal(err)
		}
		queue(t, ctx, repository, app.Record{Kind: "prices", Prices: &prices})
	}
	if count, err := (app.Relay{Repository: repository, Publisher: client, Retry: app.DefaultRetryPolicy()}).Drain(ctx, 10); err != nil || count != 4 {
		t.Fatal(count, err)
	}
	worker := consumer(t, client, repository, "m24-acceptance")
	for range 4 {
		if err := worker.Next(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for table, expected := range map[string]int{"companies": 1, "filings": 2, "economic_observations": 3, "market_prices": 4} {
		if scalar(t, ctx, repository, "SELECT count(*) FROM market_data."+table) != expected {
			t.Fatal("unexpected row count", table)
		}
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM timescaledb_information.hypertables WHERE hypertable_schema='market_data' AND hypertable_name='market_prices'") != 1 {
		t.Fatal("missing Timescale hypertable")
	}
	var value string
	if err := repository.pool.QueryRow(ctx, "SELECT value::text FROM market_data.economic_observations WHERE period_date='2024-01-01'").Scan(&value); err != nil || value != "100.123456789012345678" {
		t.Fatal("numeric precision lost", value, err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.economic_observations WHERE value IS NULL AND quality='missing'") != 1 {
		t.Fatal("missing data became zero")
	}
	if scalar(t, ctx, repository, "SELECT count(DISTINCT adjustment) FROM market_data.market_prices") != 2 {
		t.Fatal("adjusted/unadjusted series mixed")
	}
	if err := repository.pool.QueryRow(ctx, "SELECT open::text FROM market_data.market_prices ORDER BY time LIMIT 1").Scan(&value); err != nil || value != "100.12345678" {
		t.Fatal(value, err)
	}

	// Deliver a newer correction before an older snapshot; older data cannot win.
	newer := filings(t, ctx, endpoint, false)
	newer.Filings.Company.Name = "SYNTHETIC NEWER CORRECTION"
	newEvent, newData := queue(t, ctx, repository, newer)
	if _, err := repository.Apply(ctx, newEvent, newData, newer); err != nil {
		t.Fatal(err)
	}
	older := filings(t, ctx, endpoint, false)
	older.Filings.Company.Name = "SYNTHETIC OLD CORRECTION"
	older.Filings.Company.MetadataSource.RetrievedAt = newer.Filings.Company.MetadataSource.RetrievedAt.Add(-time.Hour)
	for i := range older.Filings.Filings {
		older.Filings.Filings[i].MetadataSource.RetrievedAt = older.Filings.Company.MetadataSource.RetrievedAt
	}
	oldEvent, oldData := queue(t, ctx, repository, older)
	if _, err := repository.Apply(ctx, oldEvent, oldData, older); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, "SELECT name FROM market_data.companies").Scan(&value); err != nil || value != newer.Filings.Company.Name {
		t.Fatal("out-of-order snapshot overwrote newer facts", value, err)
	}

	// Concurrent raw relay calls use SKIP LOCKED and publish each pending row once.
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := repository.DispatchNext(ctx, func(app.Delivery) app.DispatchResult {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return app.DispatchResult{Attempts: 1}
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatal("pending rows were lost or double dispatched", calls.Load())
	}
	status, err := repository.Status(ctx)
	if err != nil || !strings.Contains(string(status), "source_url") || !strings.Contains(string(status), "last_checked_at") || strings.Contains(string(status), "original_bytes") {
		t.Fatal("provenance/freshness status incomplete", err, string(status))
	}
	for kind, id := range map[string]string{"company": "cik:0001045810", "filing": "0001045810-24-000001", "economic": "fred:TESTSERIES", "prices": "figi:BBG000000001"} {
		rows, err := repository.Inspect(ctx, kind, id)
		if err != nil || len(rows) == 0 {
			t.Fatal(kind, err)
		}
	}
	if _, err := repository.Inspect(ctx, "arbitrary SQL", "1"); err == nil {
		t.Fatal("unbounded query exposed")
	}
	_, err = admin.Exec(ctx, `SET ROLE mm_identity; CREATE TABLE identity.m24_access_probe (marker TEXT); RESET ROLE`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.pool.Exec(ctx, "SELECT * FROM identity.m24_access_probe")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatal("cross-service runtime access not denied", err)
	}
	var checksum string
	_ = admin.QueryRow(ctx, "SELECT sha256 FROM market_data.schema_migrations WHERE version='001_delivery.sql'").Scan(&checksum)
	_, _ = admin.Exec(ctx, "UPDATE market_data.schema_migrations SET sha256='modified' WHERE version='001_delivery.sql'")
	if err := Migrate(ctx, os.Getenv("MARKET_TEST_ADMIN_DSN")); err == nil {
		t.Fatal("changed applied migration accepted")
	}
	_, _ = admin.Exec(ctx, "UPDATE market_data.schema_migrations SET sha256=$1 WHERE version='001_delivery.sql'", checksum)
	t.Log("M2.4: compiled CLI + HTTP -> transactional outbox -> Kafka -> PostgreSQL/Timescale passed; exact decimals, missing values, freshness, corrections and isolation verified")
}
