"""Explicit additive schema operations; no entity, claim or relationship ingestion."""

from dataclasses import asdict, dataclass
from hashlib import sha256
from pathlib import Path

from neo4j import Driver, Session

CONSTRAINTS: dict[str, tuple[str, tuple[str, ...]]] = {
    "mm_graph_migration_version": ("GraphSchemaMigration", ("version",)),
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
INDEXES: dict[str, tuple[str, tuple[str, ...]]] = {
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
    for command, expected in [("CONSTRAINTS", CONSTRAINTS), ("INDEXES", INDEXES)]:
        rows = {
            str(row["name"]): row
            for row in session.run(
                f"SHOW {command} YIELD name,type,entityType,labelsOrTypes,properties "
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


@dataclass(frozen=True)
class MigrationResult:
    version: int
    checksum: str
    newly_recorded: bool
    constraints_added: int
    indexes_added: int

    def summary(self) -> dict[str, object]:
        return asdict(self)


class GraphSchemaMigrator:
    def __init__(
        self, driver: Driver, database: str = "neo4j", directory: Path | None = None
    ) -> None:
        self._driver = driver
        self._database = database
        self._directory = directory or Path(__file__).parent / "migrations"

    def apply(self) -> list[MigrationResult]:
        self._driver.verify_connectivity()
        paths = sorted(self._directory.glob("[0-9][0-9][0-9]_*.cypher"))
        if not paths:
            raise ValueError("no reviewed graph migrations found")
        versions = {int(path.name.split("_", 1)[0]) for path in paths}
        if len(versions) != len(paths):
            raise ValueError("duplicate graph migration version")
        results: list[MigrationResult] = []
        with self._driver.session(database=self._database) as session:
            _verify_definitions(session, require_all=False)
            installed = {
                int(row["version"]): str(row["sha256"])
                for row in session.run(
                    "MATCH (m:GraphSchemaMigration) RETURN m.version AS version, m.sha256 AS sha256"
                )
            }
            if set(installed) - versions:
                raise ValueError(
                    "database contains unknown graph migration versions; do not downgrade"
                )
            for path in paths:
                version = int(path.name.split("_", 1)[0])
                body = path.read_bytes()
                checksum = sha256(body).hexdigest()
                if version in installed and installed[version] != checksum:
                    raise ValueError(
                        f"applied graph migration {version} changed; add a new version"
                    )
                constraints_added = indexes_added = 0
                # Neo4j schema DDL and data markers cannot share a transaction.
                # Each operation is additive/IF NOT EXISTS; partial application is
                # safely resumable. Record completion only after all indexes are ready.
                for statement in body.decode("utf-8").split(";"):
                    if statement.strip():
                        summary = session.run(statement).consume()
                        constraints_added += summary.counters.constraints_added
                        indexes_added += summary.counters.indexes_added
                session.run("CALL db.awaitIndexes($seconds)", seconds=60).consume()
                _verify_definitions(session, require_all=True)
                row = session.run(
                    "MERGE (m:GraphSchemaMigration {version: $version}) "
                    "ON CREATE SET m.sha256=$checksum, m.applied_at=datetime() "
                    "RETURN m.sha256 AS sha256",
                    version=version,
                    checksum=checksum,
                ).single(strict=True)
                if row is None or row["sha256"] != checksum:
                    raise ValueError("concurrent graph migration checksum conflict")
                results.append(
                    MigrationResult(
                        version,
                        checksum,
                        version not in installed,
                        constraints_added,
                        indexes_added,
                    )
                )
        return results
