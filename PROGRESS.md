# MarketMind AI — Project Progress

> **Tracking rule:** Update this file when work is actually implemented and verified. The initial status below is a plan, not a claim that code already exists.

## Project status

- **Project:** MarketMind AI
- **Objective:** Advanced AI Engineer portfolio project
- **Architecture:** Domain-Driven Microservices + Clean Architecture + Event-Driven Architecture
- **Target plan:** 12 weeks
- **Current phase:** Phase 1: Architecture and platform foundation
- **Overall status:** M1.1–M1.4 implemented and locally verified; remote CI exit criterion remains pending
- **Last updated:** 2026-11-14

### Status legend

- `[ ]` Not started
- `[-]` In progress
- `[x]` Implemented and verified
- `[!]` Blocked / needs decision

Do not mark a task `[x]` until its acceptance criteria have been tested. Add a PR, commit, test report, or demo link for completed milestones.

---

## Architecture decisions to preserve

- [x] **ADR-001:** Adopt DDD bounded contexts and independently deployable microservices.
- [x] **ADR-002:** Apply Clean/Hexagonal Architecture inside services; dependencies point inward.
- [x] **ADR-003:** Use Kafka for asynchronous integration; use HTTP/gRPC for suitable synchronous request-response operations. Recorded in ADR-0005.
- [x] **ADR-004:** Enforce service data ownership; no cross-service database writes. Recorded in ADR-0006 and permission tests.
- [x] **ADR-005:** Use PostgreSQL for transactional records, TimescaleDB for time-series, Neo4j for graph facts, Redis for ephemeral coordination, and S3-compatible object storage for permitted raw artifacts. M1.3 and ADR-0006.
- [ ] **ADR-006:** Use LangChain for model/tool integrations and LangGraph for stateful research workflows.
- [ ] **ADR-007:** Use Langfuse for LLM observability and LangSmith for workflow debugging/evaluation, with OpenTelemetry for cross-service telemetry.
- [ ] **ADR-008:** Represent sourced/inferred relationships as temporal claims with provenance.
- [ ] **ADR-009:** Keep deterministic financial calculations outside LLM reasoning.
- [ ] **ADR-010:** No autonomous trade execution; reports are research and decision support.

---

## Phase 0 — Planning and repository bootstrap

**Goal:** Establish repository conventions and validate the development environment.

- [x] Create Git repository `marketmind-ai`.
- [x] Add README.md and PROGRESS.md.
- [x] Add root `.gitignore`.
- [x] Add `.env.example` with placeholders only.
- [x] Add Makefile with planned developer commands.
- [x] Add contribution and coding conventions.
- [x] Add ADR template and architecture decision directory.
- [x] Select and pin supported runtime versions.
- [x] Decide initial market-data provider and verify its licensing/coverage.
- [x] Define the first vertical-slice demo.

**Exit criteria**
- [x] Clean checkout has documented setup instructions.
- [x] No secrets are committed.
- [x] Runtime and dependency versions are pinned.
- [x] First vertical slice is scoped.

---

## Phase 1 — Architecture and platform foundation (Weeks 1–2)

### M1.1 — Monorepo and service templates

Evidence: [Phase 1 verification report](docs/reports/phase-1-verification.md).

- [x] Create service directory structure.
- [x] Add Go service template.
- [x] Add Python service template.
- [x] Add React + TypeScript frontend scaffold.
- [x] Configure formatters, linters, and pre-commit checks.
- [x] Add CI build and unit-test workflows.

### M1.2 — API gateway and identity

Evidence: [Phase 1 verification report](docs/reports/phase-1-verification.md) and
[ADR-0007](docs/architect/ADR-0007-oidc-identity-foundation.md).

