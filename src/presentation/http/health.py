"""Health check HTTP presentation endpoints."""

from typing import Any

from fastapi import APIRouter, Depends, status
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from src.application.health.ports import HealthCheckPort
from src.application.health.use_cases import HealthCheckUseCase
from src.domain.health.models import HealthStatus
from src.infra.health.system_checker import SystemHealthChecker
from src.shared.config import Settings, get_settings

router = APIRouter(tags=["Health"])


class ComponentHealthResponse(BaseModel):
    """Component health response schema."""

    name: str = Field(..., description="Name of the component")
    status: str = Field(..., description="Health status of the component")
    details: dict[str, Any] = Field(default_factory=dict, description="Component-specific details")


class SystemHealthResponse(BaseModel):
    """System health response schema."""

    status: str = Field(..., description="Aggregated system status")
    version: str = Field(..., description="Application version")
    timestamp: str = Field(..., description="ISO 8601 evaluation timestamp")
    components: list[ComponentHealthResponse] = Field(
        default_factory=list, description="Subsystem component statuses"
    )


class LivenessResponse(BaseModel):
    """Liveness probe response schema."""

    status: str = Field("healthy", description="Process liveness state")


def get_health_use_case(
    settings: Settings = Depends(get_settings),
) -> HealthCheckUseCase:
    """Dependency provider for HealthCheckUseCase."""
    checker: HealthCheckPort = SystemHealthChecker(settings)
    return HealthCheckUseCase(checker)


@router.get(
    "/health",
    response_model=SystemHealthResponse,
    summary="Service Health",
    description="Returns aggregated service health and component status.",
)
def get_health(
    use_case: HealthCheckUseCase = Depends(get_health_use_case),
) -> SystemHealthResponse:
    """Get aggregated health overview."""
    system_health = use_case.get_readiness()
    return SystemHealthResponse(
        status=system_health.status.value,
        version=system_health.version,
        timestamp=system_health.timestamp,
        components=[
            ComponentHealthResponse(
                name=c.name,
                status=c.status.value,
                details=c.details,
            )
            for c in system_health.components
        ],
    )


@router.get(
    "/health/live",
    response_model=LivenessResponse,
    summary="Liveness Probe",
    description="Kubernetes/container liveness probe returning 200 if process is running.",
)
def get_liveness(
    use_case: HealthCheckUseCase = Depends(get_health_use_case),
) -> LivenessResponse:
    """Get process liveness."""
    liveness_status = use_case.get_liveness()
    return LivenessResponse(status=liveness_status.value)


@router.get(
    "/health/ready",
    response_model=SystemHealthResponse,
    summary="Readiness Probe",
    description="Kubernetes readiness probe returning 200 when ready, 503 if degraded/unhealthy.",
)
def get_readiness(
    use_case: HealthCheckUseCase = Depends(get_health_use_case),
) -> JSONResponse:
    """Get service readiness for receiving traffic."""
    system_health = use_case.get_readiness()
    payload = SystemHealthResponse(
        status=system_health.status.value,
        version=system_health.version,
        timestamp=system_health.timestamp,
        components=[
            ComponentHealthResponse(
                name=c.name,
                status=c.status.value,
                details=c.details,
            )
            for c in system_health.components
        ],
    ).model_dump()

    http_status = (
        status.HTTP_200_OK
        if system_health.status == HealthStatus.HEALTHY
        else status.HTTP_503_SERVICE_UNAVAILABLE
    )
    return JSONResponse(status_code=http_status, content=payload)
