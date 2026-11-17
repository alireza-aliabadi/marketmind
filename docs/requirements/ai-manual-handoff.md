# Manual AI implementation handoff — start at M3.2

**Stop boundary:** This change implements foundation work only through M3.1.
M3.2 and every later AI/retrieval/workflow milestone remain unimplemented. This
document contains requirements and acceptance criteria only: no solution code,
prompt implementations, extraction algorithms, agent templates or query solutions.

Use [PROGRESS.md](../../PROGRESS.md) as the milestone authority and the
[verification report](../reports/through-m3.1-verification.md) for actual evidence.

## What you can build on

- Existing OIDC/tenant/RBAC gateway foundation and service-ownership boundaries.
- Go SEC submissions/filing **metadata**, FRED and Massive adapter ports.
- Exact decimal and dated instrument/price/series semantics; source/rights metadata.
- Versioned raw snapshot events, transactional outbox, durable error/DLQ records,
  explicit audited replay and owned PostgreSQL/TimescaleDB persistence.
- Canonical CIK/FIGI formatting, graph labels/direction specification and additive
  Neo4j uniqueness/range-index migration. No graph facts are automatically seeded.

Not available yet: filing-document acquisition/storage, extraction, entity
resolution, claim ingestion, temporal graph APIs, embeddings, GraphRAG, LLM
adapters, LangGraph, research reports, quantitative service APIs or AI telemetry.
Market-data operations are a local CLI, not an authenticated business HTTP API.
The old `filing.ingested.v1` artifact event remains contract-only.

## Decisions and prerequisites before AI implementation

1. **Review source rights.** M2 retention permission is not permission to embed,
   send to an external model, redistribute, train or trace source content. Default
   `ai_allowed=false` must be respected. FRED's current terms prohibit retention
   and AI development absent permission; keep live use disabled unless the actual
   written grant covers the intended use and reviewed series. Massive individual
   plans are not multi-tenant/public redistribution grants. See ADR-0008.
2. **Obtain actual evidence.** SEC metadata hashes are not document hashes. Define
   permitted raw-document acquisition, content/version hashing, immutable artifact
   references and evidence locations before extracting narrative relationships.
   Verify company-authored document rights and keep attribution/retention rules.
3. **Choose models and budgets yourself.** Record model/provider/version,
   embedding model/dimension, permitted data handling, context limits, latency,
   per-run token/tool/cost caps, timeout/retry limits and human-review thresholds.
4. **Review scope/security.** Public canonical entities are global; private
   evidence/claims/events are tenant-scoped. Private names, uploaded content or
   assertions must not leak into global properties. Formatting a tenant key is
   not authorization. Verify actual deployment support for read-only graph
   permissions; Neo4j driver read-access hints are not an enforcement boundary.
5. **Prepare a reviewed benchmark.** Start with the ADR-0004 semiconductor
   universe (NVDA/TSM/ASML/MSFT), but manually verify every expected relationship,
   source, evidence span, date and calculation. Planned example relationships are
   not ground truth. Version the corpus, rights, labels and ambiguous cases.
6. **Define authenticated service interfaces.** AI tools must use owned market,
   graph and quantitative APIs/ports with tenant context and finite limits. Do not
   invoke migration/replay CLIs, expose arbitrary SQL/Cypher or access another
   service's tables/driver from an agent.

## M3.2 — Source-backed claims and relationship ingestion

### Requirements

- An inward, framework-independent claim contract with stable/versioned identity,
  subject/predicate/object, evidence/source refs, evidence type, assessment
  confidence, status, world-valid time, observed time and extraction method/version.
- Evidence locations resolving to exact permitted document versions: artifact hash
  plus page/section or byte/text span. Preserve provider IDs and canonical URLs.
- Entity resolution prioritizing reviewed canonical IDs and dated listings, with
  ambiguity/abstention. Never equate a ticker or display name with permanent
  company/security identity; keep distinct share classes and ticker reuse separate.
