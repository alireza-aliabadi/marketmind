# ADR-0007: OIDC/OAuth2 and organization-scoped identity foundation

- **Status:** Accepted
- **Date:** 2026-11-14
- **Scope:** Phase 1 / M1.2; not a production identity provisioning system

## Context

The initial gateway used a shared HS256 development secret. Tokens without a
subject, tenant or expiry could pass its verifier, role helpers were not applied
to proxy routes, and empty identity-service directories did not define the
organization/user/membership model. No gateway-to-Python flow was exercised.

## Decision

1. The gateway is an OAuth2 **resource server**, not an authorization server.
   It discovers one explicitly configured OIDC issuer and verifies RS256 JWT
   access tokens against its public JWKS, including key-cache refresh/rotation.
   `OIDC_AUDIENCE` identifies the API resource and must differ from the browser
   login client's audience. Issuer, signature, audience, expiry, issued-at,
   not-before, subject and `tenant_id` are checked. There is no HS256 fallback.
2. HTTPS is required. An explicit loopback-only HTTP exception is available in
   local/development/test environments; it is rejected in production/staging.
   Discovery/JWKS requests have a five-second timeout and do not follow redirects.
   Startup fails closed if configuration/discovery cannot be validated.
3. One token represents one IdP-authorized organization membership. `tenant_id`
   is the organization identifier. Only signed tenant-scoped `roles`,
   `permissions` and OAuth2 `scope` grant actions. Switching organizations
   requires a newly authorized token, not changing a request header. The IdP
   administrator must control claim assignment; self-editable profile metadata
   is not an authorization source.
4. The identity service owns pure domain models for organizations, users and
   memberships. External users are identified by `(issuer, subject)`, not email.
   Users may belong to multiple organizations with different roles. Inactive
   users/organizations and invited/suspended/mismatched memberships deny access.
   The gateway consumes its own verified principal projection and never imports
   identity-service internals or queries the identity database.
5. All registered proxy routes require authentication plus a method-specific
   permission. Viewer/analyst/admin roles can read; analyst/admin can submit
   research. Explicit recognized action permissions can grant an action without
   a role. Unknown roles/actions grant nothing implicitly. Other methods deny
   access with `405` and `Allow`.
6. Three explicit sample-health paths strip `/api/v1/sample` before forwarding
   to the Python sample. Caller identity/forwarding headers are replaced from
   authenticated context, including after hop-by-hop stripping. Generated and
   supplied request/correlation IDs are propagated. The API bearer token is not
   forwarded to the unauthenticated health sample. Connection/response-header
   failures yield a bounded JSON `502`; credentials are not logged.

## Alternatives

- Keeping HS256 as the gateway default was rejected: it is not OIDC, risks
  development-secret acceptance and prevents independent public-key rotation.
- Building a password store/authorization server was rejected: delegate login to
  an existing provider rather than implementing credential handling in Phase 1.
- A database-backed membership registry and immediate revocation are deferred;
  the Phase 1 task defines the model, not its CRUD/provisioning/persistence APIs.

## Consequences and limits

- Go's pinned toolchain is now 1.26.5 (module minimum 1.25) to use pinned
  `go-oidc/v3` 3.21.0 / `go-jose/v4` 4.1.4 instead of an outdated auth dependency.
- An actual configured IdP must emit the documented API audience and trusted
  tenant/role/scope claims. Opaque access-token introspection and interactive
  browser Authorization Code + PKCE login are not implemented here.
- Roles/membership revocation take effect when tokens expire or are replaced;
  short access-token lifetimes are required. The local domain models do not yet
  independently enforce IdP membership provisioning at runtime.
- The health sample contains no protected business data. Production downstream
  services must independently authenticate service calls (for example, mTLS or
  exchanged service credentials) and enforce tenant scope. Forwarded identity
  headers alone are not production service-to-service authentication.
- Research/market routing is permission-gated delegation, not implemented
  research/ingestion APIs. Database runtime credentials, migration/CRUD APIs,
  rate limits, SSE and production deployment remain later milestones.

## Evidence

- `services/api-gateway/internal/infrastructure/auth/oidc_verifier_test.go`:
  discovery/JWKS, signature/issuer/audience/time/tenant checks, OAuth2 scopes,
  cache/rotation and issuer/JWKS failure paths.
- `services/api-gateway/internal/presentation/http/proxy_test.go`: actual
  registered route guards, cross-tenant denial, spoofed/hop-by-hop header handling,
  trace propagation and unavailable downstream behavior.
- `services/identity-service/internal/domain/identity/models_test.go`: model
  invariants, per-organization roles, inactive memberships and permission policy.
- `make gateway-test`: real compiled Go gateway → real FastAPI process, disposable
  public-key issuer, wire-contract checks and failure/shutdown scenarios.
- See [Phase 1 verification](../reports/phase-1-verification.md) for executed
  commands, observed results and remote-CI limitations.
