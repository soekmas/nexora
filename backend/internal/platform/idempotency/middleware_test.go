package idempotency_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/idempotency"
)

type mockStore struct {
	mu      sync.Mutex
	records map[string]*idempotency.Record
}

func newMockStore() *mockStore {
	return &mockStore{
		records: make(map[string]*idempotency.Record),
	}
}

func (m *mockStore) compositeKey(tenantID uuid.UUID, key, operation string) string {
	return tenantID.String() + ":" + key + ":" + operation
}

func (m *mockStore) AcquireOrGet(
	_ context.Context,
	tenantID uuid.UUID,
	key, operation, requestHash string,
	ttl time.Duration,
) (*idempotency.AcquireResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cKey := m.compositeKey(tenantID, key, operation)
	existing, ok := m.records[cKey]
	if !ok {
		exp := time.Now().Add(ttl)
		rec := &idempotency.Record{
			ID:          uuid.New(),
			TenantID:    tenantID,
			Key:         key,
			Operation:   operation,
			RequestHash: requestHash,
			CreatedAt:   time.Now(),
			ExpiresAt:   &exp,
		}
		m.records[cKey] = rec
		return &idempotency.AcquireResult{
			Acquired: true,
			Record:   rec,
		}, nil
	}

	if existing.RequestHash != requestHash {
		return &idempotency.AcquireResult{
			Acquired:     false,
			Record:       existing,
			HashMismatch: true,
		}, nil
	}

	return &idempotency.AcquireResult{
		Acquired:     false,
		Record:       existing,
		HashMismatch: false,
	}, nil
}

func (m *mockStore) Complete(
	_ context.Context,
	recordID, tenantID uuid.UUID,
	status int,
	responseBody []byte,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rec := range m.records {
		if rec.ID == recordID && rec.TenantID == tenantID {
			if rec.ResponseStatus != nil {
				return idempotency.ErrReservationLost
			}
			rec.ResponseStatus = &status
			rec.ResponseBody = responseBody
			return nil
		}
	}
	return idempotency.ErrReservationLost
}

func (m *mockStore) Release(_ context.Context, recordID, tenantID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k, rec := range m.records {
		if rec.ID == recordID && rec.TenantID == tenantID && rec.ResponseStatus == nil {
			delete(m.records, k)
			return nil
		}
	}
	return nil
}

func (m *mockStore) Get(_ context.Context, tenantID uuid.UUID, key, operation string) (*idempotency.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cKey := m.compositeKey(tenantID, key, operation)
	rec, ok := m.records[cKey]
	if !ok {
		return nil, idempotency.ErrRecordNotFound
	}
	return rec, nil
}

func (m *mockStore) Delete(_ context.Context, tenantID uuid.UUID, key, operation string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cKey := m.compositeKey(tenantID, key, operation)
	delete(m.records, cKey)
	return nil
}

func (m *mockStore) CleanupExpired(_ context.Context, batchSize int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var pruned int64
	for k, rec := range m.records {
		if int(pruned) >= batchSize {
			break
		}
		if rec.ResponseStatus != nil && rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
			delete(m.records, k)
			pruned++
		}
	}
	return pruned, nil
}

func (m *mockStore) FindStuckReservations(_ context.Context, tenantID uuid.UUID, staleThreshold time.Duration, limit int) ([]*idempotency.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	threshold := time.Now().Add(-staleThreshold)
	var stuck []*idempotency.Record
	for _, rec := range m.records {
		if len(stuck) >= limit {
			break
		}
		if rec.TenantID == tenantID && rec.ResponseStatus == nil && rec.CreatedAt.Before(threshold) {
			stuck = append(stuck, rec)
		}
	}
	return stuck, nil
}

func (m *mockStore) MarkStuckAsFailed(_ context.Context, tenantID, recordID uuid.UUID, staleThreshold time.Duration, failStatus int, failBody []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	threshold := time.Now().Add(-staleThreshold)
	for _, rec := range m.records {
		if rec.ID == recordID && rec.TenantID == tenantID {
			if rec.ResponseStatus != nil || rec.CreatedAt.After(threshold) {
				return idempotency.ErrReservationLost
			}
			rec.ResponseStatus = &failStatus
			rec.ResponseBody = failBody
			return nil
		}
	}
	return idempotency.ErrReservationLost
}

