# Alerts Service (planned)

Go bounded context for watchlists, alert rules and notification delivery.
Business APIs are not implemented. Use the Go gateway as the Clean Architecture
template, with an independent module `marketmind/services/alert-service`.

Owns the reserved `alerts` PostgreSQL schema / `mm_alerts` role.
See [service ownership](../../contracts/service-ownership.json) for planned stores
and integration boundaries. Never import another service's internals.
