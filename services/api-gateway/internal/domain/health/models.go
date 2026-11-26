package health

import "time"

// HealthStatus represents health state of a service or component.
type HealthStatus string

const (
	StatusHealthy   HealthStatus = "healthy"
	StatusDegraded  HealthStatus = "degraded"
	StatusUnhealthy HealthStatus = "unhealthy"
)

// ComponentHealth represents health descriptor for a subsystem component.
type ComponentHealth struct {
	Name    string                 `json:"name"`
	Status  HealthStatus           `json:"status"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// SystemHealth represents aggregated health status.
type SystemHealth struct {
	Status     HealthStatus      `json:"status"`
	Version    string            `json:"version"`
	Timestamp  string            `json:"timestamp"`
	Components []ComponentHealth `json:"components"`
}

// NewSystemHealth creates a SystemHealth instance with current UTC timestamp.
func NewSystemHealth(status HealthStatus, version string, components []ComponentHealth) SystemHealth {
	return SystemHealth{
		Status:     status,
		Version:    version,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Components: components,
	}
}
