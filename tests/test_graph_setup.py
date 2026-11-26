"""Schema setup safety and operator CLI checks without a Neo4j server."""

import json
import sys
from unittest.mock import MagicMock

import pytest
from knowledge_graph_service.infra.schema import CONSTRAINTS, INDEXES, ensure_schema
from knowledge_graph_service.presentation import setup
from neo4j import Driver


def definitions(command: str) -> list[dict[str, object]]:
    expected = CONSTRAINTS if command == "CONSTRAINTS" else INDEXES
    return [
        {
            "name": name,
            "type": "UNIQUENESS" if command == "CONSTRAINTS" else "RANGE",
            "entityType": "NODE",
            "labelsOrTypes": [label],
            "properties": list(properties),
        }
        for name, (label, properties) in expected.items()
    ]


def test_setup_uses_only_static_idempotent_ddl_and_no_data_markers() -> None:
    driver = MagicMock(spec=Driver)
    session = driver.session.return_value.__enter__.return_value
    summary = MagicMock()
    summary.counters.constraints_added = summary.counters.indexes_added = 0

    def execute(query: str, **parameters: object) -> object:
        if query.startswith("SHOW "):
            return definitions(query.split()[1])
        assert query.startswith(("CREATE CONSTRAINT", "CREATE INDEX", "CALL db.awaitIndexes"))
        if query.startswith("CREATE "):
            assert "IF NOT EXISTS" in query
        else:
            assert parameters == {"seconds": 60}
        result = MagicMock()
        result.consume.return_value = summary
        return result

    session.run.side_effect = execute
    assert ensure_schema(driver, database="private-graph") == {
        "constraints_added": 0,
        "indexes_added": 0,
    }
    driver.verify_connectivity.assert_called_once_with()
    driver.session.assert_called_once_with(database="private-graph")
    assert session.run.call_count == len(CONSTRAINTS) + len(INDEXES) + 5


@pytest.mark.parametrize("command", ["CONSTRAINTS", "INDEXES"])
def test_conflicting_schema_fails_before_any_ddl(command: str) -> None:
    driver = MagicMock(spec=Driver)
    session = driver.session.return_value.__enter__.return_value

    def execute(query: str) -> list[dict[str, object]]:
        assert query.startswith("SHOW "), "conflicts must be checked before changing schema"
        rows = definitions(query.split()[1])
        if query.startswith("SHOW " + command):
            rows[0]["labelsOrTypes"] = ["UnrelatedLabel"]
        return rows

    session.run.side_effect = execute
    with pytest.raises(ValueError, match="conflicts"):
        ensure_schema(driver)


@pytest.mark.parametrize("command", ["CONSTRAINTS", "INDEXES"])
def test_setup_does_not_report_success_when_required_item_is_missing(command: str) -> None:
    driver = MagicMock(spec=Driver)
    session = driver.session.return_value.__enter__.return_value

    def execute(query: str, **parameters: object) -> object:
        if query.startswith("SHOW "):
            rows = definitions(query.split()[1])
            return rows[1:] if query.startswith("SHOW " + command) else rows
        result = MagicMock()
        result.consume.return_value.counters.constraints_added = 0
        result.consume.return_value.counters.indexes_added = 0
        return result

    session.run.side_effect = execute
    with pytest.raises(ValueError, match="missing"):
        ensure_schema(driver)


def configure(monkeypatch: pytest.MonkeyPatch, uri: str = "bolt://localhost:7687") -> None:
    monkeypatch.setattr(sys, "argv", ["graph-setup"])
    monkeypatch.setenv("GRAPH_SCHEMA_URI", uri)
    monkeypatch.setenv("GRAPH_SCHEMA_USERNAME", "operator")
    monkeypatch.setenv("GRAPH_SCHEMA_PASSWORD", "synthetic-sensitive-password")


@pytest.mark.parametrize(
    "uri",
    [
        "",
        "http://localhost:7474",
        "bolt://user:secret@localhost:7687",
        "bolt://:secret@localhost:7687",
        "bolt://[malformed",
    ],
)
def test_cli_rejects_invalid_operator_settings_without_connecting(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str], uri: str
) -> None:
    configure(monkeypatch, uri)
    connect = MagicMock()
    monkeypatch.setattr(setup.GraphDatabase, "driver", connect)
    assert setup.main() == 1
    connect.assert_not_called()
    output = capsys.readouterr()
    assert "operator settings are required" in output.err
    assert "secret" not in output.err and "synthetic-sensitive-password" not in output.err


def test_cli_sanitizes_driver_errors(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    configure(monkeypatch)
    driver = MagicMock(spec=Driver)
    driver.verify_connectivity.side_effect = RuntimeError("synthetic-sensitive-password")
    context = MagicMock()
    context.__enter__.return_value = driver
    monkeypatch.setattr(setup.GraphDatabase, "driver", MagicMock(return_value=context))
    assert setup.main() == 1
    output = capsys.readouterr()
    assert "setup failed" in output.err
    assert "synthetic-sensitive-password" not in output.err


def test_cli_returns_schema_counts(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    configure(monkeypatch)
    monkeypatch.setattr(setup.GraphDatabase, "driver", MagicMock())
    monkeypatch.setattr(
        setup,
        "ensure_schema",
        lambda *args, **kwargs: {
            "constraints_added": 10,
            "indexes_added": 19,
        },
    )
    assert setup.main() == 0
    assert json.loads(capsys.readouterr().out) == {"constraints_added": 10, "indexes_added": 19}
