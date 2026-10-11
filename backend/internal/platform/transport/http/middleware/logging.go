package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type responseWriterInterceptor struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriterInterceptor) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterInterceptor) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// StructuredLogger logs incoming HTTP requests and outcomes, redacting sensitive headers.
func StructuredLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := GetRequestID(r.Context())

			interceptor := &responseWriterInterceptor{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(interceptor, r)

			duration := time.Since(start)

			routePattern := ""
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				routePattern = rctx.RoutePattern()
			}
			if routePattern == "" {
				routePattern = r.URL.Path
			}

			level := slog.LevelInfo
			if interceptor.statusCode >= 500 {
				level = slog.LevelError
			} else if interceptor.statusCode >= 400 {
				level = slog.LevelWarn
			}

			logger.Log(r.Context(), level, "HTTP request completed",
				slog.String("request_id", reqID),
				slog.String("method", r.Method),
				slog.String("route", routePattern),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
				slog.Int("status", interceptor.statusCode),
				slog.Int("bytes", interceptor.bytesWritten),
				slog.Duration("duration", duration),
			)
		})
	}
}
