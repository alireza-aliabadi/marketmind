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
- Docker Engine and Docker Compose 2.24.4+ (`--wait` support)
- Go 1.25+ for Go modules (pinned/tested toolchain: 1.26.5)
- Node.js 22.12.0+ and pnpm 12.18.0 for the frontend scaffold
- GNU Make (optional aliases/check aggregation; not required to run applications)
- A valid local AIStor `minio.license` file beside `compose.yml`

### Quickstart
```bash
# 1. Clone repository
git clone <repo-url>
cd marketmind

# 2. Configure environment
test -f .env || cp .env.example .env
# Replace placeholders with local credentials before starting infrastructure.

# 3. Install dependencies including dev tools
uv sync --extra dev --frozen
pnpm --dir frontend/web install --frozen-lockfile

# 4. Start local infrastructure services (PostgreSQL, Kafka, Neo4j, Redis, MinIO)
docker compose --profile infra up --detach --wait --wait-timeout 240

# 5. Run tests and verify setup
make check
make gateway-test
```

Applications have native entry points; no Bash launcher or `PYTHONPATH` is required:

```text
uv run marketmind-api --reload
go -C services/api-gateway run ./cmd/server
uv run graph-setup
go -C services/market-data-service run ./cmd/market-data setup
```

The last two commands require explicit operator settings; the gateway requires
its OIDC settings. Make commands below are optional aliases, not application setup
requirements. Runtime users/passwords remain managed separately with least privilege.

### Local Infrastructure

M1.3 provides PostgreSQL 18 with TimescaleDB 2.31.0, single-node Kafka in KRaft
mode, Neo4j, Redis with AOF persistence, and MinIO AIStor. All host ports bind to
localhost, and all five services have health checks and named data volumes.

`compose.yml` contains shared service definitions without published host ports.
`compose.override.yml` supplies development bindings and is loaded automatically
by Docker Compose; the Makefile explicitly loads both files. Isolated tests load
only `compose.yml` and `infra/compose.test.yml`. All files use standard YAML;
empty test port lists do not need custom reset tags because the base is port-free.

Use distinct passwords and URL-safe PostgreSQL credentials, and keep
`DATABASE_URL` aligned with them. For local AIStor, `MINIO_ACCESS_KEY` and
`MINIO_SECRET_KEY` configure both the application and the server's root
credentials. The valid `minio.license` prerequisite is untracked; never commit
credentials, API keys, or licenses.

| Service | Host Application Address | Container Application Address |
| --- | --- | --- |
| PostgreSQL / TimescaleDB | `localhost:5432` | `postgres:5432` |
| Redis | `localhost:6379` | `redis:6379` |
| Kafka | `localhost:9092` | `kafka:29092` |
| Neo4j Bolt | `bolt://localhost:7687` | `bolt://neo4j:7687` |
| AIStor S3 | `localhost:9000` | `aistor:9000` |

Neo4j Browser is available at `http://localhost:7474` and the AIStor console at
`http://localhost:9001`. `make infra-tools` starts optional Kafka UI at
`http://localhost:8081`, leaving port 8080 available for the API gateway.

`make infra-up` validates configuration, starts the `infra` profile, waits for all
five services to become healthy using Compose's existing checks, without a second
startup wrapper. Optional `make infra-check` (`uv run python -m infra check`)
rechecks readiness and authenticated database/object-storage access without
restarting services. Infrastructure verification is native Python, not Bash.

`make infra-test` (`uv run python -m infra test`) uses a unique `marketmind-m13-test-*`
project, example settings, test-only credentials, and no published ports. It writes synthetic fixtures to
all five stores (including a TimescaleDB hypertable), recreates containers without
deleting their volumes, and verifies the data survived. It removes only its own
test containers, network, and volumes on exit, including failed runs. It does not
overwrite `.env` or populate the development databases. Readiness and persistence
unit tests use fake Docker commands. Configuration-merge tests use the Compose CLI
when available, but need neither a running Docker daemon nor a license; they skip
when the CLI is unavailable.

