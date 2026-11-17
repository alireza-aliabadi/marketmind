# ADR-0011: Private production ingestion profile and explicit release gates

- **Status:** Accepted for implementation; live production release remains blocked
- **Date:** 2026-11-17
- **Scope:** Phase 2 only, private ingestion on managed infrastructure; no M3.2/AI.

## Context and decision

The user requested production readiness for future use and confirmed there are
currently no live provider accounts/permissions. Local/synthetic acceptance is
not a production release. Implement fail-closed production controls now, keep
providers disabled, and list external release evidence rather than invent it.

- Explicit `MARKET_ENV=production` requires PostgreSQL `sslmode=verify-full` and
  authenticated TLS Kafka (SCRAM, PLAIN over TLS, or mTLS); no plaintext/verification
  downgrade or synthetic provider endpoint is allowed.
- Secret values can come from mounted `_FILE` settings; ambiguous value/file
  configuration and oversized files fail without echoing values. `.env` is never
  implicitly loaded by the Go process or packaged into an image.
- Keep schema owner `mm_market_data` NOLOGIN. An explicit operator provisions
  `mm_market_data_runtime` with owned-schema DML and read-only migration metadata.
  Production LOGINs inherit this group, not the owner; elevation/DDL are rejected.
- Add migration 003, retaining M2 migration checksums. Runtime verifies all expected
  versions/hashes and rejects missing, unknown or modified installed versions.
- PostgreSQL-owned request leases serialize actual provider request starts across
  processes and retain cooldown after cancellation. Journal provider attempts and
  visible failure classes, without source text/keys in logs or operational metrics.
- Supervised relay/consumer roles have bounded cycle deadlines/backoff, private
  liveness/readiness and aggregate metrics, plus graceful SIGTERM shutdown.
  Readiness checks actual dependencies, grants and Kafka durability metadata.
- Production topics require at least three replicas/two in-sync replicas; runtime
  verifies existing settings rather than silently overwriting them. Managed ACLs,
  failover/SLA and external network policies still require deployment evidence.
- Retain the exact Kafka message across handler/offset-commit failures. Fetching
  the next message first could commit past failed work; this was reproduced and
  fixed with a real broker/database outage regression.
- Trusted operator grant configuration bounds private retention/review/expiry;
  all providers are disabled in the example. Recheck policies before enqueue,
  apply, raw relay and replay. AI reuse remains denied; no actual license is issued
  by setting a reference/boolean. Legal review remains an external gate.
- Completed raw payloads can be dry-run/redacted while preserving deduplication,
  audit and pending/unreplayed work. This is not a universal license-expiry deletion
  policy for derived read models, backups, Kafka or DLQs; review those before live use.
- Use Go 1.26.11 for the ingestion release. The old 1.26.5 standard library had
  reachable vulnerabilities. Pin build/runtime images and verify the packaged
  binary as non-root/read-only; do not mistake a config manifest for deployment.

## Verification and remaining acceptance

Tests cover secure Kafka/SQL transport, bad credentials/CA/downgrade denial,
DML-only privileges, shared quotas, consumer retry integrity, actual worker health/
shutdown, full Timescale backup/restore and an isolated runtime image. See
[verification](../reports/phase-2-production-hardening.md) and
[operator/release runbook](../runbooks/phase-2-production.md).

No paid key, account purchase, license acceptance, development/production data
change, real cluster deployment or commit/push is authorized/performed. Release
requires actual live grants, managed endpoints/ACL/failover, retention/revocation,
off-cluster restore/RPO/RTO, workload capacity and remote CI evidence.
