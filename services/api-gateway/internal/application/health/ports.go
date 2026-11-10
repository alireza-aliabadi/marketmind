package health

import (
	domain "marketmind/services/api-gateway/internal/domain/health"
)

// CheckerPort defines boundary for health check evaluation.
type CheckerPort interface {
	CheckLiveness() domain.HealthStatus
	CheckReadiness() domain.SystemHealth
}
