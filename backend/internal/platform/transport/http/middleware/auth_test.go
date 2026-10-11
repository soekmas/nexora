package middleware_test

import (
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
	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/platform/transport/http/middleware"
)

type mockSessionStore struct {
	mu          sync.Mutex
	sessions    map[string]*auth.Session
	principals  map[uuid.UUID]*auth.AuthenticatedPrincipal
	touchedSess map[uuid.UUID]bool
	touchCounts map[uuid.UUID]int
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{
		sessions:    make(map[string]*auth.Session),
		principals:  make(map[uuid.UUID]*auth.AuthenticatedPrincipal),
		touchedSess: make(map[uuid.UUID]bool),
		touchCounts: make(map[uuid.UUID]int),
	}
}

func (m *mockSessionStore) GetSessionByTokenHash(_ context.Context, tokenHash string) (*auth.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[tokenHash]
	if !ok {
		return nil, auth.ErrSessionNotFound
	}
	return sess, nil
}

func (m *mockSessionStore) CreateSession(_ context.Context, sess *auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.TokenHash] = sess
	return nil
}

func (m *mockSessionStore) RevokeSession(_ context.Context, sessionID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sess := range m.sessions {
		if sess.ID == sessionID {
			now := time.Now()
			sess.RevokedAt = &now
			return nil
		}
	}
	return nil
}

func (m *mockSessionStore) RevokeUserSessions(_ context.Context, tenantID, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, sess := range m.sessions {
		if sess.TenantID == tenantID && sess.UserID == userID {
			sess.RevokedAt = &now
		}
	}
	return nil
}

func (m *mockSessionStore) TouchSession(_ context.Context, sessionID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touchedSess[sessionID] = true
	m.touchCounts[sessionID]++
	return nil
}

func (m *mockSessionStore) GetUserPrincipal(_ context.Context, tenantID, userID uuid.UUID) (*auth.AuthenticatedPrincipal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.principals[userID]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	if p.TenantID != tenantID {
		return nil, auth.ErrUserNotFound
	}
	return p, nil
}

func TestAuthenticate_TokenValidation(t *testing.T) {
	store := newMockSessionStore()

	tenantID := uuid.New()
	userID := uuid.New()
	storeID := uuid.New()

	rawToken, tokenHash, _ := auth.GenerateToken()

	session := &auth.Session{
		ID:        uuid.New(),
		TenantID:  tenantID,
		UserID:    userID,
		TokenHash: tokenHash,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	_ = store.CreateSession(context.Background(), session)

	principal := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Username:    "cashier1",
		DisplayName: "Cashier One",
		Roles:       []string{"cashier"},
		Permissions: []string{"pos:order:create"},
		Scopes: []auth.UserScope{
			{
				Type:    "STORE",
				StoreID: &storeID,
			},
		},
	}
	store.principals[userID] = principal

	var capturedPrincipal *auth.AuthenticatedPrincipal
	var capturedPostgresCtx postgres.PrincipalContext

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPrincipal, _ = auth.PrincipalFromContext(r.Context())
		capturedPostgresCtx, _ = postgres.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	pipeline := middleware.RequestID(middleware.Authenticate(store)(middleware.RequireAuth()(testHandler)))

	t.Run("valid token binds principal and postgres RLS context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.Header.Set("X-Tenant-ID", tenantID.String())
		req.Header.Set("X-Store-ID", storeID.String())
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		if capturedPrincipal == nil || capturedPrincipal.UserID != userID {
			t.Errorf("expected captured principal for user %s, got %+v", userID, capturedPrincipal)
		}
		if capturedPostgresCtx.TenantID != tenantID || capturedPostgresCtx.StoreID != storeID {
			t.Errorf("expected postgres principal context to carry tenant and store, got %+v", capturedPostgresCtx)
		}
	})

	t.Run("missing authorization header returns 401 UNAUTHORIZED", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "UNAUTHORIZED" {
			t.Errorf("expected UNAUTHORIZED, got %q", resp.Error.Code)
		}
	})

	t.Run("malformed bearer header returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer invalid-short-token")
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("unknown session token returns 401", func(t *testing.T) {
		unknownToken, _, _ := auth.GenerateToken()
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer "+unknownToken)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("expired session token returns 401 TOKEN_EXPIRED", func(t *testing.T) {
		expRaw, expHash, _ := auth.GenerateToken()
		_ = store.CreateSession(context.Background(), &auth.Session{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			TokenHash: expHash,
			IssuedAt:  time.Now().Add(-2 * time.Hour),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		})

		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer "+expRaw)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "TOKEN_EXPIRED" {
			t.Errorf("expected TOKEN_EXPIRED, got %q", resp.Error.Code)
		}
	})

	t.Run("revoked session token returns 401 SESSION_REVOKED", func(t *testing.T) {
		revRaw, revHash, _ := auth.GenerateToken()
		now := time.Now()
		_ = store.CreateSession(context.Background(), &auth.Session{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			TokenHash: revHash,
			IssuedAt:  time.Now().Add(-1 * time.Hour),
			ExpiresAt: time.Now().Add(1 * time.Hour),
			RevokedAt: &now,
		})

		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer "+revRaw)
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "SESSION_REVOKED" {
			t.Errorf("expected SESSION_REVOKED, got %q", resp.Error.Code)
		}
	})

	t.Run("tenant header mismatch returns 403 TENANT_MISMATCH", func(t *testing.T) {
		foreignTenantID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.Header.Set("X-Tenant-ID", foreignTenantID.String())
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "TENANT_MISMATCH" {
			t.Errorf("expected TENANT_MISMATCH, got %q", resp.Error.Code)
		}
	})

	t.Run("unauthorized store header returns 403 SCOPE_ACCESS_DENIED", func(t *testing.T) {
		foreignStoreID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/pos/orders", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.Header.Set("X-Tenant-ID", tenantID.String())
		req.Header.Set("X-Store-ID", foreignStoreID.String())
		rec := httptest.NewRecorder()

		pipeline.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}

		var resp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "SCOPE_ACCESS_DENIED" {
			t.Errorf("expected SCOPE_ACCESS_DENIED, got %q", resp.Error.Code)
		}
	})
}

