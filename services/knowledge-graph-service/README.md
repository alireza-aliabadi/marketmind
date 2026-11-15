# Knowledge Graph Service (planned)

Python bounded context for temporal graph facts, claims and GraphRAG retrieval.
Graph workflows are not implemented. Use the root `src/` platform sample as the
Clean Architecture template with an independent `knowledge_graph_service` import
namespace; never import another service's internals.

Owns graph writes and the reserved `knowledge_graph` PostgreSQL schema /
`mm_knowledge_graph` role. See [service ownership](../../contracts/service-ownership.json).
