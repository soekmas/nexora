package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"example.com/nexora/backend/internal/platform/apperr"
)

// Recovery catches unhandled panics, logs the stack trace internally, and renders a sanitized 500 ErrorResponse.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqID := GetRequestID(r.Context())
					stack := string(debug.Stack())

					logger.Error("Unhandled panic recovered in HTTP handler",
						slog.String("request_id", reqID),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("panic", fmt.Sprintf("%v", rec)),
						slog.String("stack", stack),
					)

					apperr.RespondError(w, reqID, apperr.NewInternalError("An unexpected server error occurred"))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
