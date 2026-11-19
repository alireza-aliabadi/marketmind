# ADR-0012: Native setup without shell-launcher requirements

- **Status:** Accepted
- **Date:** 2026-11-19
- **Scope:** Simplify existing setup/run workflows; no new ingestion, retrieval or AI.

## Context

The user requested less setup ceremony, no extra Bash scripts for Python/Go
requirements, and preservation of security and speed/cost controls. The Neo4j
schema was duplicated between Python definitions and a versioned Cypher file,
with migration nodes/checksums despite having only additive constraints/indexes.
The Python service was not installed as a package. Infrastructure readiness was
checked twice even though Compose already has health checks and `--wait`.

## Decision

- Install the Python packages through the existing project/dependency manager.
  Provide `marketmind-api` and `graph-setup`, plus native `python -m` entry points;
  no `PYTHONPATH` or Bash launcher is required. Make is an optional convenience.
- Use `ensure_schema(driver)` with one reviewed Python definition set. Keep all
  ten business uniqueness constraints, nine explicit range indexes, reserved-name
  conflict checks, bounded index readiness and idempotent `IF NOT EXISTS` DDL.
  Remove the Cypher-file/version/checksum runner and its bookkeeping constraint
  from required definitions.
  Do not delete old bookkeeping records, constraints or unrelated data in a DB.
- Keep graph setup explicit and operator-only. It does not ingest business nodes,
  change permissions, read `.env`, overwrite conflicts or transform existing data.
- Add one native Go `market-data setup` command for its own NOLOGIN owner/schema,
  TimescaleDB extension, embedded SQL migrations and DML-only runtime group.
  Do not require the external all-service SQL bootstrap for that binary. Keep
  transactional SQL migration hashes, separate operator/runtime credentials,
  TLS verification, source grants, quotas, retries and worker limits unchanged.
  Managed LOGIN/password provisioning and Kafka ACL/topic administration remain
  explicit responsibilities; no automatic credential generation or elevation.
- Remove the two project Bash infrastructure-verification scripts. Use Compose
  health checks/`--wait` for startup and native `python -m infra check/test` for
  optional deep checks. Preserve authenticated probes, private test projects,
  persistence verification and cleanup limited to newly allocated resources.
- Package Python images with frozen dependencies, cached dependency installation
  separate from source, compiled bytecode and the existing non-root runtime.
  Whitelist only runtime inputs in the build context; never send secrets/caches.

## Consequences

Node creation never needed migration history. Constraints/indexes still provide
identity correctness and lookup performance. Setup is safe to retry but is not
an automatic destructive migration or all-or-nothing Neo4j transaction. Breaking
graph changes need an explicit data/backfill plan and operator review. Normal
workers must not receive operator privileges to avoid a separate setup command.

Verification and limitations are recorded in [PROGRESS.md](../../PROGRESS.md).
