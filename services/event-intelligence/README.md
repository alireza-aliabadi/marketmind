# Event Intelligence Service (planned)

Python bounded context for source normalization, extraction and provenance.
Business extraction is not implemented. Use the root `src/` platform sample as
the Clean Architecture template, but use the independent `event_intelligence`
import namespace; do not import the sample or another service's internals.

Owns the reserved `event_intelligence` PostgreSQL schema / `mm_event_intelligence`
role. See [service ownership](../../contracts/service-ownership.json) for planned
object storage and event boundaries.
