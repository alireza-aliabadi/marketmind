-- Additive operational state; do not rewrite the verified M2 migrations.
CREATE TABLE market_data.provider_request_limits (
    provider TEXT PRIMARY KEY CHECK (provider IN ('sec-edgar','fred','massive')),
    next_allowed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE market_data.provider_runs (
    run_id UUID PRIMARY KEY,
    provider TEXT NOT NULL CHECK (provider IN ('sec-edgar','fred','massive')),
    correlation_id TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    state TEXT NOT NULL CHECK (state IN ('running','succeeded','failed')),
    error_class TEXT,
    event_key TEXT,
    CHECK ((state = 'running') = (completed_at IS NULL))
);
CREATE INDEX provider_runs_recent_idx ON market_data.provider_runs (provider,started_at DESC);

ALTER TABLE market_data.ingestion_events ADD COLUMN payload_redacted_at TIMESTAMPTZ;
ALTER TABLE market_data.outbox ADD COLUMN payload_redacted_at TIMESTAMPTZ;
ALTER TABLE market_data.ingestion_errors ADD COLUMN payload_redacted_at TIMESTAMPTZ;
