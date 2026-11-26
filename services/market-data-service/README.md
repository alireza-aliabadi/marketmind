# Market Data Service — M2 ingestion

## Future production profile

Production hardening is implemented and locally verified, with live sources
disabled until you have actual grants/access. Use `MARKET_ENV=production`,
verified PostgreSQL TLS, authenticated Kafka TLS, mounted `_FILE` secrets and
`MARKET_PROVIDER_GRANTS_FILE`. Production LOGINs use the DML-only
`mm_market_data_runtime` group, **not** the owner-group local compatibility
described below. Run the explicit operator `setup` command before workers.

New operations: `worker --role relay/consumer` for supervised private workers,
`check` for dependencies/durability, and `retention --retention-days N` for a dry
run (only explicit `--apply` redacts eligible completed payloads). The ingestion
release uses patched Go 1.26.11. `make market-production-test` verifies transport,
privileges/quotas/retry, worker lifecycle, full Timescale restore and non-root image.

See [verification](../../docs/reports/phase-2-production-hardening.md) and
[release runbook](../../docs/runbooks/phase-2-production.md). Managed deployment,
ACL/failover, provider rights and source-specific retention/revocation still need
release evidence; this is not an already deployed production service. No M3.2/AI
was added, and the operational HTTP listener is not a public business API.

Go SEC submissions/filing metadata, FRED series and Massive (formerly Polygon)
daily-price adapters. Existing user models/port/SEC work were continued in place.
The local CLI composes inward ports with Kafka and owned PostgreSQL/TimescaleDB.

## Boundary

Implemented: bounded provider history/pagination/retries, native IDs/hashes and
UTC timestamps, exact decimals, FRED vintages/missing data, CIK/FIGI/MIC/currency,
delay/split semantics, atomic receipt/outbox, separate relay/normalizer, duplicate
and out-of-order protection, DLQ/replay audit, checksum migrations, source/quality/
error/freshness records and bounded owned queries.

Not implemented: filing-document download/S3 artifact publication, claims, graph
enrichment, business HTTP API or AI. `filing.ingested.v1` is artifact-only and is
not emitted for submissions metadata.

## Configuration

Go does **not** load `.env`; export reviewed values without printing/committing
secrets. Root `.env.example` lists settings.

- SEC requires an application/contact User-Agent. The example `.local` contact is
  a placeholder, not a valid live operational contact.
- FRED requires a key, actual written permission and reviewed-series allowlist;
  current terms prohibit retention/AI development absent permission.
- Massive requires a key, documented Stocks plan and reviewed permission. Initial
  Developer semantics are 15-minute delayed, not a purchased entitlement or
  public redistribution/AI grant. No silent Yahoo fallback exists.
- `MARKET_DATABASE_URL`: a separately provisioned, non-elevated member of
  `mm_market_data`, with no other service-schema access. Never use the administrator
  or Python's `postgresql+psycopg` URL as a runtime fallback.
- `MARKET_MIGRATION_DATABASE_URL`: explicit operator-only migration connection.
- `KAFKA_BOOTSTRAP_SERVERS`: reviewed local addresses; production ACL/SASL/TLS are
  not implemented by the plaintext development stack.

## Operations

From this directory, use `go run ./cmd/market-data` followed by:

| Command | Required/optional arguments |
|---|---|
| `sec` | `--cik`, optional bounded `--history` |
| `fred` | `--series`, `--start`, `--end`, optional pinned `--vintage` |
| `prices` | `--ticker`, `--start`, `--end`, optional `--adjusted` (splits, not dividends) |
| provider command + `--enqueue` | Atomically queue raw snapshot/outbox; default is fetch/output only |
| `setup` | Operator-only: creates its owned schema/NOLOGIN owner, enables installed TimescaleDB, applies embedded migrations and grants the DML runtime group |
| `migrate` / `runtime-init` | Optional lower-level operator commands; not required separately when using `setup` |
| `topics` | Explicit three raw topics/DLQ creation; auto-topic creation stays disabled |
| `relay --limit N` | Dispatch pending outbox with bounded retry |
| `consume --group NAME --limit N` | Apply or durably park before offset acknowledgement |
| `status` | Bounded counts, source provenance/freshness and error/replay metadata |
| `inspect --kind KIND --id ID` | Owned company/filing/economic/prices lookup, at most 100 rows |
| `replay --id FAILURE_ID` | Validated, audited original-byte replay; then relay/consume |

Commands default to a 90-second deadline, cap `--timeout` at ten minutes and
message limits at 10,000. Inspect is not a complete paginated public API. Recent
SEC history is explicitly incomplete; oversized snapshots (2 MiB) fail without
truncation. `--fixture-endpoint` is loopback-only, labels synthetic rights and is
never evidence of real provider/financial data.

From the repository root, `go -C services/market-data-service run ./cmd/market-data setup`
is the same native command; `make market-setup` is only a convenience alias.
No Bash/psql bootstrap script or migration files beside the compiled binary are
needed. `setup` creates only its own service schema/roles plus the TimescaleDB
extension, rejects conflicting/elevated ownership and never resets data.
SQL migration history stays
because it protects evolving persisted tables, not just optional graph indexes.
Runtime LOGINs/passwords remain managed explicit secrets; setup does not invent
credentials or give worker accounts schema ownership. Kafka topic provisioning
stays a separate native `topics` operation with operator permissions.

## Verification

`make check` includes this module. `make market-data-test` executes the compiled
CLI and actual PostgreSQL/TimescaleDB/Kafka workflows through Testcontainers. Only
private synthetic containers/data are removed; development settings/volumes remain
unchanged. Existing databases require explicit reviewed migration.

See [permissions/semantics](../../docs/architect/ADR-0008-provider-permissions-and-price-semantics.md),
[delivery ADR](../../docs/architect/ADR-0009-durable-snapshot-ingestion.md),
[ownership](../../contracts/service-ownership.json), [contracts](../../contracts/README.md),
and [evidence](../../docs/reports/through-m3.1-verification.md).