PostgreSQL stores data at `/var/lib/postgresql/18/docker`. The initialization SQL
in `infra/postgres/init/` runs only for an empty database directory; it is not an
upgrade or migration mechanism. Back up and explicitly migrate any existing data
before changing PostgreSQL versions or volume layouts. Changing `.env` passwords
does not rotate credentials in an already-initialized PostgreSQL or Neo4j volume.
`make infra-down` preserves development volumes. Do not run
`docker compose down --volumes` against the development project unless you
deliberately intend to delete its data.

This stack is for local development, not production: Kafka uses plaintext, Redis
has no authentication, and the application uses local AIStor root credentials.
Production deployment requires separate security, backup, and availability work.

### M1.4 Contracts and Ownership

`make check` now includes OpenAPI/event validation and static architecture checks
before the Python/Go suites. `make contracts-check` validates offline references,
examples, schema versions, and registered owners. CI also compares event schemas
and bindings against the pull-request/push base revision, rejecting rewritten
released IDs or silently rebound event types.

`make ownership-test` is an opt-in real PostgreSQL test. It starts only PostgreSQL
in a uniquely named, port-free project, creates transactional synthetic probes,
tests owner access and all cross-owner denials, and deletes only its own resources.
It needs Docker but no AIStor license. These 51 tests are skipped by ordinary
pytest runs so the default suite remains usable without a database daemon.

Fresh PostgreSQL volumes run `020-service-ownership.sql` to reserve empty schemas
with NOLOGIN owner roles. Existing development volumes are deliberately unchanged;
review the bootstrap and run optional `make ownership-init` only when intentionally
reserving all planned service namespaces in that database. The market-data binary
creates its own namespace through native `setup` and does not need this external
bootstrap. Conflicting preexisting owners/elevated roles fail
rather than being silently changed. Runtime LOGIN accounts and per-service secrets
must be provisioned separately; the local administrator is not a business-service
credential. These owner roles do not implement application tenant/RBAC security.

See [contract conventions](contracts/README.md),
[ownership manifest](contracts/service-ownership.json), and ADR-0005/ADR-0006 for
current behavior and remaining limitations. The artifact filing event stays
synthetic/contract-only; M2 raw metadata/time-series events are implemented,
while document acquisition and AI research remain later work.

### M2 Ingestion and M3.1 Schema Boundary

`make market-data-test` uses the compiled CLI and actual Kafka/PostgreSQL/TimescaleDB
through private Testcontainers with synthetic HTTP providers. `make graph-schema-test`
checks actual Neo4j Community constraints/indexes, replay and conflict failure.
Both remove only owned resources, need no live key/AIStor license, and leave
development settings/data untouched. Default pytest skips these Docker scenarios.

Existing databases are never automatically migrated. Explicitly review/export
`MARKET_MIGRATION_DATABASE_URL` for native `market-data setup` and
`GRAPH_SCHEMA_URI/USERNAME/PASSWORD` for `uv run graph-setup`. The Go setup creates
its owned schema/roles and enables installed TimescaleDB before embedded migrations;
Python setup installs missing constraints/indexes without a migration ledger.
Both reject conflicts without overwrite. Market runtime
requires a separately provisioned non-elevated own-role account and rejects the
administrator. Schema-operator credentials must never become AI tool credentials.

FRED terms prohibit retention/AI development absent permission; Massive live/
redistribution/AI rights need review. Synthetic acceptance grants no live rights.
SEC metadata hashes are not filing-document hashes. See ADR-0008/ADR-0009,
[schema v1](docs/data-model/graph-schema.md), [evidence](docs/reports/through-m3.1-verification.md)
and the [code-free AI handoff](docs/requirements/ai-manual-handoff.md). M3.2+ remains
unimplemented; graph writer/time/tenant rules are specifications, not Community
enforcement.

### M1.2 Gateway and Identity Foundation

