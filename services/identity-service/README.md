# Identity & Access foundation

`internal/domain/identity` defines service-owned organizations (tenants), users
keyed externally by `(issuer, subject)`, and organization-scoped memberships.
Active memberships grant viewer/analyst/admin permissions; invited/suspended
memberships, disabled users/organizations and cross-organization access fail closed.

```bash
go test ./...
```

This is the Phase 1 **domain foundation**, not a running identity CRUD API or a
database-backed registry. Persistence, provisioning, audit events and immediate
membership revocation are not implemented. The gateway currently trusts the
configured IdP's signed `tenant_id` and tenant-scoped role/scope claims; see
[ADR-0007](../../docs/architect/ADR-0007-oidc-identity-foundation.md).

Only this service owns the reserved PostgreSQL `identity` schema / `mm_identity`
role. No gateway or other service may import its internals or query its tables.
