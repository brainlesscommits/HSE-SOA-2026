package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	EventViewStarted  = "VIEW_STARTED"
	EventViewFinished = "VIEW_FINISHED"
	EventViewPaused   = "VIEW_PAUSED"
	EventViewResumed  = "VIEW_RESUMED"
	EventLiked        = "LIKED"
	EventSearched     = "SEARCHED"
)

const (
	DeviceMobile  = "MOBILE"
	DeviceDesktop = "DESKTOP"
	DeviceTV      = "TV"
	DeviceTablet  = "TABLET"
)

var validEventTypes = map[string]struct{}{
	EventViewStarted:  {},
	EventViewFinished: {},
	EventViewPaused:   {},
	EventViewResumed:  {},
	EventLiked:        {},
	EventSearched:     {},
}

var validDeviceTypes = map[string]struct{}{
	DeviceMobile:  {},
	DeviceDesktop: {},
	DeviceTV:      {},
	DeviceTablet:  {},
}

type MovieEvent struct {
	EventID         string    `json:"event_id" avro:"event_id"`
	UserID          string    `json:"user_id" avro:"user_id"`
	MovieID         string    `json:"movie_id" avro:"movie_id"`
	EventType       string    `json:"event_type" avro:"event_type"`
	Timestamp       time.Time `json:"timestamp" avro:"timestamp"`
	DeviceType      string    `json:"device_type" avro:"device_type"`
	SessionID       string    `json:"session_id" avro:"session_id"`
	ProgressSeconds int32     `json:"progress_seconds" avro:"progress_seconds"`
}

func (e *MovieEvent) Validate() error {
	var errs []string

	if strings.TrimSpace(e.EventID) == "" {
		errs = append(errs, "event_id is required")
	} else if _, err := uuid.Parse(e.EventID); err != nil {
		errs = append(errs, fmt.Sprintf("event_id must be a valid UUID: %v", err))
	}

	if strings.TrimSpace(e.UserID) == "" {
		errs = append(errs, "user_id is required")
	}

	if strings.TrimSpace(e.MovieID) == "" {
		errs = append(errs, "movie_id is required")
	}

	if _, ok := validEventTypes[e.EventType]; !ok {
		errs = append(errs, fmt.Sprintf("invalid event_type %q", e.EventType))
	}

	if e.Timestamp.IsZero() {
		errs = append(errs, "timestamp is required (RFC3339 UTC)")
	}

	if _, ok := validDeviceTypes[e.DeviceType]; !ok {
		errs = append(errs, fmt.Sprintf("invalid device_type %q", e.DeviceType))
	}

	if strings.TrimSpace(e.SessionID) == "" {
		errs = append(errs, "session_id is required")
	}

	if e.ProgressSeconds < 0 {
		errs = append(errs, "progress_seconds must be >= 0")
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	e.Timestamp = e.Timestamp.UTC().Truncate(time.Millisecond)
	return nil
}
