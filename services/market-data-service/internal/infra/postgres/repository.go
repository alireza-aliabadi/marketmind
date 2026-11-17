package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "marketmind/services/market-data-service/internal/application/ingestion"
	"marketmind/services/market-data-service/internal/infra/events"
)

type Repository struct {
	pool       *pgxpool.Pool
	production bool
	policy     app.SourcePolicy
}

var _ app.Repository = (*Repository)(nil)

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func New(ctx context.Context, dsn string) (*Repository, error) {
	return NewConfigured(ctx, Config{URL: dsn})
}

func NewConfigured(ctx context.Context, connection Config) (*Repository, error) {
	dsn := connection.URL
	if err := validateConnection(connection); err != nil {
		return nil, err
	}
	if dsn == "" {
		return nil, fmt.Errorf("MARKET_DATABASE_URL is required; administrator fallback is forbidden")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid market database configuration")
	}
	config.MaxConns = 8
	config.ConnConfig.ConnectTimeout = 10 * time.Second
	config.ConnConfig.RuntimeParams["statement_timeout"] = "20000"
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	config.ConnConfig.RuntimeParams["search_path"] = "market_data,pg_catalog"
	config.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("market database unavailable")
	}
	repository := &Repository{pool: pool, production: connection.Production, policy: connection.Policy}
	if err := repository.checkRuntimeRole(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return repository, nil
}

func (r *Repository) checkRuntimeRole(ctx context.Context) error {
	var permitted bool
	err := r.pool.QueryRow(ctx, `SELECT NOT (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication)
        AND (pg_has_role(current_user,'mm_market_data','MEMBER') OR EXISTS (
            SELECT 1 FROM pg_roles g WHERE g.rolname='mm_market_data_runtime'
            AND pg_has_role(current_user,g.oid,'MEMBER')))
        AND NOT EXISTS (
            SELECT 1 FROM pg_namespace WHERE nspname IN
                ('identity','alerts','event_intelligence','knowledge_graph','ai_orchestrator','quantitative')
            AND has_schema_privilege(current_user,oid,'USAGE')
        ) FROM pg_roles WHERE rolname=current_user`).Scan(&permitted)
	if err != nil || !permitted {
		return fmt.Errorf("market runtime credentials must be least-privilege and own only market_data")
	}
	if r.production {
		// A production LOGIN may use only the DML group, never the schema owner.
		err = r.pool.QueryRow(ctx, `SELECT pg_has_role(current_user,'mm_market_data_runtime','MEMBER')
            AND NOT pg_has_role(current_user,'mm_market_data','MEMBER')
            AND NOT has_schema_privilege(current_user,'market_data','CREATE')
            AND NOT has_table_privilege(current_user,'market_data.schema_migrations','UPDATE')`).Scan(&permitted)
		if err != nil || !permitted {
			return fmt.Errorf("production runtime must use the DML-only group, not schema ownership")
		}
	}
	if err := verifyMigrations(ctx, r.pool); err != nil {
		return err
	}
	return nil
}

func (r *Repository) Close() { r.pool.Close() }

func (r *Repository) Enqueue(ctx context.Context, event app.Envelope, data app.RawData) (bool, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	_, verified, record, err := (events.Codec{}).Decode(event.Type, body)
	if err != nil || verified.IdempotencyKey != data.IdempotencyKey {
		return false, app.ErrInvalidData
	}
	if r.policy != nil {
		if err := r.policy.Check(record); err != nil {
			return false, err
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO market_data.ingestion_events
        (idempotency_key,topic,envelope,first_seen_at,last_seen_at) VALUES ($1,$2,$3,$4,$4)
        ON CONFLICT DO NOTHING`, data.IdempotencyKey, event.Type, body, event.Time)
	if err != nil {
		return false, err
	}
	inserted := result.RowsAffected() == 1
	if inserted {
		if err := putOutbox(ctx, tx, event, data.IdempotencyKey); err != nil {
			return false, err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE market_data.ingestion_events
            SET first_seen_at=least(first_seen_at,$2),last_seen_at=greatest(last_seen_at,$2)
            WHERE idempotency_key=$1`, data.IdempotencyKey, event.Time); err != nil {
			return false, err
		}
	}
	return inserted, tx.Commit(ctx)
}

