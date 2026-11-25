package schema

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"cinema-analytics/internal/model"
	"github.com/hamba/avro/v2"
)

type Encoder struct {
	schemaID int
	schema   avro.Schema
}

type registerSchemaRequest struct {
	Schema     string `json:"schema"`
	SchemaType string `json:"schemaType"`
}

type registerSchemaResponse struct {
	ID int `json:"id"`
}

func NewEncoder(ctx context.Context, registryURL, subject, schemaPath string) (*Encoder, error) {
	rawSchema, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read avro schema %s: %w", schemaPath, err)
	}

	parsed, err := avro.Parse(string(rawSchema))
	if err != nil {
		return nil, fmt.Errorf("parse avro schema: %w", err)
	}

	schemaID, err := registerSchemaWithRetry(ctx, strings.TrimRight(registryURL, "/"), subject, string(rawSchema))
	if err != nil {
		return nil, fmt.Errorf("register schema in registry: %w", err)
	}

	return &Encoder{
		schemaID: schemaID,
		schema:   parsed,
	}, nil
}

func registerSchemaWithRetry(ctx context.Context, registryURL, subject, schemaStr string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := fmt.Sprintf("%s/subjects/%s/versions", registryURL, subject)

	bodyBytes, err := json.Marshal(registerSchemaRequest{
		Schema:     schemaStr,
		SchemaType: "AVRO",
	})
	if err != nil {
		return 0, err
	}

	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= 15; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")

		resp, err := client.Do(req)
		if err == nil {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				var parsed registerSchemaResponse
				if err := json.Unmarshal(respBody, &parsed); err != nil {
					return 0, fmt.Errorf("decode schema registry response: %w", err)
				}
				return parsed.ID, nil
			}
			lastErr = fmt.Errorf("schema registry HTTP %d: %s", resp.StatusCode, string(respBody))
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(backoff):
			if backoff < 5*time.Second {
				backoff *= 2
			}
		}
	}
	return 0, lastErr
}

func (e *Encoder) SchemaID() int {
	return e.schemaID
}

func (e *Encoder) Encode(ev *model.MovieEvent) ([]byte, error) {
	if err := ev.Validate(); err != nil {
		return nil, fmt.Errorf("event validation failed: %w", err)
	}

	payload, err := avro.Marshal(e.schema, ev)
	if err != nil {
		return nil, fmt.Errorf("avro marshal failed: %w", err)
	}

	out := make([]byte, 5+len(payload))
	out[0] = 0x00
	binary.BigEndian.PutUint32(out[1:5], uint32(e.schemaID))
	copy(out[5:], payload)
	return out, nil
}
