# MarketMind Contracts

This directory contains cross-service interface definitions:

- [`events/`](./events/): Kafka event envelope definitions and schema versioning.
- [`openapi/`](./openapi/): OpenAPI 3.1 specifications for external and internal service APIs.
- [`protobuf/`](./protobuf/): Protocol Buffers for high-throughput gRPC communication where needed.

## Contract Conventions

- OpenAPI 3.1 JSON documents cover implemented APIs, with semantic `info.version`
  and `x-marketmind-service` ownership. Local component references only; validation
  must not fetch arbitrary schemas from the network.
- Public gateway process health remains unauthenticated. Three explicit sample
  health paths require verified OIDC/OAuth2 API bearer credentials and `sample:read`.
  Research/market prefixes have method-specific role/action gates but are still
  delegation-only, not implemented business APIs. The gateway contract records
  their policies as extensions rather than claiming nonexistent producer APIs.
- Current wire content types/statuses are documented exactly. Future owned API
  error contracts should use RFC 9457 problem details; prototype legacy responses
  are not silently advertised as that format.
- Request/correlation context crosses service boundaries. Tenant context must come
  from authenticated identity, not an untrusted caller's identity headers.
- Domain/application code uses inward ports and native domain types, not shared
  internal service packages or generated transport DTOs as domain entities.

## Versioned Events

`events/envelope.json` is CloudEvents 1.0 JSON with an independent immutable schema
ID. `events/registry.json` pins each event type to a versioned payload schema,
producer, consumers, delivery semantics, and a synthetic validation example.

`filing.ingested.v1` remains a **contract-only artifact boundary**, not an implemented
document publisher or real filing. The example uses a reserved `.example` source URL and
an illustrative artifact/hash. No provider was contacted or financial claim
verified by this fixture.

Preserve provider identity, source URL, artifact hash, observation time, and known
publication time. Unknown publication time is null; invalid ordering is rejected.
Consumers derive raw-filing replay identity from provider/source ID/artifact hash.
That identity is not an exactly-once delivery or storage guarantee.

M2 implements `market.filings.raw.v1`, `market.economics.raw.v1`,
`market.prices.raw.v1` and `market.ingestion.dlq.v1`. Raw means minimally normalized
snapshots, not extracted claims/document bytes. The immutable CloudEvents envelope
is reused; new closed payloads carry replay keys, native provenance, exact decimals,
observation/vintage/adjustment/delay semantics and source permissions. Acceptance
uses synthetic providers with actual stores, not licensed live-data claims.

The owner atomically queues receipt/outbox, separately relays and idempotently
applies raw records before offset acknowledgement. Failures preserve original
bytes and stage-specific IDs; replay is explicit, validated and audited. DLQ
inspection/replay is an operator responsibility, not an automatic claim/AI
consumer. `make market-data-test` validates actual Kafka/outbox envelopes against
these schemas. See ADR-0008/ADR-0009 and through-M3.1 evidence.

Closed payload schemas and IDs already present in the pull-request base are
immutable. A structural change gets a new major event type and schema file; old
contracts remain available. CloudEvents `specversion` must not stand in for the
payload version. Unknown types/schema IDs and owner mismatches are rejected.

## Executable Gates

```bash
make contracts-check
make architecture-check
make ownership-test
```

Contract checks validate JSON Schema/OpenAPI definitions, examples, version/owner
consistency, and local references. The CI pull-request gate also supplies the base
Git SHA to reject rewritten/removed versioned event schemas. Producer tests check
  actual API responses; `make gateway-test` additionally checks the real Go → Python
  flow and success/authorization/failure contracts. Negative fixtures cover malformed
  evidence and dependency violations. PostgreSQL permission tests need Docker but no
  AIStor license and remove only their own private test project.

[service-ownership.json](./service-ownership.json) and
[ADR-0005](../docs/architect/ADR-0005-service-communication.md) /
[ADR-0006](../docs/architect/ADR-0006-service-data-ownership.md) define responsibility,
communication, migration, and remaining runtime-security limitations.
