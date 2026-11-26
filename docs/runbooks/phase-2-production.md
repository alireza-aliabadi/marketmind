# Phase 2 private-ingestion production runbook

## Release status and boundary

Production hardening is implemented for **future private ingestion on managed
PostgreSQL/TimescaleDB and Kafka**. Live sources are disabled by default. This is
not a signed-off live production deployment, a public market API or an AI feature.
M3.2 and later work remain unimplemented. See ADR-0011 and the verification report.

## Before enabling any workload

1. Obtain actual provider rights/accounts and a real SEC contact. FRED's current
   terms require written permission for retention/this project's use; Massive
   rights must cover the chosen private ingestion/storage. Do not assume a key,
   developer plan or public-domain label grants redistribution/AI permissions.
2. Provision managed PostgreSQL with supported TimescaleDB, TLS certificate
   verification, backups/PITR, encryption-at-rest and separate operator/runtime
   accounts. Use `sslmode=verify-full` and the appropriate trusted CA, never an
   insecure fallback. Do not point tests/migrations at development data by accident.
3. Provision managed Kafka with authenticated TLS and three replicas/minimum ISR
   two per topic. Keep auto-topic creation disabled. Configure the runtime
   principal to write/describe the three raw topics and DLQ, read the raw topics,
   describe their configs, and access only its reviewed consumer group. Use a
   separate operator principal for topic creation/configuration/ACL administration.
4. Review deployment ingress/egress, secrets rotation, retention/revocation,
   capacity, monitoring/on-call and RPO/RTO. Verify them on the actual target.

## Explicit database setup

- Back up first and review the embedded setup/migrations. Existing conflicting
  ownership is a migration decision, not a reset. TimescaleDB must be installed
  and supported by the database server; the Go setup enables its extension.
- Export only the reviewed `MARKET_MIGRATION_DATABASE_URL` or `_FILE` operator
  setting and production mode. Run
  `go -C services/market-data-service run ./cmd/market-data setup`
  (`make market-setup` is optional). The Go binary reserves its own schema/owner,
  applies embedded migrations and provisions the DML-only runtime group; no Bash/
  psql initialization script is needed. This remains an explicit operator command,
  never worker startup or an AI tool. Migration 003 is additive; never rewrite
  versions 001/002. `migrate`/`runtime-init` remain optional lower-level commands.
- Provision a runtime LOGIN via your managed database/secrets system, inheriting
  `mm_market_data_runtime` only, with no owner membership, superuser, BYPASSRLS,
  database/role administration or cross-service schema access. No LOGIN password
  is generated/committed by this repository. Runtime checks enforce these boundaries.
- Store its `verify-full` URL as a mounted secret. The runtime can read migration
  hashes but cannot update them or create/alter/drop business tables.

## Process configuration

Go does not load `.env`. Use explicit process settings or mounted secrets:

| Setting | Meaning |
|---|---|
| MARKET_ENV | `production` for every live workload |
| MARKET_DATABASE_URL / _FILE | Verified-TLS, DML-only runtime connection |
| MARKET_MIGRATION_DATABASE_URL / _FILE | Separate operator; absent from worker pods |
| KAFKA_BOOTSTRAP_SERVERS | Reviewed managed broker host:port list |
| KAFKA_SASL_MECHANISM | `scram-sha-512`, `scram-sha-256`, `plain` over TLS, or `mtls` |
| KAFKA_SASL_USERNAME / PASSWORD / PASSWORD_FILE | Scoped runtime principal; avoid values in logs/build args |
| KAFKA_TLS_CA_FILE | Custom trusted CA when needed; system trust is otherwise used |
| KAFKA_TLS_CERT_FILE / KEY_FILE | Client certificate/key together for mTLS |
| MARKET_OPERATIONAL_ADDRESS | Private health/metrics listener, e.g. pod port 8082 |
| MARKET_PROVIDER_GRANTS_FILE | Operator-reviewed, versioned permission configuration |

Use exactly one secret value or its `_FILE` alternative. File/value ambiguity
fails. No `InsecureSkipVerify` or synthetic endpoint option exists for production.

`infra/market-data/providers.example.json` deliberately enables nothing. An enabled
provider entry needs a permission reference, future UTC expiry, reviewed date,
private-ingestion approval and retention of at least 14 days (the default DLQ
window). FRED additionally needs a reviewed series allowlist. Set credentials only
after review. Configuration is an operator attestation, not legal proof or AI consent.
Restart/redeploy after grant/secret rotation; expired/unapproved source data fails
before enqueue, apply, raw publication or replay.