- [x] Implement Go API gateway.
- [x] Add health/readiness endpoints.
- [x] Add request ID and correlation ID middleware.
- [x] Add OIDC/OAuth2 integration. Configured-issuer discovery, RS256 JWKS, API audience/time/tenant checks and key rotation; no HS256 runtime fallback.
- [x] Define organization and user model. Identity-service domain models include organization-scoped memberships and active/inactive invariants; persistence/CRUD remain later work.
- [x] Implement role/permission checks. Enforced on registered sample, market and research proxy routes with method-specific action policies.
- [x] Add tenant-isolation tests.

### M1.3 — Local infrastructure

- [x] Add Docker Compose for PostgreSQL.
- [x] Add TimescaleDB extension configuration.
- [x] Add Neo4j.
- [x] Add Kafka in KRaft mode.
- [x] Add Redis.
- [x] Add S3-compatible object storage.
- [x] Add persistent volumes and health checks.
- [x] Add startup/readiness verification.

### M1.4 — Contracts and architecture governance

- [x] Add OpenAPI contract conventions.
- [x] Add Kafka event envelope and schema versioning.
- [x] Define service ownership matrix.
- [x] Document synchronous vs asynchronous communication rules.
- [x] Add ADRs for architecture decisions.
- [x] Verify no cross-service database access. Static boundaries and isolated PostgreSQL owner-role tests; runtime app credentials remain planned.

**Phase 1 exit criteria**
- [x] Local infrastructure starts successfully from a clean checkout.
- [x] Gateway reaches a sample service. Real compiled Go → FastAPI health flow passes `make gateway-test`.
- [x] Authentication and tenant authorization have automated tests. OIDC/JWKS, registered-route permissions, missing/invalid claims, header spoofing and cross-organization denials are covered.
- [-] Event contracts validate in CI. Gates and negative/base-revision checks pass locally; observing remote CI requires committing/pushing these changes.

Local acceptance evidence: [Phase 1 verification report](docs/reports/phase-1-verification.md).
Phase 1 is implemented locally, not yet fully signed off against the remote-CI
exit criterion. This is not a claim of identity CRUD, ingestion or production IAM.

---

## Phase 2 — Market data and ingestion (Weeks 3–4)

### M2.1 — Provider abstraction and SEC integration

- [ ] Define provider port/interface.
- [ ] Implement Go provider adapter structure.
- [ ] Integrate SEC EDGAR submissions.
- [ ] Retrieve company identifiers and filing metadata.
- [ ] Preserve provider IDs, source URLs, and timestamps.
- [ ] Respect SEC access requirements and User-Agent policy.

### M2.2 — Economic and market data

- [ ] Integrate FRED API.
- [ ] Select and integrate a market-price provider.
- [ ] Document data coverage and licensing.
- [ ] Normalize currency, exchange, instrument, and timestamps.
- [ ] Model delayed vs real-time data explicitly.
- [ ] Model corporate-action and adjusted-price semantics.

### M2.3 — Kafka ingestion

- [ ] Create versioned raw topics.
- [ ] Implement ingestion event envelope.
- [ ] Add idempotency keys.
- [ ] Add retries with bounded backoff.
- [ ] Add dead-letter topic and replay procedure.
- [ ] Implement outbox pattern where database publication requires atomicity.
- [ ] Test duplicate and out-of-order events.

### M2.4 — Persistence and data quality

- [ ] Add PostgreSQL migrations.
- [ ] Add TimescaleDB hypertable and indexes.
- [ ] Add data source registry.
- [ ] Add validation and freshness metadata.
- [ ] Add ingestion status and error records.
- [ ] Add integration tests with Testcontainers.

**Phase 2 exit criteria**
- [ ] At least one company/filing pipeline runs end to end.
- [ ] At least one economic or market time-series pipeline runs end to end.
- [ ] Duplicate replay is idempotent.
- [ ] Source provenance and freshness are queryable.
- [ ] Provider licensing assumptions are documented.

---

## Phase 3 — Temporal knowledge graph and GraphRAG (Weeks 5–6)

### M3.1 — Graph schema

