"""Health check interface ports."""

from typing import Protocol

from src.domain.health.models import HealthStatus, SystemHealth


class HealthCheckPort(Protocol):
    """Port for probing system and component health."""

    def check_liveness(self) -> HealthStatus:
        """Verify the service process is alive and responding."""
        ...

    def check_readiness(self) -> SystemHealth:
        """Verify the service dependencies and configuration are ready to serve traffic."""
        ...
