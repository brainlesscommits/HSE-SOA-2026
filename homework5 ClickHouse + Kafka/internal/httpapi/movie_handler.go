package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cinema-analytics/internal/model"
	"cinema-analytics/internal/producer"
	"github.com/google/uuid"
)

type MovieHandler struct {
	producer *producer.KafkaProducer
	logger   *slog.Logger
}

func NewMovieHandler(prod *producer.KafkaProducer, logger *slog.Logger) *MovieHandler {
	return &MovieHandler{
		producer: prod,
		logger:   logger,
	}
}

func (h *MovieHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("POST /api/v1/events", h.handleCreateEvent)
}

func (h *MovieHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *MovieHandler) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var ev model.MovieEvent
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ev); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON payload: " + err.Error(),
		})
		return
	}

	if strings.TrimSpace(ev.EventID) == "" {
		ev.EventID = uuid.NewString()
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	if err := ev.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if err := h.producer.Publish(r.Context(), &ev); err != nil {
		h.logger.Error("failed to publish event via API", "event_id", ev.EventID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to publish event to kafka",
		})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"event_id": ev.EventID,
		"status":   "published",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