func TestAuthenticate_ConcurrentRequests(t *testing.T) {
	store := newMockSessionStore()
	tenantID := uuid.New()
	userID := uuid.New()

	rawToken, tokenHash, _ := auth.GenerateToken()
	_ = store.CreateSession(context.Background(), &auth.Session{
		ID:        uuid.New(),
		TenantID:  tenantID,
		UserID:    userID,
		TokenHash: tokenHash,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	})
	store.principals[userID] = &auth.AuthenticatedPrincipal{
		TenantID: tenantID,
		UserID:   userID,
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	pipeline := middleware.RequestID(middleware.Authenticate(store)(middleware.RequireAuth()(testHandler)))

	totalConcurrent := 50
	var wg sync.WaitGroup
	wg.Add(totalConcurrent)

	statusCodes := make(chan int, totalConcurrent)

	for i := 0; i < totalConcurrent; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+rawToken)
			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)
			statusCodes <- rec.Code
		}()
	}

	wg.Wait()
	close(statusCodes)

	for code := range statusCodes {
		if code != http.StatusOK {
			t.Errorf("expected 200 OK across concurrent requests, got %d", code)
		}
	}
}

func TestAuthenticate_TouchSessionThrottling(t *testing.T) {
	store := newMockSessionStore()
	tenantID := uuid.New()
	userID := uuid.New()

	rawToken, tokenHash, _ := auth.GenerateToken()

	sessionID := uuid.New()
	store.sessions[tokenHash] = &auth.Session{
		ID:        sessionID,
		TenantID:  tenantID,
		UserID:    userID,
		TokenHash: tokenHash,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	store.principals[userID] = &auth.AuthenticatedPrincipal{
		UserID:      userID,
		TenantID:    tenantID,
		Username:    "testuser",
		DisplayName: "Test User",
		Roles:       []string{"cashier"},
		Permissions: []string{"pos:order:create"},
	}

	pipeline := middleware.Authenticate(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Execute 20 rapid authenticated requests in quick succession
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		rec := httptest.NewRecorder()
		pipeline.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d failed with code %d", i, rec.Code)
		}
	}

	// Brief pause to allow the async touch goroutine to complete
	time.Sleep(50 * time.Millisecond)

	store.mu.Lock()
	count := store.touchCounts[sessionID]
	store.mu.Unlock()

	// Prove that 20 rapid requests resulted in exactly 1 TouchSession invocation due to debounce
	if count != 1 {
		t.Errorf("expected exactly 1 TouchSession database call for 20 rapid requests, got %d", count)
	}
}
