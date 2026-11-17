package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate is an explicit operator operation. Runtime New never migrates or
// provisions credentials, and existing schemas/data are not reset.
func Migrate(ctx context.Context, operatorDSN string) error {
	return MigrateConfigured(ctx, Config{URL: operatorDSN})
}

func MigrateConfigured(ctx context.Context, connection Config) error {
	if err := validateConnection(connection); err != nil {
		return err
	}
	operatorDSN := connection.URL
	if operatorDSN == "" {
		return fmt.Errorf("an explicit migration operator connection is required")
	}
	pool, err := pgxpool.New(ctx, operatorDSN)
	if err != nil {
		return fmt.Errorf("invalid migration connection configuration")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migration database unavailable")
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'; SET LOCAL statement_timeout = '60s';
        SELECT pg_advisory_xact_lock(713241);
        SET LOCAL ROLE mm_market_data;
        CREATE TABLE IF NOT EXISTS market_data.schema_migrations (
            version TEXT PRIMARY KEY, sha256 TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
        )`); err != nil {
		return fmt.Errorf("migration ownership/bootstrap prerequisite failed: %w", err)
	}
	var owner string
	if err := tx.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE schemaname='market_data' AND tablename='schema_migrations'`).Scan(&owner); err != nil || owner != "mm_market_data" {
		return fmt.Errorf("existing migration table has conflicting ownership")
	}
	entries, _ := migrations.ReadDir("migrations")
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		body, _ := migrations.ReadFile("migrations/" + entry.Name())
		hash := sha256.Sum256(body)
		checksum := hex.EncodeToString(hash[:])
		var existing string
		err := tx.QueryRow(ctx, `SELECT sha256 FROM market_data.schema_migrations WHERE version=$1`, entry.Name()).Scan(&existing)
		if err == nil {
			if existing != checksum {
				return fmt.Errorf("applied migration %s was modified; add a new migration", entry.Name())
			}
			continue
		}
		if !isNoRows(err) {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s failed: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO market_data.schema_migrations (version,sha256) VALUES ($1,$2)`, entry.Name(), checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func verifyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("cannot read compiled migration registry")
	}
	rows, err := pool.Query(ctx, `SELECT version,sha256 FROM market_data.schema_migrations`)
	if err != nil {
		return fmt.Errorf("explicit market migrations required before startup")
	}
	defer rows.Close()
	installed := map[string]string{}
	for rows.Next() {
		var version, checksum string
		if rows.Scan(&version, &checksum) != nil {
			return fmt.Errorf("invalid migration registry")
		}
		installed[version] = checksum
	}
	if rows.Err() != nil || len(installed) != len(entries) {
		return fmt.Errorf("missing/unknown market migration versions")
	}
	for _, entry := range entries {
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		if installed[entry.Name()] != hex.EncodeToString(hash[:]) {
			return fmt.Errorf("market migration checksum mismatch; do not modify applied versions")
		}
	}
	return nil
}
