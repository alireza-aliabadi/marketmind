# Verification through M3.1

Date: 2026-11-17. Scope stops before M3.2. Local evidence only; no commit,
push, remote CI run, provider subscription or live financial-data acceptance.

This report preserves the original verification history. The 2026-11-19 native
setup changes supersede its graph migration bookkeeping and shell-tooling details;
see [ADR-0012](../architect/ADR-0012-native-setup.md) and the latest
[engineering log](../../PROGRESS.md#engineering-log) for current verified behavior.

## Preservation and baseline

The initial working tree contained only an untracked market-data Go module and
three unfinished Go files. Their original bytes were backed up under
`/tmp/opencode/marketmind-user-start-6hql3cgz` before editing. The existing CIK,
company/filing/provenance models, provider/use-case names and SEC adapter structure
were continued in place. No credentials, development volumes or license changed.
This checkout has no `.agent/` state documents; `PROGRESS.md` is its task tracker.

The initial `go test ./...` in `services/market-data-service` failed on incomplete
syntax and misspelled imports. Completing those files and their adapter fixed the
causal errors, rather than hiding/excluding the unfinished service.

## M2.1 — SEC provider boundary

Command: `go test -race -count=1 ./...` in `services/market-data-service`.
Result: all application/domain/SEC packages passed; SEC runtime tests took ~6 s.

- Real HTTP adapter requests to disposable loopback servers, using clearly
  synthetic fixtures; no external provider, secret or existing database used.
- CIK padding and invalid identifiers; provider-port delegation and nil failure.
- Company metadata, accession IDs, canonical source/document URLs, SHA-256 of
  retrieved metadata bytes, UTC normalization and unknown publication timestamps.
- Recent-only coverage and bounded historical retrieval, accession deduplication,
  deterministic ordering and no partial-success claim after a history failure.
- Malformed/ragged arrays, mismatched CIKs, traversal paths and future timestamps.
- 400/403/404/429/503 responses; bounded retry count, long Retry-After, cancellation,
  User-Agent validation, restricted fixture endpoints and concurrent fair access.

Regression command: `make check`. Result: Ruff lint/format, strict mypy,
contracts/architecture, 134 Python tests (51 opt-in DB tests skipped), the existing
Go modules and frontend build all passed. One existing Starlette/httpx warning.
The market-data module is now included in the normal Go gates as well.

## M2.2 — Economic and price adapters

Command: `go test -race -count=1 ./...` in `services/market-data-service`.
Result: application/domain/SEC/FRED/Massive packages passed. Additional commands:
`make architecture-check contracts-check` passed.

- Actual adapter HTTP requests against synthetic loopback endpoints, including
  pagination and non-success responses; no credentials or provider content used.
- FRED native units, notices, vintage/real-time dates, exact decimal strings,
  negative economic values and missing `.` values represented as null, not zero.
- FRED rejects stalled/incorrect pagination, malformed observations and
  unreviewed series. Live configuration requires permission covering this use.
- Massive daily OHLCV, UTC instants preserving ET aggregation boundaries, exact
  prices and fractional volume, share-class FIGI identity, CIK, MIC and currency.
- Explicit EOD/delayed/realtime plan semantics; split adjustment is not total
  return. Endpoint identity checks reject ranges whose ticker changes instrument.
- Invalid OHLC/volume/adjustment, duplicate bars, future dates, foreign pagination
  URLs and embedded API keys are rejected/removed. Credentials stay in transport.

The terms at `https://fred.stlouisfed.org/legal/` were reviewed on 2026-11-17;
their API prohibitions explicitly cover retention and AI development without
permission. Massive plan documentation and individual-use terms were reviewed.
ADR-0008 records the actual restrictions. Live accounts, grants, entitlements and
live-data acceptance remain unverified; synthetic records are labeled as such.

## M2.3 — Kafka delivery, failures and replay

Command: `make market-data-test` (scenario `TestKafkaDeliveryMilestone`).
Final result: Go race-enabled scenario passed in ~7.2 s; both M2 scenarios passed
as two pytest integration checks in ~55 s, including private-store startup/cleanup.

Environment: Testcontainers 4.15.0, Apache Kafka 4.4.0, TimescaleDB 2.31.0 /
PostgreSQL 18, ephemeral loopback bindings, test-only LOGIN role/credentials and
synthetic HTTP metadata. No development service, provider key or AIStor license.

- Actual versioned raw topics and CloudEvents envelopes; producer/consumer codec
  validates schema/owner/UTC/correlation/identity, permissions and payload shape.
- Duplicate re-fetch produces one receipt/outbox. Native/normalized content changes
  change identity; retrieval-only timestamp changes do not.
- A real closed loopback broker fails bounded publication; original bytes survive
  in error/DLQ state. After recovery, actual Kafka DLQ delivery and explicit audited
  replay publish the original raw message successfully.
- A real PostgreSQL trigger injects three processing failures. Each transaction
  rolls back company/filing writes; durable parking allows the offset commit.
  Transport and processing histories for identical bytes remain separate.
- Removing only that private test trigger, replaying and consuming persists one
  filing. Further duplicate Kafka delivery does not duplicate it. Invalid original
  bytes cannot be blindly replayed. Unit tests also cover unavailable parking,
  key mismatch, malformed messages, cancelled execution and bounded retry policies.
- Runtime envelopes read from Kafka/outbox are validated against the actual JSON
  schemas; this is producer conformance, not only hand-written example validation.

Diagnostics: the first fixture run used invalid Docker tmpfs size syntax; fixed
the option, not the production stack. A later test incorrectly counted a pending
DLQ as pending raw data after a real connection failure. Corrected the expectation
to explicit failed-publication replay. Distinct failure-stage identities prevent
subsequent processing failure from erasing transport history. Repeats passed.

## M2.4 — Persistence, quality and isolation

Command: `make market-data-test` (scenario `TestPersistenceMilestone`).
Final result: real Go race-enabled scenario passed in ~6.6 s.

- Applied both additive, owner-scoped migrations twice; repeat is idempotent.
  Modified applied checksum is rejected rather than silently accepted.
- A real outbox insertion trigger fails enqueue; its receipt does not commit.
- The actual compiled CLI fetches SEC metadata and atomically enqueues it. Real
  FRED/price adapters, outbox relay, Kafka and consumer complete ingestion.
- Stored one company, two filing metadata rows, three economic observations and
  four price bars across separate split-adjusted/unadjusted series.
- Actual Timescale price hypertable/index presence; exact NUMERIC values
  `100.123456789012345678` and `100.12345678`; missing economic value stays null.
- Newer correction followed by older observation preserves newer company data.
  Concurrent locked pending dispatch processes distinct pending rows.
- Bounded inspect/status return source IDs/URLs/hashes, rights, observation/check
  freshness, quality and replay metadata without raw error bytes or arbitrary SQL.
- Runtime administrator credentials are rejected; actual runtime query of an
  identity-owned probe fails with SQLSTATE 42501. No cross-service data ownership
  or tenant/production IAM equivalence is claimed.

## M3.1 — Graph schema only

Command: `make graph-schema-test`. Result: five checks passed; final repeat ~23 s.
Environment: Neo4j 5.26.33 Community / driver 6.4.1, private Testcontainers
instance, ephemeral loopback Bolt and image-owned anonymous volumes removed on exit.

- CIK/FIGI and global/private scoped-key formatting and typed relationship rules.
- Eleven actual uniqueness constraints and nine explicit indexes; queried index
  readiness is ONLINE, and duplicate scoped/canonical identities fail in Neo4j.
- First additive migration records its checksum; repeat adds nothing and retains
  unrelated synthetic probe data. Rewriting the applied file fails.
- A deliberately conflicting reserved constraint name is not overwritten; explicit
  conflict failure is tested, then only that private test constraint is restored.
- Initial 512 MiB test tmpfs could not fit Neo4j transaction-log preallocation;
  actual debug logs reported ENOSPC/read-only startup. Image-owned disposable
  volumes fixed the fixture without modifying any development volume/config.

Required field/direction/time/tenant rules are schema specifications, **not**
Community-enforced business validation. No claim model/ingestion, resolution,
temporal queries, vector/full-text retrieval, GraphRAG or model/agent code exists.
Synthetic constraint probes do not claim a populated business knowledge graph.

## Final regression and remaining boundaries

- `make check`: Ruff lint/format, strict mypy over 21 sources, architecture,
  2 OpenAPI / 5 event contracts, 136 Python tests, all three Go modules and frontend
  build passed. Ordinary pytest skipped 56 opt-in store checks; actual relevant
  stores were tested separately as described above.
- `make gateway-test ownership-test`: actual Go→FastAPI/OIDC rotation and
  authorization/failure regressions passed; all 51 isolated SQL ownership tests
  passed. The existing Starlette/httpx warning remains.
- `make test-go GO_TEST_FLAGS='-race -count=1'`: all three modules passed again
  after final validation changes. `go mod verify` passed in the market module;
  `uv sync --extra dev --frozen` passed without changing the lock.
- `uv run python -m contracts.validation --base-ref HEAD` passed, retaining the
  previously released envelope/artifact schema and event bindings unchanged.
- Configured pre-commit hooks passed on all 80 changed/new files (including YAML,
  JSON, Ruff, Go formatting/vet, contracts and architecture). Frontend hooks had
  no changed files; its actual lint/format/build already passed `make check`.
  `git diff --check` passed. The final Docker listing found no remaining private
  `marketmind-m2*` or `marketmind-m31*` test containers.
- New dependencies are frozen in `uv.lock` and Go module sums. This environment
  blocked Go proxy ZIP downloads for compression dependencies; official GitHub
  tag archives were repacked in `/tmp/opencode` and successfully verified by Go
  against `sum.golang.org`. No checksum/TLS verification or persistent Go proxy
  configuration was disabled.
- CI now includes the verified ingestion/graph scenarios; configuring that job
  is not evidence of a remote workflow execution. No commit/push was performed.

Live SEC/FRED/Massive financial-data acceptance, live grants/accounts/entitlements,
remote CI and production IAM/retention/scheduling remain unverified. Raw filing
documents/S3 artifacts are not implemented. No existing `.env`, license,
development data or original user work was discarded. The requested stop is M3.1;
[manual AI handoff](../requirements/ai-manual-handoff.md) is specifications only.
