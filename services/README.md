# MarketMind Services

This directory contains the independently deployable domain microservices that comprise the MarketMind AI platform.

Each service follows **Clean Architecture** internally:
```
cmd/ or src/
  ├── presentation/   # HTTP handlers, consumers, serializers
  ├── application/    # Use cases, ports, DTOs
  ├── domain/         # Entities, value objects, domain events (pure logic)
  └── infrastructure/ # DB adapters, external clients, Kafka producers/consumers
```

## Service Catalog

| Service | Language | Role / Bounded Context | Primary Store | Status |
|---|---|---|---|---|
| [`api-gateway`](./api-gateway) | Go | Routing and OIDC/RBAC foundation; rate limits/SSE planned | Redis (planned) | Implemented Phase 1 platform API |
| [`identity-service`](./identity-service) | Go | Users, organizations and scoped memberships | PostgreSQL (reserved) | Tested domain models; API/persistence planned |
| `market-data-service` | Go | Market pricing, SEC EDGAR filings, FRED data ingestion | PostgreSQL + TimescaleDB | Planned; directory README added |
| `alert-service` | Go | Real-time threshold and event alerts | PostgreSQL + Redis | Planned; directory README added |
| `event-intelligence` | Python | SEC filing narrative parser, corporate event extraction | PostgreSQL + MinIO | Planned; directory README added |
| `knowledge-graph-service` | Python | Temporal entity and claim extraction, GraphRAG queries | Neo4j + PostgreSQL | Planned; directory README added |
| `ai-orchestrator` | Python | LangGraph research workflows, multi-agent coordination | PostgreSQL + Checkpointer | Planned; directory README added |
| `quantitative-service` | Python | Deterministic calculations, financial formulas, metrics | PostgreSQL | Planned; directory README added |

## Architecture Invariants

1. **Inward Dependencies Only:** Presentation and Infrastructure depend on Application and Domain. Domain depends on nothing external.
2. **Database Isolation:** Cross-service database queries or writes are forbidden. Services communicate strictly via synchronous REST/gRPC or asynchronous Kafka events.
3. **Deterministic Math:** Financial calculations reside in `quantitative-service` in pure deterministic code, never inside LLM generation prompts.

## Ownership and Enforcement

The authoritative [machine-readable manifest](../contracts/service-ownership.json)
declares each service root/import namespace, PostgreSQL schema/NOLOGIN owner role,
and planned Redis, S3, Neo4j, and topic responsibilities. The table above describes
intended bounded contexts, not completed business implementations.

| Service | Reserved PostgreSQL Schema | NOLOGIN Owner Role |
| --- | --- | --- |
| identity-service | `identity` | `mm_identity` |
| market-data-service | `market_data` | `mm_market_data` |
| alert-service | `alerts` | `mm_alerts` |
| event-intelligence | `event_intelligence` | `mm_event_intelligence` |
| knowledge-graph-service | `knowledge_graph` | `mm_knowledge_graph` |
| ai-orchestrator | `ai_orchestrator` | `mm_ai_orchestrator` |
| quantitative-service | `quantitative` | `mm_quantitative` |

The gateway and the legacy Python platform sample have no business PostgreSQL
ownership. New Python services use distinct import namespaces from the manifest,
not another service's internal `src` package. Domains/applications use inward
ports and do not import frameworks, I/O adapters, or another service's internals.
The existing sample's two dependency-composition files are explicitly scoped;
they are not a general exception for business code.

`make architecture-check` verifies ownership consistency and static Python/Go
dependency boundaries. `make ownership-test` starts only PostgreSQL in an isolated
port-free project and verifies owner access and cross-owner denials. It needs no
AIStor license and leaves development data untouched.

The bootstrap reserves empty schemas and owner roles, not runtime LOGIN accounts
or credentials. Existing databases require explicit reviewed `make ownership-init`;
entrypoint scripts affect only empty data directories. Runtime users must inherit
only their own owner role and must not use the bootstrap administrator. Planned
object-store reads are explicit interfaces (for example, extraction of permitted
raw filing artifacts), not permission to query the producer's database.

See [ADR-0005](../docs/architect/ADR-0005-service-communication.md) and
[ADR-0006](../docs/architect/ADR-0006-service-data-ownership.md) for communication,
replay, namespace isolation, and remaining runtime-security limitations.