- [ ] Define canonical Company and Instrument identities.
- [ ] Define MarketEvent, Sector, Industry, Product, Source, and Claim nodes.
- [ ] Define typed relationships and direction semantics.
- [ ] Add Neo4j uniqueness constraints and indexes.
- [ ] Document graph schema and migration/versioning strategy.

### M3.2 — Claim and relationship ingestion

- [ ] Build source-backed claim model.
- [ ] Implement entity resolution.
- [ ] Extract initial company relationships.
- [ ] Store source ID and evidence type for each claim.
- [ ] Store valid-time and observed-time fields.
- [ ] Support disputed, superseded, and unverified claims.

### M3.3 — Temporal graph queries

- [ ] Implement bounded relationship traversal.
- [ ] Enforce valid-time filtering on every traversed edge.
- [ ] Add as-of query support.
- [ ] Add query depth, timeout, and result limits.
- [ ] Add tenant-aware authorization where graph data is tenant-scoped.
- [ ] Test historical relationship correctness.

### M3.4 — GraphRAG retrieval

- [ ] Integrate maintained `neo4j-graphrag` package.
- [ ] Build vector and full-text indexes.
- [ ] Implement hybrid retrieval.
- [ ] Implement vector + Cypher graph expansion.
- [ ] Return evidence IDs and graph paths.
- [ ] Add retrieval ranking and deduplication.
- [ ] Add retrieval quality test fixtures.

**Phase 3 exit criteria**
- [ ] Graph contains canonical company entities.
- [ ] Material relationships carry provenance and temporal validity.
- [ ] Multi-hop retrieval returns bounded, explainable paths.
- [ ] Historical query tests exclude relationships not valid at the requested date.
- [ ] Retrieval returns evidence references suitable for report citations.

---

## Phase 4 — Agentic research engine (Weeks 7–8)

### M4.1 — AI integration boundary

- [ ] Define model-provider abstraction.
- [ ] Define typed tool contracts.
- [ ] Define structured output schemas.
- [ ] Keep LangChain/LangGraph types out of domain entities.
- [ ] Add model timeout, retry, and rate-limit policies.
- [ ] Add prompt versioning conventions.

### M4.2 — LangGraph workflow

- [ ] Implement research state schema.
- [ ] Implement supervisor/planner node.
- [ ] Implement entity/intent resolution node.
- [ ] Add persistent checkpointer.
- [ ] Add workflow version and terminal states.
- [ ] Add cancellation and resume behavior.
- [ ] Add bounded retry and idempotent tool execution.

### M4.3 — Specialized agents

- [ ] Implement Market Intelligence Agent.
- [ ] Implement Graph Research Agent.
- [ ] Implement Quantitative Analysis Agent.
- [ ] Implement Fundamental Analysis Agent.
- [ ] Implement Sentiment/Event Agent.
- [ ] Implement Risk/Counterevidence Agent.
- [ ] Implement Evidence Verification Agent.

### M4.4 — Report and evidence pipeline

- [ ] Implement evidence aggregator.
- [ ] Implement source and timestamp validation.
- [ ] Implement deterministic numerical validation.
- [ ] Implement report schema and versioning.
- [ ] Link each material conclusion to evidence or labeled inference.
- [ ] Add failure/recovery integration tests.

**Phase 4 exit criteria**
- [ ] One research question completes the end-to-end graph.
- [ ] Workflow can resume after worker interruption.
- [ ] Quantitative results are generated by deterministic tools.
- [ ] Unsupported or unverified claims are flagged.
- [ ] Report contains citations, assumptions, and uncertainty.

---

## Phase 5 — Observability and evaluation (Weeks 9–10)

### M5.1 — Langfuse

- [ ] Integrate current Langfuse SDK/OTel-based tracing.
- [ ] Capture model calls and tool spans.
- [ ] Track token usage and cost where provider metadata allows.
- [ ] Version prompts.
- [ ] Add feedback and quality scores.
- [ ] Configure redaction and retention.

### M5.2 — LangSmith

