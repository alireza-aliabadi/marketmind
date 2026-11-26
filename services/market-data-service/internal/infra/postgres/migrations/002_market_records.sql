CREATE TABLE market_data.data_sources (
    source_id TEXT PRIMARY KEY CHECK (source_id ~ '^[a-f0-9]{64}$'),
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    source_url TEXT NOT NULL CHECK (source_url LIKE 'https://%'),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[a-f0-9]{64}$'),
    first_retrieved_at TIMESTAMPTZ NOT NULL,
    last_retrieved_at TIMESTAMPTZ NOT NULL,
    license_classification TEXT NOT NULL,
    permission_reference TEXT NOT NULL CHECK (permission_reference <> ''),
    retention_allowed BOOLEAN NOT NULL CHECK (retention_allowed),
    ai_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    CHECK (last_retrieved_at >= first_retrieved_at)
);
CREATE INDEX source_provider_identity_idx ON market_data.data_sources (provider, external_id);
CREATE INDEX source_freshness_idx ON market_data.data_sources (last_retrieved_at DESC);

CREATE TABLE market_data.event_sources (
    event_key TEXT NOT NULL REFERENCES market_data.ingestion_events (idempotency_key),
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    PRIMARY KEY (event_key, source_id)
);

CREATE TABLE market_data.companies (
    company_id TEXT PRIMARY KEY CHECK (company_id ~ '^cik:[0-9]{10}$'),
    cik TEXT NOT NULL UNIQUE CHECK (cik ~ '^[0-9]{10}$' AND cik <> '0000000000'),
    name TEXT NOT NULL CHECK (name <> ''),
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);
CREATE INDEX company_name_idx ON market_data.companies (lower(name));

CREATE TABLE market_data.filings (
    accession_number TEXT PRIMARY KEY CHECK (accession_number ~ '^[0-9]{10}-[0-9]{2}-[0-9]{6}$'),
    company_id TEXT NOT NULL REFERENCES market_data.companies (company_id),
    form TEXT NOT NULL,
    filing_date DATE NOT NULL,
    report_date DATE,
    published_at TIMESTAMPTZ,
    document_url TEXT,
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    CHECK (published_at IS NULL OR published_at <= observed_at)
);
CREATE INDEX filing_company_date_idx ON market_data.filings (company_id, filing_date DESC);

CREATE TABLE market_data.instruments (
    instrument_id TEXT PRIMARY KEY CHECK (instrument_id ~ '^figi:BBG[A-Z0-9]{9}$'),
    company_id TEXT CHECK (company_id IS NULL OR company_id ~ '^cik:[0-9]{10}$'),
    name TEXT NOT NULL,
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);
-- Company identity is a reference, not an arrival-order FK: SEC may arrive later.
CREATE TABLE market_data.instrument_listings (
    instrument_id TEXT NOT NULL REFERENCES market_data.instruments (instrument_id),
    ticker TEXT NOT NULL,
    exchange_mic TEXT NOT NULL CHECK (exchange_mic ~ '^[A-Z0-9]{4}$'),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    as_of_date DATE NOT NULL,
    list_date DATE,
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instrument_id, exchange_mic, as_of_date),
    CHECK (list_date IS NULL OR list_date <= as_of_date)
);
CREATE INDEX listing_symbol_idx ON market_data.instrument_listings (ticker, exchange_mic, as_of_date);

CREATE TABLE market_data.economic_series (
    series_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    units TEXT NOT NULL,
    frequency TEXT NOT NULL,
    seasonal_adjustment TEXT NOT NULL,
    notices TEXT NOT NULL,
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);
CREATE TABLE market_data.economic_observations (
    series_id TEXT NOT NULL REFERENCES market_data.economic_series (series_id),
    period_date DATE NOT NULL,
    realtime_start DATE NOT NULL,
    realtime_end DATE NOT NULL,
    value NUMERIC,
    quality TEXT NOT NULL CHECK (quality IN ('valid', 'missing')),
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    PRIMARY KEY (series_id, period_date, realtime_start, realtime_end),
    CHECK (realtime_start <= realtime_end),
    CHECK ((quality = 'missing') = (value IS NULL))
);
CREATE INDEX economic_period_idx ON market_data.economic_observations (series_id, period_date DESC);

CREATE TABLE market_data.market_prices (
    time TIMESTAMPTZ NOT NULL,
    instrument_id TEXT NOT NULL REFERENCES market_data.instruments (instrument_id),
    provider TEXT NOT NULL,
    exchange_mic TEXT NOT NULL CHECK (exchange_mic ~ '^[A-Z0-9]{4}$'),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    adjustment TEXT NOT NULL CHECK (adjustment IN ('unadjusted', 'split_adjusted')),
    delay_mode TEXT NOT NULL CHECK (delay_mode IN ('end_of_day', 'delayed', 'realtime', 'unknown')),
    delay_seconds INTEGER,
    open NUMERIC NOT NULL CHECK (open >= 0),
    high NUMERIC NOT NULL CHECK (high >= 0),
    low NUMERIC NOT NULL CHECK (low >= 0),
    close NUMERIC NOT NULL CHECK (close >= 0),
    volume NUMERIC NOT NULL CHECK (volume >= 0),
    source_id TEXT NOT NULL REFERENCES market_data.data_sources (source_id),
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    PRIMARY KEY (time, instrument_id, provider, exchange_mic, currency, adjustment),
    CHECK (high >= low AND high >= open AND high >= close AND low <= open AND low <= close),
    CHECK (time <= observed_at),
    CHECK ((delay_mode IN ('end_of_day', 'unknown') AND delay_seconds IS NULL)
        OR (delay_mode = 'delayed' AND delay_seconds > 0)
        OR (delay_mode = 'realtime' AND delay_seconds = 0))
);
SELECT create_hypertable('market_data.market_prices', by_range('time'), if_not_exists => TRUE);
CREATE INDEX price_instrument_time_idx ON market_data.market_prices (instrument_id, time DESC);

CREATE TABLE market_data.ingestion_quality (
    event_key TEXT PRIMARY KEY REFERENCES market_data.ingestion_events (idempotency_key),
    record_count INTEGER NOT NULL CHECK (record_count >= 0),
    missing_count INTEGER NOT NULL CHECK (missing_count >= 0 AND missing_count <= record_count),
    validated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE VIEW market_data.source_freshness AS
SELECT s.source_id, s.provider, s.external_id, s.source_url, s.payload_sha256,
       s.first_retrieved_at, s.last_retrieved_at,
       greatest(s.last_retrieved_at, coalesce(max(e.last_seen_at), s.last_retrieved_at)) AS last_checked_at,
       s.license_classification, s.permission_reference, s.retention_allowed, s.ai_allowed
FROM market_data.data_sources s
LEFT JOIN market_data.event_sources es ON es.source_id = s.source_id
LEFT JOIN market_data.ingestion_events e ON e.idempotency_key = es.event_key
GROUP BY s.source_id;
