package postgres

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestProductionDatabaseControls(t *testing.T) {
	ctx, legacy, _, _, admin := stack(t)
	if err := ProvisionRuntimeRole(ctx, Config{URL: admin.Config().ConnConfig.ConnString()}); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE ROLE market_dml_test LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE
        NOREPLICATION NOBYPASSRLS PASSWORD 'synthetic-test-only-password' IN ROLE mm_market_data_runtime`); err != nil {
		t.Fatal(err)
	}
	dsn := strings.Replace(admin.Config().ConnConfig.ConnString(), "postgresql://postgres:", "postgresql://market_dml_test:", 1)
	repository, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := SetupConfigured(ctx, Config{URL: dsn}); err == nil {
		t.Fatal("DML-only runtime credentials were allowed to perform operator setup")
	}
	// TLS is separately tested at the transport boundary; this private fixture
	// exercises the actual SQL privilege guard without weakening production config.
	repository.production = true
	if err := repository.checkRuntimeRole(ctx); err != nil {
		t.Fatal(err)
	}
	legacy.production = true
	if err := legacy.checkRuntimeRole(ctx); err == nil {
		t.Fatal("schema-owner account accepted for production")
	}
	for _, sql := range []string{
		"CREATE TABLE market_data.forbidden_ddl (id int)",
		"ALTER TABLE market_data.companies ADD COLUMN forbidden int",
		"UPDATE market_data.schema_migrations SET sha256='tampered'",
		"SET ROLE mm_market_data",
	} {
		_, err := repository.pool.Exec(ctx, sql)
		pgError, ok := err.(*pgconn.PgError)
		if !ok || pgError.Code != "42501" {
			t.Fatal("DDL/elevation not denied", sql, err)
		}
	}
	runID, err := repository.BeginRun(ctx, "sec-edgar", "private-operation")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.FinishRun(ctx, runID, "", "provider_unavailable"); err != nil {
		t.Fatal(err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.provider_runs WHERE state='failed'") != 1 {
		t.Fatal("provider failure not journaled")
	}
	second, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var mu sync.Mutex
	var starts []time.Time
	var wg sync.WaitGroup
	for _, store := range []*Repository{repository, second, repository} {
		wg.Go(func() {
			release, err := store.Acquire(ctx, "sec-edgar", 100*time.Millisecond)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			if err := release(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(starts) != 3 {
		t.Fatal("shared request leases incomplete")
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 100*time.Millisecond {
			t.Fatal("provider quota was process-local or requests bunched", starts)
		}
	}
	if _, err := NewConfigured(ctx, Config{URL: dsn, Production: true}); err == nil {
		t.Fatal("production plaintext DB permitted")
	}
	_, err = repository.RedactCompletedPayloads(ctx, time.Now().UTC(), true)
	if err == nil {
		t.Fatal("unsafe retention duration accepted")
	}
	record := filings(t, ctx, fixtureServer(t), false)
	event, data := queue(t, ctx, repository, record)
	if _, err := repository.Apply(ctx, event, data, record); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE market_data.ingestion_events SET processed_at=clock_timestamp()-interval '20 days' WHERE idempotency_key=$1`, data.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE market_data.outbox SET delivered_at=clock_timestamp()-interval '20 days' WHERE idempotency_key=$1`, data.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC().Add(-14 * 24 * time.Hour)
	if count, err := repository.RedactCompletedPayloads(ctx, cutoff, false); err != nil || count != 1 {
		t.Fatal("retention dry run", count, err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.ingestion_events WHERE envelope<>'{}'") != 1 {
		t.Fatal("dry run mutated payload")
	}
	if count, err := repository.RedactCompletedPayloads(ctx, cutoff, true); err != nil || count != 1 {
		t.Fatal("retention apply", count, err)
	}
	if scalar(t, ctx, repository, "SELECT count(*) FROM market_data.ingestion_events WHERE payload_redacted_at IS NOT NULL AND envelope='{}'") != 1 {
		t.Fatal("completed payload not redacted")
	}
	if queued, err := repository.Enqueue(ctx, event, data); err != nil || queued {
		t.Fatal("redaction lost replay tombstone", queued, err)
	}
	t.Log("DML-only login, migration/DDL/elevation denial, durable provider failure and cross-connection quota serialization verified")
}
