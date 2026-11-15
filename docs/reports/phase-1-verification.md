# Phase 1 verification — 2026-11-14

## Outcome

M1.1–M1.4 implementation and local acceptance checks pass. The original M1.2 gaps
(OIDC, organization/membership definitions, registered-route permissions and the
gateway/sample integration) are closed. Frontend lint/pre-commit/build/CI gaps are
also addressed. **Remote CI execution remains unobserved**: no commit or push was
requested/performed, so that exit criterion remains pending in `PROGRESS.md`.

This report is local test evidence, not a production IAM/security certification
or a claim of implemented identity CRUD, market ingestion, research agents or UI
product pages.

## Environment

- Linux working tree, preserving pre-existing uncommitted M1.3/M1.4 work.
- Go 1.26.5; Go modules require 1.25+ and pin OIDC/Jose dependencies/checksums.
- Python 3.14.8 and locked `uv` development dependencies.
- pnpm 12.18.0; frontend also checked with official Node 22.12.0 placed on `PATH`
  in a disposable directory after SHA-256 verification against official sums.
  Normal workspace checks also passed with installed Node 26.7.0.
- Docker Compose 5.6.0; existing valid, untracked AIStor license used for the
  isolated five-store test. PostgreSQL ownership tests do not need that license.

## Executed checks

Commands below run from the repository root unless otherwise specified.

| Check | Actual result |
| --- | --- |
| `GOFLAGS=-mod=readonly make check` | PASS: Ruff lint/format, strict mypy (13 source files), Go format/vet, static ownership/import boundaries, 2 OpenAPI specs / 1 versioned event, 134 Python tests, both Go modules and frontend/Go builds. 51 DB tests intentionally skipped by the ordinary suite. |
| `make test-go GO_TEST_FLAGS='-race -count=1'` | PASS: both independent Go modules, including domain, OIDC, configuration, middleware and registered-route proxy tests. |
| `make gateway-test` | PASS: actual compiled Go executable → actual FastAPI process, disposable RS256 discovery/JWKS issuer, authorization/failure contracts and cleanup. |
| `RUN_GATEWAY_INTEGRATION_TESTS=1 TMPDIR=/tmp/opencode GOFLAGS=-mod=readonly go test -race -count=2 -v ./cmd/server -run TestGatewayEndToEnd` in `services/api-gateway` | PASS: two complete runtime scenarios with race detection enabled for the Go test target. |
| `make ownership-test` | PASS: 51 real PostgreSQL checks, including seven owners and 42 directed cross-owner table denials in an isolated port-free project. |
| `make infra-test` | PASS: fresh startup/readiness and persistence after container recreation for PostgreSQL/TimescaleDB, Redis, Kafka, Neo4j and AIStor. Only the private test resources were removed. |
| `uv run python -m contracts.validation --base-ref HEAD` | PASS: API/event schemas, owner/version binding, examples and base-revision event-schema immutability. |
| `go mod verify` in `services/api-gateway` | `all modules verified`. |
| `pnpm --dir frontend/web install --frozen-lockfile` | PASS: lockfile unchanged. |
| Fresh `UV_PROJECT_ENVIRONMENT` with `uv sync --extra dev --frozen` / `uv run --frozen pytest`, plus a source-only frontend copy with frozen pnpm install and lint/format/build | PASS: 134 Python tests (51 opt-in skips), 59 locked Python packages and 28 frontend packages installed; clean frontend gates passed. Development dependency environments were not replaced. |
| `make frontend-check` with Node 22.12.0 on `PATH` | PASS: Oxlint, Prettier, TypeScript and Vite production build. |
| `make pre-commit-check` | PASS: all configured tracked-file hooks, including strict YAML, safe pnpm multi-document YAML, Go/frontend and contract/architecture checks. |
| `git ls-files -- .env minio.license` | No tracked credential/license files returned. No existing private file was changed. |

The pre-commit `--all-files` command covers tracked files; new Go source is also
checked by all-module format/vet/tests and the contract/architecture tools scan
the current filesystem, including new files. Representative new Go/JSON/Python
files were separately exercised with the configured hooks.

## Visible runtime scenarios

- Public gateway health probes respond without credentials.
- A viewer token reaches each `/api/v1/sample/health{,/live,/ready}` endpoint as the
  corresponding real Python `/health{,/live,/ready}` operation and returns healthy
  JSON with propagated request/correlation IDs.
- Missing, expired, wrong-issuer/audience, missing-organization and future-`nbf`
  credentials return `401`; API bearer challenges and JSON contracts validate.
- Missing action permissions, cross-organization requests and duplicate tenant
  selectors return `403`. Viewer research submission also returns `403`.
- Disallowed sample methods return `405` with `Allow`; the wire schema validates.
- A rotated signing key is fetched from the provider's JWKS and authenticates.
- Stopping the sample yields a JSON `502` while gateway process liveness remains
  `200`. Both owned processes stop, and an unconfigured gateway refuses startup.
- Registered-route tests separately prove rejected requests never call upstream,
  role/scope/permission policies apply on the real registered prefixes, caller
  identity/forwarding headers cannot elevate privilege, and hop-by-hop stripping
  cannot remove the subsequently rebuilt trusted identity/tracing context.

## Verification corrections

- Uvicorn completes ASGI shutdown and then re-raises the captured `SIGTERM`.
  The runtime test now requires completed shutdown logs and accepts only that
  exact expected signal, not arbitrary child-process errors or forced termination.
- Repeated race-enabled E2E execution found a test-issuer publication race:
  discovery could read its server pointer after the HTTP goroutine had started.
  The fixture now assigns an unstarted server before starting request goroutines.
  The precise repeated race command subsequently passed.
- pnpm 12 emits a valid multi-document lockfile, so only that known lockfile uses
  the safe multi-document YAML hook; other YAML retains single-document checking.
  Baseline missing-final-newline/format-only issues were normalized, with no editor
  setting values or application behavior changed.

## Remaining limits

- CI jobs and their commands are implemented/locally exercised, but this report
  is **not** evidence of a GitHub Actions run against these uncommitted changes.
- OIDC is verified with a disposable standards-based issuer, not a provisioned
  third-party account. Browser login/PKCE and opaque-token introspection are absent.
- Identity models are a pure domain foundation. Runtime membership provisioning,
  persistence/CRUD/audit APIs and immediate revocation remain unimplemented.
  Current gateway authorization trusts IdP-controlled signed membership claims.
- Downstream service authentication, runtime database LOGIN credentials, rate
  limits, SSE and production deployment are future work; forwarded identity
  headers alone are not production service authentication.
- The ordinary Python suite still emits the existing Starlette/httpx deprecation
  warning. It does not fail the checks; no unrelated dependency migration was made.
- Fresh dependency installation emitted cross-filesystem cache hardlink warnings
  and used the supported copy/local-store fallback; installation and checks passed.

See [PROGRESS.md](../../PROGRESS.md),
[gateway setup](../../services/api-gateway/README.md), and
[ADR-0007](../architect/ADR-0007-oidc-identity-foundation.md).
