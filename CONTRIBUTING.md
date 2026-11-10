# Contributing to MarketMind AI

Thank you for contributing to MarketMind AI. This document outlines development standards, coding conventions, architectural invariants, and submission guidelines.

---

## 1. Architectural Invariants

MarketMind AI adheres strictly to **Domain-Driven Design (DDD)** and **Clean Architecture** (see [ADR-0001](file:///home/alireza/projects/me/ai-projects/marketmind/docs/architect/ADR-0001-architecture-style.md)):

```
Presentation Layer (HTTP, gRPC, Consumers)
       ↓
Application Layer (Use cases, Ports, DTOs)
       ↓
Domain Layer (Entities, Value Objects, Domain Events)
       ↑ (implements ports)
Infrastructure Layer (PostgreSQL, Kafka, Neo4j, Redis, MinIO, External APIs)
```

### Dependency Rules:
1. **Dependencies point inward only:**
   - **Domain:** Pure Python logic. **Must NEVER import** FastAPI, Pydantic, SQLAlchemy, Kafka, Neo4j, LangChain, or LangGraph.
   - **Application:** Defines interface ports and orchestrates use cases. Depends only on Domain.
   - **Presentation & Infrastructure:** Implement adapters and interface with frameworks, drivers, and networks.
2. **Deterministic Financial Computations:**
   - LLMs must **never** be used for numerical calculations. All arithmetic, growth rates, and margins must run through deterministic code in the Quantitative Analytics domain.
3. **Temporal Claims & Provenance:**
   - Every relationship extracted into the knowledge graph must carry source provenance (`source_id`, `url`) and temporal validity (`valid_from`, `valid_to`, `observed_at`).

---

## 2. Local Environment Setup

### Prerequisites
- Python 3.14+ (managed via `uv`)
- Docker Engine & Docker Compose
- GNU Make

### Quickstart
```bash
# 1. Clone repository
git clone <repo-url>
cd marketmind

# 2. Configure environment
cp .env.example .env

# 3. Install dependencies including dev tools
uv sync --extra dev

# 4. Start local infrastructure services (PostgreSQL, Kafka, Neo4j, Redis, MinIO)
make infra-up

# 5. Run tests and verify setup
make check
```

---

## 3. Developer Commands

All common tasks are coordinated through the `Makefile`:

- `make infra-up`: Starts Docker infrastructure in background.
- `make infra-down`: Stops Docker infrastructure.
- `make api` / `make run`: Launches the FastAPI server with hot-reloading at `http://localhost:8000`.
- `make test`: Runs test suite via `pytest`.
- `make lint`: Runs `ruff check` and `mypy` strict type checking.
- `make format`: Auto-formats code with `ruff`.
- `make check`: Runs `format`, `lint`, and `test` in sequence.

---

## 4. Coding & Style Conventions

### Python Guidelines
- **Python Version:** 3.14+
- **Style & Linting:** Enforced by `ruff` (PEP 8, import sorting, bugbear rules). Run `make format && make lint`.
- **Type Annotations:** Strict typing is enforced via `mypy`. All function arguments and return types must be explicitly typed.
- **Error Handling:** Use domain-specific exceptions defined in the domain/application layer; map them to HTTP/gRPC status codes only in the presentation layer.
- **Logging:** Use `structlog` for structured, key-value logging. Always include contextual metadata (`request_id`, `entity_id`).

### Git Commit Conventions
We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

- `feat:` A new feature or capability
- `fix:` A bug fix
- `docs:` Documentation updates or ADR additions
- `refactor:` Code refactoring without behavioral changes
- `test:` Adding or updating tests
- `chore:` Tooling, dependency, or configuration updates

Examples:
- `feat(health): add liveness and readiness endpoints`
- `docs(adr): add ADR-0003 market data provider selection`
- `test(api): add contract tests for health endpoints`

---

## 5. Architectural Decision Records (ADRs)

Whenever a significant architectural decision, provider selection, or schema boundary is proposed:
1. Copy [docs/architect/template.md](file:///home/alireza/projects/me/ai-projects/marketmind/docs/architect/template.md).
2. Number it sequentially (e.g., `ADR-0005-<short-slug>.md`).
3. Fill out the context, decision drivers, options, and consequences.
4. Submit it as part of the relevant pull request.

---

## 6. Definition of Done (DoD)

Before marking a milestone or submitting a pull request, ensure:
1. `make check` passes cleanly (no lint errors, types pass strict mypy, all tests pass).
2. Acceptance criteria from [PROGRESS.md](file:///home/alireza/projects/me/ai-projects/marketmind/PROGRESS.md) are met.
3. No secrets, credentials, or private keys are committed.
4. Clean Architecture dependency rules are preserved.