- Typed relationship directions from graph schema v1. Uncertain assertions remain
  claims, not unquestionable direct edges; projections reference authoritative
  claims and inherit their scope, provenance, temporal interval and status.
- Active/disputed/superseded/unverified lifecycle with retained history and
  counterevidence; reported facts, calculations and inference remain distinct.
- Bounded, idempotent event processing and explicit invalid/partial/failure states.
  Untrusted documents are data, never instructions or tool permissions.

### Acceptance criteria

- Every accepted material claim resolves to a stored, permitted source/version and
  an independently checkable evidence location; metadata-only/fabricated citations
  are rejected or explicitly remain unverified.
- Duplicate input/retry creates no duplicate authoritative claims or projections.
  Changed source content produces a traceable new version rather than overwriting
  the old assertion. Out-of-order versions do not become the current fact.
- Reviewed fixtures cover supplier/customer direction, symmetric competition,
  ambiguous names, reused tickers, multiple share classes and unknown entities.
  Ambiguity does not silently merge entities.
- Unknown dates are not invented from publication/retrieval dates. Invalid
  intervals are rejected; explicit unknown validity cannot pass as historical fact.
- Disputed/superseded/unverified cases preserve evidence and remain distinguishable
  from active verified facts. Extraction confidence is not labeled truth probability.
- Malformed output, missing rights/source, duplicate delivery, provider failure,
  worker interruption, prompt injection and cross-tenant writes have automated tests.
- Claim and entity extraction are scored against the reviewed dataset; project
  targets are entity F1 ≥0.90 and relationship F1 ≥0.85, not preexisting achievements.

## M3.3 — Bounded temporal graph queries

### Requirements

- Reviewed, parameterized, allowlisted graph operations exposed by the owning
  service. No arbitrary model-generated Cypher, schema mutation or admin credential.
- Separate world-valid `as_of` from knowledge/observation cutoff where historical
  availability matters. Apply interval, lifecycle and scope filters to **every**
  traversed material edge/claim, not just the starting node.
- Finite depth, DB timeout, path/node/result caps and pagination/truncation flags.
  Agree values before implementation; suggested starting limits are depth 3,
  hard depth 5, DB timeout 2 s and at most 100 paths/200 nodes per request.
- Return stable entity/claim/source IDs and explainable paths; no private facts
  become visible via global entities, neighbors, counts or error responses.

### Acceptance criteria

- Fixtures immediately before/at/after each half-open boundary return only valid
  paths; expired, not-yet-valid and disallowed observed-time versions are excluded.
- Multi-hop paths containing one invalid edge are excluded even if endpoints and
  other edges are valid. Unknown validity and disputed facts follow documented policy.
- High-degree/cyclic graphs terminate within enforced limits and identify partial
  results. Oversized requests, injection attempts and unauthorized scopes fail closed.
- Every returned path can be explained using claim/evidence refs; tenant A cannot
  discover tenant B's private nodes, edges, sources or derived metadata.
- Actual Neo4j tests prove behavior, including DB unavailability/timeouts; merely
  adding indexes or setting a driver read mode does not satisfy this milestone.

## M3.4 — GraphRAG retrieval

### Requirements

- Use maintained `neo4j-graphrag`, not deprecated `neo4j-genai`; keep SDK/framework
  types in adapters. Pin embedding model, dimension, preprocessing and index version.
- Only approved-for-AI content enters embeddings, retrieval contexts or external
  models. Preserve corpus/source version, attribution and retention boundaries.
- Lexical/vector hybrid retrieval plus bounded, typed, time/scope-aware graph
  expansion; evidence deduplication/ranking with source, validity and freshness.
- Return evidence IDs, scores, source versions and graph paths suitable for report
  citations. Empty or insufficient retrieval is explicit, not hallucinated evidence.

### Acceptance criteria

