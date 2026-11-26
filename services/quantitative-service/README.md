# Quantitative Analytics Service (planned)

Python bounded context for deterministic financial calculations and analytics.
Calculations are not implemented. Use the root `src/` platform sample as the Clean
Architecture template with an independent `quantitative_service` import namespace;
never import another service's internals or use LLM output as numerical truth.

Owns the reserved `quantitative` PostgreSQL schema / `mm_quantitative` role.
See [service ownership](../../contracts/service-ownership.json) for planned stores.
