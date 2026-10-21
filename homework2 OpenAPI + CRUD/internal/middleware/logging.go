package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	identityKey  contextKey = "identity"
	logInfoKey   contextKey = "log_info"
)

type Identity struct {
	UserID uuid.UUID
	Role   string
}

type logInfo struct{ userID string }

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func IdentityFrom(ctx context.Context) (Identity, bool) {
	value, ok := ctx.Value(identityKey).(Identity)
	return value, ok
}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	if info, ok := ctx.Value(logInfoKey).(*logInfo); ok {
		info.userID = identity.UserID.String()
	}
	return context.WithValue(ctx, identityKey, identity)
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			info := &logInfo{}
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			ctx = context.WithValue(ctx, logInfoKey, info)
			r = r.WithContext(ctx)
			w.Header().Set("X-Request-Id", requestID)

			var body []byte
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				body, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
			}

			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			started := time.Now()
			next.ServeHTTP(recorder, r)

			attrs := []any{
				"request_id", requestID,
				"method", r.Method,
				"endpoint", r.URL.Path,
				"status_code", recorder.status,
				"duration_ms", float64(time.Since(started).Microseconds()) / 1000,
				"timestamp", started.UTC().Format(time.RFC3339Nano),
			}
			if info.userID != "" {
				attrs = append(attrs, "user_id", info.userID)
			} else {
				attrs = append(attrs, "user_id", nil)
			}
			if len(body) > 0 {
				attrs = append(attrs, "request_body", sanitizeBody(body))
			}
			logger.Info("api request", attrs...)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}

func sanitizeBody(body []byte) any {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return "<non-json-body>"
	}
	maskSensitive(value)
	return value
}

var sensitiveKeys = map[string]struct{}{
	"password": {}, "password_hash": {}, "refresh_token": {}, "access_token": {},
}

func maskSensitive(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if _, ok := sensitiveKeys[key]; ok {
				v[key] = "***"
				continue
			}
			maskSensitive(nested)
		}
	case []any:
		for _, nested := range v {
			maskSensitive(nested)
		}
	}
}
