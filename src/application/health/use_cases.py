"""Health check use cases."""

from src.application.health.ports import HealthCheckPort
from src.domain.health.models import HealthStatus, SystemHealth


class HealthCheckUseCase:
    """Application use case for evaluating service health."""

    def __init__(self, health_checker: HealthCheckPort) -> None:
        self._health_checker = health_checker

    def get_liveness(self) -> HealthStatus:
        """Execute liveness probe."""
        return self._health_checker.check_liveness()

    def get_readiness(self) -> SystemHealth:
        """Execute readiness probe evaluating components."""
        return self._health_checker.check_readiness()