- [ ] Enable LangGraph tracing.
- [ ] Create evaluation datasets.
- [ ] Add experiment runs.
- [ ] Evaluate intermediate agent outputs.
- [ ] Add regression comparison workflow.

### M5.3 — Benchmark and evaluators

- [ ] Create manually reviewed market research benchmark.
- [ ] Add entity extraction evaluator.
- [ ] Add relationship extraction evaluator.
- [ ] Add Recall@K / MRR retrieval evaluator.
- [ ] Add evidence coverage and citation precision evaluator.
- [ ] Add temporal validity evaluator.
- [ ] Add numerical correctness fixtures.
- [ ] Add unsupported-claim evaluator.

### M5.4 — Ablation and operational metrics

- [ ] Compare vector RAG vs hybrid RAG.
- [ ] Compare hybrid RAG vs graph-enhanced RAG.
- [ ] Compare graph retrieval vs full multi-agent GraphRAG.
- [ ] Record latency and cost per experiment.
- [ ] Add OpenTelemetry collector.
- [ ] Add Prometheus/Grafana service dashboards.
- [ ] Correlate request, research, thread, and trace IDs.

**Phase 5 exit criteria**
- [ ] A research run can be traced across services and AI tools.
- [ ] Evaluation datasets and results are versioned.
- [ ] Ablation results include quality, latency, and cost.
- [ ] Sensitive values are redacted from traces.
- [ ] Regressions can be detected in CI or a repeatable evaluation job.

---

## Phase 6 — Frontend, deployment, and portfolio release (Weeks 11–12)

### M6.1 — Frontend foundation

- [ ] Build application shell and navigation.
- [ ] Build Market Overview.
- [ ] Build Company Explorer.
- [ ] Build Research Workspace.
- [ ] Add SSE progress updates.
- [ ] Add accessible loading/error/empty states.

### M6.2 — Evidence and graph experience

- [ ] Build interactive graph explorer with Cytoscape.js.
- [ ] Build evidence panel with source metadata.
- [ ] Build report view with citations.
- [ ] Show assumptions, uncertainty, and data freshness.
- [ ] Add graph path highlighting.

### M6.3 — End-to-end and security

- [ ] Add Playwright research journey.
- [ ] Add tenant isolation E2E tests.
- [ ] Add API and graph query limits.
- [ ] Add dependency and container scanning.
- [ ] Add load test for API and SSE.
- [ ] Test provider, Kafka, database, and LLM failure paths.

### M6.4 — Deployment and portfolio presentation

- [ ] Verify Docker Compose quickstart.
- [ ] Add Kubernetes manifests or Helm charts.
- [ ] Add secrets and configuration guidance.
- [ ] Add backup/restore runbook.
- [ ] Add CI/CD deployment workflow.
- [ ] Add architecture diagrams and ADRs.
- [ ] Add benchmark/ablation report.
- [ ] Record product demo.
- [ ] Update README with verified commands and screenshots.

**Phase 6 exit criteria**
- [ ] User can submit a research question and receive a report.
- [ ] User can inspect evidence and graph paths.
- [ ] Principal journey passes E2E.
- [ ] Deployment and recovery instructions are tested.
- [ ] Portfolio claims are backed by reproducible evidence.

---

## Quality gates

| Area | Initial target / gate |
|---|---|
| Entity extraction | F1 >= 0.90 on reviewed benchmark |
| Relationship extraction | F1 >= 0.85 on reviewed benchmark |
| Retrieval | Recall@10 >= 0.85 on benchmark |
| Citation precision | >= 0.95 on reviewed benchmark |
| Numerical calculations | Exact match against deterministic fixtures |
| Temporal graph | No known invalid-time edges in historical test set |
| Security | No cross-tenant access in authorization tests |
| Reliability | Duplicate event replay is idempotent |
| AI quality | Unsupported claims measured and driven toward zero |
| Cost/latency | Reported per workflow and compared across experiments |

