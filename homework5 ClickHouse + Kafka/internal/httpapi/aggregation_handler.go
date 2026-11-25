package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cinema-analytics/internal/aggregator"
	"cinema-analytics/internal/exporter"
)

type AggregationHandler struct {
	service  *aggregator.Service
	exporter *exporter.S3Exporter
	logger   *slog.Logger
}

func NewAggregationHandler(svc *aggregator.Service, exp *exporter.S3Exporter, logger *slog.Logger) *AggregationHandler {
	return &AggregationHandler{
		service:  svc,
		exporter: exp,
		logger:   logger,
	}
}

func (h *AggregationHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("POST /api/v1/aggregations/run", h.handleRunAggregation)
	mux.HandleFunc("POST /api/v1/export/run", h.handleRunExport)
}

func (h *AggregationHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AggregationHandler) handleRunAggregation(w http.ResponseWriter, r *http.Request) {
	targetDate, err := parseDateParam(r.URL.Query().Get("date"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	res, err := h.service.RunForDate(r.Context(), targetDate)
	if err != nil {
		h.logger.Error("manual aggregation failed", "date", targetDate.Format("2006-01-02"), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, res)
}

func (h *AggregationHandler) handleRunExport(w http.ResponseWriter, r *http.Request) {
	targetDate, err := parseDateParam(r.URL.Query().Get("date"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	uri, err := h.exporter.ExportDate(r.Context(), targetDate)
	if err != nil {
		h.logger.Error("manual s3 export failed", "date", targetDate.Format("2006-01-02"), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"metric_date": targetDate.Format("2006-01-02"),
		"s3_uri":      uri,
		"status":      "exported",
	})
}

func parseDateParam(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
