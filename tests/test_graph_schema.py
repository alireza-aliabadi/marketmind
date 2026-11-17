"""M3.1 identity/schema only; temporary probes are not M3.2 business ingestion."""

import os
import tempfile
import uuid
from collections.abc import Generator
from pathlib import Path

import pytest
from knowledge_graph_service.domain.identity import (
    NodeLabel,
    canonical_company_id,
    canonical_instrument_id,
    scoped_key,
)
from knowledge_graph_service.domain.schema import RELATIONSHIPS, RelationshipType
from knowledge_graph_service.infra.schema import GraphSchemaMigrator
from neo4j import Driver, GraphDatabase
from neo4j.exceptions import ConstraintError


def test_canonical_identities_and_scoped_keys() -> None:
    assert canonical_company_id("1045810") == "cik:0001045810"
    assert canonical_company_id(" 0001045810 ") == "cik:0001045810"
    assert canonical_instrument_id("bbg000000001") == "figi:BBG000000001"
    for raw in ["", "0", "0000000000", "-1", "１２３", "12345678901", "NVDA"]:
        with pytest.raises(ValueError):
            canonical_company_id(raw)
    with pytest.raises(ValueError):
        canonical_instrument_id("NVDA")
    assert scoped_key(NodeLabel.COMPANY, "cik:0001045810") == "global|Company|cik:0001045810"
    assert scoped_key(NodeLabel.CLAIM, "claim:fixture", "tenant:a") != scoped_key(
        NodeLabel.CLAIM, "claim:fixture", "tenant:b"
    )
    with pytest.raises(ValueError):
        scoped_key(NodeLabel.COMPANY, "cik:0001045810", "tenant:a")
    for scope in ["", "tenant:../a", "tenant:a|global"]:
        with pytest.raises(ValueError):
            scoped_key(NodeLabel.SOURCE, "source:fixture", scope)


def test_relationship_direction_specification() -> None:
    assert set(RELATIONSHIPS) == set(RelationshipType)
    assert RELATIONSHIPS[RelationshipType.ISSUED].sources == {NodeLabel.COMPANY}
    assert RELATIONSHIPS[RelationshipType.ISSUED].targets == {NodeLabel.INSTRUMENT}
    assert RELATIONSHIPS[RelationshipType.IMPACTS].sources == {NodeLabel.MARKET_EVENT}
    assert RELATIONSHIPS[RelationshipType.SUPPLIES].material
    assert RELATIONSHIPS[RelationshipType.COMPETES_WITH].symmetric
    assert RELATIONSHIPS[RelationshipType.SUPPORTED_BY].targets == {NodeLabel.SOURCE}
    assert not RELATIONSHIPS[RelationshipType.SUPPORTED_BY].material


@pytest.fixture(scope="module")
def graph() -> Generator[Driver, None, None]:
    if os.environ.get("RUN_GRAPH_SCHEMA_TESTS") != "1":
        pytest.skip("opt-in Neo4j Testcontainers schema test: make graph-schema-test")
    from testcontainers.core.container import DockerContainer
    from testcontainers.core.wait_strategies import ExecWaitStrategy

    password = "synthetic-schema-test-only"
    container = (
        DockerContainer("neo4j:5.26.33-community", name=f"marketmind-m31-{uuid.uuid4().hex[:16]}")
        .with_envs(
            NEO4J_AUTH=f"neo4j/{password}",
            NEO4J_server_memory_heap_initial__size="256m",
            NEO4J_server_memory_heap_max__size="512m",
            NEO4J_server_memory_pagecache_size="256m",
        )
        # Neo4j preallocates transaction logs for system/default databases. Use
        # image-owned anonymous volumes, removed by Testcontainers, not a 512m tmpfs.
        .with_kwargs(mem_limit="1g")
        .waiting_for(
            ExecWaitStrategy(
                [
                    "cypher-shell",
                    "--address",
                    "bolt://localhost:7687",
                    "--username",
                    "neo4j",
                    "--password",
                    password,
                    "RETURN 1",
                ]
            ).with_startup_timeout(180)
        )
    )
    container.ports["7687/tcp"] = ("127.0.0.1", None)
    with container:
        with GraphDatabase.driver(
            f"bolt://127.0.0.1:{container.get_exposed_port(7687)}",
            auth=("neo4j", password),
            connection_timeout=10,
        ) as driver:
            driver.verify_connectivity()
            yield driver