- Reviewed exact-name/ticker, semantic, multi-hop, ambiguous and historical queries
  are scored; project target Recall@10 ≥0.85, with MRR and actual results reported.
- Source/evidence refs survive ranking, deduplication and expansion. Every expanded
  path still meets M3.3 temporal, status, scope and resource limits.
- Excluded/unlicensed/private sources never enter another tenant's retrieval or
  external model context. Missing/stale evidence yields explicit insufficiency.
- Embedding dimension/model changes require versioned indexes and a documented
  backfill/compatibility plan; existing migration/evidence IDs are not rewritten.

## M4.1 — Model/tool boundary

### Requirements

- Native domain/application model-provider ports, typed bounded tool inputs/results,
  structured output contracts, prompt versions and explicit failures.
- Model timeouts, bounded retries/backoff, rate-limit handling and per-run budgets.
  Tool allowlists and authenticated tenant/service context; no DB/admin/migration,
  credential-disclosure or trade-execution tool.
- Financial arithmetic delegated to deterministic quantitative tools preserving
  units, currency, vintage, missing-data and price-adjustment semantics.

### Acceptance criteria

- Domain/application imports contain no LangChain/LangGraph/provider SDK types.
- Invalid/truncated/model-refused output, rate limits, timeout, cancellation and
  budget exhaustion produce typed visible failures, never unbounded retries.
- Mock-model tests pass without paid accounts; separate permissioned live smoke
  tests record exact provider/model/prompt versions and actual costs/limitations.
- Prompt injection cannot expand tools, change tenant identity or access secrets.
  Numeric outputs match deterministic fixtures rather than model arithmetic.

## M4.2 — Persistent LangGraph workflow

### Requirements

- Compact versioned research state referencing evidence IDs rather than copying
  whole documents; explicit planner/routing, concurrency/reducer rules and terminal
  complete/partial/failed/cancelled outcomes.
- Service-owned persistent checkpointer and research records; resumable threads,
  cancellation, deadlines, bounded re-planning/retries and idempotent tool calls.
- Human-review gates for insufficient/conflicting evidence or sensitive actions;
  only safe execution summaries are exposed, never hidden chain-of-thought.

### Acceptance criteria

- One reviewed research question completes an end-to-end workflow with a cited
  result; insufficient evidence terminates partial/failed rather than fabricating.
- Killing/restarting a worker after a checkpoint resumes the same authorized run
  without duplicate tool side effects, evidence, reports or cross-tenant state.
- Concurrent branches do not lose results; retries/re-planning stop at their caps.
  Cancellation prevents later publication and remains durable after restart.
- Workflow-version incompatibility is handled explicitly, and checkpoints are not
  treated as long-term graph facts or interchangeable research product records.

## M4.3 — Specialized agent responsibilities

| Responsibility | Required acceptance behavior |
|---|---|
| Market Intelligence | Finds permitted events/filings with source IDs and dates; states freshness/coverage limits |
| Fundamental Analysis | Uses source-backed financial facts and periods; does not invent unavailable line items |
| Graph Research | Uses bounded temporal retrieval; explains relationships through claim/path/evidence refs |
| Quantitative Analysis | Invokes deterministic tools and preserves units/adjustment/vintage; does not calculate through LLM prose |
| Sentiment/Event | Separates reported event from assessed sentiment/inference; records source/method/uncertainty |
| Risk/Counterevidence | Searches for contradictions/alternative explanations; explicitly reports when evidence is insufficient |
| Evidence Verification | Checks source/version/date/tenant/citation and calculation replay; can reject publication |

All outputs must be typed, bounded, tenant-aware and independently testable.
Implement the narrow vertical slice first; additional roles/breadth do not replace
proof that the principal workflow works.

## M4.4 — Evidence verification and report publication

### Requirements

- Versioned report/evidence contracts, stable citations and explicit assumptions,
  uncertainty, freshness, source coverage and partial-result reasons.
