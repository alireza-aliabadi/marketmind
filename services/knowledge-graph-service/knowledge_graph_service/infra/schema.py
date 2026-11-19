"""Idempotent constraints and indexes; no business-node or relationship ingestion."""

from typing import LiteralString

from neo4j import Driver, Session

CONSTRAINTS: dict[LiteralString, tuple[LiteralString, tuple[LiteralString, ...]]] = {
    "mm_company_key": ("Company", ("key",)),
    "mm_company_canonical_id": ("Company", ("canonical_id",)),
    "mm_instrument_key": ("Instrument", ("key",)),
    "mm_instrument_canonical_id": ("Instrument", ("canonical_id",)),
    "mm_market_event_key": ("MarketEvent", ("key",)),
    "mm_sector_key": ("Sector", ("key",)),
    "mm_industry_key": ("Industry", ("key",)),
    "mm_product_key": ("Product", ("key",)),
    "mm_source_key": ("Source", ("key",)),
    "mm_claim_key": ("Claim", ("key",)),
}
INDEXES: dict[LiteralString, tuple[LiteralString, tuple[LiteralString, ...]]] = {
    "mm_company_name": ("Company", ("name",)),
    "mm_instrument_symbol": ("Instrument", ("ticker", "exchange")),
    "mm_source_provider_id": ("Source", ("provider", "external_id")),
    "mm_claim_scope_predicate_status": ("Claim", ("scope", "predicate", "status")),
    "mm_claim_valid_from": ("Claim", ("valid_from",)),
    "mm_claim_valid_to": ("Claim", ("valid_to",)),
    "mm_claim_observed_at": ("Claim", ("observed_at",)),
    "mm_event_occurred_at": ("MarketEvent", ("occurred_at",)),
    "mm_source_published_at": ("Source", ("published_at",)),
}


def _verify_definitions(session: Session, require_all: bool) -> None:
    for command, expected in (("CONSTRAINTS", CONSTRAINTS), ("INDEXES", INDEXES)):
        prefix: LiteralString = "SHOW CONSTRAINTS" if command == "CONSTRAINTS" else "SHOW INDEXES"
        rows = {
            str(row["name"]): row
            for row in session.run(
                f"{prefix} YIELD name,type,entityType,labelsOrTypes,properties "
                "RETURN name,type,entityType,labelsOrTypes,properties"
            )
        }
        for name, (label, properties) in expected.items():
            row = rows.get(name)
            if row is None:
                if require_all:
                    raise ValueError(f"required graph schema item {name} is missing")
                continue
            correct_type = (
                "UNIQUENESS" in str(row["type"])
                if command == "CONSTRAINTS"
                else row["type"] == "RANGE"
            )
            if not (
                correct_type
                and row["entityType"] == "NODE"
                and tuple(row["labelsOrTypes"]) == (label,)
                and tuple(row["properties"]) == properties
            ):
                raise ValueError(f"existing graph schema item {name} conflicts; migrate explicitly")


def ensure_schema(driver: Driver, database: str = "neo4j") -> dict[str, int]:
    """Install missing schema items without overwriting conflicts or touching data.

    Identifiers come only from the definitions above, never caller input. Each DDL
    statement commits separately; IF NOT EXISTS makes interrupted setup resumable.
    Existing migration records/constraints from older releases are left untouched.
    """
    driver.verify_connectivity()
    constraints_added = indexes_added = 0
    with driver.session(database=database) as session:
        _verify_definitions(session, require_all=False)
        for name, (label, properties) in CONSTRAINTS.items():
            expression = ", ".join(f"n.{property_name}" for property_name in properties)
            summary = session.run(
                f"CREATE CONSTRAINT {name} IF NOT EXISTS FOR (n:{label}) "
                f"REQUIRE ({expression}) IS UNIQUE"
            ).consume()
            constraints_added += summary.counters.constraints_added
            indexes_added += summary.counters.indexes_added
        for name, (label, properties) in INDEXES.items():
            expression = ", ".join(f"n.{property_name}" for property_name in properties)
            summary = session.run(
                f"CREATE INDEX {name} IF NOT EXISTS FOR (n:{label}) ON ({expression})"
            ).consume()
            indexes_added += summary.counters.indexes_added
        session.run("CALL db.awaitIndexes($seconds)", seconds=60).consume()
        _verify_definitions(session, require_all=True)
    return {"constraints_added": constraints_added, "indexes_added": indexes_added}
