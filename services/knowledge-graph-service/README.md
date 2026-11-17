# Knowledge Graph Service — M3.1 schema foundation

The distinct `knowledge_graph_service` namespace contains pure canonical CIK/FIGI
formatting, typed labels/direction rules and an additive Neo4j schema migrator.
No claim model/ingestion, entity-resolution workflow, temporal query API, GraphRAG,
embedding or AI is implemented. Migration seeds no business knowledge.

Owns graph writes and the reserved `knowledge_graph` PostgreSQL schema /
`mm_knowledge_graph` role. See [service ownership](../../contracts/service-ownership.json).

`make graph-migrate` requires explicit `GRAPH_SCHEMA_URI/USERNAME/PASSWORD`
operator settings. It does not load `.env`, default to development data, provision
users, reset a graph or grant AI driver access. Ordered checksums and reserved-name
definition checks reject rewritten/conflicting migrations without overwrite.

`make graph-schema-test` verifies actual constraints/indexes, additive replay and
conflict failure in a private Neo4j 5.26.33 Community container. Testcontainers
removes its private anonymous volumes; development data is never mounted.
Community uniqueness does not enforce mandatory properties, time, provenance,
relationship directions, tenant visibility or truth. Those remain later writer/
query requirements, not claimed behavior.

See [schema v1](../../docs/data-model/graph-schema.md),
[ADR-0010](../../docs/architect/ADR-0010-graph-schema-and-scope.md),
[evidence](../../docs/reports/through-m3.1-verification.md), and the
[code-free AI handoff](../../docs/requirements/ai-manual-handoff.md).
