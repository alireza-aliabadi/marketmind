# AI Research Orchestrator (planned)

Python bounded context for research runs, agent coordination and report assembly.
AI workflows are not implemented. Use the root `src/` platform sample as the Clean
Architecture template with an independent `ai_orchestrator` import namespace.
Graph and market operations go through their owners' APIs/events, not their tables
or internal packages.

Owns the reserved `ai_orchestrator` PostgreSQL schema / `mm_ai_orchestrator` role.
See [service ownership](../../contracts/service-ownership.json).
