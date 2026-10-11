package observability

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *metricsResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *metricsResponseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}

// HTTPMetricsMiddleware returns a chi middleware that records in-flight, request counter, and latency histograms.
func HTTPMetricsMiddleware(m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m == nil {
				next.ServeHTTP(w, r)
				return
			}

			m.IncHTTPInFlight()
			start := time.Now()

			interceptor := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			defer func() {
				m.DecHTTPInFlight()
				duration := time.Since(start)

				routePattern := ""
				if rctx := chi.RouteContext(r.Context()); rctx != nil {
					routePattern = rctx.RoutePattern()
				}
				if routePattern == "" {
					routePattern = r.URL.Path
				}

				m.RecordHTTPRequest(r.Method, routePattern, interceptor.statusCode, duration)
			}()

			next.ServeHTTP(interceptor, r)
		})
	}
}
