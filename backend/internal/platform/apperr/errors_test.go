package apperr_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/nexora/backend/internal/platform/apperr"
)

func TestAppError_RespondError(t *testing.T) {
	appErr := &apperr.AppError{
		HTTPStatus: http.StatusBadRequest,
		Detail: apperr.ErrorDetail{
			Code:    "INVALID_ORDER_STATE",
			Message: "Order cannot be confirmed in DRAFT state",
			Field:   "status",
			Details: map[string]any{
				"current_status": "DRAFT",
			},
		},
	}

	rec := httptest.NewRecorder()
	requestID := "req_test_12345"

	apperr.RespondError(rec, requestID, appErr)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", contentType)
	}

	var response apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.RequestID != requestID {
		t.Errorf("expected request_id %q, got %q", requestID, response.RequestID)
	}
	if response.Error.Code != "INVALID_ORDER_STATE" {
		t.Errorf("expected error.code %q, got %q", "INVALID_ORDER_STATE", response.Error.Code)
	}
	if response.Error.Field != "status" {
		t.Errorf("expected error.field %q, got %q", "status", response.Error.Field)
	}
	if response.Error.Message != "Order cannot be confirmed in DRAFT state" {
		t.Errorf("expected error.message %q, got %q", "Order cannot be confirmed in DRAFT state", response.Error.Message)
	}
}

func TestAppError_Constructors(t *testing.T) {
	tests := []struct {
		name       string
		err        *apperr.AppError
		wantStatus int
		wantCode   string
		wantMsg    string
		wantField  string
	}{
		{
			name:       "validation error",
			err:        apperr.NewValidationError("email", "email is required"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
			wantMsg:    "email is required",
			wantField:  "email",
		},
		{
			name:       "bad request error",
			err:        apperr.NewBadRequestError("malformed json"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
			wantMsg:    "malformed json",
		},
		{
			name:       "unauthorized error default",
			err:        apperr.NewUnauthorizedError(""),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
			wantMsg:    "Authentication required",
		},
		{
			name:       "forbidden error default",
			err:        apperr.NewForbiddenError(""),
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
			wantMsg:    "Access denied",
		},
		{
			name:       "not found error default",
			err:        apperr.NewNotFoundError(""),
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
			wantMsg:    "Resource not found",
		},
		{
			name:       "method not allowed",
			err:        apperr.NewMethodNotAllowedError("POST"),
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "METHOD_NOT_ALLOWED",
			wantMsg:    "Method POST not allowed for this endpoint",
		},
		{
			name:       "conflict error default code",
			err:        apperr.NewConflictError("", "version conflict"),
			wantStatus: http.StatusConflict,
			wantCode:   "CONFLICT",
			wantMsg:    "version conflict",
		},
		{
			name:       "conflict error custom code",
			err:        apperr.NewConflictError("IDEMPOTENCY_CONFLICT", "duplicate key with different payload"),
			wantStatus: http.StatusConflict,
			wantCode:   "IDEMPOTENCY_CONFLICT",
			wantMsg:    "duplicate key with different payload",
		},
		{
			name:       "rate limit error default",
			err:        apperr.NewRateLimitError("", 60),
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "RATE_LIMIT_EXCEEDED",
			wantMsg:    "Too many requests. Please retry later.",
		},
		{
			name:       "internal error default",
			err:        apperr.NewInternalError(""),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
			wantMsg:    "An internal server error occurred",
		},
		{
			name:       "timeout error default",
			err:        apperr.NewTimeoutError(""),
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   "TIMEOUT_ERROR",
			wantMsg:    "The request timed out",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.HTTPStatus != tc.wantStatus {
				t.Errorf("status = %d; want %d", tc.err.HTTPStatus, tc.wantStatus)
			}
			if tc.err.Detail.Code != tc.wantCode {
				t.Errorf("code = %q; want %q", tc.err.Detail.Code, tc.wantCode)
			}
			if tc.err.Detail.Message != tc.wantMsg {
				t.Errorf("message = %q; want %q", tc.err.Detail.Message, tc.wantMsg)
			}
			if tc.wantField != "" && tc.err.Detail.Field != tc.wantField {
				t.Errorf("field = %q; want %q", tc.err.Detail.Field, tc.wantField)
			}
			if tc.err.Error() != tc.wantMsg {
				t.Errorf("Error() = %q; want %q", tc.err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestAppError_FromError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if apperr.FromError(nil) != nil {
			t.Fatal("expected nil from nil error")
		}
	})

	t.Run("existing AppError unwrapped", func(t *testing.T) {
		orig := apperr.NewNotFoundError("user not found")
		wrapped := fmt.Errorf("service layer: %w", orig)
		conv := apperr.FromError(wrapped)
		if conv.HTTPStatus != http.StatusNotFound {
			t.Errorf("expected 404, got %d", conv.HTTPStatus)
		}
		if conv.Detail.Code != "NOT_FOUND" {
			t.Errorf("expected NOT_FOUND, got %q", conv.Detail.Code)
		}
	})

	t.Run("context DeadlineExceeded converted to timeout", func(t *testing.T) {
		conv := apperr.FromError(context.DeadlineExceeded)
		if conv.HTTPStatus != http.StatusGatewayTimeout {
			t.Errorf("expected 504, got %d", conv.HTTPStatus)
		}
		if conv.Detail.Code != "TIMEOUT_ERROR" {
			t.Errorf("expected TIMEOUT_ERROR, got %q", conv.Detail.Code)
		}
	})

	t.Run("unrecognized internal error is masked", func(t *testing.T) {
		rawErr := errors.New("FATAL: connection to postgres failed on 10.0.0.1:5432 with password secret123")
		conv := apperr.FromError(rawErr)
		if conv.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", conv.HTTPStatus)
		}
		if conv.Detail.Code != "INTERNAL_SERVER_ERROR" {
			t.Errorf("expected INTERNAL_SERVER_ERROR, got %q", conv.Detail.Code)
		}
		// Confirm secret is NOT leaked in message
		if conv.Detail.Message == rawErr.Error() {
			t.Errorf("internal error raw message was leaked: %q", conv.Detail.Message)
		}
	})
}

func TestAppError_RespondErrorFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	apperr.RespondError(rec, "", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.RequestID != "req_unknown" {
		t.Errorf("expected req_unknown, got %q", resp.RequestID)
	}
	if resp.Error.Code != "INTERNAL_SERVER_ERROR" {
		t.Errorf("expected INTERNAL_SERVER_ERROR, got %q", resp.Error.Code)
	}
}
