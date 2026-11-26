CREATE TABLE market_data.ingestion_events (
    idempotency_key TEXT PRIMARY KEY CHECK (idempotency_key ~ '^[a-f0-9]{64}$'),
    topic TEXT NOT NULL,
    envelope JSONB NOT NULL,
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'processed', 'rejected', 'failed')),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ,
    error_code TEXT,
    CHECK (last_seen_at >= first_seen_at)
);

CREATE TABLE market_data.outbox (
    idempotency_key TEXT PRIMARY KEY CHECK (idempotency_key ~ '^[a-f0-9]{64}$'),
    topic TEXT NOT NULL,
    message_key TEXT NOT NULL,
    envelope JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error_code TEXT,
    CHECK (delivered_at IS NULL OR failed_at IS NULL)
);
CREATE INDEX outbox_pending_idx ON market_data.outbox (created_at, idempotency_key)
    WHERE delivered_at IS NULL AND failed_at IS NULL;

CREATE TABLE market_data.ingestion_errors (
    failure_id TEXT PRIMARY KEY CHECK (failure_id ~ '^[a-f0-9]{64}$'),
    original_topic TEXT NOT NULL,
    original_key TEXT NOT NULL,
    event_key TEXT,
    original_bytes BYTEA NOT NULL,
    code TEXT NOT NULL CHECK (code IN ('invalid_event', 'processing_failed', 'delivery_exhausted')),
    attempts INTEGER NOT NULL CHECK (attempts > 0),
    failed_at TIMESTAMPTZ NOT NULL,
    replayed_at TIMESTAMPTZ,
    replay_count INTEGER NOT NULL DEFAULT 0 CHECK (replay_count >= 0)
);
CREATE INDEX ingestion_errors_status_idx ON market_data.ingestion_errors (failed_at, replayed_at);