Targets are engineering goals, not achieved metrics. Record actual results, dataset version, model version, prompt version, and run date.

---

## Current blockers / decisions

- [!] Observe a successful remote Phase 1 CI run against these changes. No commit/push was requested or performed.
- [x] Choose initial licensed market-data provider and plan (Resolved in ADR-0003: SEC EDGAR + FRED + Polygon/yfinance adapter).
- [ ] Choose initial LLM provider/model and embedding model.
- [ ] Decide whether graph data is global, tenant-specific, or a combination.
- [ ] Decide initial news provider and permitted content retention.
- [ ] Confirm deployment target and budget.
- [ ] Define benchmark universe and manually reviewed ground truth.

---

## Engineering log

Append entries newest-first.

### 2026-11-14 - Phase 1 gap closure and acceptance verification

- Audited the previous status against code: HS256-only authentication, missing
  organization/membership models, unenforced proxy permissions and absent sample
  integration were genuine M1.2 gaps. Frontend lint also failed because its ESLint
  command had no installed/configured linter, and frontend CI was absent.
- Added configured-issuer OIDC discovery and rotating RS256 JWKS verification,
  API audience/issuer/signature/time/subject/tenant checks, OAuth2 action scopes,
  bounded provider calls and fail-closed configuration/startup. Removed the
  executable's default JWT secret; the legacy HS256 adapter is test-only.
- Added identity-service organization/user/membership domain models, with external
  identity keyed by issuer/subject, per-organization roles and inactive/mismatched
  membership denials. No database registry or provisioning API is claimed.
- Applied action/method guards to all registered proxy routes. Added explicit
  sample-health routing, prefix stripping, trusted identity/header replacement,
  generated tracing propagation, credential stripping and JSON failure responses.
  Updated the gateway OpenAPI contract and added ADR-0007/setup documentation.
- Added scoped READMEs so previously empty planned service directories survive
  checkouts when committed.
  Added both Go modules to build/test/format/vet/race gates, pinned current OIDC/Go
  dependencies, and repaired frontend lint/format/build with pinned Oxlint and
  Prettier. CI now includes real gateway integration and frontend jobs.
- Verified pre-commit hooks, including strict YAML checks plus safe multi-document
  parsing specifically for pnpm 12's lockfile. Normalized baseline formatting/end
  newlines without changing editor settings or unrelated behavior. Diagnosed and
  fixed test-only Uvicorn shutdown expectations and issuer-startup publication race;
  repeated race-enabled runtime scenarios subsequently passed.
- Validation: `make check` passed Ruff lint/format, strict mypy, architecture and
  contract gates, 134 Python tests (51 opt-in DB tests skipped), both Go modules and
  frontend builds. Both Go modules passed race tests; `make gateway-test` and two
  race-enabled repeats passed real routing, OIDC rotation, 200/401/403/405/502 wire
  contracts, tenant/RBAC denials, downstream outage and shutdown/startup failure.
- Validation: all 51 actual PostgreSQL owner/schema checks passed separately, and
  fresh isolated startup/recreation/persistence passed for all five stores. Event
  immutability checks against `HEAD`, Go module checksums, frozen frontend install
  and frontend checks with the pinned Node 22.12.0 runtime passed. The existing
  Starlette/httpx deprecation warning remains. See the linked verification report.
- Repeated the frozen Python install/tests and frontend install/lint/format/build
  in fresh disposable dependency environments; all passed without replacing the
  development environments.
- Preserved existing uncommitted work, development data, credentials and license.
  No commit, push or remote workflow was performed; the CI exit flag stays pending.

### 2026-11-12 - M1.3 verification and M1.4 contract governance

- Fresh M1.3 readiness and isolated recreation/persistence checks passed for all
  five stores, including a TimescaleDB hypertable, Kafka message, and S3 artifact.
- Added self-contained OpenAPI 3.1 contracts for implemented Go/Python health APIs,
  producer-response conformance tests, and response/path/status drift checks.
  Future business APIs and OIDC are not falsely described as implemented.
