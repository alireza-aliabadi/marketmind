# Knowledge Graph Service — M3.1 schema foundation

The distinct `knowledge_graph_service` namespace contains pure canonical CIK/FIGI
formatting, a shared relationship vocabulary and idempotent Neo4j schema setup.
No claim model/ingestion, entity-resolution workflow, temporal query API, GraphRAG,
embedding or AI is implemented. Setup seeds no business knowledge.

Owns graph writes and the reserved `knowledge_graph` PostgreSQL schema /
`mm_knowledge_graph` role. See [service ownership](../../contracts/service-ownership.json).

From the repository root, install with `uv sync`, then run `uv run graph-setup`
or `uv run python -m knowledge_graph_service`. No Bash script or `PYTHONPATH` is
needed. `make graph-setup` is optional; the old `make graph-migrate` command remains
an alias for it.

Setup requires explicit `GRAPH_SCHEMA_URI/USERNAME/PASSWORD` operator settings;
`--database` defaults to `neo4j`. It does not load `.env`, default to development
credentials, provision users, reset a graph or grant AI driver access. The Python
`ensure_schema(driver)` function creates missing constraints/indexes with
`IF NOT EXISTS`, checks reserved-name definitions, and waits for index readiness.
It creates no migration/version/checksum nodes and reads no external Cypher files.
Old migration records/constraints are left untouched; conflicts require deliberate
operator resolution rather than overwrite. Ordinary node creation does not require
schema setup; these ten business uniqueness constraints and nine indexes protect
identifiers and lookup performance.

`make graph-schema-test` verifies the native CLI, actual constraints/indexes,
idempotent replay, conflict failure and legacy-record preservation in a private
Neo4j 5.26.33 Community container. Testcontainers removes its private anonymous
volumes; development data is never mounted.
Community uniqueness does not enforce mandatory properties, time, provenance,
relationship directions, tenant visibility or truth. Those remain later writer/
query requirements, not claimed behavior.

See [schema v1](../../docs/data-model/graph-schema.md),
[ADR-0010](../../docs/architect/ADR-0010-graph-schema-and-scope.md),
[native setup decision](../../docs/architect/ADR-0012-native-setup.md),
[evidence](../../docs/reports/through-m3.1-verification.md), and the
[code-free AI handoff](../../docs/requirements/ai-manual-handoff.md).
