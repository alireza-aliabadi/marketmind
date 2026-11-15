"""Producer conformance checks exercise real handlers and frozen wire schemas."""

import json
from pathlib import Path
from typing import Any

import pytest
from fastapi.testclient import TestClient
from jsonschema import Draft202012Validator, FormatChecker, ValidationError
from openapi_spec_validator import validate

from contracts.validation import check_refs
from src.application.health.use_cases import HealthCheckUseCase
from src.domain.health.models import ComponentHealth, HealthStatus, SystemHealth
from src.presentation.http.health import get_health_use_case
from src.presentation.http.main import create_app
from src.shared.config import Settings

ROOT = Path(__file__).resolve().parents[1]


def reference(document: dict[str, Any], value: dict[str, Any]) -> dict[str, Any]:
    while "$ref" in value:
        target: Any = document
        for part in value["$ref"].removeprefix("#/").split("/"):
            target = target[part.replace("~1", "/").replace("~0", "~")]
        value = target
    return value


@pytest.mark.parametrize("name", ["api-gateway", "platform-sample"])
def test_openapi_spec_is_valid_and_offline(name: str) -> None:
    path = ROOT / f"contracts/openapi/{name}.json"
    document = json.loads(path.read_text())
    check_refs(document)
    validate(document, base_uri=path.as_uri())
    assert document["x-marketmind-service"] == name
    assert document["security"] == []


def test_gateway_sample_contract_is_protected_without_claiming_business_apis() -> None:
    document = json.loads((ROOT / "contracts/openapi/api-gateway.json").read_text())
    assert set(document["components"]["securitySchemes"]) == {"OidcAccessToken"}
    for path, operations in document["paths"].items():
        operation = operations["get"]
        if path.startswith("/api/v1/sample/"):
            assert operation["security"] == [{"OidcAccessToken": []}]
            assert operation["x-required-permission"] == "sample:read"
            assert {"200", "401", "403", "405", "502"}.issubset(operation["responses"])
        else:
            assert path.startswith("/health")
            assert operation.get("security", document["security"]) == []
    policies = {entry["prefix"]: entry for entry in document["x-marketmind-proxy-policies"]}
    assert policies["/api/v1/research"]["methods"]["POST"] == "research:write"
    assert policies["/api/v1/market"]["methods"] == {
        "GET": "market:read",
        "HEAD": "market:read",
    }


class Checker:
    def __init__(self, status: HealthStatus) -> None:
        self.status = status

    def check_liveness(self) -> HealthStatus:
        return HealthStatus.HEALTHY

    def check_readiness(self) -> SystemHealth:
        return SystemHealth(
            status=self.status,
            version="0.1.0",
            components=[ComponentHealth("fixture", self.status, {"diagnostic": "synthetic"})],
        )


@pytest.mark.parametrize("state", list(HealthStatus))
@pytest.mark.parametrize("path", ["/health", "/health/live", "/health/ready"])
def test_python_health_producer_matches_contract(state: HealthStatus, path: str) -> None:
    document = json.loads((ROOT / "contracts/openapi/platform-sample.json").read_text())
    app = create_app(Settings(_env_file=None, app_env="test", app_version="0.1.0"))
    app.dependency_overrides[get_health_use_case] = lambda: HealthCheckUseCase(Checker(state))
    with TestClient(app) as client:
        response = client.get(path, headers={"X-Request-ID": "contract-test"})
    expected = 503 if path == "/health/ready" and state != HealthStatus.HEALTHY else 200
    assert response.status_code == expected
    assert response.headers["content-type"].split(";")[0] == "application/json"
    contract = reference(document, document["paths"][path]["get"]["responses"][str(expected)])
    schema = contract["content"]["application/json"]["schema"]
    validator = Draft202012Validator(
        {**document, "$ref": schema["$ref"]}, format_checker=FormatChecker()
    )
    validator.validate(response.json())
    for header in contract["headers"]:
        assert response.headers[header]
    assert response.headers["X-Request-ID"] == "contract-test"
    assert response.headers["X-Correlation-ID"] == "contract-test"


def test_python_route_surface_matches_declared_contract() -> None:
    document = json.loads((ROOT / "contracts/openapi/platform-sample.json").read_text())
    app = create_app(Settings(_env_file=None))
    generated = app.openapi()
    assert set(generated["paths"]) == set(document["paths"])
    for path in document["paths"]:
        assert set(generated["paths"][path]) == set(document["paths"][path])


@pytest.mark.parametrize("mutation", ["missing", "wrong-type", "invalid-time", "extra-field"])
def test_health_contract_rejects_response_drift(mutation: str) -> None:
    document = json.loads((ROOT / "contracts/openapi/platform-sample.json").read_text())
    value: dict[str, Any] = {
        "status": "healthy",
        "version": "0.1.0",
        "timestamp": "2026-11-12T06:00:00Z",
        "components": [],
    }
    if mutation == "missing":
        del value["components"]
    elif mutation == "wrong-type":
        value["version"] = 1
    elif mutation == "invalid-time":
        value["timestamp"] = "not-a-date"
    else:
        value["uncontracted_secret"] = "fixture-only"
    validator = Draft202012Validator(
        {**document, "$ref": "#/components/schemas/SystemHealth"},
        format_checker=FormatChecker(),
    )
    with pytest.raises(ValidationError):
        validator.validate(value)
