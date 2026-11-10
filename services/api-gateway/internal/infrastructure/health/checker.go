package health

import (
	domain "marketmind/services/api-gateway/internal/domain/health"
)

// SystemChecker evaluates local process and configuration health.
type SystemChecker struct {
	appName string
	version string
	env     string
}

// NewSystemChecker creates a new system checker instance.
func NewSystemChecker(appName, version, env string) *SystemChecker {
	return &SystemChecker{
		appName: appName,
		version: version,
		env:     env,
	}
}

// CheckLiveness verifies the process is running.
func (c *SystemChecker) CheckLiveness() domain.HealthStatus {
	return domain.StatusHealthy
}

// CheckReadiness checks component configuration and readiness.
func (c *SystemChecker) CheckReadiness() domain.SystemHealth {
	components := []domain.ComponentHealth{
		{
			Name:   "gateway_engine",
			Status: domain.StatusHealthy,
			Details: map[string]interface{}{
				"app": c.appName,
				"env": c.env,
			},
		},
		{
			Name:   "router",
			Status: domain.StatusHealthy,
		},
	}

	overall := domain.StatusHealthy
	for _, comp := range components {
		if comp.Status == domain.StatusUnhealthy {
			overall = domain.StatusUnhealthy
			break
		}
		if comp.Status == domain.StatusDegraded {
			overall = domain.StatusDegraded
		}
	}

	return domain.NewSystemHealth(overall, c.version, components)
}
