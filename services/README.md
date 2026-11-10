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
| [`api-gateway`](./api-gateway) | Go | Routing, authentication proxy, rate limits, SSE proxy | Redis | Scaffolded (M1.1 / M1.2) |
| `identity-service` | Go | Users, tenants, organizations, RBAC | PostgreSQL | Directory created |
| `market-data-service` | Go | Market pricing, SEC EDGAR filings, FRED data ingestion | PostgreSQL + TimescaleDB | Directory created |
| `alert-service` | Go | Real-time threshold and event alerts | PostgreSQL + Redis | Directory created |
| `event-intelligence` | Python | SEC filing narrative parser, corporate event extraction | PostgreSQL + MinIO | Directory created |
| `knowledge-graph-service` | Python | Temporal entity and claim extraction, GraphRAG queries | Neo4j + PostgreSQL | Directory created |
| `ai-orchestrator` | Python | LangGraph research workflows, multi-agent coordination | PostgreSQL + Checkpointer | Directory created |
| `quantitative-service` | Python | Deterministic calculations, financial formulas, metrics | PostgreSQL | Directory created |

## Architecture Invariants

1. **Inward Dependencies Only:** Presentation and Infrastructure depend on Application and Domain. Domain depends on nothing external.
2. **Database Isolation:** Cross-service database queries or writes are forbidden. Services communicate strictly via synchronous REST/gRPC or asynchronous Kafka events.
3. **Deterministic Math:** Financial calculations reside in `quantitative-service` in pure deterministic code, never inside LLM generation prompts.
