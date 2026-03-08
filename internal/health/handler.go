package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is an interface for checking database connectivity.
// *pgxpool.Pool satisfies this interface.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Response is the JSON response from the health endpoint.
type Response struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Error     string `json:"error,omitempty"`
}

// Handler is the HTTP handler for the /health endpoint.
type Handler struct {
	pinger Pinger
}

// NewHandler creates a new health check handler.
func NewHandler(pinger Pinger) *Handler {
	return &Handler{pinger: pinger}
}

// ServeHTTP handles HTTP requests to the health endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	resp := Response{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if err := h.pinger.Ping(ctx); err != nil {
		resp.Status = "unhealthy"
		resp.Error = err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		resp.Status = "healthy"
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("health handler failed to encode response", "error", err)
	}
}

// RegisterHandlers wires the health and metrics endpoints onto the provided mux.
// The metrics handler is optional and only registered when non-nil.
func RegisterHandlers(mux *http.ServeMux, pinger Pinger, metricsHandler http.Handler) {
	if mux == nil {
		return
	}

	var healthHandler http.Handler
	if pinger == nil {
		healthHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			_ = reqCtx
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			resp := Response{
				Status:    "unhealthy",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "health pinger not configured",
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				slog.Error("health handler failed to encode response", "error", err)
			}
		})
	} else {
		healthHandler = NewHandler(pinger)
	}

	mux.Handle("/health", healthHandler)
	if metricsHandler != nil {
		mux.Handle("/metrics", metricsHandler)
	}
}
