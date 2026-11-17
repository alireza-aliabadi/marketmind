# Graph schema v1 — M3.1 boundary

This is a schema specification and additive migration, **not** a graph population,
claim extraction, entity-resolution or retrieval implementation. Required writer
properties below are requirements for M3.2; Community DDL enforces uniqueness only.

## Identity and visibility

All nodes carry a stable `key` combining scope, label and immutable identity.
Public scope is `global`; private scope is `tenant:<authenticated organization>`.
The same private claim/source ID in two organizations has different keys. Scope
does not grant access: future application authorization must enforce visibility.

| Label | Immutable identity | Required writer properties / meaning |
|---|---|---|
| Company | `cik:<10 ASCII digits>` for current US-filer catalog | `key`, `canonical_id`, `scope=global`, name, identifier provenance, observed time; CIK is filer identity, not a listing |
| Instrument | `figi:<provider share-class FIGI>` | `key`, `canonical_id`, `scope=global`, share-class FIGI, observed metadata; ticker/MIC/currency belong to dated listing observations |
| MarketEvent | Stable `event:<allocated UUID>` or reviewed provider-qualified identity | `key`, ID, scope, event type, title, source refs, occurred/published/observed times where known |
| Sector | `sector:<taxonomy-version>:<code>` | `key`, ID, global scope, name, taxonomy/version/code; no silent GICS licensing assumption |
| Industry | `industry:<taxonomy-version>:<code>` | As Sector, with explicit sector classification |
| Product | Stable `product:<allocated UUID>` or reviewed provider ID | `key`, ID, scope, name, identifier/provenance metadata; never merge by display name alone |
| Source | `source:<owner>:<source registry/artifact ID>` plus content version | `key`, ID, scope, provider/external ID, canonical URL, native content hash, artifact/evidence reference, published/retrieved times, rights/attribution |
| Claim | Stable versioned `claim:<allocated UUID>` | `key`, ID, scope, predicate, subject/object refs, source/evidence refs, evidence type, status, valid interval, observed time, method/version, nullable assessment confidence |

Company and Instrument have both key uniqueness and global canonical-ID
uniqueness. Other labels have scoped-key uniqueness. Non-CIK/non-FIGI canonical
allocation is reserved, not implemented. A ticker is never a company ID or an
instrument ID. CIK canonical formatting matches the Go market-data service.

## Relationship vocabulary and direction

The framework-free [domain specification](../../services/knowledge-graph-service/knowledge_graph_service/domain/schema.py)
declares label compatibility. It does not insert relationships.

| Type | Direction | Semantics |
|---|---|---|
| ISSUED | Company → Instrument | Issuer to issued security |
| BELONGS_TO | Company → Industry | Company classification |
| IN_SECTOR | Industry → Sector | Versioned taxonomy classification |
| OPERATES_IN | Company → Industry/Sector | Business operation exposure, not automatically classification |
| SUPPLIES | Supplier Company → Customer Company | Documented supply relationship |
| CUSTOMER_OF | Customer Company → Supplier Company | Inverse semantic view of SUPPLIES; do not count both as independent evidence |
| COMPETES_WITH | Company ↔ Company | Symmetric meaning; one canonical ordered pair per claim |
| PRODUCES | Company → Product | Product production/exposure |
| AFFECTED_BY | Entity → MarketEvent | Entity affected by an event |
| IMPACTS | MarketEvent → Entity | Event impact; inverse semantic view, not automatically inferred truth |
| REPORTED_IN | MarketEvent → Source | Event report provenance |
| ABOUT | Source/MarketEvent → Entity | Sourced relevance association |
| SUBJECT / OBJECT | Claim → Entity | Structural assertion endpoints |
| SUPPORTED_BY | Claim → Source | Structural source/evidence reference |
| SUPERSEDES | New Claim → Old Claim | Version/lifecycle link; retain prior assertion |
| DISPUTES | Counterclaim → Claim | Contradiction/counterevidence link |

Entity means Company, Instrument, Sector, Industry, Product or MarketEvent where
allowed by the typed specification. ADR-0004's `SUPPLIED_BY` sketch describes the
customer→supplier direction; it is not a separate authoritative predicate in v1.
Future ingestion must explicitly normalize that alias, not reverse the meaning.

## Assertion, time and provenance requirements (not yet implemented)

- A reified Claim is authoritative; direct material relationships are derived,
  revocable projections carrying claim key(s), scope, source/evidence refs, valid
  interval, observation time, evidence type and status. No unsourced edge becomes
  an active fact simply because it was created.
- Evidence types: reported fact, calculated metric, inference. Assessments of
  extraction confidence are not calibrated truth probabilities.
- Statuses: active, disputed, superseded, unverified. Preserve lifecycle history
  and counterevidence; deletion is not a substitute for supersession.
- `valid_from <= as_of < valid_to`, with a known open end represented explicitly;
  unknown validity is distinct from unbounded validity. Observation time records
  when the system learned a version. Publication/retrieval/world time are separate.
- A supported claim needs actual evidence location: document/version hash plus
  page/section or byte/text offsets. **M2 hashes SEC submission metadata, not the
  primary filing document.** Metadata URLs/hashes do not prove narrative claims.
- Tenant-private facts/sources cannot be projected globally. Global public
  entities must not reveal private upload content through names or properties.
- Schema indexes improve lookup only. They do not enforce time, provenance,
  status, numeric consistency or tenant authorization.

## Installed DDL and evolution

Migration: [001_graph_schema.cypher](../../services/knowledge-graph-service/knowledge_graph_service/infra/migrations/001_graph_schema.cypher).
Eleven uniqueness constraints (eight label keys, two canonical IDs, one migration
version) and nine explicit range indexes support names/symbols, source identifiers,
claim scope/predicate/status, validity/observation dates and event/source dates.
Vector and full-text indexes are deferred to M3.4.

Apply via `make graph-migrate` only after explicitly exporting schema-operator
settings. Existing `.env`/development databases are not automatically migrated.
The runner records SHA-256 of each ordered migration after DDL/index readiness;
repeat applications add nothing. Partial DDL is additive/resumable. Never edit an
applied version, downgrade an unknown newer version, drop data or reset a graph to
make a migration pass. Breaking changes require a new migration, documented
backfill/compatibility plan and preserved evidence IDs.

Runtime acceptance: `make graph-schema-test` creates a disposable Neo4j
5.26.33 Community instance. Synthetic constraint probes are not a knowledge graph
population or an AI demo. See [verification](../reports/through-m3.1-verification.md).
