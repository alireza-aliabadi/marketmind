# ADR-0006: Service-owned namespaces and least-privilege data access

- **Status:** Accepted as the M1.4 implementation baseline
- **Date:** 2026-11-12

## Context and Problem Statement

The local platform shares a PostgreSQL/TimescaleDB instance for cost and setup
simplicity. Shared physical infrastructure must not become shared business tables,
cross-service joins, or a single runtime database administrator credential.

## Decision Drivers

- Independent service evolution with explicit data owners.
- Enforceable boundaries, not just a service diagram.
- A small local platform without prematurely deploying seven database servers.
- Honest separation between reserved namespaces and implemented applications.

## Considered Options

1. Shared tables and a shared administrator account.
2. A separate physical database server for every service immediately.
3. Service-owned schemas/roles on local shared infrastructure, separable later.

## Decision Outcome

Choose option 3 for the local baseline. The machine-readable
[ownership manifest](../../contracts/service-ownership.json) is authoritative for
service roots, import namespaces, PostgreSQL owners, and planned resource/topic
responsibilities. API gateway and the legacy sample have no business PostgreSQL
ownership. Planned services may have missing source directories in clean Git
checkouts; implemented/template service roots must exist.

Seven reserved PostgreSQL schemas have distinct NOLOGIN owner roles. The
[bootstrap](../../infra/postgres/init/020-service-ownership.sql) creates empty
namespaces, rejects conflicting preexisting ownership/elevated roles, and removes
PUBLIC access to each owned schema. It does not create business tables, assign
runtime passwords, rotate credentials, or silently take ownership of existing data.

Runtime LOGIN accounts must be provisioned separately from secrets, have no
superuser/BYPASSRLS/role-administration privileges, and inherit only their owning
service's role. The local bootstrap/health administrator is not a business-service
credential. Existing volumes require an explicit reviewed bootstrap application;
entrypoint SQL runs only on an empty data directory.

Services must not read/write other schemas directly, share internal packages,
perform cross-service SQL joins, or use arbitrary SQL as an AI tool. They exchange
versioned contracts through bounded APIs or events and maintain their own
projections. Deterministic financial computation stays outside LLM generation.

Redis key spaces, S3 prefixes, and Neo4j responsibilities in the manifest are
PLANNED; this decision does not claim their production IAM is provisioned. Raw
filing artifacts have a single owner. Evidence extraction may read permitted
artifact references through the documented object-storage interface, not through
the owner's database. Derived extraction artifacts have a separate owner.

Static checks enforce Python/Go import boundaries and domain/application purity.
Actual PostgreSQL tests exercise owner access, all directed cross-owner denials,
NOLOGIN/non-elevated roles, and schema-USAGE grants in a temporary port-free
project. Static import checks cannot prove all dynamic SQL is safe; grants and
least-privilege runtime credential provisioning remain essential.

## Consequences and Trade-offs

- Ownership is explicit and schema access can be tested without an AIStor license.
- Current local physical sharing does not prevent later independent scaling.
- Projections can be eventually consistent and require versioned events.
- Runtime credential/IAM rollout and business migrations remain later work.
- PostgreSQL namespace isolation is distinct from application tenant/RBAC checks;
  this does not finish the missing OIDC/organization foundation in M1.2.

## Validation

- [Static architecture checks](../../scripts/check-architecture.py)
- [Negative architecture fixtures](../../tests/test_architecture.py)
- [Real PostgreSQL permission tests](../../tests/test_db_ownership.py)
