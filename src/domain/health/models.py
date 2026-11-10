"""Health domain models."""

from dataclasses import dataclass, field
from datetime import UTC, datetime
from enum import StrEnum
from typing import Any


class HealthStatus(StrEnum):
    """Health status levels."""

    HEALTHY = "healthy"
    DEGRADED = "degraded"
    UNHEALTHY = "unhealthy"


@dataclass(frozen=True)
class ComponentHealth:
    """Individual system component health descriptor."""

    name: str
    status: HealthStatus
    details: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True)
class SystemHealth:
    """Aggregated health status for the service."""

    status: HealthStatus
    version: str
    components: list[ComponentHealth] = field(default_factory=list)
    timestamp: str = field(default_factory=lambda: datetime.now(UTC).isoformat())
