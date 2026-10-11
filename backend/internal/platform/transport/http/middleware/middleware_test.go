package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/transport/http/middleware"
)

func TestRequestID(t *testing.T) {
	t.Run("generates request id when missing", func(t *testing.T) {
		handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := middleware.GetRequestID(r.Context())
			if reqID == "" {
				t.Error("expected request ID in context, got empty")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		respID := rec.Header().Get("X-Request-ID")
		if respID == "" {
			t.Error("expected X-Request-ID header on response, got empty")
		}
		if len(respID) < 8 {
			t.Errorf("expected generated request ID length >= 8, got %d", len(respID))
		}
	})

	t.Run("preserves valid client request id", func(t *testing.T) {
		validID := "req_client_abc12345"
		var ctxID string

		handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxID = middleware.GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Request-ID", validID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if ctxID != validID {
			t.Errorf("expected context request ID %q, got %q", validID, ctxID)
		}
		if rec.Header().Get("X-Request-ID") != validID {
			t.Errorf("expected response header %q, got %q", validID, rec.Header().Get("X-Request-ID"))
		}
	})

	t.Run("sanitizes invalid client request id", func(t *testing.T) {
		invalidIDs := []string{
			"short",                            // < 8 chars
			"invalid<script>alert(1)</script>", // illegal characters
			"bad id with spaces",               // spaces
			string(make([]byte, 150)),          // > 128 chars
		}

		for _, badID := range invalidIDs {
			var ctxID string
			handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctxID = middleware.GetRequestID(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("X-Request-ID", badID)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if ctxID == badID {
				t.Errorf("bad request ID %q was unexpectedly accepted", badID)
			}
			if len(ctxID) < 8 {
				t.Errorf("generated replacement ID too short: %q", ctxID)
			}
			if rec.Header().Get("X-Request-ID") != ctxID {
				t.Errorf("response header did not match context ID: %q vs %q", rec.Header().Get("X-Request-ID"), ctxID)
			}
		}
	})
}

func TestRecovery(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, nil))

	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("database connection string postgres://admin:secret_password@db:5432 failed unexpectedly")
	})

	pipeline := middleware.RequestID(middleware.Recovery(logger)(panicHandler))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()

	pipeline.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if resp.Error.Code != "INTERNAL_SERVER_ERROR" {
		t.Errorf("expected error code INTERNAL_SERVER_ERROR, got %q", resp.Error.Code)
	}

	// Ensure no internal panic payload or secret is leaked into response body
	bodyStr := rec.Body.String()
	if bytes.Contains([]byte(bodyStr), []byte("secret_password")) {
		t.Errorf("secret leaked into HTTP response body: %s", bodyStr)
	}
	if bytes.Contains([]byte(bodyStr), []byte("database connection string")) {
		t.Errorf("panic message leaked into HTTP response body: %s", bodyStr)
	}

	// Ensure stack was logged internally
	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("panic recovered")) {
		t.Errorf("expected logger to capture panic, got: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte("stack")) {
		t.Errorf("expected logger to record stack trace, got: %s", logOutput)
	}
}

func TestStructuredLogger_Redaction(t *testing.T) {
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, nil))

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	pipeline := middleware.RequestID(middleware.StructuredLogger(logger)(dummyHandler))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("Authorization", "Bearer sensitive_token_xyz")
	req.Header.Set("Cookie", "session=super_secret_cookie")
	req.Header.Set("X-Api-Key", "api_key_secret_123")
	rec := httptest.NewRecorder()

	pipeline.ServeHTTP(rec, req)

	logOutput := logBuf.String()
	if bytes.Contains([]byte(logOutput), []byte("sensitive_token_xyz")) {
		t.Errorf("Authorization token leaked into logger: %s", logOutput)
	}
	if bytes.Contains([]byte(logOutput), []byte("super_secret_cookie")) {
		t.Errorf("Cookie session leaked into logger: %s", logOutput)
	}
	if bytes.Contains([]byte(logOutput), []byte("api_key_secret_123")) {
		t.Errorf("API key leaked into logger: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte("HTTP request completed")) {
		t.Errorf("expected HTTP request completed log entry, got: %s", logOutput)
	}
}

