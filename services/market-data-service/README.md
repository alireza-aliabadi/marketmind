# Market Data Service (planned)

Go bounded context for provider adapters and normalized market observations.
Business ingestion starts in Phase 2; this directory is not a running service.
Use the Go gateway as the Clean Architecture template, with an independent module
`marketmind/services/market-data-service`.

Owns the reserved `market_data` PostgreSQL schema / `mm_market_data` role.
See [service ownership](../../contracts/service-ownership.json) for planned stores,
topics and API/event boundaries. Never import another service's internals.
