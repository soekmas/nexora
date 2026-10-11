package idempotency

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// ResponseInterceptor records HTTP status code and response body for idempotency persistence.
type ResponseInterceptor struct {
	http.ResponseWriter
	statusCode  int
	body        bytes.Buffer
	wroteHeader bool
}

// WriteHeader records the status code and delegates to ResponseWriter.
func (rw *ResponseInterceptor) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

// Write intercepts the response bytes for persistence and writes to the client.
func (rw *ResponseInterceptor) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

// RequireIdempotency returns an HTTP middleware that strictly enforces the presence and
// correctness of the Idempotency-Key header on mutating operations.
func RequireIdempotency(store Store, operation string, ttl time.Duration) func(http.Handler) http.Handler {
	return Idempotent(store, operation, true, ttl)
}

// Idempotent constructs an HTTP middleware providing database-backed idempotency protection.
// If mandatory is false and the Idempotency-Key header is absent, the request passes through unconstrained.
func Idempotent(store Store, operation string, mandatory bool, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := platformmw.GetRequestID(r.Context())

			// 1. Check if method is safe (GET, HEAD, OPTIONS); safe methods bypass idempotency
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get(HeaderIdempotencyKey)
			if key == "" {
				if mandatory {
					apperr.RespondError(w, reqID, apperr.NewValidationError("Idempotency-Key", "Idempotency-Key header is required for this operation"))
					return
				}
				// Optional and missing: pass through
				next.ServeHTTP(w, r)
				return
			}

			// 2. Validate key format and bounds
			if err := ValidateKey(key); err != nil {
				apperr.RespondError(w, reqID, apperr.NewValidationError("Idempotency-Key", err.Error()))
				return
			}

			// 3. Resolve authenticated tenant context (fail-closed if unauthenticated)
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal == nil {
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required to execute idempotent operation"))
				return
			}
			tenantID := principal.TenantID

			// 4. Read and buffer request body for hashing and downstream handler consumption
			var bodyBytes []byte
			if r.Body != nil {
				var err error
				bodyBytes, err = io.ReadAll(r.Body)
				if err != nil {
					apperr.RespondError(w, reqID, apperr.NewBadRequestError("Failed to read request body"))
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			requestHash, err := HashRequestPayload(r.Header.Get("Content-Type"), bodyBytes)
			if err != nil {
				apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed JSON request body: cannot compute idempotency fingerprint"))
				return
			}
			opName := operation
			if opName == "" {
				opName = NormalizeOperation(r.Method, r.URL.Path)
			}

			// 5. Attempt atomic acquisition or existing record lookup
			acquireRes, err := store.AcquireOrGet(r.Context(), tenantID, key, opName, requestHash, ttl)
			if err != nil {
				apperr.RespondError(w, reqID, apperr.NewInternalError("Idempotency coordination failure"))
				return
			}

			// 6. Handle payload hash mismatch (same key + different request payload = 409 conflict)
			if acquireRes.HashMismatch {
				apperr.RespondError(w, reqID, apperr.NewIdempotencyKeyReusedError(
					"Idempotency key was reused with a different request payload",
					map[string]any{
						"idempotency_key": key,
						"operation":       opName,
					},
				))
				return
			}

			// 7. Handle existing record replay vs in-progress conflict
			if !acquireRes.Acquired {
				rec := acquireRes.Record
				if rec.IsCompleted() {
					// Cache HIT: Replay completed response
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.Header().Set("X-Cache", "HIT")
					w.WriteHeader(*rec.ResponseStatus)
					if len(rec.ResponseBody) > 0 {
						_, _ = w.Write(rec.ResponseBody)
					}
					return
				}

				// Cache IN_PROGRESS: Concurrent duplicate request or previous uncertain execution
				apperr.RespondError(w, reqID, apperr.NewIdempotencyInProgressError(
					"An identical request is currently being processed",
					map[string]any{
						"idempotency_key": key,
						"operation":       opName,
					},
				))
				return
			}

			// 8. Newly acquired in-progress record: execute downstream handler with response interception
			rec := acquireRes.Record
			interceptor := &ResponseInterceptor{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			w.Header().Set("X-Cache", "MISS")
			reqWithRecord := r.WithContext(WithRecord(r.Context(), rec))
			next.ServeHTTP(interceptor, reqWithRecord)

			// 9. Persist deterministic responses (2xx, 3xx, and client 4xx).
			// Server errors (5xx), panics, and uncompleted executions leave the in-progress
			// record in PostgreSQL to prevent automatic duplicate execution when the business outcome
			// is uncertain. Retries fail closed with IDEMPOTENCY_IN_PROGRESS.
			if interceptor.statusCode >= 200 && interceptor.statusCode < 500 {
				completeCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
				defer cancel()
				_ = store.Complete(completeCtx, rec.ID, tenantID, interceptor.statusCode, interceptor.body.Bytes())
			}
		})
	}
}
