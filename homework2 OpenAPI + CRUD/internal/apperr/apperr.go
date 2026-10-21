package apperr

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"marketplace/internal/api"
)

type Error struct {
	Status  int
	Code    api.ErrorCode
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func New(status int, code api.ErrorCode, message string, details map[string]any) *Error {
	return &Error{Status: status, Code: code, Message: message, Details: details}
}

func Validation(message string, details map[string]any) *Error {
	return New(http.StatusBadRequest, api.VALIDATIONERROR, message, details)
}

func ValidationField(field, reason string) *Error {
	return Validation("request validation failed", map[string]any{
		"fields": []map[string]string{{"field": field, "reason": reason}},
	})
}

func AccessDenied() *Error {
	return New(http.StatusForbidden, api.ACCESSDENIED, "insufficient permissions", nil)
}

func Internal() *Error {
	return New(http.StatusInternalServerError, api.INTERNALERROR, "internal server error", nil)
}

func Write(w http.ResponseWriter, err error) {
	var appErr *Error
	if !errors.As(err, &appErr) {
		slog.Error("unhandled error", "error", err)
		appErr = Internal()
	}
	body := api.ErrorResponse{ErrorCode: appErr.Code, Message: appErr.Message}
	if appErr.Details != nil {
		body.Details = &appErr.Details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.Status)
	_ = json.NewEncoder(w).Encode(body)
}
