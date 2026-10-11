package observability_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"example.com/nexora/backend/internal/platform/observability"
)

func TestMetrics_Normalization(t *testing.T) {
	t.Run("NormalizeMethod", func(t *testing.T) {
		if got := observability.NormalizeMethod("get"); got != "GET" {
			t.Errorf("expected GET, got %s", got)
		}
		if got := observability.NormalizeMethod("POST"); got != "POST" {
			t.Errorf("expected POST, got %s", got)
		}
		if got := observability.NormalizeMethod("custom_weird_method"); got != "OTHER" {
			t.Errorf("expected OTHER, got %s", got)
		}
	})

	t.Run("NormalizeRoute", func(t *testing.T) {
		if got := observability.NormalizeRoute("/api/v1/healthz?foo=bar"); got != "/api/v1/healthz" {
			t.Errorf("expected query params stripped, got %s", got)
		}
		if got := observability.NormalizeRoute("   "); got != "/" {
			t.Errorf("expected /, got %s", got)
		}
		longRoute := "/" + strings.Repeat("a", 200)
		if got := observability.NormalizeRoute(longRoute); got != "/unmatched" {
			t.Errorf("expected long route to be collapsed to /unmatched, got %s", got)
		}
		if got := observability.NormalizeRoute("/route with spaces"); got != "/unmatched" {
			t.Errorf("expected space route to be collapsed to /unmatched, got %s", got)
		}
	})

	t.Run("NormalizeStatusClass", func(t *testing.T) {
		if got := observability.NormalizeStatusClass(200); got != "2xx" {
			t.Errorf("expected 2xx, got %s", got)
		}
		if got := observability.NormalizeStatusClass(404); got != "4xx" {
			t.Errorf("expected 4xx, got %s", got)
		}
		if got := observability.NormalizeStatusClass(500); got != "5xx" {
			t.Errorf("expected 5xx, got %s", got)
		}
		if got := observability.NormalizeStatusClass(999); got != "unknown" {
			t.Errorf("expected unknown, got %s", got)
		}
	})

	t.Run("NormalizeWorkerStatus", func(t *testing.T) {
		if got := observability.NormalizeWorkerStatus("SUCCESS"); got != "success" {
			t.Errorf("expected success, got %s", got)
		}
		if got := observability.NormalizeWorkerStatus("retry"); got != "retry" {
			t.Errorf("expected retry, got %s", got)
		}
		if got := observability.NormalizeWorkerStatus("dead_letter"); got != "dead_letter" {
			t.Errorf("expected dead_letter, got %s", got)
		}
		if got := observability.NormalizeWorkerStatus("random_failure"); got != "failed" {
			t.Errorf("expected failed, got %s", got)
		}
	})
}

func TestMetrics_PrometheusExposition(t *testing.T) {
	m := observability.NewMetrics()

	// Record HTTP metrics
	m.RecordHTTPRequest("GET", "/healthz", 200, 10*time.Millisecond)
	m.RecordHTTPRequest("POST", "/api/v1/auth/login", 401, 25*time.Millisecond)
	m.IncHTTPInFlight()
	if m.HTTPInFlight() != 1 {
		t.Errorf("expected in flight = 1, got %d", m.HTTPInFlight())
	}
	m.DecHTTPInFlight()
	if m.HTTPInFlight() != 0 {
		t.Errorf("expected in flight = 0, got %d", m.HTTPInFlight())
	}

	// Record Worker metrics
	m.SetWorkerActive(true)
	m.RecordWorkerClaim(5)
	m.RecordWorkerPollCycle()
	m.RecordWorkerDispatch("success")
	m.RecordWorkerDispatch("dead_letter")

	// Record DB Pool stats
	m.SetDBPoolStatsFunc(func() observability.DBPoolStats {
		return observability.DBPoolStats{
			TotalConns:    10,
			AcquiredConns: 3,
			IdleConns:     7,
			MaxConns:      25,
		}
	})

	buf := &bytes.Buffer{}
	if err := m.WritePrometheus(buf); err != nil {
		t.Fatalf("failed to write prometheus output: %v", err)
	}

	out := buf.String()

	expectedSubstrings := []string{
		"# HELP http_requests_total",
		`http_requests_total{method="GET",route="/healthz",status_class="2xx"} 1`,
		`http_requests_total{method="POST",route="/api/v1/auth/login",status_class="4xx"} 1`,
		"# HELP http_request_duration_seconds",
		`http_request_duration_seconds_bucket{method="GET",route="/healthz"`,
		"# HELP http_requests_in_flight",
		"http_requests_in_flight 0",
		"# HELP worker_active",
		"worker_active 1",
		"# HELP worker_events_claimed_total",
		"worker_events_claimed_total 5",
		"# HELP worker_poll_cycles_total",
		"worker_poll_cycles_total 1",
		`worker_dispatch_total{status="success"} 1`,
		`worker_dispatch_total{status="dead_letter"} 1`,
		"# HELP db_pool_total_conns",
		"db_pool_total_conns 10",
		"db_pool_acquired_conns 3",
		"db_pool_idle_conns 7",
		"db_pool_max_conns 25",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(out, sub) {
			t.Errorf("expected prometheus output to contain %q, but got:\n%s", sub, out)
		}
	}
}

