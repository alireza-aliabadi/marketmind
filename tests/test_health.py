"""Unit and integration tests for health check endpoints and use cases."""

import asyncio
from unittest.mock import Mock, call
from uuid import UUID

import pytest
from fastapi.testclient import TestClient

from src.application.health.ports import HealthCheckPort
from src.application.health.use_cases import HealthCheckUseCase
from src.domain.health.models import ComponentHealth, HealthStatus, SystemHealth
from src.presentation.http.health import get_health_use_case
from src.presentation.http.main import create_app, lifespan
from src.shared.config import Settings


class FakeUnhealthyChecker(HealthCheckPort):
    """Test fake returning unhealthy system status."""

    def check_liveness(self) -> HealthStatus:
        return HealthStatus.HEALTHY

    def check_readiness(self) -> SystemHealth:
        return SystemHealth(
            status=HealthStatus.UNHEALTHY,
            version="0.1.0",
            components=[
                ComponentHealth(
                    name="database",
                    status=HealthStatus.UNHEALTHY,
                    details={"error": "connection refused"},
                )
            ],
        )


def test_get_health_endpoint(client: TestClient) -> None:
    """Verify /health returns 200 with aggregated status and components."""
    response = client.get("/health")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "healthy"
    assert data["version"] == "0.1.0"
    assert "timestamp" in data
    assert len(data["components"]) >= 2
    request_id = response.headers["X-Request-ID"]
    assert UUID(request_id).version == 4
    assert response.headers["X-Correlation-ID"] == request_id


def test_get_liveness_endpoint(client: TestClient) -> None:
    """Verify /health/live returns 200 with healthy status."""
    response = client.get("/health/live")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "healthy"


def test_get_readiness_endpoint_healthy(client: TestClient) -> None:
    """Verify /health/ready returns 200 when system is healthy."""
    response = client.get("/health/ready")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "healthy"
    assert len(data["components"]) > 0


def test_get_readiness_endpoint_unhealthy(test_settings: Settings) -> None:
    """Verify /health/ready returns 503 when a component is unhealthy."""
    app = create_app(settings=test_settings)
    app.dependency_overrides[get_health_use_case] = lambda: HealthCheckUseCase(
        FakeUnhealthyChecker()
    )

    with TestClient(app) as test_client:
        response = test_client.get("/health/ready")
        assert response.status_code == 503
        data = response.json()
        assert data["status"] == "unhealthy"
        assert data["components"][0]["name"] == "database"
        assert data["components"][0]["status"] == "unhealthy"


def test_request_id_propagation(client: TestClient) -> None:
    """Verify custom X-Request-ID is preserved and reflected in the response."""
    custom_id = "test-request-id-12345"
    response = client.get("/health/live", headers={"X-Request-ID": custom_id})
    assert response.status_code == 200
    assert response.headers.get("X-Request-ID") == custom_id
    assert response.headers.get("X-Correlation-ID") == custom_id


@pytest.mark.parametrize("request_id", [None, "test-request-id-12345"])
def test_correlation_id_propagation(client: TestClient, request_id: str | None) -> None:
    """Verify custom correlation IDs are preserved with generated or supplied request IDs."""
    correlation_id = "test-correlation-id-12345"
    headers = {"X-Correlation-ID": correlation_id}
    if request_id is not None:
        headers["X-Request-ID"] = request_id

    response = client.get("/health/live", headers=headers)

    assert response.status_code == 200
    assert response.headers["X-Correlation-ID"] == correlation_id
    if request_id is None:
        assert UUID(response.headers["X-Request-ID"]).version == 4
    else:
        assert response.headers["X-Request-ID"] == request_id


def test_health_use_case_logic() -> None:
    """Direct unit test of HealthCheckUseCase logic."""
    fake_checker = FakeUnhealthyChecker()
    use_case = HealthCheckUseCase(fake_checker)

    assert use_case.get_liveness() == HealthStatus.HEALTHY
    readiness = use_case.get_readiness()
    assert readiness.status == HealthStatus.UNHEALTHY
    assert len(readiness.components) == 1


@pytest.mark.parametrize("raises_error", [False, True])
def test_lifespan_logs_shutdown_on_exit(
    test_settings: Settings, monkeypatch: pytest.MonkeyPatch, raises_error: bool
) -> None:
    """Verify shutdown logging on normal and exceptional lifespan exit."""
    logger = Mock()
    setup_logging = Mock()
    monkeypatch.setattr("src.presentation.http.main.logger", logger)
    monkeypatch.setattr("src.presentation.http.main.setup_logging", setup_logging)
    app = create_app(settings=test_settings)
    error = RuntimeError("lifespan failure")
    startup_call = call(
        "Application starting up",
        app_name=test_settings.app_name,
        env=test_settings.app_env,
        version=test_settings.app_version,
    )

    async def run_lifespan() -> None:
        async with lifespan(app):
            setup_logging.assert_called_once_with(
                log_level=test_settings.log_level, app_env=test_settings.app_env
            )
            assert logger.info.call_args_list == [startup_call]
            if raises_error:
                raise error

    if raises_error:
        with pytest.raises(RuntimeError) as exc_info:
            asyncio.run(run_lifespan())
        assert exc_info.value is error
    else:
        asyncio.run(run_lifespan())

    assert logger.info.call_args_list == [
        startup_call,
        call("Application shutting down", app_name=test_settings.app_name),
    ]
