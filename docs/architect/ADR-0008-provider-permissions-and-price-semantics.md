# ADR-0008: Provider permissions, precision and price semantics

- **Status:** Accepted for local M2 implementation
- **Date:** 2026-11-17
- **Supersedes:** ADR-0003's blanket licensing assumptions; preserves its SEC,
  FRED and Polygon-adapter direction. No provider account or purchase authorized.

## Decision

Use SEC submissions for company/filing **metadata**, the FRED series API behind
its own port, and Massive (formerly Polygon) REST behind the price-provider port.
Do not add an undocumented Yahoo/yfinance fallback or silently change providers
after a failure. First local acceptance uses only synthetic fixtures.

SEC is publicly accessible, not evidence that every company-authored filing is
public-domain or licensed for every downstream use. This implementation retains
submissions metadata; it does not download source documents. A configured
application/contact User-Agent, process-wide <=8 requests/sec pacing, bounded
bodies, cancellation and bounded retries implement conservative fair access.
Multiple service processes require an aggregate external rate-limit budget.

FRED's current [terms](https://fred.stlouisfed.org/legal/) explicitly prohibit
retaining API content and using it in connection with AI development/training
without permission. Third-party series carry additional rights and attribution.
An API key, public-domain label or personal-use plan does **not** override these
terms. Live FRED configuration fails closed without a permission reference and
reviewed-series allowlist. Operator review must cover retention and this project;
AI reuse remains independently denied. Include the required notice wherever FRED
integration is presented: “This product uses the FRED® API but is not endorsed or
certified by the Federal Reserve Bank of St. Louis.”

Massive's [Stocks Developer](https://massive.com/docs/rest/stocks/aggregates/custom-bars)
plan is the initial adapter semantics: daily US equity bars, up to ten years of
history, 15-minute-delayed recency. This is selection, not a purchased entitlement.
Basic is EOD; Starter/Developer are delayed; Advanced/Business advertise realtime.
Require an explicit documented plan and reviewed permission reference for live
operation. Individual plans are personal/non-business; public demos,
redistribution, shared tenants and AI use require separate source/contract review.
See [terms](https://massive.com/terms) and
[market-data terms](https://massive.com/legal/market-data-terms-of-service).

## Data semantics

- Native financial decimals stay exact strings until PostgreSQL NUMERIC storage;
  no binary floating-point ingestion or LLM calculations.
- FRED period dates and vintage/real-time intervals are not publication instants.
  Pin a vintage per request, retain units/frequency/notices, and represent missing
  values as null. Latest revised data does not prove historical availability.
- Instruments use provider share-class FIGIs; CIK-backed companies use padded
  CIKs. Ticker/MIC/date describe a listing, not permanent company identity.
- Price instants are UTC, with original daily ET boundaries intact; currency is
  uppercase ISO 4217 and exchange is the provider's ISO MIC.
- Split-adjusted bars exclude dividend adjustments and are not total-return
  series. Store adjustment and recency explicitly and never blend semantics.
  Corporate-action types distinguish ratios from cash dividends; fetching,
  applying and quantitative calculation of actions are not claimed here.
- Metadata at both ends of a range must have matching instrument/MIC/currency.
  Complex intra-range ticker changes still require source review; historical
  reference metadata does not prove point-in-time disclosure availability.
- Source records preserve provider IDs, canonical URLs, retrieval times, native
  payload hashes, retention permission and a separate AI-use policy. Defaults deny
  AI use. Synthetic fixtures have synthetic-only classifications.

## Consequences and verification

Live permission/account verification is a documented prerequisite, not bypassed
by tests. No live FRED content is cached or passed into AI by this work.
Transport endpoint overrides are loopback-only and never accept arbitrary
provider-supplied hosts. Pagination credentials are removed; errors do not echo
provider response bodies or credential-bearing URLs. The current transport is a
local-development implementation, not production quota/security administration.

Acceptance evidence: [through-M3.1 report](../reports/through-m3.1-verification.md).