- Added versioned CloudEvents/payload schemas, a contract-only filing event,
  synthetic fixtures, owner/version matching, offline-reference checks, temporal
  validation, and base-revision immutability checks for schemas/event bindings.
- Added a machine-readable service ownership matrix, negative Python/Go import
  fixtures, communication/data-ownership ADRs, and reserved NOLOGIN schema owners.
- Actual PostgreSQL tests verified seven owners, 42 directed cross-owner table
  denials, role privilege limits, and schema-USAGE isolation in a private project.
  Existing development data/credentials were not migrated or overwritten.
- Added `make contracts-check`, `make architecture-check`, `make ownership-test`,
  opt-in `make ownership-init`, and CI gates using frozen dependencies. SQL tests
  require Docker but no AIStor license; runtime LOGIN accounts/IAM remain planned.
- Validation: `make check` passed Ruff, strict mypy, 133 Python tests and native Go
  tests; 51 opt-in DB checks passed separately. Local base-ref contract checks,
  YAML validation, and M1.3 persistence checks passed. The existing Starlette/httpx
  warning remains; remote CI has not been run because no commit/push was requested.
- Corrected premature M1.2 completion flags: OIDC, organization modeling, actual
  proxy-route authorization, and gateway-to-sample integration still need work.

### 2026-11-11 - M1.3 follow-up: Portable Compose overrides

- Replaced custom reset tags in `infra/compose.test.yml` with standard YAML.
- Moved development-only host bindings into `compose.override.yml`; shared
  services remain port-free, and the Makefile explicitly loads both development
  files. Removed the special unsafe YAML-validation exception.
- Added four configuration-merge regressions covering the shared base, isolated
  tests, explicit development files, and automatic development override loading.
- Validation: strict YAML checks, `make check` (76 Python tests and Go tests), and
  isolated startup/persistence verification passed. Existing development services
  and their host ports were not restarted or modified.

### 2026-11-11 - M1.3: Local infrastructure and verification

- Completed the existing Compose work with PostgreSQL 18 / TimescaleDB 2.31.0,
  pinned Redis, Kafka, Neo4j, and AIStor images, health checks, bounded logs,
  localhost port bindings, and persistent named volumes.
- Added explicit TimescaleDB initialization and corrected the PostgreSQL 18
  data-volume layout. Kafka broker logs and KRaft metadata use sibling directories
  within the same persistent volume; Redis enables AOF persistence.
- Added `make infra-config`, readiness-gated `make infra-up`, `make infra-check`,
  isolated `make infra-test`, logs, and optional Kafka UI on port 8081.
- Added 62 fake-Docker regression cases for readiness, authentication-probe
  failures, forwarded Compose options, persistence-probe exit propagation, and
  test cleanup isolation. No business data is seeded in development databases.
- Documented the valid, untracked AIStor license prerequisite, local credential
  mapping, host/container addresses, persistence verification, and migration limits.
- Validation: normal `make infra-up` and authenticated readiness checks passed;
  isolated clean startup and persistence across container recreation passed for all
  five stores, including a synthetic TimescaleDB hypertable and a Kafka message.
- Validation: `make check` passed Ruff, strict mypy, 72 Python tests, and Go tests.
  Shell syntax and the configured YAML hooks passed. One existing Starlette/httpx
  deprecation warning remains. A transient Docker Hub pull reset was resolved by
  retrying; temporary test resources were cleaned up, and development services
  remain running and healthy.

### 2026-11-10 — M1.1 & M1.2: Monorepo Foundation & Go API Gateway Implementation

- Completed **M1.1 (Monorepo and service templates)**:
  - Created service directories (`services/api-gateway`, `services/identity-service`, `services/market-data-service`, `services/alert-service`, `services/event-intelligence`, `services/knowledge-graph-service`, `services/ai-orchestrator`, `services/quantitative-service`).
  - Added Go and Python service templates.
  - Added React + TypeScript frontend scaffold in `frontend/web/`.
  - Configured linters, formatters, and `.pre-commit-config.yml`.
  - Added GitHub Actions CI pipeline in `.github/workflows/ci.yml`.