func putOutbox(ctx context.Context, tx pgx.Tx, event app.Envelope, key string) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO market_data.outbox (idempotency_key,topic,message_key,envelope)
        VALUES ($1,$2,$1,$3) ON CONFLICT DO NOTHING`, key, event.Type, body)
	return err
}

func (r *Repository) Apply(ctx context.Context, event app.Envelope, data app.RawData, record app.Record) (bool, error) {
	if err := record.Validate(); err != nil {
		return false, err
	}
	if r.policy != nil {
		if err := r.policy.Check(record); err != nil {
			return false, err
		}
	}
	if events.Identity(record) != data.IdempotencyKey {
		return false, app.ErrInvalidData
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	body, _ := json.Marshal(event)
	if _, err := tx.Exec(ctx, `INSERT INTO market_data.ingestion_events
        (idempotency_key,topic,envelope,first_seen_at,last_seen_at) VALUES ($1,$2,$3,$4,$4)
        ON CONFLICT DO NOTHING`, data.IdempotencyKey, event.Type, body, event.Time); err != nil {
		return false, err
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM market_data.ingestion_events WHERE idempotency_key=$1 FOR UPDATE`, data.IdempotencyKey).Scan(&state); err != nil {
		return false, err
	}
	if state == "processed" {
		return false, tx.Commit(ctx)
	}
	for _, source := range record.Sources() {
		id, err := saveSource(ctx, tx, source)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO market_data.event_sources (event_key,source_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, data.IdempotencyKey, id); err != nil {
			return false, err
		}
	}
	if err := saveRecord(ctx, tx, record); err != nil {
		return false, err
	}
	count, missing := 0, 0
	if record.Filings != nil {
		count = 1 + len(record.Filings.Filings)
	}
	if record.Prices != nil {
		count = len(record.Prices.Bars)
	}
	if record.Economics != nil {
		count = len(record.Economics.Observations)
		for _, observation := range record.Economics.Observations {
			if observation.Value == nil {
				missing++
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO market_data.ingestion_quality (event_key,record_count,missing_count)
        VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, data.IdempotencyKey, count, missing); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE market_data.ingestion_events SET state='processed',processed_at=now(),error_code=NULL WHERE idempotency_key=$1`, data.IdempotencyKey); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func quarantine(ctx context.Context, tx pgx.Tx, failure app.Failure) error {
	id := events.FailureID(failure)
	if _, err := tx.Exec(ctx, `INSERT INTO market_data.ingestion_errors
        (failure_id,original_topic,original_key,event_key,original_bytes,code,attempts,failed_at)
        VALUES ($1,$2,$3,nullif($4,''),$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		id, failure.Topic, failure.MessageKey, failure.EventKey, failure.Value, failure.Code, failure.Attempts, failure.FailedAt); err != nil {
		return err
	}
	event, _ := events.DeadLetter(failure)
	if err := putOutbox(ctx, tx, event, id); err != nil {
		return err
	}
	if failure.EventKey != "" {
		state := "failed"
		if failure.Code == "invalid_event" {
			state = "rejected"
		}
		_, err := tx.Exec(ctx, `UPDATE market_data.ingestion_events SET state=$3,error_code=$2
            WHERE idempotency_key=$1 AND state<>'processed'`, failure.EventKey, failure.Code, state)
		return err
	}
	return nil
}

func (r *Repository) Quarantine(ctx context.Context, failure app.Failure) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := quarantine(ctx, tx, failure); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) DispatchNext(ctx context.Context, deliver func(app.Delivery) app.DispatchResult) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var delivery app.Delivery
	var key string
	err = tx.QueryRow(ctx, `SELECT idempotency_key,topic,message_key,envelope FROM market_data.outbox
        WHERE delivered_at IS NULL AND failed_at IS NULL ORDER BY created_at,idempotency_key
        FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&key, &delivery.Topic, &delivery.Key, &delivery.Value)
	if isNoRows(err) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	if r.policy != nil && delivery.Topic != app.DLQTopic {
		_, _, record, err := (events.Codec{}).Decode(delivery.Topic, delivery.Value)
		if err != nil {
			return false, err
		}
		if err := r.policy.Check(record); err != nil {
			return false, err
		}
	}
	result := deliver(delivery)
	publicationErr := result.Err
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if result.Attempts < 1 || errors.Is(publicationErr, context.Canceled) || errors.Is(publicationErr, context.DeadlineExceeded) {
		return false, publicationErr // Roll back; leave pending for the next relay.
	}
	if publicationErr == nil {
		_, err = tx.Exec(ctx, `UPDATE market_data.outbox SET delivered_at=now(),attempts=attempts+$2 WHERE idempotency_key=$1`, key, result.Attempts)
	} else {
		_, err = tx.Exec(ctx, `UPDATE market_data.outbox SET failed_at=now(),attempts=attempts+$2,error_code='delivery_exhausted' WHERE idempotency_key=$1`, key, result.Attempts)
		if err == nil && delivery.Topic != app.DLQTopic {
			err = quarantine(ctx, tx, app.Failure{Topic: delivery.Topic, MessageKey: delivery.Key,
				EventKey: key, Value: delivery.Value, Code: "delivery_exhausted", Attempts: result.Attempts, FailedAt: time.Now().UTC()})
		} // Never recursively DLQ a failed DLQ; its durable failed row remains inspectable.
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, publicationErr
}

func (r *Repository) Replay(ctx context.Context, id string) error {
	if !app.ValidHash(id) {
		return app.ErrInvalidData
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var delivery app.Delivery
	if err := tx.QueryRow(ctx, `SELECT original_topic,original_key,original_bytes FROM market_data.ingestion_errors
        WHERE failure_id=$1 FOR UPDATE`, id).Scan(&delivery.Topic, &delivery.Key, &delivery.Value); err != nil {
		return err
	}
	event, data, record, err := (events.Codec{}).Decode(delivery.Topic, delivery.Value)
	if err != nil || delivery.Key != data.IdempotencyKey {
		return fmt.Errorf("invalid DLQ bytes cannot be replayed; correct the producer and enqueue a new record")
	}
	if r.policy != nil {
		if err := r.policy.Check(record); err != nil {
			return err
		}
	}
	if err := putOutbox(ctx, tx, event, data.IdempotencyKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE market_data.outbox SET delivered_at=NULL,failed_at=NULL,error_code=NULL,attempts=0 WHERE idempotency_key=$1`, data.IdempotencyKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE market_data.ingestion_errors SET replay_count=replay_count+1,replayed_at=now() WHERE failure_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE market_data.ingestion_events SET state='queued',error_code=NULL WHERE idempotency_key=$1 AND state<>'processed'`, data.IdempotencyKey); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
