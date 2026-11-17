package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	market "marketmind/services/market-data-service/internal/application/market"
	"marketmind/services/market-data-service/internal/infra/events"
)

var _ market.RequestLimiter = (*Repository)(nil)

// Acquire serializes actual request starts across processes using an owned row
// lock, not merely scheduled slots that could bunch after a delayed worker wakes.
// One connection/transaction is held for one bounded provider HTTP attempt.
func (r *Repository) Acquire(ctx context.Context, provider string, interval time.Duration) (func() error, error) {
	if provider != "sec-edgar" && provider != "fred" && provider != "massive" {
		return nil, fmt.Errorf("unknown provider request gate")
	}
	if interval < 0 || interval > time.Minute {
		return nil, fmt.Errorf("invalid provider request interval")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO market_data.provider_request_limits (provider) VALUES ($1) ON CONFLICT DO NOTHING`, provider); err != nil {
		rollback()
		return nil, err
	}
	var seconds float64
	if err := tx.QueryRow(ctx, `SELECT greatest(extract(epoch FROM next_allowed_at-clock_timestamp()),0)::float8
        FROM market_data.provider_request_limits WHERE provider=$1 FOR UPDATE`, provider).Scan(&seconds); err != nil {
		rollback()
		return nil, err
	}
	timer := time.NewTimer(time.Duration(seconds * float64(time.Second)))
	select {
	case <-ctx.Done():
		timer.Stop()
		rollback()
		return nil, ctx.Err()
	case <-timer.C:
	}
	var once sync.Once
	var releaseErr error
	release := func() error {
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			defer tx.Rollback(cleanup)
			if _, err := tx.Exec(cleanup, `UPDATE market_data.provider_request_limits
                SET next_allowed_at=clock_timestamp()+($2::double precision * interval '1 second') WHERE provider=$1`, provider, interval.Seconds()); err != nil {
				releaseErr = err
				return
			}
			releaseErr = tx.Commit(cleanup)
		})
		return releaseErr
	}
	return release, nil
}

func (r *Repository) BeginRun(ctx context.Context, provider, correlation string) (string, error) {
	if !app.ValidToken(correlation) {
		return "", app.ErrInvalidData
	}
	id := events.NewID()
	_, err := r.pool.Exec(ctx, `INSERT INTO market_data.provider_runs (run_id,provider,correlation_id,state) VALUES ($1,$2,$3,'running')`, id, provider, correlation)
	return id, err
}

func (r *Repository) FinishRun(ctx context.Context, id, key, errorClass string) error {
	state := "succeeded"
	if errorClass != "" {
		state = "failed"
	}
	_, err := r.pool.Exec(ctx, `UPDATE market_data.provider_runs SET state=$2,completed_at=clock_timestamp(),
        event_key=nullif($3,''),error_class=nullif($4,'') WHERE run_id=$1 AND state='running'`, id, state, key, errorClass)
	return err
}

func (r *Repository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

type Backlog struct {
	Pending       int64   `json:"pending"`
	Failed        int64   `json:"failed"`
	OldestSeconds float64 `json:"oldest_pending_seconds"`
	OpenRuns      int64   `json:"open_provider_runs"`
}

func (r *Repository) Backlog(ctx context.Context) (Backlog, error) {
	var result Backlog
	err := r.pool.QueryRow(ctx, `SELECT
        count(*) FILTER (WHERE delivered_at IS NULL AND failed_at IS NULL),
        count(*) FILTER (WHERE failed_at IS NOT NULL),
        coalesce(extract(epoch FROM clock_timestamp()-min(created_at) FILTER
            (WHERE delivered_at IS NULL AND failed_at IS NULL)),0)::float8,
        (SELECT count(*) FROM market_data.provider_runs WHERE state='running') FROM market_data.outbox`).Scan(
		&result.Pending, &result.Failed, &result.OldestSeconds, &result.OpenRuns)
	return result, err
}

// RedactCompletedPayloads preserves deduplication/audit identifiers and never
// silently discards pending/unreplayed work or deletes business read models.
// Licensed read-model retention still needs a provider-specific release review.
func (r *Repository) RedactCompletedPayloads(ctx context.Context, before time.Time, apply bool) (int64, error) {
	if before.After(time.Now().UTC().Add(-14 * 24 * time.Hour)) {
		return 0, fmt.Errorf("payload retention must be at least 14 days")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var count int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM market_data.ingestion_events e
        WHERE e.state='processed' AND e.processed_at<$1 AND e.payload_redacted_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM market_data.outbox o WHERE o.idempotency_key=e.idempotency_key AND o.delivered_at IS NULL)
        AND NOT EXISTS (SELECT 1 FROM market_data.ingestion_errors f WHERE f.event_key=e.idempotency_key AND f.replayed_at IS NULL)`, before).Scan(&count); err != nil {
		return 0, err
	}
	if !apply {
		return count, nil
	}
	_, err = tx.Exec(ctx, `WITH eligible AS (
        SELECT e.idempotency_key FROM market_data.ingestion_events e
        WHERE e.state='processed' AND e.processed_at<$1 AND e.payload_redacted_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM market_data.outbox o WHERE o.idempotency_key=e.idempotency_key AND o.delivered_at IS NULL)
        AND NOT EXISTS (SELECT 1 FROM market_data.ingestion_errors f WHERE f.event_key=e.idempotency_key AND f.replayed_at IS NULL)
        FOR UPDATE
    ), redacted AS (
        UPDATE market_data.ingestion_events SET envelope='{}',payload_redacted_at=clock_timestamp()
        WHERE idempotency_key IN (SELECT idempotency_key FROM eligible) RETURNING idempotency_key
    ) UPDATE market_data.outbox SET envelope='{}',payload_redacted_at=clock_timestamp()
        WHERE idempotency_key IN (SELECT idempotency_key FROM redacted) AND delivered_at IS NOT NULL`, before)
	if err != nil {
		return 0, err
	}
	return count, tx.Commit(ctx)
}
