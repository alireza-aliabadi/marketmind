package postgres

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	app "marketmind/services/market-data-service/internal/application/ingestion"
)

type Config struct {
	URL        string
	Production bool
	Policy     app.SourcePolicy
}

func validateConnection(config Config) error {
	if config.URL == "" {
		return fmt.Errorf("explicit database connection required")
	}
	if config.Production {
		parsed, err := url.Parse(config.URL)
		if err != nil || parsed.Hostname() == "" || parsed.Query().Get("sslmode") != "verify-full" {
			return fmt.Errorf("production PostgreSQL requires verify-full TLS; plaintext/fallback is forbidden")
		}
	}
	return nil
}

// ProvisionRuntimeRole is explicit operator setup, not startup/password provisioning.
// Existing elevated/conflicting roles fail instead of being altered or taken over.
func ProvisionRuntimeRole(ctx context.Context, config Config) error {
	if err := validateConnection(config); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, config.URL)
	if err != nil {
		return fmt.Errorf("invalid runtime-role operator configuration")
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `DO $$ BEGIN
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mm_market_data_runtime') THEN
            CREATE ROLE mm_market_data_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        ELSIF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mm_market_data_runtime'
            AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)) THEN
            RAISE EXCEPTION 'Conflicting market runtime role';
        END IF;
        IF pg_has_role('mm_market_data_runtime','mm_market_data','MEMBER') THEN
            RAISE EXCEPTION 'Runtime role must not inherit schema ownership';
        END IF;
        END $$;
        GRANT USAGE ON SCHEMA market_data TO mm_market_data_runtime;
        GRANT SELECT ON ALL TABLES IN SCHEMA market_data TO mm_market_data_runtime;
        GRANT SELECT,INSERT,UPDATE,DELETE ON
            market_data.ingestion_events,market_data.outbox,market_data.ingestion_errors,
            market_data.data_sources,market_data.event_sources,market_data.companies,
            market_data.filings,market_data.instruments,market_data.instrument_listings,
            market_data.economic_series,market_data.economic_observations,market_data.market_prices,
            market_data.ingestion_quality,market_data.provider_request_limits,market_data.provider_runs
            TO mm_market_data_runtime;`)
	if err != nil {
		return fmt.Errorf("runtime role setup failed; review ownership and migrations")
	}
	return nil
}
