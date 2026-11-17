# Phase 2 production-hardening verification

Date: 2026-11-17. Target: future private ingestion on managed infrastructure.
User has no live provider access yet. **Hardening is verified locally; live
production release is not signed off.** M3.2 and AI remain unimplemented.

## Implemented

- Production rejects plaintext/fallback SQL, unauthenticated Kafka and fixtures.
  Mounted-secret handling rejects ambiguous/oversized inputs without logging values.
- PostgreSQL verify-full TLS, non-owner DML group, DDL/elevation/migration-update
  denial, additive migration 003 and installed version/checksum verification.
- Authenticated verified Kafka TLS with 3-replica/2-ISR durability/retention checks.
- Shared provider gates, durable run/failure journal, supervised bounded workers,
  private health/aggregate metrics and graceful SIGTERM.
- Exact-message retention across handling/commit failure, reproduced with a real
  broker/database outage regression before fixing the old skip risk.
- Trusted source policy rechecks before enqueue/apply/raw relay/replay; sources
  disabled by default, AI reuse denied. Completed-payload dry-run/redaction
  retains deduplication tombstones and protects unfinished work.
- Patched Go 1.26.11, pinned build/runtime images, non-root/read-only execution,
  disabled deployment template, private API contract, alerts/runbook and CI gates.

## Executed verification

`make market-production-test` passed **5 database/Kafka/worker scenarios, 2 secure
transport scenarios and 1 image scenario**, all using owned synthetic resources:

- M2 regressions, failed-message retry, DML privileges/shared quotas/run journal,
  and compiled worker readiness/metrics/SIGTERM passed.
- Full PostgreSQL/Timescale dump/restore to a separate private database preserved
  chunk rows, exact decimals and owner roles. Schema-only dumps are insufficient.
- Actual TLS/SASL Kafka produce/read rejected bad credentials/untrusted CA/weak
  replication. Actual verify-full PostgreSQL migration/runtime rejected untrusted
  CA/downgrade and used the DML-only role.
- Packaged binary ran as UID/GID 65532, read-only, no network; unapproved live
  sources failed before access without logging their secret.
- Final database-control repeat passed positive retention dry-run/apply and
  retained deduplication after redaction; dry-run did not mutate payloads.

`make check` passed Ruff, strict mypy, architecture, **3 OpenAPI/5 event contracts**,
136 Python tests (62 opt-in checks skipped by default), all Go modules and frontend
build. Phase 2 race tests and `git diff --check` passed.

Pinned `govulncheck@v1.9.0` found 17 reachable standard-library issues on the old
Go 1.26.5 patch. Go 1.26.11 scan reported **No vulnerabilities found**.
The existing Starlette/httpx warning remains.

## Preservation and remaining release gates

Pre-hardening files: `/tmp/opencode/marketmind-phase2-pre-production-qz7g9ytd`.
No user's changes, development credentials/licenses/data were discarded/migrated.
No live key/request, purchase, license acceptance, cluster deployment, commit or
push was performed; remote CI is unobserved.

The image test packages a verified binary into the pinned runtime. It does not
claim a deployed image signature or managed-environment sign-off. Redaction is
not universal license-expiry deletion of read models, DLQs, Kafka or backups.

Live release still needs real rights/accounts/contact, managed ACL/failover and
deployment, source-specific retention/revocation, off-cluster RPO/RTO, capacity,
monitoring/on-call, reviewed egress and remote CI/staging/live acceptance.
See [release runbook/checklist](../runbooks/phase-2-production.md).
