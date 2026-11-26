"""Offline contract tooling; application and domain code do not depend on it."""

import argparse
import json
import re
import subprocess
from datetime import datetime
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator, FormatChecker
from openapi_spec_validator import validate

ROOT = Path(__file__).resolve().parents[1]
SEMVER = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")


def load_json(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected a JSON object")
    return value


def local_file(root: Path, name: str) -> Path:
    path = (root / name).resolve()
    if not path.is_relative_to(root.resolve()) or not path.is_file():
        raise ValueError(f"Invalid or missing local contract file: {name}")
    return path


def check_refs(value: Any) -> None:
    if isinstance(value, dict):
        reference = value.get("$ref")
        if reference is not None and (
            not isinstance(reference, str) or not reference.startswith("#/")
        ):
            raise ValueError(
                "Contract references must be local document fragments, not remote URLs"
            )
        for item in value.values():
            check_refs(item)
    elif isinstance(value, list):
        for item in value:
            check_refs(item)


def event_contracts(root: Path = ROOT) -> tuple[dict[str, Any], dict[str, Any]]:
    directory = root / "contracts" / "events"
    registry = load_json(directory / "registry.json")
    if registry.get("version") != 1 or not registry.get("events"):
        raise ValueError("The event registry must declare version 1 and at least one contract")
    envelope = load_json(local_file(directory, registry["envelope"]))
    Draft202012Validator.check_schema(envelope)
    check_refs(envelope)
    identifiers = {envelope["$id"]}
    for name, entry in registry["events"].items():
        schema = load_json(local_file(directory, entry["schema"]))
        Draft202012Validator.check_schema(schema)
        check_refs(schema)
        version = schema["$id"].rsplit(":", 1)[-1]
        if not SEMVER.fullmatch(version) or not name.endswith(f".v{version.split('.')[0]}"):
            raise ValueError(
                f"{name}: event major version and immutable schema identifier disagree"
            )
        if schema["$id"] in identifiers:
            raise ValueError(f"Duplicate schema identifier: {schema['$id']}")
        identifiers.add(schema["$id"])
        entry["definition"] = schema
    return envelope, registry


def validate_event(event: dict[str, Any], root: Path = ROOT) -> None:
    envelope, registry = event_contracts(root)
    Draft202012Validator(envelope, format_checker=FormatChecker()).validate(event)
    entry = registry["events"].get(event["type"])
    if entry is None:
        raise ValueError(f"Unsupported event type: {event['type']}")
    if event["dataschema"] != entry["definition"]["$id"]:
        raise ValueError("Unsupported payload schema identifier")
    if event["source"] != f"urn:marketmind:service:{entry['producer']}":
        raise ValueError("Event producer does not match the declared owner")
    Draft202012Validator(entry["definition"], format_checker=FormatChecker()).validate(
        event["data"]
    )
    if event["type"] == "filing.ingested.v1":
        data = event["data"]
        observed = datetime.fromisoformat(data["observed_at"])
        published = data["published_at"]
        if published is not None and datetime.fromisoformat(published) > observed:
            raise ValueError("Provider publication cannot follow observation time")
        if observed > datetime.fromisoformat(event["time"]):
            raise ValueError("Observation cannot follow event publication time")
    elif event["type"] in {
        "market.filings.raw.v1",
        "market.economics.raw.v1",
        "market.prices.raw.v1",
    }:
        published = datetime.fromisoformat(event["time"])

        def check_sources(value: Any) -> None:
            if isinstance(value, dict):
                source = value.get("metadata_source")
                if source is not None:
                    observed = datetime.fromisoformat(source["retrieved_at"])
                    if observed > published:
                        raise ValueError("Source observation cannot follow event publication")
                    if (
                        value.get("published_at") is not None
                        and datetime.fromisoformat(value["published_at"]) > observed
                    ):
                        raise ValueError("Filing publication cannot follow source observation")
                    if "time" in value and datetime.fromisoformat(value["time"]) > observed:
                        raise ValueError("Price period cannot follow source observation")
                    if "api_key" in source["url"].lower() or "apikey" in source["url"].lower():
                        raise ValueError("Credential-bearing source URL is forbidden")
                for child in value.values():
                    check_sources(child)
            elif isinstance(value, list):
                for child in value:
                    check_sources(child)

        check_sources(event["data"]["snapshot"])
    elif event["type"] == "market.ingestion.dlq.v1":
        if datetime.fromisoformat(event["data"]["failed_at"]) > datetime.fromisoformat(
            event["time"]
        ):
            raise ValueError("Failure cannot follow DLQ event publication")


def replay_identity(event: dict[str, Any]) -> tuple[str, str, str] | str:
    """Legacy artifact identity or the implemented raw snapshot's replay key."""
    data = event["data"]
    if event["type"].startswith("market."):
        return data.get("idempotency_key", data.get("failure_id"))
    return data["provider"], data["source_id"], data["artifact"]["sha256"]


def check_immutable_schemas(root: Path, base_ref: str) -> None:
    """Retain released schema identities; formatting-only changes remain harmless."""
    result = subprocess.run(
        ["git", "ls-tree", "-r", "--name-only", base_ref, "--", "contracts/events"],
        cwd=root,
        capture_output=True,
        text=True,
        check=True,
    )
    for name in result.stdout.splitlines():
        if not name.endswith(".json"):
            continue
        old = subprocess.run(
            ["git", "show", f"{base_ref}:{name}"],
            cwd=root,
            capture_output=True,
            text=True,
            check=True,
        )
        baseline = json.loads(old.stdout)
        if name == "contracts/events/registry.json" and isinstance(baseline, dict):
            current = load_json(root / name)
            if current.get("envelope") != baseline.get("envelope"):
                raise ValueError(
                    "The envelope binding requires an explicit registry/protocol migration"
                )
            for event_type, entry in baseline.get("events", {}).items():
                replacement = current.get("events", {}).get(event_type)
                if replacement is None or any(
                    replacement.get(field) != entry.get(field) for field in ("schema", "producer")
                ):
                    raise ValueError(f"Registered event {event_type} cannot be removed or rebound")
        if isinstance(baseline, dict) and "$id" in baseline:
            if not (root / name).is_file() or load_json(root / name) != baseline:
                raise ValueError(f"Released schema {name} is immutable; add a new version instead")


def check_contracts(root: Path = ROOT, base_ref: str | None = None) -> None:
    specifications = list((root / "contracts" / "openapi").glob("*.json"))
    services: set[str] = set()
    for path in specifications:
        spec = load_json(path)
        check_refs(spec)
        validate(spec, base_uri=path.as_uri())
        if spec.get("openapi") != "3.1.0" or not SEMVER.fullmatch(spec["info"]["version"]):
            raise ValueError(f"{path.name}: OpenAPI 3.1 and semantic API versions are required")
        service = spec["x-marketmind-service"]
        if service in services:
            raise ValueError(f"Duplicate API contract for {service}")
        services.add(service)
    if not {"api-gateway", "platform-sample"}.issubset(services):
        raise ValueError("Gateway and Python sample OpenAPI contracts are required")

    _, registry = event_contracts(root)
    ownership = load_json(root / "contracts" / "service-ownership.json")
    owners = {service["name"] for service in ownership["services"]}
    topics = {topic["name"]: topic for topic in ownership["topics"]}
    for name, entry in registry["events"].items():
        topic = topics.get(name)
        if entry["producer"] not in owners or not set(entry["consumers"]).issubset(owners):
            raise ValueError(f"{name}: unknown producer or consumer")
        if (
            topic is None
            or topic["publisher"] != entry["producer"]
            or set(topic["consumers"]) != set(entry["consumers"])
        ):
            raise ValueError(f"{name}: registry and service ownership disagree")
        example = load_json(local_file(root / "contracts" / "events", entry["example"]))
        validate_event(example, root)
    if base_ref:
        check_immutable_schemas(root, base_ref)
    print(
        f"PASS: {len(specifications)} OpenAPI specs and {len(registry['events'])} event contracts"
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate versioned, offline MarketMind contracts")
    parser.add_argument(
        "--base-ref", help="Git revision used to enforce released schema immutability"
    )
    args = parser.parse_args()
    check_contracts(base_ref=args.base_ref)


if __name__ == "__main__":
    main()
