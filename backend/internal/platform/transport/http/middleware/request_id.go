// Package middleware provides HTTP middlewares for NEXORA API.
package middleware

import (
	"context"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

type requestIDKey struct{}

var (
	reqIDKey         = requestIDKey{}
	validRequestIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
)

// RequestID middleware extracts or generates a unique request ID for each HTTP request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incomingID := r.Header.Get("X-Request-ID")
		if incomingID == "" {
			incomingID = r.Header.Get("Request-Id")
		}

		var reqID string
		if incomingID != "" && validRequestIDRe.MatchString(incomingID) {
			reqID = incomingID
		} else {
			reqID = "req_" + uuid.New().String()
		}

		// Inject into response header
		w.Header().Set("X-Request-ID", reqID)

		// Inject into context
		ctx := context.WithValue(r.Context(), reqIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID extracts the request ID from context, or returns fallback if absent.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return "req_unknown"
	}
	if id, ok := ctx.Value(reqIDKey).(string); ok && id != "" {
		return id
	}
	return "req_unknown"
}