func TestMiddleware_MandatoryKey(t *testing.T) {
	store := newMockStore()
	tenantID := uuid.New()
	userID := uuid.New()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	pipeline := idempotency.RequireIdempotency(store, "POST /pos/orders", 1*time.Hour)(handler)

	t.Run("missing key returns 400 VALIDATION_ERROR", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader([]byte(`{"item":"test"}`)))
		ctx := auth.WithPrincipal(req.Context(), &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "VALIDATION_ERROR" {
			t.Fatalf("expected VALIDATION_ERROR, got %s", resp.Error.Code)
		}
	})

	t.Run("malformed key returns 400 VALIDATION_ERROR", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader([]byte(`{"item":"test"}`)))
		req.Header.Set("Idempotency-Key", "has space in key")
		ctx := auth.WithPrincipal(req.Context(), &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated request fails closed with 401 UNAUTHORIZED", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader([]byte(`{"item":"test"}`)))
		req.Header.Set("Idempotency-Key", "valid-key-123")
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("safe GET request bypasses idempotency", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for GET, got %d", rec.Code)
		}
	})
}

func TestMiddleware_FirstAndDuplicateExecution(t *testing.T) {
	store := newMockStore()
	tenantID := uuid.New()
	userID := uuid.New()

	executionCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executionCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"order_id":"ord-999","total":150000}`))
	})

	pipeline := idempotency.RequireIdempotency(store, "POST /pos/orders", 1*time.Hour)(handler)

	key := "ord-key-001"
	payload := []byte(`{"qty": 2, "item": "A"}`)

	// 1. First Request
	req1 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req1.Header.Set("Idempotency-Key", key)
	ctx := auth.WithPrincipal(req1.Context(), &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
	req1 = req1.WithContext(ctx)
	rec1 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on first request, got %d (body: %s)", rec1.Code, rec1.Body.String())
	}
	if rec1.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("expected X-Cache: MISS on first request, got %q", rec1.Header().Get("X-Cache"))
	}
	if executionCount != 1 {
		t.Fatalf("expected handler to execute exactly 1 time, got %d", executionCount)
	}

	// 2. Duplicate Request with identical payload
	req2 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req2.Header.Set("Idempotency-Key", key)
	req2 = req2.WithContext(ctx)
	rec2 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on duplicate request, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT on duplicate request, got %q", rec2.Header().Get("X-Cache"))
	}
	if rec2.Body.String() != rec1.Body.String() {
		t.Fatalf("expected cached body %s, got %s", rec1.Body.String(), rec2.Body.String())
	}
	if executionCount != 1 {
		t.Fatalf("expected handler NOT to re-execute on duplicate request, got %d", executionCount)
	}

	// 3. Same Key with DIFFERENT payload: must return 409 IDEMPOTENCY_KEY_REUSED
	diffPayload := []byte(`{"qty": 5, "item": "B"}`)
	req3 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(diffPayload))
	req3.Header.Set("Idempotency-Key", key)
	req3 = req3.WithContext(ctx)
	rec3 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on payload mismatch, got %d", rec3.Code)
	}

	var resp apperr.ErrorResponse
	_ = json.Unmarshal(rec3.Body.Bytes(), &resp)
	if resp.Error.Code != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("expected IDEMPOTENCY_KEY_REUSED, got %q", resp.Error.Code)
	}
	if executionCount != 1 {
		t.Fatalf("expected handler NOT to execute on key reuse conflict, got %d", executionCount)
	}
}

func TestMiddleware_InProgressConflict(t *testing.T) {
	store := newMockStore()
	tenantID := uuid.New()
	userID := uuid.New()

	// Simulate an existing in-progress record (ResponseStatus is nil)
	key := "in-progress-key"
	op := "POST /payments"
	hash := idempotency.HashPayload([]byte(`{"amount": 50000}`))

	_, _ = store.AcquireOrGet(context.Background(), tenantID, key, op, hash, 1*time.Hour)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	pipeline := idempotency.RequireIdempotency(store, op, 1*time.Hour)(handler)

	req := httptest.NewRequest(http.MethodPost, "/payments", bytes.NewReader([]byte(`{"amount": 50000}`)))
	req.Header.Set("Idempotency-Key", key)
	ctx := auth.WithPrincipal(req.Context(), &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	pipeline.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for in-progress request, got %d", rec.Code)
	}

	var resp apperr.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Code != "IDEMPOTENCY_IN_PROGRESS" {
		t.Fatalf("expected IDEMPOTENCY_IN_PROGRESS, got %q", resp.Error.Code)
	}
}

func TestMiddleware_UncertainHandlerFailureFailsClosed(t *testing.T) {
	store := newMockStore()
	tenantID := uuid.New()
	userID := uuid.New()

	var executionCount int
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executionCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"db unavailable"}`))
	})

	pipeline := idempotency.RequireIdempotency(store, "POST /pos/orders", 1*time.Hour)(handler)

	key := "uncertain-failure-key"
	payload := []byte(`{"item": "retry"}`)

	// 1. Initial attempt fails with 500
	req1 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req1.Header.Set("Idempotency-Key", key)
	ctx := auth.WithPrincipal(req1.Context(), &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
	req1 = req1.WithContext(ctx)
	rec1 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on failure, got %d", rec1.Code)
	}
	if executionCount != 1 {
		t.Fatalf("expected 1 execution, got %d", executionCount)
	}

	// Verify in-progress record remains in store to prevent duplicate execution when outcome is uncertain
	rec, _ := store.Get(context.Background(), tenantID, key, "POST /pos/orders")
	if rec == nil {
		t.Fatalf("expected in-progress record to remain in store after uncertain failure")
	}
	if rec.ResponseStatus != nil {
		t.Fatalf("expected response_status to be nil (in-progress)")
	}

	// 2. Retry with same key fails closed with 409 IDEMPOTENCY_IN_PROGRESS
	req2 := httptest.NewRequest(http.MethodPost, "/pos/orders", bytes.NewReader(payload))
	req2.Header.Set("Idempotency-Key", key)
	req2 = req2.WithContext(ctx)
	rec2 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 on retry after uncertain failure, got %d", rec2.Code)
	}

	var errResp apperr.ErrorResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &errResp)
	if errResp.Error.Code != "IDEMPOTENCY_IN_PROGRESS" {
		t.Fatalf("expected IDEMPOTENCY_IN_PROGRESS, got %s", errResp.Error.Code)
	}
	if executionCount != 1 {
		t.Fatalf("expected handler NOT to be re-executed, but was called %d times", executionCount)
	}
}