func TestTimeout(t *testing.T) {
	t.Run("fast handler succeeds within deadline", func(t *testing.T) {
		handler := middleware.RequestID(middleware.Timeout(500 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Custom-Header", "success-val")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"created"}`))
		})))

		req := httptest.NewRequest(http.MethodGet, "/fast", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
		if rec.Header().Get("X-Custom-Header") != "success-val" {
			t.Errorf("expected X-Custom-Header preserved, got %q", rec.Header().Get("X-Custom-Header"))
		}
		if rec.Body.String() != `{"status":"created"}` {
			t.Errorf("expected body preserved, got %q", rec.Body.String())
		}
	})

	t.Run("slow handler is interrupted and returns 504 canonical envelope", func(t *testing.T) {
		handler := middleware.RequestID(middleware.Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(300 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
				// Context cancelled by timeout
				return
			}
		})))

		req := httptest.NewRequest(http.MethodGet, "/slow", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected 504, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if resp.Error.Code != "TIMEOUT_ERROR" {
			t.Errorf("expected TIMEOUT_ERROR, got %q", resp.Error.Code)
		}
		if resp.RequestID == "" {
			t.Error("expected non-empty request_id in timeout response")
		}
	})

	t.Run("concurrent header mutation and write contention during timeout", func(t *testing.T) {
		// Stress test handler mutating headers and writing concurrently while timeout path executes
		const iterations = 50
		for i := 0; i < iterations; i++ {
			handler := middleware.RequestID(middleware.Timeout(5 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				stop := make(chan struct{})
				doneHandler := make(chan struct{})

				go func() {
					defer close(doneHandler)
					for {
						select {
						case <-stop:
							return
						default:
							w.Header().Set("X-Stress-Key", "mutation")
							w.Header().Add("X-Stress-List", "item")
							_ = w.Header().Get("X-Stress-Key")
						}
					}
				}()

				// Wait until timeout context cancels
				<-r.Context().Done()
				close(stop)
				<-doneHandler

				// Late write attempts after timeout
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("late payload"))
			})))

			req := httptest.NewRequest(http.MethodGet, "/concurrent-stress", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusGatewayTimeout {
				t.Fatalf("iteration %d: expected 504, got %d", i, rec.Code)
			}

			var resp apperr.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("iteration %d: failed to parse JSON: %v", i, err)
			}
			if resp.Error.Code != "TIMEOUT_ERROR" {
				t.Errorf("iteration %d: expected TIMEOUT_ERROR, got %s", i, resp.Error.Code)
			}
			// Verify handler's stress headers did NOT leak into timeout response
			if rec.Header().Get("X-Stress-Key") != "" {
				t.Errorf("iteration %d: handler header leaked into 504 response: %s", i, rec.Header().Get("X-Stress-Key"))
			}
		}
	})

	t.Run("late handler writes after timeout return ErrHandlerTimeout and do not corrupt response", func(t *testing.T) {
		writeErrChan := make(chan error, 1)
		handlerDone := make(chan struct{})

		handler := middleware.RequestID(middleware.Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			defer close(handlerDone)
			// Sleep well beyond timeout deadline
			time.Sleep(80 * time.Millisecond)

			w.Header().Set("X-Late-Header", "late-value")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"late":"data"}`))
			writeErrChan <- err
		})))

		req := httptest.NewRequest(http.MethodGet, "/late", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("expected 504, got %d", rec.Code)
		}

		// Wait for the late handler to complete its write attempt
		<-handlerDone
		writeErr := <-writeErrChan
		if writeErr != http.ErrHandlerTimeout {
			t.Errorf("expected http.ErrHandlerTimeout on late write, got: %v", writeErr)
		}

		var resp apperr.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse timeout JSON response: %v", err)
		}
		if resp.Error.Code != "TIMEOUT_ERROR" {
			t.Errorf("expected TIMEOUT_ERROR, got %q", resp.Error.Code)
		}
		if rec.Header().Get("X-Late-Header") != "" {
			t.Errorf("late header leaked into timeout response: %q", rec.Header().Get("X-Late-Header"))
		}
	})
}

func TestRateLimit(t *testing.T) {
	limit := 3
	window := 200 * time.Millisecond
	limiter := middleware.NewMemoryRateLimiter(limit, window)
	defer limiter.Stop()

	handler := middleware.RequestID(middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))

	// First 3 requests should pass
	for i := 1; i <= limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
		if rec.Header().Get("X-RateLimit-Limit") != "3" {
			t.Errorf("expected X-RateLimit-Limit 3, got %s", rec.Header().Get("X-RateLimit-Limit"))
		}
	}

	// 4th request must be rate limited with 429
	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("expected Retry-After header on 429 response")
	}

	var resp apperr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse 429 response: %v", err)
	}

	if resp.Error.Code != "RATE_LIMIT_EXCEEDED" {
		t.Errorf("expected RATE_LIMIT_EXCEEDED, got %q", resp.Error.Code)
	}

	// Requests from a different IP address should succeed independently
	reqOther := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	reqOther.RemoteAddr = "192.168.1.101:12345"
	recOther := httptest.NewRecorder()
	handler.ServeHTTP(recOther, reqOther)

	if recOther.Code != http.StatusOK {
		t.Fatalf("expected 200 for different IP, got %d", recOther.Code)
	}

	// Wait for window expiration and verify recovery
	time.Sleep(window + 50*time.Millisecond)

	reqRecovered := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	reqRecovered.RemoteAddr = "192.168.1.100:12345"
	recRecovered := httptest.NewRecorder()
	handler.ServeHTTP(recRecovered, reqRecovered)

	if recRecovered.Code != http.StatusOK {
		t.Fatalf("expected 200 after window recovery, got %d", recRecovered.Code)
	}
}

func TestRateLimit_Concurrency(t *testing.T) {
	limit := 50
	window := 1 * time.Second
	limiter := middleware.NewMemoryRateLimiter(limit, window)
	defer limiter.Stop()

	handler := middleware.RequestID(middleware.RateLimit(limiter)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// No-op
	})))

	totalRequests := 100
	var wg sync.WaitGroup
	wg.Add(totalRequests)

	results := make(chan int, totalRequests)

	for i := 0; i < totalRequests; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/concurrent", nil)
			req.RemoteAddr = "10.0.0.1:8888"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			results <- rec.Code
		}()
	}

	wg.Wait()
	close(results)

	status200Count := 0
	status429Count := 0

	for code := range results {
		switch code {
		case http.StatusOK:
			status200Count++
		case http.StatusTooManyRequests:
			status429Count++
		}
	}

	if status200Count != limit {
		t.Errorf("expected exactly %d requests with 200 OK, got %d", limit, status200Count)
	}
	if status429Count != (totalRequests - limit) {
		t.Errorf("expected exactly %d requests with 429 Too Many Requests, got %d", totalRequests-limit, status429Count)
	}
}

func TestTrustedProxy_Resolution(t *testing.T) {
	trustedCIDRs := []string{"10.0.0.0/8", "172.16.0.0/12", "127.0.0.1/32"}
	limiter := middleware.NewMemoryRateLimiter(10, time.Minute)
	defer limiter.Stop()

	if err := limiter.SetTrustedProxies(trustedCIDRs); err != nil {
		t.Fatalf("failed to set trusted proxies: %v", err)
	}

	t.Run("invalid CIDR returns error", func(t *testing.T) {
		err := limiter.SetTrustedProxies([]string{"invalid-cidr"})
		if err == nil {
			t.Fatal("expected error for invalid CIDR, got nil")
		}
	})

	t.Run("direct untrusted client cannot spoof X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "198.51.100.1:45678" // Untrusted public IP
		req.Header.Set("X-Forwarded-For", "203.0.113.195")

		ip := limiter.ExtractClientIP(req)
		if ip != "198.51.100.1" {
			t.Errorf("expected direct untrusted client IP 198.51.100.1, got %s", ip)
		}
	})

	t.Run("direct untrusted client cannot spoof X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "198.51.100.1:45678" // Untrusted public IP
		req.Header.Set("X-Real-IP", "203.0.113.195")

		ip := limiter.ExtractClientIP(req)
		if ip != "198.51.100.1" {
			t.Errorf("expected direct untrusted client IP 198.51.100.1, got %s", ip)
		}
	})

	t.Run("single trusted proxy forwards authentic client IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.5:45678" // Trusted proxy
		req.Header.Set("X-Forwarded-For", "203.0.113.195")

		ip := limiter.ExtractClientIP(req)
		if ip != "203.0.113.195" {
			t.Errorf("expected forwarded client IP 203.0.113.195, got %s", ip)
		}
	})

	t.Run("multiple proxy hops resolved right-to-left, spoofed leftmost IP ignored", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:45678" // Immediate hop (trusted)
		// Client (203.0.113.5) spoofed 1.1.1.1, forwarded through edge proxy (10.0.0.2) to internal proxy (10.0.0.1)
		req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.5, 10.0.0.2")

		ip := limiter.ExtractClientIP(req)
		if ip != "203.0.113.5" {
			t.Errorf("expected real client IP 203.0.113.5, got %s", ip)
		}
	})

	t.Run("malformed hops handled safely", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:45678" // Trusted proxy
		req.Header.Set("X-Forwarded-For", "not-an-ip, 203.0.113.5")

		ip := limiter.ExtractClientIP(req)
		if ip != "203.0.113.5" {
			t.Errorf("expected valid IP 203.0.113.5, got %s", ip)
		}
	})

	t.Run("fallback to X-Real-IP when X-Forwarded-For is empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:45678" // Trusted proxy
		req.Header.Set("X-Real-IP", "203.0.113.50")

		ip := limiter.ExtractClientIP(req)
		if ip != "203.0.113.50" {
			t.Errorf("expected X-Real-IP 203.0.113.50, got %s", ip)
		}
	})
}

func TestRedisRateLimiter(t *testing.T) {
	// 1. Test fail-open with nil/unreachable client
	t.Run("fail-open when redis client is nil", func(t *testing.T) {
		rl := middleware.NewRedisRateLimiter(nil, 5, time.Second)
		rl.SetFallback(nil) // disable fallback to test pure fail-open
		allowed, retryAfter, remaining := rl.Allow("127.0.0.1")
		if !allowed {
			t.Error("expected fail-open to allow request when redis is nil")
		}
		if retryAfter != 0 {
			t.Errorf("expected 0 retryAfter, got %v", retryAfter)
		}
		if remaining != 5 {
			t.Errorf("expected remaining 5, got %d", remaining)
		}
	})

	t.Run("fail-closed when configured on redis failure", func(t *testing.T) {
		rl := middleware.NewRedisRateLimiter(nil, 5, time.Second)
		rl.SetFallback(nil)
		rl.SetFailClosed(true)
		allowed, retryAfter, remaining := rl.Allow("127.0.0.1")
		if allowed {
			t.Error("expected fail-closed to deny request when redis is nil")
		}
		if retryAfter != time.Second {
			t.Errorf("expected retryAfter 1s, got %v", retryAfter)
		}
		if remaining != 0 {
			t.Errorf("expected remaining 0, got %d", remaining)
		}
	})

	t.Run("fallback in-memory limiter used on redis error", func(t *testing.T) {
		rl := middleware.NewRedisRateLimiter(nil, 2, time.Second)
		fallback := middleware.NewMemoryRateLimiter(2, time.Second)
		defer fallback.Stop()
		rl.SetFallback(fallback)

		// First request allowed
		allowed, _, remaining := rl.Allow("10.1.1.1")
		if !allowed || remaining != 1 {
			t.Errorf("expected request 1 allowed with 1 remaining, got allowed=%v remaining=%d", allowed, remaining)
		}
		// Second request allowed
		allowed, _, remaining = rl.Allow("10.1.1.1")
		if !allowed || remaining != 0 {
			t.Errorf("expected request 2 allowed with 0 remaining, got allowed=%v remaining=%d", allowed, remaining)
		}
		// Third request rate limited
		allowed, retryAfter, _ := rl.Allow("10.1.1.1")
		if allowed || retryAfter <= 0 {
			t.Errorf("expected request 3 blocked by fallback, got allowed=%v retryAfter=%v", allowed, retryAfter)
		}
	})

	// 2. Test live Redis if available locally
	t.Run("live redis distributed shared quotas and concurrency", func(t *testing.T) {
		rdb := redis.NewClient(&redis.Options{
			Addr: "127.0.0.1:6379",
		})
		defer func() { _ = rdb.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			t.Skipf("Live Redis not reachable on 127.0.0.1:6379, skipping live redis test: %v", err)
			return
		}

		limit := 10
		window := 2 * time.Second
		testIP := fmt.Sprintf("test-ip-%d", time.Now().UnixNano())

		// Create two separate limiter instances sharing the same Redis backend
		limiter1 := middleware.NewRedisRateLimiter(rdb, limit, window)
		limiter2 := middleware.NewRedisRateLimiter(rdb, limit, window)

		// Concurrently send requests from both limiters
		totalReqs := 20
		var wg sync.WaitGroup
		wg.Add(totalReqs)
		results := make(chan bool, totalReqs)

		for i := 0; i < totalReqs; i++ {
			go func(idx int) {
				defer wg.Done()
				var allowed bool
				if idx%2 == 0 {
					allowed, _, _ = limiter1.Allow(testIP)
				} else {
					allowed, _, _ = limiter2.Allow(testIP)
				}
				results <- allowed
			}(i)
		}

		wg.Wait()
		close(results)

		allowedCount := 0
		blockedCount := 0
		for allowed := range results {
			if allowed {
				allowedCount++
			} else {
				blockedCount++
			}
		}

		if allowedCount != limit {
			t.Errorf("expected exactly %d allowed across shared limiters, got %d", limit, allowedCount)
		}
		if blockedCount != (totalReqs - limit) {
			t.Errorf("expected exactly %d blocked, got %d", totalReqs-limit, blockedCount)
		}

		// Clean up test key
		_ = rdb.Del(context.Background(), "nexora:ratelimit:ip:"+testIP).Err()
	})
}
