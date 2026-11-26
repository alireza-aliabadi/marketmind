package http

import (
	"encoding/json"
	"net/http"

	appHealth "marketmind/services/api-gateway/internal/application/health"
	domainHealth "marketmind/services/api-gateway/internal/domain/health"
)

// Handler serves HTTP endpoints for the service.
type Handler struct {
	healthUseCase *appHealth.UseCase
}

// NewHandler creates a new HTTP handler instance.
func NewHandler(healthUseCase *appHealth.UseCase) *Handler {
	return &Handler{
		healthUseCase: healthUseCase,
	}
}

// RegisterRoutes registers endpoints onto the provided ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /health/live", h.handleLiveness)
	mux.HandleFunc("GET /health/ready", h.handleReadiness)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := h.healthUseCase.GetReadiness()
	writeJSON(w, http.StatusOK, health)
}

func (h *Handler) handleLiveness(w http.ResponseWriter, r *http.Request) {
	status := h.healthUseCase.GetLiveness()
	writeJSON(w, http.StatusOK, map[string]string{"status": string(status)})
}

func (h *Handler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	health := h.healthUseCase.GetReadiness()
	statusCode := http.StatusOK
	if health.Status != domainHealth.StatusHealthy {
		statusCode = http.StatusServiceUnavailable
	}
	writeJSON(w, statusCode, health)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