func TestMetrics_Handler(t *testing.T) {
	m := observability.NewMetrics()
	handler := m.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("expected text/plain content type, got %s", contentType)
	}

	// Test method not allowed for POST
	postReq := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", postRec.Code)
	}
}

func TestMetrics_MiddlewareIntegration(t *testing.T) {
	m := observability.NewMetrics()
	r := chi.NewRouter()

	r.Use(observability.HTTPMetricsMiddleware(m))
	r.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/items/12345", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	buf := &bytes.Buffer{}
	_ = m.WritePrometheus(buf)
	out := buf.String()

	// Assert route pattern /items/{id} is recorded, NOT raw id 12345!
	if !strings.Contains(out, `route="/items/{id}"`) {
		t.Errorf("expected route template /items/{id} in metrics, got:\n%s", out)
	}
	if strings.Contains(out, "12345") {
		t.Errorf("raw id leaked into metric label: %s", out)
	}
}

func TestMetrics_ConcurrencyRace(t *testing.T) {
	m := observability.NewMetrics()
	const workers = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m.IncHTTPInFlight()
				m.RecordHTTPRequest("GET", "/test", 200, time.Duration(j)*time.Millisecond)
				m.DecHTTPInFlight()
			}
		}(i)

		go func(_ int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m.RecordWorkerClaim(1)
				m.RecordWorkerPollCycle()
				m.RecordWorkerDispatch("success")
			}
		}(i)
	}

	wg.Wait()

	buf := &bytes.Buffer{}
	if err := m.WritePrometheus(buf); err != nil {
		t.Fatalf("WritePrometheus failed after concurrent access: %v", err)
	}
}

func TestTracing_W3CTraceContext(t *testing.T) {
	traceID := observability.GenerateTraceID()
	spanID := observability.GenerateSpanID()

	if len(traceID) != 32 {
		t.Errorf("expected 32-char hex trace ID, got %d", len(traceID))
	}
	if len(spanID) != 16 {
		t.Errorf("expected 16-char hex span ID, got %d", len(spanID))
	}

	tc := observability.TraceContext{
		TraceID: traceID,
		SpanID:  spanID,
	}

	headerVal := tc.FormatTraceParent()
	if !strings.HasPrefix(headerVal, "00-") {
		t.Errorf("expected 00- prefix in traceparent, got %s", headerVal)
	}

	parsed, ok := observability.ParseTraceParent(headerVal)
	if !ok {
		t.Fatalf("failed to parse valid traceparent header: %s", headerVal)
	}
	if parsed.TraceID != traceID || parsed.SpanID != spanID {
		t.Errorf("parsed mismatch: %+v vs %+v", parsed, tc)
	}

	// Test invalid header
	if _, ok := observability.ParseTraceParent("invalid-header"); ok {
		t.Error("expected invalid header to fail parsing")
	}
}