## Build and supervise

- The ingestion release uses patched Go 1.26.11; run the pinned vulnerability
  checker and record its current result. Pin the release binary/image digest and
  review/sign/scan the runtime image in your release pipeline.
- `services/market-data-service/Dockerfile` is a pinned Linux-amd64 source build.
  `Dockerfile.runtime` packages the same runtime around a verified CI binary. No
  source key, `.env`, grants or license belongs in an image layer/build argument.
- Run separate supervised `worker --role relay` and `worker --role consumer`
  processes, with an explicitly reviewed consumer group and bounded cycle timeout.
  Schedule bounded provider CLI jobs separately, with shared PostgreSQL pacing;
  never enable unlicensed providers or parallelize around fair-access quotas.
- `infra/market-data/production.yaml` is a reviewable starting manifest, not an
  applied deployment. It intentionally has zero replicas, placeholder endpoints/
  release image, no secrets and private operational ingress. Create a separate
  consumer workload with role/group arguments and the same runtime restrictions.
  Add actual managed-endpoint egress policy before release; do not invent IP ranges.
- Use non-root UID/GID 65532, read-only root filesystem, dropped capabilities,
  seccomp, no service-account token and a 45-second termination grace period.

## Operations and incident recovery

- Liveness reports process health; readiness stays unavailable until dependency,
  grants and actual topic durability checks pass. Do not route business data through
  this listener. Operational metrics are aggregate only; restrict monitoring access.
- Watch readiness, parked rows, oldest pending age, worker failures and open provider
  runs. `infra/market-data/alerts.yml` is a starting alert set, not an installed
  monitor/on-call proof. Review alert thresholds against actual workload/SLOs.
- A consumer keeps the exact failed/uncommitted message until handling/commit
  succeeds. On restart Kafka redelivery plus DB deduplication preserves correctness.
- Database/parking failure prevents offset acknowledgement. Bounded relay failure
  preserves original bytes/stage in durable error/DLQ state. No DLQ recursively
  creates more DLQs. Restore service first, inspect the error and current rights,
  then explicitly `replay --id FAILURE_ID`; relay/consume afterward. Never blindly
  replay invalid data or reset consumer offsets/data to make tests pass.
- Provider attempts/failures have a durable run journal and redacted error class.
  Investigate stale running entries after interruption rather than assuming success.
- `retention --retention-days N` is dry-run by default; `--apply` redacts eligible
  completed raw receipts/outbox payloads while keeping deduplication/audit keys and
  protecting pending/unreplayed work. It is **not** automatic deletion of current
  read models, unreplayed DLQs, Kafka, backups or artifacts on license expiry.
  Implement/verify source-specific retention and revocation policy before live use.

## Backup and restore

Use managed encrypted backups/PITR and prove off-cluster restore under your target
RPO/RTO. Do not copy real provider data to unapproved storage. A schema-only dump
is insufficient for Timescale: chunks/catalog must be included in a full database
backup. Before restoring, provision the reserved owner/runtime roles separately,
install the compatible Timescale extension, run `timescaledb_pre_restore`, restore
the full dump and run `timescaledb_post_restore`. Verify schema ownership,
migration hashes, chunk data, exact decimals, dedup/outbox/error records and runtime
privileges before starting workers. Never restore over live/development volumes
without explicit approval and a recovery plan.

The local test performs a full synthetic PostgreSQL/Timescale dump/restore into a
separate private database and checks chunk rows, exact values and owner roles.
That is not an off-cluster managed-backup SLA or real disaster-recovery sign-off.

## Release acceptance checklist

- [ ] Actual provider grant/account/contact and permission expiry/retention approved.
- [ ] Managed database certificate/privileges, secret rotation and encryption tested.
- [ ] Kafka ACL least privilege, 3-replica/2-ISR settings and broker failover proven.
- [ ] Source-specific read-model/DLQ/Kafka/backup retention and revocation proven.
- [ ] Off-cluster restore meets agreed RPO/RTO; ownership/dedup/state are intact.
- [ ] Load/backlog/cost limits and monitoring/on-call thresholds approved/tested.
- [ ] Immutable release image scanned/signed and manifest/egress reviewed.
- [ ] Repeat `make check`, `make market-production-test`, race/contract/security gates.
- [ ] Remote CI and staging/live smoke evidence recorded with no secret/data leakage.

Do not mark a release production-ready solely because local fixtures pass. No
live account purchase, key use, cluster deployment, commit or push was performed.
