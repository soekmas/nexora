package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/config"
	transport "example.com/nexora/backend/internal/platform/transport/http"
)

func newTestServer(cfgMutator func(*config.Config)) (*transport.Server, *config.Config) {
	cfg := &config.Config{
		Env:               "testing",
		HTTPPort:          8080,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		RateLimitRequests: 100,
		RateLimitWindow:   1 * time.Minute,
	}
	if cfgMutator != nil {
		cfgMutator(cfg)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := transport.NewServer(cfg, logger, nil, nil)
	return srv, cfg
}

func TestHealthzEndpoint(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}

func TestReadyzEndpoint_NoDependencies(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if body["status"] != "ready" {
		t.Errorf("expected status 'ready', got %v", body["status"])
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if body["service"] != "nexora-api" {
		t.Errorf("expected service 'nexora-api', got %q", body["service"])
	}
	if body["environment"] != "testing" {
		t.Errorf("expected environment 'testing', got %q", body["environment"])
	}
}

func TestNotFound_CanonicalEnvelope(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/non-existent-route", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", contentType)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("expected error code NOT_FOUND, got %q", resp.Error.Code)
	}
	if resp.RequestID == "" {
		t.Error("expected non-empty request_id in 404 response")
	}
}

func TestMethodNotAllowed_CanonicalEnvelope(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// /healthz only supports GET, invoke with POST
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", contentType)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("expected error code METHOD_NOT_ALLOWED, got %q", resp.Error.Code)
	}
	if resp.RequestID == "" {
		t.Error("expected non-empty request_id in 405 response")
	}
}

func TestHandlerPanic_CanonicalEnvelope(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	srv.Router().Get("/test-panic", func(_ http.ResponseWriter, _ *http.Request) {
		panic("database pool exhausted: secret_token_xyz")
	})

	req := httptest.NewRequest(http.MethodGet, "/test-panic", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.Error.Code != "INTERNAL_SERVER_ERROR" {
		t.Errorf("expected INTERNAL_SERVER_ERROR, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "An unexpected server error occurred" {
		t.Errorf("expected masked message, got %q", resp.Error.Message)
	}
	if resp.RequestID == "" {
		t.Error("expected non-empty request_id in 500 response")
	}
}

func TestServerRateLimiting(t *testing.T) {
	srv, _ := newTestServer(func(c *config.Config) {
		c.RateLimitRequests = 2
		c.RateLimitWindow = 200 * time.Millisecond
	})
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// First 2 requests succeed
	for i := 1; i <= 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	// 3rd request receives 429
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.Error.Code != "RATE_LIMIT_EXCEEDED" {
		t.Errorf("expected RATE_LIMIT_EXCEEDED, got %q", resp.Error.Code)
	}
}

func TestResponder_WriteError(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	srv.Router().Get("/test-custom-error", func(w http.ResponseWriter, r *http.Request) {
		transport.WriteError(w, r, apperr.NewConflictError("ORDER_CONFLICT", "Order is already closed"))
	})

	srv.Router().Get("/test-raw-error", func(w http.ResponseWriter, r *http.Request) {
		transport.WriteError(w, r, errors.New("raw unhandled internal error"))
	})

	t.Run("writes structured AppError", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test-custom-error", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if resp.Error.Code != "ORDER_CONFLICT" {
			t.Errorf("expected ORDER_CONFLICT, got %q", resp.Error.Code)
		}
		if resp.Error.Message != "Order is already closed" {
			t.Errorf("expected 'Order is already closed', got %q", resp.Error.Message)
		}
	})

	t.Run("masks raw Go error into 500", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test-raw-error", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if resp.Error.Code != "INTERNAL_SERVER_ERROR" {
			t.Errorf("expected INTERNAL_SERVER_ERROR, got %q", resp.Error.Code)
		}
		if resp.Error.Message == "raw unhandled internal error" {
			t.Errorf("raw error message was leaked to client")
		}
	})
}

func TestMetricsEndpoint(t *testing.T) {
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Issue a request to trigger metrics
	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(healthRec, healthReq)

	// Fetch /metrics
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("expected text/plain content type, got %s", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Errorf("expected http_requests_total in metrics output: %s", body)
	}
	if !strings.Contains(body, `route="/healthz"`) {
		t.Errorf("expected route=\"/healthz\" in metrics output: %s", body)
	}
	if !strings.Contains(body, "process_uptime_seconds") {
		t.Errorf("expected process_uptime_seconds in metrics output: %s", body)
	}
}

func TestHealthz_LivenessIndependence(t *testing.T) {
	// Liveness must remain OK even if dependencies are unconfigured or failing
	srv, _ := newTestServer(nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected liveness 200, got %d", rec.Code)
	}
}

func TestReadyzEndpoint_DatabaseFailure(t *testing.T) {
	cfg := &config.Config{
		Env:          "testing",
		HTTPPort:     8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Create a pool and close it immediately so Ping fails
	pool, err := pgxpool.New(context.Background(), "postgres://dummy_user:dummy_pass@127.0.0.1:54321/dummy_db")
	if err != nil {
		t.Fatalf("failed to create pool config: %v", err)
	}
	pool.Close()

	srv := transport.NewServer(cfg, logger, pool, nil)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// 1. /readyz must return 503 SERVICE_UNAVAILABLE
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.Error.Code != "SERVICE_UNAVAILABLE" {
		t.Errorf("expected SERVICE_UNAVAILABLE, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "Database connection failed" {
		t.Errorf("expected 'Database connection failed', got %q", resp.Error.Message)
	}
	// Verify raw credentials / connection details are not leaked
	if strings.Contains(rec.Body.String(), "dummy_pass") {
		t.Errorf("password leaked in error response: %s", rec.Body.String())
	}

	// 2. /healthz (liveness) must remain 200 OK even when DB dependency fails
	livenessReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	livenessRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(livenessRec, livenessReq)
	if livenessRec.Code != http.StatusOK {
		t.Fatalf("expected liveness 200 even with DB down, got %d", livenessRec.Code)
	}
}

func TestReadyzEndpoint_RedisFailure(t *testing.T) {
	cfg := &config.Config{
		Env:          "testing",
		HTTPPort:     8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rdb := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:65530", // unreachable
	})
	defer func() { _ = rdb.Close() }()

	srv := transport.NewServer(cfg, logger, nil, rdb)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.Error.Code != "SERVICE_UNAVAILABLE" {
		t.Errorf("expected SERVICE_UNAVAILABLE, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "Redis connection failed" {
		t.Errorf("expected 'Redis connection failed', got %q", resp.Error.Message)
	}
	if strings.Contains(rec.Body.String(), "65530") {
		t.Errorf("internal connection details leaked in error response: %s", rec.Body.String())
	}
}
