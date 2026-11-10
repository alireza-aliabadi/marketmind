# ADR-0004: First vertical-slice demo definition

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** MarketMind Architecture Team

## Context and Problem Statement

To validate the multi-tier microservices, Clean Architecture, temporal knowledge graph, GraphRAG, and LangGraph multi-agent research engine without building broad horizontal services blindly, we must define an explicit, end-to-end vertical slice early.

## Decision Drivers

- **High-Impact Real-World Relevance:** The scenario should reflect a complex, real-world supply chain and regulatory market question.
- **Traceable Ground Truth:** Claims must map to unambiguous public filings and reproducible numbers.
- **Full Architecture Exercise:** The slice must touch every architectural layer: Provider Ingestion -> Kafka Event -> Storage (Postgres/Neo4j) -> GraphRAG -> LangGraph Orchestrator -> Evidence Report.

## Decision Outcome: The Semiconductor Supply Chain & Export Slice

### 1. Target Universe
- **Primary Entity:** NVIDIA Corporation (`NVDA`, CIK: `0001045810`).
- **Related Entities:** Taiwan Semiconductor Manufacturing Co. (`TSMC`, `TSM`), ASML Holding (`ASML`), Microsoft (`MSFT`).

### 2. Research Prompt
> *"Evaluate the potential impact of advanced lithography supply constraints on NVIDIA's data center revenue trajectory, identifying direct suppliers, historical margin sensitivity, and key customer exposure."*

### 3. Pipeline Stages for the Vertical Slice

1. **Ingestion (Market Data Service):**
   - Ingest NVDA FY2024 Form 10-K and relevant Form 8-K filings from SEC EDGAR.
   - Fetch historical quarterly gross margins and revenue from SEC XBRL company facts.
   - Publish `filing.ingested.v1` and `company_metrics.updated.v1` events to Kafka.

2. **Knowledge Graph & Claims (Knowledge Graph Service):**
   - Extract supplier/customer relationship claims from Item 1 ("Business") and Item 1A ("Risk Factors"):
     - `(NVDA)-[:SUPPLIED_BY {component: "Advanced Packaging / CoWoS", valid_from: "2023-01-01", source_id: "sec:nvda:10k:2024"}]->(TSM)`
     - `(TSM)-[:SUPPLIED_BY {equipment: "EUV Lithography", source_id: "sec:tsm:20f:2024"}]->(ASML)`
   - Neo4j indexes enforce temporal validity filtering (`valid_from` <= as_of_date < `valid_to`).

3. **Deterministic Financial Analytics (Quantitative Analytics Service):**
   - Calculate gross margin progression and YoY data center growth using deterministic Python code (`pandas` / numpy arithmetic).
   - Zero LLM generation of financial calculations.

4. **Agentic Research Graph (AI Orchestrator):**
   - LangGraph supervisor initiates 3 coordinated steps:
     1. Entity & relationship retrieval via Neo4j GraphRAG.
     2. Quantitative analytics invocation via typed tool interface.
     3. Evidence verification: Cross-reference conclusions against source document byte offsets.

5. **Dossier Output:**
   - Synthesizes an executive research briefing.
   - Every claim is annotated with an evidence footnote (SEC accession number, filing date, URL).
   - Graph traversal path rendered in Cytoscape-compatible JSON.

### Acceptance Criteria

- Research run finishes end-to-end in < 45 seconds on local environment.
- 100% of numerical calculations match SEC XBRL figures without rounding error.
- All relationships returned cite an ingested document ID with valid temporal range.
- Trace available in Langfuse/OTel with request, research, and span correlation IDs.
