# ADR-0003: Initial market data providers and licensing

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** MarketMind Architecture Team

## Context and Problem Statement

MarketMind requires access to three categories of market data:
1. Public company filings, metadata, and disclosures.
2. Macroeconomic indicators and time-series series (interest rates, inflation, GDP).
3. Public equity pricing, volume, and company identifiers (CIK, ticker, exchange).

We need an initial provider selection that is legally unencumbered for research, cost-effective for portfolio development, reliable, and easily abstracted behind Clean Architecture ports.

## Decision Drivers

- **Licensing & Redistribution:** Must permit research, derivative knowledge graphs, and decision-support analytics without enterprise licensing violations.
- **Cost & Accessibility:** Free or developer-tier access with reliable rate limits.
- **Coverage:** Primary coverage of US equities (Russell 1000 / S&P 500) and US macroeconomic metrics.
- **Architecture Compatibility:** Adapters must isolate external schemas from the core domain model.

## Considered Options

1. **Company Filings:**
   - SEC EDGAR Submissions API (Direct, official)
   - Commercial aggregators (SEC-API.io, FMP)

2. **Macroeconomic Indicators:**
   - St. Louis Fed FRED API
   - World Bank / IMF Open Data

3. **Pricing and Security Identifiers:**
   - Polygon.io (Developer plan)
   - Alpha Vantage
   - Financial Modeling Prep (FMP)
   - Yahoo Finance API / `yfinance` (local dev fallback)

## Decision Outcome

Adopt a **multi-tier provider adapter pattern** behind a unified `MarketDataPort`:

1. **Company Disclosures & Filings: SEC EDGAR API**
   - **Coverage:** 10-K, 10-Q, 8-K filings, company facts (XBRL), company submissions metadata.
   - **Licensing:** Public domain US Government data.
   - **Compliance:** Enforce SEC fair-access policy (declare custom `User-Agent: MarketMindResearch <admin@marketmind.local>`, rate-limit to <= 10 req/sec with token bucket).

2. **Macroeconomic Data: St. Louis Fed FRED API**
   - **Coverage:** CPI, Federal Funds Rate, 10-Year Treasury Yield, GDP, M2 Money Supply.
   - **Licensing:** Public domain / FRED API terms allow personal and research use.
   - **Authentication:** Free API key via `FRED_API_KEY`.

3. **Equity Market Data: Standardized `PriceProvider` Port**
   - **Primary:** Polygon.io REST API (standardized OHLCV, splits, dividends).
   - **Development fallback:** yfinance / Yahoo Finance adapter for unauthenticated local development.
   - **Domain rule:** Raw price data is persisted into TimescaleDB with provenance (`source_id`, `observed_at`, `as_of`).

### Consequences

- All external provider payloads are normalized at the infrastructure boundary into canonical domain events and entities.
- Domain and application layers have zero provider-specific dependencies.
- Provider outages or schema changes are absorbed entirely within the respective adapter module.
