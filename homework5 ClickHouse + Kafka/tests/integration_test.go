package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestPipelineEndToEnd(t *testing.T) {
	movieServiceURL := envOr("MOVIE_SERVICE_URL", "http://localhost:8080")
	clickhouseAddr := envOr("CLICKHOUSE_ADDR", "localhost:9000")
	clickhouseDB := envOr("CLICKHOUSE_DB", "analytics")
	clickhouseUser := envOr("CLICKHOUSE_USER", "default")
	clickhousePass := envOr("CLICKHOUSE_PASSWORD", "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{clickhouseAddr},
		Auth: clickhouse.Auth{
			Database: clickhouseDB,
			Username: clickhouseUser,
			Password: clickhousePass,
		},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to open clickhouse connection: %v", err)
	}
	defer chConn.Close()

	if err := chConn.Ping(ctx); err != nil {
		t.Fatalf("failed to ping clickhouse: %v", err)
	}

	eventID := uuid.NewString()
	userID := "it-user-" + uuid.NewString()[:8]
	movieID := "it-movie-inception"
	sessionID := "it-sess-" + uuid.NewString()[:8]
	eventType := "VIEW_FINISHED"
	deviceType := "TV"
	progressSec := int32(5420)
	ts := time.Now().UTC().Truncate(time.Millisecond)

	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		_ = chConn.Exec(cleanCtx,
			"ALTER TABLE analytics.movie_events DELETE WHERE event_id = toUUID(?) SETTINGS mutations_sync = 1",
			eventID,
		)
	})

	payload := map[string]any{
		"event_id":         eventID,
		"user_id":          userID,
		"movie_id":         movieID,
		"event_type":       eventType,
		"timestamp":        ts.Format(time.RFC3339Nano),
		"device_type":      deviceType,
		"session_id":       sessionID,
		"progress_seconds": progressSec,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	resp, err := http.Post(movieServiceURL+"/api/v1/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/events failed: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		t.Fatalf("decode api response: %v", err)
	}
	if apiResp.EventID != eventID {
		t.Fatalf("expected event_id %s, got %s", eventID, apiResp.EventID)
	}

	var (
		gotEventID    uuid.UUID
		gotUserID     string
		gotMovieID    string
		gotEventType  string
		gotTimestamp  time.Time
		gotDeviceType string
		gotSessionID  string
		gotProgress   int32
	)

	deadline := time.Now().Add(30 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		row := chConn.QueryRow(ctx, `
			SELECT
				event_id,
				user_id,
				movie_id,
				toString(event_type),
				timestamp,
				toString(device_type),
				session_id,
				progress_seconds
			FROM analytics.movie_events
			WHERE event_id = toUUID(?)
			LIMIT 1
		`, eventID)

		if err := row.Scan(
			&gotEventID,
			&gotUserID,
			&gotMovieID,
			&gotEventType,
			&gotTimestamp,
			&gotDeviceType,
			&gotSessionID,
			&gotProgress,
		); err == nil {
			found = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !found {
		t.Fatalf("event %s did not appear in ClickHouse analytics.movie_events within timeout", eventID)
	}

	if gotEventID.String() != eventID {
		t.Errorf("event_id mismatch: want %s, got %s", eventID, gotEventID.String())
	}
	if gotUserID != userID {
		t.Errorf("user_id mismatch: want %s, got %s", userID, gotUserID)
	}
	if gotMovieID != movieID {
		t.Errorf("movie_id mismatch: want %s, got %s", movieID, gotMovieID)
	}
	if gotEventType != eventType {
		t.Errorf("event_type mismatch: want %s, got %s", eventType, gotEventType)
	}
	if gotDeviceType != deviceType {
		t.Errorf("device_type mismatch: want %s, got %s", deviceType, gotDeviceType)
	}
	if gotSessionID != sessionID {
		t.Errorf("session_id mismatch: want %s, got %s", sessionID, gotSessionID)
	}
	if gotProgress != progressSec {
		t.Errorf("progress_seconds mismatch: want %d, got %d", progressSec, gotProgress)
	}
	if !gotTimestamp.UTC().Equal(ts) {
		t.Errorf("timestamp mismatch: want %s, got %s", ts.Format(time.RFC3339Nano), gotTimestamp.UTC().Format(time.RFC3339Nano))
	}
}

func TestProducerValidationError(t *testing.T) {
	movieServiceURL := envOr("MOVIE_SERVICE_URL", "http://localhost:8080")

	invalidPayload := []byte(`{
		"event_id": "not-a-uuid",
		"user_id": "user-1",
		"movie_id": "movie-1",
		"event_type": "UNKNOWN_EVENT",
		"device_type": "MOBILE",
		"session_id": "sess-1",
		"progress_seconds": -5
	}`)

	resp, err := http.Post(movieServiceURL+"/api/v1/events", "application/json", bytes.NewReader(invalidPayload))
	if err != nil {
		t.Fatalf("POST /api/v1/events failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid event, got %d", resp.StatusCode)
	}
}

func TestAggregationAndS3ExportEndpoints(t *testing.T) {
	aggServiceURL := envOr("AGGREGATION_SERVICE_URL", "http://localhost:8081")
	today := time.Now().UTC().Format("2006-01-02")

	aggResp, err := http.Post(fmt.Sprintf("%s/api/v1/aggregations/run?date=%s", aggServiceURL, today), "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/aggregations/run failed: %v", err)
	}
	aggBody, _ := io.ReadAll(aggResp.Body)
	aggResp.Body.Close()
	if aggResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from aggregation endpoint, got %d: %s", aggResp.StatusCode, string(aggBody))
	}

	expResp, err := http.Post(fmt.Sprintf("%s/api/v1/export/run?date=%s", aggServiceURL, today), "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/export/run failed: %v", err)
	}
	expBody, _ := io.ReadAll(expResp.Body)
	expResp.Body.Close()
	if expResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from export endpoint, got %d: %s", expResp.StatusCode, string(expBody))
	}
}