`make gateway-test` runs the compiled Go gateway and real FastAPI sample with a
disposable loopback OIDC discovery/JWKS issuer. It verifies successful proxying,
API responses, issuer/audience/signature/time checks, key rotation, tenant/role
denial, downstream outage and process shutdown. It does not read `.env`, contact
external providers or require Docker. It removes only its own temporary processes
and artifacts.

For manual operation, start `make api` and separately export `OIDC_ISSUER_URL`,
`OIDC_AUDIENCE`, `GATEWAY_PORT` and other gateway settings before `make gateway`.
The Go process does not load `.env`; it fails closed without a valid configured
issuer/audience. Use an OAuth2 **API access token**, not a browser ID token.
See [gateway setup and claim/route policy](services/api-gateway/README.md).

The identity service defines organizations, external users and organization-scoped
memberships with tested active/inactive and role invariants. It does not yet
implement a database-backed registry or identity CRUD/provisioning APIs. The
gateway trusts only IdP-assigned signed tenant/role/scope claims; forwarded headers
alone are not production service-to-service authentication. See ADR-0007 for the
current trust boundary and revocation limits.

### Frontend and Pre-commit

The React scaffold has pinned dependencies, Oxlint (compatible with native
TypeScript 7), Prettier and a TypeScript/Vite build. `make frontend-check` exercises
all these gates; frontend product pages remain later work. All three Go modules are
included in build/test/format/vet checks and CI.

After installing the prerequisites/dependencies above, run `make pre-commit-check`.
This repository uses the `.yml` configuration name, so install hooks explicitly:

```bash
uv tool run pre-commit install --config .pre-commit-config.yml
```

The hooks understand the independent Go modules and frontend workspace, and check
contracts/architecture. No hook installation is required to run the test suites.

---

## 3. Developer Commands

The `Makefile` provides optional aliases/aggregation for native commands:

- `make infra-config`: Validates Compose and local license-file presence without printing credentials.
- `make infra-up`: Starts Docker infrastructure and waits for built-in health checks.
- `make infra-check`: Rechecks infrastructure readiness without starting or stopping containers.
- `make infra-test`: Verifies clean startup and persistence in an isolated Docker project.
- `make infra-tools`: Starts optional Kafka UI at `http://localhost:8081`.
- `make infra-logs`: Follows the latest infrastructure logs.
- `make infra-down`: Stops Docker infrastructure without deleting named volumes.
- `make contracts-check`: Validates implemented API contracts, versioned events, examples, and owners.
- `make architecture-check`: Enforces Python/Go inward dependencies and service boundaries.
- `make ownership-test`: Runs isolated PostgreSQL permission tests without an AIStor license.
- `make ownership-init`: Explicitly applies reserved ownership namespaces to an existing local database.
- `make api` / `make run`: Launches the FastAPI server with hot-reloading at `http://localhost:8000`.
- `make gateway`: Runs the OIDC gateway with exported issuer/audience settings, defaulting to `127.0.0.1:8080`.
- `make gateway-test`: Tests the real Go → Python runtime flow and OIDC/tenant/RBAC failure paths.
- `make market-data-test`: Verifies real M2.3/M2.4 ingestion/failure/replay in private stores.
- `make graph-schema-test`: Verifies M3.1 on private Neo4j Community.
- `make market-setup` / `make graph-setup`: Native, explicit operator setup; no Bash bootstrap.
- `make market-migrate` / `make market-runtime-init`: Optional lower-level Go operator commands.
- `make frontend-check`: Lints, format-checks, type-checks and builds the React scaffold.
- `make pre-commit-check`: Runs all configured hooks on tracked files with the explicit `.yml` configuration.
- `make test`: Runs Python (`pytest`) and Go test suites.
- `make lint`: Runs Ruff lint/format, strict mypy, Go format/vet and frontend lint/format checks.
- `make format`: Auto-formats code with `ruff`.
- `make check`: Runs all lint/format/type/contract/architecture gates, Python/Go suites, and Go/frontend builds.

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
