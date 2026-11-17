package postgres

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSecurePostgresRuntime(t *testing.T) {
	if os.Getenv("RUN_MARKET_SECURE_TESTS") != "1" {
		t.Skip("opt-in TLS PostgreSQL")
	}
	ctx, stop := context.WithTimeout(context.Background(), 45*time.Second)
	defer stop()
	dsn := os.Getenv("MARKET_SECURE_POSTGRES")
	if err := MigrateConfigured(ctx, Config{URL: dsn, Production: true}); err != nil {
		t.Fatal("verified TLS migration failed", err)
	}
	if err := ProvisionRuntimeRole(ctx, Config{URL: dsn, Production: true}); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `CREATE ROLE private_ingestor LOGIN PASSWORD 'synthetic-secret' IN ROLE mm_market_data_runtime`); err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(dsn)
	parsed.User = url.UserPassword("private_ingestor", "synthetic-secret")
	runtime, err := NewConfigured(ctx, Config{URL: parsed.String(), Production: true})
	if err != nil {
		t.Fatal("DML-only TLS runtime failed", err)
	}
	defer runtime.Close()
	if err := runtime.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Del("sslrootcert")
	parsed.RawQuery = query.Encode()
	if untrusted, err := NewConfigured(ctx, Config{URL: parsed.String(), Production: true}); err == nil {
		untrusted.Close()
		t.Fatal("untrusted database certificate accepted")
	}
	query.Set("sslmode", "prefer")
	parsed.RawQuery = query.Encode()
	if untrusted, err := NewConfigured(ctx, Config{URL: parsed.String(), Production: true}); err == nil {
		untrusted.Close()
		t.Fatal("TLS downgrade permitted")
	}
	t.Log("actual verify-full TLS migration/runtime, DML role, untrusted CA and downgrade denial passed")
}
