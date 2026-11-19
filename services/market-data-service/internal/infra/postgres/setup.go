package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetupConfigured owns database setup inside the Go binary, not a shell/psql
// script. It is operator-only; normal workers never provision roles or run DDL.
// Repeated or interrupted setup is safe, and conflicting ownership is not changed.
func SetupConfigured(ctx context.Context, connection Config) error {
	if err := validateConnection(connection); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, connection.URL)
	if err != nil {
		return fmt.Errorf("invalid setup operator configuration")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("setup database unavailable")
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'; SET LOCAL statement_timeout = '60s';
        SELECT pg_advisory_xact_lock(713241);
        DO $$ BEGIN
            IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mm_market_data') THEN
                CREATE ROLE mm_market_data NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
            ELSIF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='mm_market_data'
                AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)) THEN
                RAISE EXCEPTION 'Conflicting market owner role';
            END IF;
            IF EXISTS (SELECT 1 FROM pg_namespace WHERE nspname='market_data'
                AND pg_get_userbyid(nspowner)<>'mm_market_data') THEN
                RAISE EXCEPTION 'Conflicting market schema ownership';
            END IF;
            CREATE SCHEMA IF NOT EXISTS market_data AUTHORIZATION mm_market_data;
            REVOKE ALL ON SCHEMA market_data FROM PUBLIC;
        END $$;
        CREATE EXTENSION IF NOT EXISTS timescaledb;`); err != nil {
		return fmt.Errorf("market ownership setup failed; review operator privileges and existing owners")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("market ownership setup failed")
	}
	if err := MigrateConfigured(ctx, connection); err != nil {
		return err
	}
	return ProvisionRuntimeRole(ctx, connection)
}
