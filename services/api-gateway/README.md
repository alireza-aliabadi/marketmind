# API Gateway

Phase 1 provides public process health, OIDC/OAuth2 bearer authentication,
organization-scoped role/action checks and a working proxy to the Python sample.

## Run the verified, disposable scenario

From the repository root, after `uv sync --extra dev --frozen`:

```bash
make gateway-test
```

This compiles/runs the gateway and starts the actual FastAPI sample on temporary
loopback ports with a disposable RS256 discovery/JWKS issuer. It checks successful
routing, API contracts, authentication/tenant/permission denial, key rotation,
downstream failure and shutdown. It needs no Docker, external account, credentials
or `.env`, and stops only its own processes. The test issuer is not a development
or production login service.

## Run with your OIDC provider

Start the sample in one terminal (`make api`). In another terminal, export the
gateway configuration and run it:

```bash
export APP_NAME=marketmind-gateway APP_ENV=local
export GATEWAY_HOST=127.0.0.1 GATEWAY_PORT=8080
export DOWNSTREAM_URL=http://127.0.0.1:8000
export OIDC_ISSUER_URL=https://your-issuer.example/realms/marketmind
export OIDC_AUDIENCE=marketmind-api
make gateway
```

Replace the placeholder issuer with the exact issuer from your provider's
discovery document. The Go gateway reads **process environment**, not `.env`.
`PORT` belongs to the Python sample; use `GATEWAY_PORT` for the gateway.
For an explicitly trusted local issuer only, use a loopback HTTP issuer with
`OIDC_ALLOW_INSECURE_HTTP=true`; production/staging rejects this exception.
Missing/invalid issuer/audience or failed discovery stops startup; there is no
default development secret or HS256 mode.

Obtain an **OAuth2 API access token**, not a browser ID token. Configure your IdP
to emit RS256 tokens with `iss`, `aud` containing the API audience, positive Unix
`iat`/`exp`, `sub` and one trusted `tenant_id`. `nbf`, when present, cannot be in the
future. `roles` is an array of tenant-scoped `viewer`, `analyst` or `admin` roles;
alternatively grant recognized actions through `permissions` (array) or `scope`
(space-separated OAuth2 scopes). Never source these claims from self-editable
user profile attributes. Email is optional metadata and grants no access.

For example, after setting `ACCESS_TOKEN` from your provider:

```bash
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/api/v1/sample/health/ready \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "X-Request-ID: local-sample-check"
```

Do not put tokens in committed files or logs. This setup path is provider-agnostic;
the automated verification uses a disposable standards-based issuer, not a
provisioned third-party account or a browser login flow.

## Route policy

| Route | Allowed methods | Required action | Role grant |
| --- | --- | --- | --- |
| `/health`, `/health/live`, `/health/ready` | GET/HEAD | Public process probes | None |
| `/api/v1/sample/health{,/live,/ready}` | GET/HEAD | `sample:read` | viewer/analyst/admin |
| `/api/v1/market{,/...}` | GET/HEAD | `market:read` | viewer/analyst/admin |
| `/api/v1/research{,/...}` | GET/HEAD | `research:read` | viewer/analyst/admin |
| `/api/v1/research{,/...}` | POST | `research:write` | analyst/admin |

Research/market prefixes are **delegation only**; their business APIs are not yet
implemented by the sample. Unknown methods return `405`. Invalid credentials
return JSON `401` with a bearer challenge; tenant mismatch/insufficient permission
returns `403` without calling downstream; connection/header timeout returns `502`.
Sample readiness propagates downstream `503`. Gateway process readiness is not a
continuous downstream/IdP availability check; use the proxied sample readiness
endpoint to check the sample itself.

`X-Tenant-ID` may optionally select the signed organization but cannot switch it;
duplicate selectors are rejected. User/role/action headers come only from verified
claims. Caller forwarding headers are discarded, bearer credentials are stripped
before the health sample, and request/correlation IDs propagate across both services.
`X-User-ID` currently carries the verified OIDC `sub`, not a provisioned local user
ID; local identity resolution belongs to the future registry integration.

## Boundaries

The identity service owns organization/user/membership domain models. Phase 1 does
not implement identity persistence, membership provisioning/revocation APIs,
browser login, rate limiting or production service-to-service authentication.
The unauthenticated sample has health data only; future business services must
authenticate service callers and independently enforce tenant ownership.

See [OpenAPI](../../contracts/openapi/api-gateway.json),
[ADR-0007](../../docs/architect/ADR-0007-oidc-identity-foundation.md), and
[Phase 1 verification](../../docs/reports/phase-1-verification.md).