- Material conclusions either supported by exact evidence/calculation replay or
  clearly labeled inference; unsupported conclusions are flagged, not laundered
  through another agent's summary. Correlation is not automatically causation.
- Deterministic number/unit/period/adjustment validation and recovery/idempotent
  publication; no autonomous trades or personalized/guaranteed investment advice.

### Acceptance criteria

- Every material conclusion has valid support or an explicit inference/unsupported
  label; no fabricated source, date, number or evidence location passes verification.
- Citation precision is measured against reviewed labels (target ≥0.95), evidence
  coverage is reported, and unsupported-claim rate is measured and driven down.
- Numeric fixtures match exactly under documented rounding/unit policies; missing
  data is not zero and adjusted/unadjusted/dividend/vintage series are not mixed.
- Contradictory/missing sources and failed tools yield review/partial/failure states.
  Repeated resume/publication does not duplicate the report or citations.
- UI/SSE summaries expose status/evidence activity only, not secrets, private source
  content across tenants or internal chain-of-thought.

## M5.1–M5.2 — AI observability

### Requirements

- Langfuse for model/tool spans, prompt versions, tokens/cost/feedback; LangSmith
  for LangGraph debugging, datasets and experiments; OpenTelemetry for service
  correlation. Define complementary ownership instead of duplicating all content.
- Correlate request, organization, research, thread, workflow, trace and experiment
  IDs. Apply access control, redaction, sampling and retention before export.

### Acceptance criteria

- A permitted run can be traced across services, tools and workflow transitions.
  Model/provider/prompt versions and actual token/cost metadata are recorded;
  unavailable usage/cost is unknown, never silently zero.
- Seeded secrets, private tenant text and disallowed licensed content do not appear
  in traces/prompts/datasets for unauthorized observers or external systems.
- Telemetry failures do not silently corrupt workflow state or trigger unlimited
  duplicate model calls; retention and deletion/redaction behavior are tested.

## M5.3–M5.4 — Evaluation, regression and ablation

### Requirements

- Versioned manually reviewed entities, relationships, source/evidence spans,
  valid/observed times, calculations, answer requirements and adversarial cases.
- Entity/relationship extraction, Recall@K/MRR, citation/evidence coverage,
  temporal/numerical correctness, unsupported claims, recovery, latency and cost.
- Compare vector-only, hybrid, graph-expanded and full multi-agent modes on the
  same permitted corpus/questions; record configurations, versions and run dates.

### Acceptance criteria

- Repeatable evaluations report dataset size/version, actual metrics and failures,
  not only targets. Numerical fixtures match exactly and the reviewed historical
  test set has no known invalid-time paths.
- A repeatable job/CI gate detects seeded regressions in retrieval, citations,
  time, arithmetic, tenant isolation and workflow recovery.
- Ablations include quality, latency and cost; no “GraphRAG is better” claim without
  comparable measured results. Model nondeterminism and unknown costs are disclosed.
- Reviewed prompt-injection, fabricated citation, ambiguous entity, stale/contradictory
  source, outage, duplicate/out-of-order event and worker-restart cases are included.

## Shared definition of done

- Keep M3.2+ unchecked until its own unit, contract and actual integration/evaluation
  scenarios pass; record commands, environments, versions, outputs and limitations.
- Preserve service ownership, existing work, evidence IDs and migration history.
  Never reset a database/graph or modify released schemas merely to pass a check.
- Re-run relevant existing gates (`make check`, `make market-data-test`,
  `make graph-schema-test`, gateway/ownership tests as affected).
- Update contracts, schema/ADRs, progress and evidence reports from verified facts.
- Leave live grants/accounts, model/embedding choice, budgets, news rights,
  production IAM, deployment and benchmark sign-off explicitly unresolved until
  you have their evidence. Local/synthetic checks are not remote CI or live-data
  licensing acceptance.
