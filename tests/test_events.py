"""Versioned events reject malformed, ownerless, or temporally invalid evidence."""

import copy
import json
from pathlib import Path
from typing import Any

import pytest
from jsonschema import ValidationError

from contracts.validation import (
    ROOT,
    check_immutable_schemas,
    check_refs,
    event_contracts,
    replay_identity,
    validate_event,
)


@pytest.fixture
def filing_event() -> dict[str, Any]:
    return json.loads((ROOT / "contracts/events/examples/filing.ingested.v1.json").read_text())


def test_valid_filing_contract(filing_event: dict[str, Any]) -> None:
    validate_event(filing_event)


def test_unknown_publication_time_is_preserved(filing_event: dict[str, Any]) -> None:
    filing_event["data"]["published_at"] = None
    validate_event(filing_event)


@pytest.mark.parametrize(
    ("path", "value"),
    [
        (("id",), "not-a-uuid"),
        (("type",), "unregistered.event.v1"),
        (("dataschema",), "urn:marketmind:events:filing.ingested:2.0.0"),
        (("source",), "urn:marketmind:service:identity-service"),
        (("correlationid",), ""),
        (("time",), "2026-11-12T06:00:02+00:00"),
        (("data", "artifact", "sha256"), "short"),
        (("data", "source_url"), "http://sec.example/fixture"),
        (("data", "source_id"), "invalid-accession"),
        (("data", "published_at"), "2026-11-12T10:00:00Z"),
        (("data", "observed_at"), "2026-11-12T10:00:00Z"),
    ],
)
def test_invalid_filing_evidence(
    filing_event: dict[str, Any], path: tuple[str, ...], value: Any
) -> None:
    target = filing_event
    for field in path[:-1]:
        target = target[field]
    target[path[-1]] = value
    with pytest.raises((ValueError, ValidationError)):
        validate_event(filing_event)


@pytest.mark.parametrize("field", ["id", "source", "dataschema", "correlationid", "data"])
def test_missing_required_envelope_field(filing_event: dict[str, Any], field: str) -> None:
    del filing_event[field]
    with pytest.raises(ValidationError):
        validate_event(filing_event)


def test_uncontracted_pii_is_rejected(filing_event: dict[str, Any]) -> None:
    filing_event["data"]["user_email"] = "fixture@example.com"
    with pytest.raises(ValidationError):
        validate_event(filing_event)


def test_schema_references_cannot_fetch_remote_resources() -> None:
    with pytest.raises(ValueError, match="local"):
        check_refs({"properties": {"payload": {"$ref": "https://untrusted.example/schema"}}})


def test_registry_rejects_path_escape(tmp_path: Path) -> None:
    directory = tmp_path / "contracts/events"
    directory.mkdir(parents=True)
    registry = {"version": 1, "envelope": "../../outside.json", "events": {"x.v1": {}}}
    (directory / "registry.json").write_text(json.dumps(registry))
    with pytest.raises(ValueError, match="local"):
        event_contracts(tmp_path)


def test_replay_identity_is_deterministic(filing_event: dict[str, Any]) -> None:
    retry = copy.deepcopy(filing_event)
    retry["time"] = "2026-11-12T06:01:00Z"
    validate_event(retry)
    assert retry["id"] == filing_event["id"]
    assert replay_identity(retry) == replay_identity(filing_event)


@pytest.mark.parametrize("mutation", ["change", "remove", "replace-id"])
def test_released_schema_is_immutable(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, mutation: str
) -> None:
    from subprocess import CompletedProcess

    baseline = {"$id": "urn:marketmind:test:1.0.0", "type": "object"}
    name = "contracts/events/released.json"
    path = tmp_path / name
    path.parent.mkdir(parents=True)
    current = dict(baseline)
    if mutation == "change":
        current["type"] = "string"
    elif mutation == "replace-id":
        current["$id"] = "urn:marketmind:test:2.0.0"
    if mutation != "remove":
        path.write_text(json.dumps(current))

    def git(command: list[str], **_: Any) -> CompletedProcess[str]:
        output = name + "\n" if command[1] == "ls-tree" else json.dumps(baseline)
        return CompletedProcess(command, 0, stdout=output, stderr="")

    monkeypatch.setattr("contracts.validation.subprocess.run", git)
    with pytest.raises(ValueError, match="immutable"):
        check_immutable_schemas(tmp_path, "base-sha")


def test_schema_formatting_does_not_break_immutability(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    from subprocess import CompletedProcess

    baseline = {"$id": "urn:marketmind:test:1.0.0", "type": "object"}
    name = "contracts/events/released.json"
    path = tmp_path / name
    path.parent.mkdir(parents=True)
    path.write_text(json.dumps(baseline, indent=4))

    def git(command: list[str], **_: Any) -> CompletedProcess[str]:
        output = name + "\n" if command[1] == "ls-tree" else json.dumps(baseline)
        return CompletedProcess(command, 0, stdout=output, stderr="")

    monkeypatch.setattr("contracts.validation.subprocess.run", git)
    check_immutable_schemas(tmp_path, "base-sha")


@pytest.mark.parametrize("mutation", ["schema", "producer", "remove", "envelope"])
def test_registered_event_binding_cannot_be_rewritten(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, mutation: str
) -> None:
    from subprocess import CompletedProcess

    baseline = {
        "envelope": "envelope.json",
        "events": {"filing.ingested.v1": {"schema": "v1.json", "producer": "market-data-service"}},
    }
    current = copy.deepcopy(baseline)
    if mutation == "envelope":
        current["envelope"] = "envelope-v2.json"
    elif mutation == "remove":
        current["events"] = {}
    else:
        current["events"]["filing.ingested.v1"][mutation] = "changed-without-a-new-type"
    name = "contracts/events/registry.json"
    path = tmp_path / name
    path.parent.mkdir(parents=True)
    path.write_text(json.dumps(current))

    def git(command: list[str], **_: Any) -> CompletedProcess[str]:
        output = name + "\n" if command[1] == "ls-tree" else json.dumps(baseline)
        return CompletedProcess(command, 0, stdout=output, stderr="")

    monkeypatch.setattr("contracts.validation.subprocess.run", git)
    with pytest.raises(ValueError, match="migration|rebound"):
        check_immutable_schemas(tmp_path, "base-sha")
