# ADR-0009: Durable snapshots, outbox and idempotent normalization

- **Status:** Accepted for local M2.3/M2.4 ingestion
- **Date:** 2026-11-17
- **Scope:** No filing-document extraction, graph ingestion or AI workflows.

## Decision

Adapters normalize provider transport into native snapshots, retaining provider
IDs, canonical query/source URLs, exact decimals, observation times, native
content hashes and reviewed rights. Versioned `market.*.raw.v1` topics carry
minimally normalized snapshots, not claims or document bytes. The artifact
`filing.ingested.v1` remains contract-only; metadata is not an S3 filing artifact.

Explicit provider CLI enqueue atomically stores a receipt and raw outbox in
`market_data`. A separate relay locks pending rows with `SKIP LOCKED`, uses
bounded retry and all-broker acknowledgements. Consumers atomically apply
records/source/quality/deduplication state before acknowledging Kafka offsets.
Invalid/exhausted processing is acknowledged only after durable error/DLQ parking;
database/parking failure prevents acknowledgement. This is at-least-once, not
distributed exactly-once delivery.

Replay identity binds normalized content and native source identity/hash/query/
permission, excluding retrieval timestamps. Identical re-fetches update check
metadata without duplicate outbox/records. Corrections change identity; current
read models only accept newer source-observation versions. Retain raw receipts
and source versions, not only the current row.

Failures retain original bytes, stage/code, attempts, date and explicit replay
audit. IDs include stage so repeated identical stage failures deduplicate without
erasing a later processing failure after transport replay. Failed DLQs do not
recursively create more DLQs; durable failed/pending outbox/error rows remain.

Replay validates original envelope/key and requeues only the owned raw outbox.
Already processed records remain deduplicated. Invalid bytes require producer
correction and a new record, not blind replay. Raw snapshots cap at 2 MiB and
transport/DLQ at 4 MiB; oversized history fails rather than silently truncating.

## Persistence and operation boundaries

Additive checksum SQL migrations run as `mm_market_data` through an explicit
operator connection. Runtime never migrates, provisions credentials or falls
back to an administrator. Startup rejects elevated accounts and access to other
reserved schemas. Existing development databases are not reset/initialized.

NUMERIC values retain precision; missing FRED values remain null. Prices separate
instrument/provider/MIC/currency/adjustment and delay semantics in a Timescale
hypertable. Dated listings, source freshness/rights and errors are queryable through
bounded owned CLI operations. Status/errors describe queued/applied/parked
messages; provider failures before enqueue are surfaced by the CLI, not recorded
as successful persisted runs.

This is a local CLI, not a public business API or production Kafka security stack.
Plaintext Kafka, retention review, owner-level runtime DDL, scheduling and IAM need
separate hardening. AI uses future authenticated owned APIs/ports, never these
administrative commands or cross-service database/driver access.

## Verification

Unit/codec/provider/race tests and isolated Testcontainers exercise real Kafka,
PostgreSQL/TimescaleDB, original-byte DLQs, stage failures, replay, atomic rollback,
duplicates, exact decimals, corrections, freshness and cross-service denial.
See [evidence](../reports/through-m3.1-verification.md).