- Completed **M1.2 (API Gateway and Identity)**:
  - Implemented Go API Gateway with reverse proxy routing in `services/api-gateway/cmd/server/main.go`.
  - Implemented health check routes (`/health`, `/health/live`, `/health/ready`).
  - Implemented `TracingMiddleware` for `X-Request-ID` and `X-Correlation-ID`.
  - Implemented standard library JWT HMAC-SHA256 verifier in `internal/infrastructure/auth/jwt_verifier.go`.
  - Defined tenant and user identity domain model in `internal/domain/identity/models.go` (`UserClaims`, `Role`, `TenantID`, `Permissions`).
  - Implemented `AuthenticateMiddleware` and `RequireRole` RBAC guards in `internal/presentation/http/auth_middleware.go`.
  - Added comprehensive automated unit and tenant-isolation test suite in `internal/presentation/http/auth_middleware_test.go` (cross-tenant access returns 403 Forbidden, expired/missing tokens return 401, valid claims propagate `X-Tenant-ID` and `X-User-ID`).
  - Verified `make check` passes 100% across all Python and Go test suites.

### 2026-11-07 — Phase 0 bootstrap and baseline service implementation

- Created sanitized `.env.example` placeholder file.
- Fixed `Makefile` syntax (replaced spaces with tabs, fixed `.PHONY`), added developer workflow targets (`make help`, `infra-up`, `infra-down`, `api`, `run`, `test`, `lint`, `format`, `check`).
- Added version-pinning files (`.python-version`, `.tool-versions`).
- Created ADR template (`docs/architect/template.md`) and added `docs/adr` compatibility symlink.
- Added `docs/architect/ADR-0003-initial-market-data-provider.md` (SEC EDGAR, FRED, Polygon/Yahoo Finance fallback).
- Added `docs/architect/ADR-0004-first-vertical-slice.md` (NVIDIA semiconductor supply chain and gross margin trajectory slice).
- Created `CONTRIBUTING.md` documenting DDD and Clean Architecture invariants, local setup, commit conventions, and DoD.
- Implemented Python Clean Architecture baseline service template:
  - `src/domain/health/` (pure Python health models, `HealthStatus` StrEnum, `SystemHealth`, `ComponentHealth`)
  - `src/application/health/` (`HealthCheckPort` protocol, `HealthCheckUseCase`)
  - `src/infra/health/` (`SystemHealthChecker` adapter implementing `HealthCheckPort`)
  - `src/shared/` (`config.py` using `pydantic-settings`, `logging.py` using `structlog`)
  - `src/presentation/http/` (FastAPI app factory `create_app()`, `RequestTracingMiddleware` with `X-Request-ID` / `X-Correlation-ID`, and health routes: `/health`, `/health/live`, `/health/ready`)
- Added comprehensive unit and integration tests in `tests/test_health.py` and `tests/conftest.py`.
- Verified `make check` passes cleanly (100% test pass rate with 6 tests, ruff format/lint clean, strict mypy typing passed).

### 2026-10-31 — Initial architecture and roadmap

- Created initial project plan.
- Selected DDD microservices + Clean Architecture + Event-Driven Architecture.
- Defined service boundaries, storage ownership, data pipeline, GraphRAG, LangGraph workflow, observability, testing, and 12-week milestones.
- No implementation tasks are marked complete yet.

---

## Definition of done

A task is complete only when:

1. Code is implemented and reviewed.
2. Unit and relevant integration/contract tests pass.
3. Authorization, failure behavior, and observability are addressed.
4. Data provenance and temporal semantics are preserved where applicable.
5. Documentation and API/event contracts are updated.
6. Evidence of completion is recorded in the engineering log.

**Reminder:** Keep this file truthful. Planned architecture is not implemented functionality.
