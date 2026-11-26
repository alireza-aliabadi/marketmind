"""Infrastructure adapter implementing health check port."""

from src.application.health.ports import HealthCheckPort
from src.domain.health.models import ComponentHealth, HealthStatus, SystemHealth
from src.shared.config import Settings


class SystemHealthChecker(HealthCheckPort):
    """System health checker verifying process and local configuration."""

    def __init__(self, settings: Settings) -> None:
        self._settings = settings

    def check_liveness(self) -> HealthStatus:
        """Return healthy if process is executing."""
        return HealthStatus.HEALTHY

    def check_readiness(self) -> SystemHealth:
        """Verify readiness of configured services."""
        components: list[ComponentHealth] = [
            ComponentHealth(
                name="application",
                status=HealthStatus.HEALTHY,
                details={
                    "app_name": self._settings.app_name,
                    "env": self._settings.app_env,
                    "version": self._settings.app_version,
                },
            ),
            ComponentHealth(
                name="configuration",
                status=HealthStatus.HEALTHY,
                details={
                    "api_prefix": self._settings.api_v1_prefix,
                    "log_level": self._settings.log_level,
                },
            ),
        ]

        overall_status = HealthStatus.HEALTHY
        for c in components:
            if c.status == HealthStatus.UNHEALTHY:
                overall_status = HealthStatus.UNHEALTHY
                break
            if c.status == HealthStatus.DEGRADED:
                overall_status = HealthStatus.DEGRADED

        return SystemHealth(
            status=overall_status,
            version=self._settings.app_version,
            components=components,
        )
