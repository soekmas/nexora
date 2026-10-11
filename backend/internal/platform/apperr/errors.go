// Package apperr provides canonical NEXORA error structures, constructors, and serialization.
package apperr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrorDetail represents the structured error detail payload defined in OpenAPI components/schemas/ErrorDetail.
type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Field   string         `json:"field,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// ErrorResponse represents the top-level error envelope defined in OpenAPI components/schemas/ErrorResponse.
// It strictly guarantees error.code, error.message, error.field, error.details, and request_id.
type ErrorResponse struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

// AppError is an error implementing error interface and carrying HTTP status and structured detail.
type AppError struct {
	HTTPStatus int
	Detail     ErrorDetail
}

func (e *AppError) Error() string {
	return e.Detail.Message
}

// NewAppError constructs a new AppError with status, code, and message.
func NewAppError(status int, code string, message string) *AppError {
	return &AppError{
		HTTPStatus: status,
		Detail: ErrorDetail{
			Code:    code,
			Message: message,
		},
	}
}

// NewValidationError constructs an AppError for validation failures (HTTP 400).
func NewValidationError(field string, message string) *AppError {
	return &AppError{
		HTTPStatus: http.StatusBadRequest,
		Detail: ErrorDetail{
			Code:    "VALIDATION_ERROR",
			Message: message,
			Field:   field,
		},
	}
}

// NewBadRequestError constructs an AppError for malformed client requests (HTTP 400).
func NewBadRequestError(message string) *AppError {
	return NewAppError(http.StatusBadRequest, "BAD_REQUEST", message)
}

// NewUnauthorizedError constructs an AppError for missing or invalid authentication (HTTP 401).
func NewUnauthorizedError(message string) *AppError {
	if message == "" {
		message = "Authentication required"
	}
	return NewAppError(http.StatusUnauthorized, "UNAUTHORIZED", message)
}

// NewSessionRevokedError constructs an AppError for revoked sessions (HTTP 401).
func NewSessionRevokedError(message string) *AppError {
	if message == "" {
		message = "Session has been revoked"
	}
	return NewAppError(http.StatusUnauthorized, "SESSION_REVOKED", message)
}

// NewTokenExpiredError constructs an AppError for expired tokens (HTTP 401).
func NewTokenExpiredError(message string) *AppError {
	if message == "" {
		message = "Session token has expired"
	}
	return NewAppError(http.StatusUnauthorized, "TOKEN_EXPIRED", message)
}

// NewForbiddenError constructs an AppError for authorization or permission denials (HTTP 403).
func NewForbiddenError(message string) *AppError {
	if message == "" {
		message = "Access denied"
	}
	return NewAppError(http.StatusForbidden, "FORBIDDEN", message)
}

// NewScopeAccessDeniedError constructs an AppError for scope authorization denials (HTTP 403).
func NewScopeAccessDeniedError(message string, details map[string]any) *AppError {
	if message == "" {
		message = "Access to requested scope is denied"
	}
	err := NewAppError(http.StatusForbidden, "SCOPE_ACCESS_DENIED", message)
	err.Detail.Details = details
	return err
}

// NewTenantMismatchError constructs an AppError when client tenant header does not match session tenant (HTTP 403).
func NewTenantMismatchError(message string) *AppError {
	if message == "" {
		message = "Tenant header does not match authenticated session"
	}
	return NewAppError(http.StatusForbidden, "TENANT_MISMATCH", message)
}

// NewNotFoundError constructs an AppError for missing resources (HTTP 404).
func NewNotFoundError(message string) *AppError {
	if message == "" {
		message = "Resource not found"
	}
	return NewAppError(http.StatusNotFound, "NOT_FOUND", message)
}

// NewMethodNotAllowedError constructs an AppError when HTTP method is not supported (HTTP 405).
func NewMethodNotAllowedError(method string) *AppError {
	return &AppError{
		HTTPStatus: http.StatusMethodNotAllowed,
		Detail: ErrorDetail{
			Code:    "METHOD_NOT_ALLOWED",
			Message: fmt.Sprintf("Method %s not allowed for this endpoint", method),
		},
	}
}

// NewConflictError constructs an AppError for business state or unique conflicts (HTTP 409).
func NewConflictError(code string, message string) *AppError {
	if code == "" {
		code = "CONFLICT"
	}
	return NewAppError(http.StatusConflict, code, message)
}

// NewIdempotencyInProgressError constructs an AppError when an identical request is already being processed (HTTP 409).
func NewIdempotencyInProgressError(message string, details map[string]any) *AppError {
	if message == "" {
		message = "An identical request is currently being processed"
	}
	err := NewAppError(http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS", message)
	err.Detail.Details = details
	return err
}

// NewIdempotencyKeyReusedError constructs an AppError when an idempotency key is reused with a different payload (HTTP 409).
func NewIdempotencyKeyReusedError(message string, details map[string]any) *AppError {
	if message == "" {
		message = "Idempotency key was reused with a different request payload"
	}
	err := NewAppError(http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", message)
	err.Detail.Details = details
	return err
}

// NewRateLimitError constructs an AppError for exceeded request quotas (HTTP 429).
func NewRateLimitError(message string, retryAfterSeconds int) *AppError {
	if message == "" {
		message = "Too many requests. Please retry later."
	}
	err := NewAppError(http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", message)
	if retryAfterSeconds > 0 {
		err.Detail.Details = map[string]any{
			"retry_after_seconds": retryAfterSeconds,
		}
	}
	return err
}

// NewInternalError constructs an AppError for unexpected server faults (HTTP 500).
func NewInternalError(message string) *AppError {
	if message == "" {
		message = "An internal server error occurred"
	}
	return NewAppError(http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", message)
}

// NewTimeoutError constructs an AppError for request timeout (HTTP 504).
func NewTimeoutError(message string) *AppError {
	if message == "" {
		message = "The request timed out"
	}
	return NewAppError(http.StatusGatewayTimeout, "TIMEOUT_ERROR", message)
}

// FromError converts any Go error into a canonical AppError, preventing internal leakages.
func FromError(err error) *AppError {
	if err == nil {
		return nil
	}

	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return NewTimeoutError("Request deadline exceeded")
	}

	// Internal or unrecognized errors are masked
	return NewInternalError("An unexpected server error occurred")
}

// RespondError writes the canonical NEXORA ErrorResponse JSON to the response writer.
func RespondError(w http.ResponseWriter, requestID string, err *AppError) {
	if err == nil {
		err = NewInternalError("")
	}
	if requestID == "" {
		requestID = "req_unknown"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(err.HTTPStatus)

	resp := ErrorResponse{
		Error:     err.Detail,
		RequestID: requestID,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
