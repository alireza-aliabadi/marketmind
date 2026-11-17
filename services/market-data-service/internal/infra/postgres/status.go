package postgres

import (
	"context"
	"encoding/json"
)

// Status exposes bounded metadata, never raw DLQ bytes, credentials or arbitrary SQL.
func (r *Repository) Status(ctx context.Context) (json.RawMessage, error) {
	var result []byte
	err := r.pool.QueryRow(ctx, `SELECT jsonb_build_object(
        'queued',(SELECT count(*) FROM market_data.ingestion_events WHERE state='queued'),
        'processed',(SELECT count(*) FROM market_data.ingestion_events WHERE state='processed'),
        'pending_outbox',(SELECT count(*) FROM market_data.outbox WHERE delivered_at IS NULL AND failed_at IS NULL),
        'failed_outbox',(SELECT count(*) FROM market_data.outbox WHERE failed_at IS NOT NULL),
        'companies',(SELECT count(*) FROM market_data.companies),
        'filings',(SELECT count(*) FROM market_data.filings),
        'economic_observations',(SELECT count(*) FROM market_data.economic_observations),
        'price_bars',(SELECT count(*) FROM market_data.market_prices),
        'missing_observations',(SELECT coalesce(sum(missing_count),0) FROM market_data.ingestion_quality),
        'source_count',(SELECT count(*) FROM market_data.data_sources),
        'sources',coalesce((SELECT jsonb_agg(row_to_json(s)) FROM (
            SELECT *, extract(epoch FROM now()-last_checked_at) AS checked_age_seconds
            FROM market_data.source_freshness ORDER BY last_checked_at DESC LIMIT 100
        ) s),'[]'::jsonb),
        'errors',coalesce((SELECT jsonb_agg(row_to_json(e)) FROM (
            SELECT failure_id,original_topic,code,attempts,failed_at,replayed_at,replay_count
            FROM market_data.ingestion_errors ORDER BY failed_at DESC LIMIT 100
        ) e),'[]'::jsonb)
    )`).Scan(&result)
	return result, err
}
