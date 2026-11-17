# ADR-0010: Canonical graph identity, scope and schema versioning

- **Status:** Accepted for the M3.1 schema foundation only
- **Date:** 2026-11-17
- **Boundary:** No M3.2 claim ingestion/resolution, temporal retrieval, GraphRAG or AI.

## Decision

Keep a global **public canonical catalog** of Company, Instrument, Sector and
Industry identities. Tenant-private evidence and assertions use scoped Source,
Claim, MarketEvent and, when needed, Product identities. Private uploaded facts
must never be copied into the global catalog merely because an entity is shared.
Scope formatting is not authorization; future writers/queries must obtain tenant
identity from authenticated service context.

Preserve M2's padded CIK company and share-class FIGI instrument identity formats.
Names/tickers/exchange listings are mutable attributes, not permanent identity.
Missing identifiers must remain unresolved until M3.2 handles reviewed resolution;
do not invent a canonical company from a name or ticker. Stable allocated UUID
identities for non-CIK/non-FIGI entities are reserved for later reviewed extension.

Reified Claim nodes will be the authoritative assertions. Material direct edges
are derived projections tied to claims, not unsourced facts. Claim-to-subject,
object/source and lifecycle links are structural. World valid time uses half-open
intervals; observation time is separate. Unknown bounds must not be silently
invented from retrieval time. See the [schema specification](../data-model/graph-schema.md).

Use additive, ordered, checksum-recorded Neo4j migrations. Version 1 creates
uniqueness constraints and range indexes only, and seeds no business knowledge.
DDL/data markers cannot share a Neo4j transaction: idempotent DDL plus a final
checksum marker permits safe resumption after partial failure. Applied checksum
changes and unknown future versions fail; upgrades roll forward, not reset/drop.
Run migrations explicitly with operator credentials; AI/retrieval may never use
those credentials or invoke schema migration commands.

## Limitations

Neo4j Community uniqueness constraints do not enforce mandatory properties,
labels/direction, temporal intervals, provenance, tenant visibility or truth.
Those are specified now and require writer/query validation in later milestones.
Current indexes are not historical-query enforcement. Vector/full-text retrieval
indexes, entity/claim ingestion, domain claim models, temporal queries and model
integrations are intentionally absent.

Community deployment capabilities must be checked before promising DB-enforced
read-only roles or fine-grained access control. Driver read-access hints are not
an authorization boundary. The Knowledge Graph service owns future graph writes;
other contexts use its bounded APIs/events, never its driver/database directly.

Verification: [through-M3.1 report](../reports/through-m3.1-verification.md).
