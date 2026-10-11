// Package http provides the HTTP server foundation and probe endpoints.
package http

import (
	"encoding/json"
	"net/http"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// RespondJSON writes a JSON payload with the specified HTTP status code.
func RespondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// RespondError writes a canonical error response using apperr.RespondError.
func RespondError(w http.ResponseWriter, reqID string, err *apperr.AppError) {
	apperr.RespondError(w, reqID, err)
}

// WriteError inspects the request context for a request ID, converts the error
// using apperr.FromError, and writes the canonical error response.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	reqID := middleware.GetRequestID(r.Context())
	appErr := apperr.FromError(err)
	apperr.RespondError(w, reqID, appErr)
}