def test_additive_schema_migration_and_real_constraints(graph: Driver) -> None:
    with graph.session() as session:
        session.run("CREATE (:UserSchemaProbe {marker: 'preserve-unrelated-data'})").consume()
    migrator = GraphSchemaMigrator(graph)
    first = migrator.apply()[0]
    second = migrator.apply()[0]
    assert first.newly_recorded and first.constraints_added == 11
    assert not second.newly_recorded and second.constraints_added == 0 and second.indexes_added == 0
    with graph.session() as session:
        assert (
            session.run("MATCH (:UserSchemaProbe) RETURN count(*) AS count").single()["count"] == 1
        )
        names = {row["name"] for row in session.run("SHOW CONSTRAINTS YIELD name RETURN name")}
        assert len([name for name in names if name.startswith("mm_")]) == 11
        indexes = {
            row["name"]: row["state"]
            for row in session.run("SHOW INDEXES YIELD name,state RETURN name,state")
        }
        for name in [
            "mm_claim_valid_from",
            "mm_claim_valid_to",
            "mm_claim_observed_at",
            "mm_source_provider_id",
        ]:
            assert indexes[name] == "ONLINE"
        for label in NodeLabel:
            key = scoped_key(label, f"fixture:{label.value}")
            session.run(f"CREATE (n:{label.value} {{key: $key}})", key=key).consume()
            with pytest.raises(ConstraintError):
                session.run(f"CREATE (n:{label.value} {{key: $key}})", key=key).consume()
        for label, identifier in [
            (NodeLabel.COMPANY, "cik:0001045810"),
            (NodeLabel.INSTRUMENT, "figi:BBG000000001"),
        ]:
            session.run(
                f"CREATE (n:{label.value} {{key: $key, canonical_id: $id}})",
                key=scoped_key(label, identifier),
                id=identifier,
            ).consume()
            with pytest.raises(ConstraintError):
                session.run(
                    f"CREATE (n:{label.value} {{key: $key, canonical_id: $id}})",
                    key=f"different-key:{label.value}",
                    id=identifier,
                ).consume()
    print(
        "M3.1: 11 uniqueness constraints and online indexes; "
        "additive replay and canonical/scoped identities verified"
    )


def test_applied_graph_migration_cannot_be_rewritten(graph: Driver) -> None:
    GraphSchemaMigrator(graph).apply()
    source = (
        Path(__file__).resolve().parents[1]
        / "services/knowledge-graph-service/knowledge_graph_service/infra/migrations"
        / "001_graph_schema.cypher"
    )
    with tempfile.TemporaryDirectory(
        prefix="marketmind-m31-drift-", dir="/tmp/opencode"
    ) as directory:
        changed = Path(directory) / source.name
        changed.write_bytes(source.read_bytes() + b"\n// a rewritten released migration\n")
        with pytest.raises(ValueError, match="changed"):
            GraphSchemaMigrator(graph, directory=Path(directory)).apply()
    with graph.session() as session:
        assert (
            session.run("MATCH (m:GraphSchemaMigration) RETURN count(m) AS count").single()["count"]
            == 1
        )


def test_conflicting_reserved_schema_name_fails_without_overwriting(graph: Driver) -> None:
    GraphSchemaMigrator(graph).apply()
    with graph.session() as session:
        session.run("DROP CONSTRAINT mm_company_key").consume()
        session.run(
            "CREATE CONSTRAINT mm_company_key FOR (n:UnrelatedTestLabel) REQUIRE n.key IS UNIQUE"
        ).consume()
    try:
        with pytest.raises(ValueError, match="conflicts"):
            GraphSchemaMigrator(graph).apply()
        with graph.session() as session:
            row = session.run(
                "SHOW CONSTRAINTS YIELD name,labelsOrTypes WHERE name='mm_company_key' "
                "RETURN labelsOrTypes"
            ).single()
            assert row["labelsOrTypes"] == ["UnrelatedTestLabel"]
    finally:
        # Only the deliberately created test constraint in this private container.
        with graph.session() as session:
            session.run("DROP CONSTRAINT mm_company_key").consume()
        GraphSchemaMigrator(graph).apply()
