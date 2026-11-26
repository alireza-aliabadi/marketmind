# ADR-0005: Explicit API contracts and at-least-once event communication

- **Status:** Accepted as the M1.4 implementation baseline
- **Date:** 2026-11-12

## Context and Problem Statement

MarketMind will combine provider ingestion, deterministic analytics, evidence
extraction, and research workflows. Sharing Python/Go internal types or assuming
exactly-once Kafka delivery would couple independently deployable services and
hide replay, provenance, and failure semantics.

## Decision Drivers

- Reproducible source evidence and distinct publication/observation timestamps.
- Explicit transport contracts and automated producer conformance tests.
- Bounded synchronous work and durable asynchronous workflow boundaries.
- No claim that planned ingestion or AI workflows already exist.

## Considered Options

1. Synchronous service-to-service chains for all work.
2. Kafka for every request, including immediate queries.
3. HTTP/gRPC for bounded queries/commands; Kafka for durable domain facts.

## Decision Outcome

Choose option 3. HTTP is appropriate when the caller needs an immediate bounded
response. gRPC is reserved for demonstrated typed/high-throughput needs, not
added merely to increase the technology count. Kafka communicates completed
domain facts and decouples ingestion, extraction, and later projections.

OpenAPI 3.1 describes published HTTP operations. JSON Schema draft 2020-12 and a
local registry define Kafka payloads; CloudEvents 1.0 is the envelope protocol,
not the payload schema version. Schema references resolve offline. CI validates
examples and producer responses, and rejects changed or removed schema documents
that already had a versioned `$id` in the pull-request base.

The baseline registry pins one immutable payload schema per event type. Because
payload objects are closed, a structural change requires a new major event type
and a new schema identifier/file; old contracts remain available during rollout.
Envelope changes require an explicit registry/protocol migration, not silently
rewriting `specversion`. Documentation-only formatting changes do not alter a
schema's parsed content. API changes use semantic versions and explicit review.

Delivery is at least once. Retries of the same occurrence retain the event ID;
consumers use provider/source ID/artifact hash as the raw-filing deduplication
identity. Storage transactions, outbox publication, replay handling, dead-letter
queues, and real producers are Phase 2 work, not guarantees supplied by a schema.

For new synchronous adapters, target a 2-second connection timeout and a bounded
10-second total request budget. Retry only idempotent operations, at most three
attempts with jitter inside the caller's deadline, honoring provider Retry-After
and access policies. Do not retry a non-idempotent command without a defined
idempotency key. These are adapter conventions; the current generic gateway
proxy does not yet implement a complete downstream deadline/retry policy.

Source URLs/IDs, artifact hashes, observed timestamps, and known publication
timestamps survive the boundary. Missing publication times remain null, not
invented. Extracted/inferred financial claims belong to later typed contracts;
the initial filing event carries artifact metadata, not verified financial facts.

## Consequences and Trade-offs

- Consumers own projections rather than joining another service's tables.
- Schema and producer drift fail tests instead of surprising consumers.
- Event processing requires idempotency and explicit operational recovery.
- Conservative immutable contracts require coordinated version rollouts.
- The prototype still uses HS256 bearer auth; OIDC remains M1.2 work.

## Validation

- [Contracts and conventions](../../contracts/README.md)
- [Event validation and temporal/replay tests](../../tests/test_events.py)
- [CI contract gate](../../.github/workflows/ci.yml)