func TestMiddleware_ClientErrorReplays(t *testing.T) {
	store := newMockStore()
	tenantID := uuid.New()
	userID := uuid.New()

	var executionCount int
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executionCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"BUSINESS_RULE_VIOLATION","message":"insufficient points"}}`))
	})

	pipeline := idempotency.RequireIdempotency(store, "POST /pos/redeem", 1*time.Hour)(handler)

	key := "client-err-key"
	payload := []byte(`{"points": 500}`)

	// 1. Initial attempt returns 422
	req1 := httptest.NewRequest(http.MethodPost, "/pos/redeem", bytes.NewReader(payload))
	req1.Header.Set("Idempotency-Key", key)
	ctx := auth.WithPrincipal(req1.Context(), &auth.AuthenticatedPrincipal{TenantID: tenantID, UserID: userID})
	req1 = req1.WithContext(ctx)
	rec1 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec1.Code)
	}
	if rec1.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("expected X-Cache: MISS, got %s", rec1.Header().Get("X-Cache"))
	}
	if executionCount != 1 {
		t.Fatalf("expected 1 execution, got %d", executionCount)
	}

	// 2. Retry with same key replays 422 without re-executing handler
	req2 := httptest.NewRequest(http.MethodPost, "/pos/redeem", bytes.NewReader(payload))
	req2.Header.Set("Idempotency-Key", key)
	req2 = req2.WithContext(ctx)
	rec2 := httptest.NewRecorder()

	pipeline.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on replay, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT on replay, got %s", rec2.Header().Get("X-Cache"))
	}
	if executionCount != 1 {
		t.Fatalf("expected handler NOT to re-execute, got %d calls", executionCount)
	}
}
