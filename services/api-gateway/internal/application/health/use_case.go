package health

import (
	domain "marketmind/services/api-gateway/internal/domain/health"
)

// UseCase coordinates health evaluation workflows.
type UseCase struct {
	checker CheckerPort
}

// NewUseCase creates an application health use case instance.
func NewUseCase(checker CheckerPort) *UseCase {
	return &UseCase{
		checker: checker,
	}
}

// GetLiveness returns whether the process is alive.
func (u *UseCase) GetLiveness() domain.HealthStatus {
	return u.checker.CheckLiveness()
}

// GetReadiness evaluates dependencies and returns aggregated readiness.
func (u *UseCase) GetReadiness() domain.SystemHealth {
	return u.checker.CheckReadiness()
}
