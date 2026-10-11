package http_test

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
	"example.com/nexora/backend/internal/platform/config"
	transporthttp "example.com/nexora/backend/internal/platform/transport/http"
)

type mockRefreshTokenStore struct {
	mu           sync.Mutex
	tokens       map[string]*auth.RefreshToken
	rotatedCount int
	rotateFunc   func(ctx context.Context, oldTokenHash string, expectedTenantID *uuid.UUID, accessTTL, refreshTTL time.Duration) (*auth.RefreshTokenRotationResult, error)
}

func newMockRefreshTokenStore() *mockRefreshTokenStore {
	return &mockRefreshTokenStore{
		tokens: make(map[string]*auth.RefreshToken),
	}
}

func (m *mockRefreshTokenStore) CreateRefreshToken(_ context.Context, rt *auth.RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[rt.TokenHash] = rt
	return nil
}

func (m *mockRefreshTokenStore) GetRefreshTokenByHash(_ context.Context, tokenHash string) (*auth.RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.tokens[tokenHash]
	if !ok {
		return nil, auth.ErrRefreshTokenNotFound
	}
	return rt, nil
}

func (m *mockRefreshTokenStore) RotateRefreshToken(ctx context.Context, oldTokenHash string, expectedTenantID *uuid.UUID, accessTTL, refreshTTL time.Duration) (*auth.RefreshTokenRotationResult, error) {
	if m.rotateFunc != nil {
		return m.rotateFunc(ctx, oldTokenHash, expectedTenantID, accessTTL, refreshTTL)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.tokens[oldTokenHash]
	if !ok {
		return nil, auth.ErrRefreshTokenNotFound
	}

	if rt.RevokedAt != nil {
		return nil, auth.ErrRefreshTokenReplayDetected
	}

	if time.Now().After(rt.ExpiresAt) {
		return nil, auth.ErrRefreshTokenExpired
	}

	if expectedTenantID != nil && rt.TenantID != *expectedTenantID {
		return nil, auth.ErrRefreshTokenTenantMismatch
	}

	m.rotatedCount++
	now := time.Now()
	rt.RevokedAt = &now

	newRawAccess, newAccessHash, _ := auth.GenerateToken()
	newRawRefresh, newRefreshHash, _ := auth.GenerateRefreshToken()

	newRT := &auth.RefreshToken{
		ID:        uuid.New(),
		TenantID:  rt.TenantID,
		UserID:    rt.UserID,
		SessionID: rt.SessionID,
		TokenHash: newRefreshHash,
		FamilyID:  rt.FamilyID,
		IssuedAt:  now,
		ExpiresAt: now.Add(refreshTTL),
	}
	m.tokens[newRefreshHash] = newRT

	return &auth.RefreshTokenRotationResult{
		SessionID:        rt.SessionID,
		TenantID:         rt.TenantID,
		UserID:           rt.UserID,
		RawAccessToken:   newRawAccess,
		AccessTokenHash:  newAccessHash,
		RawRefreshToken:  newRawRefresh,
		RefreshTokenHash: newRefreshHash,
		AccessExpiresIn:  int(accessTTL.Seconds()),
		Principal: &auth.AuthenticatedPrincipal{
			SessionID:   rt.SessionID,
			TenantID:    rt.TenantID,
			UserID:      rt.UserID,
			Username:    "cashier1",
			DisplayName: "Cashier One",
			Roles:       []string{"CASHIER"},
			Permissions: []string{"pos.sale.create"},
		},
	}, nil
}

func (m *mockRefreshTokenStore) RevokeTokenFamily(_ context.Context, familyID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, t := range m.tokens {
		if t.FamilyID == familyID && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	return nil
}

func setupTestServer(store auth.RefreshTokenStore) *transporthttp.Server {
	cfg := &config.Config{
		HTTPPort:     8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	srv := transporthttp.NewServer(cfg, nil, nil, nil)
	srv.SetAuthStore(store)
	return srv
}

func TestAuthRefresh_Endpoint(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	sessID := uuid.New()
	familyID := uuid.New()

	rawRT, hashRT, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}

	store := newMockRefreshTokenStore()
	store.tokens[hashRT] = &auth.RefreshToken{
		ID:        uuid.New(),
		TenantID:  tenantID,
		UserID:    userID,
		SessionID: sessID,
		TokenHash: hashRT,
		FamilyID:  familyID,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	srv := setupTestServer(store)

	t.Run("successful rotation returns 201 with AuthResponse", func(t *testing.T) {
		reqBody := transporthttp.RefreshRequest{RefreshToken: rawRT}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", tenantID.String())
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected HTTP 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp transporthttp.AuthResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON response: %v", err)
		}

		if resp.AccessToken == "" || resp.RefreshToken == "" {
			t.Fatalf("expected non-empty tokens in response, got access=%q refresh=%q", resp.AccessToken, resp.RefreshToken)
		}
		if resp.TokenType != "Bearer" {
			t.Errorf("expected token_type 'Bearer', got %q", resp.TokenType)
		}
		if resp.ExpiresIn <= 0 {
			t.Errorf("expected positive expires_in, got %d", resp.ExpiresIn)
		}
		if resp.User.Username != "cashier1" {
			t.Errorf("expected username cashier1, got %q", resp.User.Username)
		}
		// Access token must have nex_at_ prefix
		if !auth.ValidateTokenFormat(resp.AccessToken) {
			t.Errorf("returned access token has invalid format: %s", resp.AccessToken)
		}
		// Refresh token must have nex_rt_ prefix
		if !auth.ValidateRefreshTokenFormat(resp.RefreshToken) {
			t.Errorf("returned refresh token has invalid format: %s", resp.RefreshToken)
		}
	})

	t.Run("mounted at /api/v1/auth/refresh prefix identically", func(t *testing.T) {
		// Generate another token to test /api/v1/auth/refresh
		rawRT2, hashRT2, _ := auth.GenerateRefreshToken()
		store.tokens[hashRT2] = &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessID,
			TokenHash: hashRT2,
			FamilyID:  familyID,
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}

		reqBody := transporthttp.RefreshRequest{RefreshToken: rawRT2}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected HTTP 201 on /api/v1/auth/refresh, got %d", rec.Code)
		}
	})

	t.Run("missing refresh_token field returns 400 VALIDATION_ERROR canonical envelope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte(`{"refresh_token":""}`)))
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "VALIDATION_ERROR" {
			t.Errorf("expected VALIDATION_ERROR, got %q", errResp.Error.Code)
		}
		if errResp.Error.Field != "refresh_token" {
			t.Errorf("expected field 'refresh_token', got %q", errResp.Error.Field)
		}
	})

	t.Run("access token cannot be substituted for refresh token", func(t *testing.T) {
		rawAccessToken, _, _ := auth.GenerateToken() // nex_at_...

		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte(`{"refresh_token":"`+rawAccessToken+`"}`)))
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 when passing access token as refresh token, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "UNAUTHORIZED" {
			t.Errorf("expected UNAUTHORIZED, got %q", errResp.Error.Code)
		}
	})

	t.Run("tenant mismatch returns 403 TENANT_MISMATCH", func(t *testing.T) {
		rawRT3, hashRT3, _ := auth.GenerateRefreshToken()
		store.tokens[hashRT3] = &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessID,
			TokenHash: hashRT3,
			FamilyID:  familyID,
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}

		otherTenantID := uuid.New()
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte(`{"refresh_token":"`+rawRT3+`"}`)))
		req.Header.Set("X-Tenant-ID", otherTenantID.String())
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "TENANT_MISMATCH" {
			t.Errorf("expected TENANT_MISMATCH, got %q", errResp.Error.Code)
		}
	})

	t.Run("expired refresh token returns 401 TOKEN_EXPIRED", func(t *testing.T) {
		rawRTExp, hashRTExp, _ := auth.GenerateRefreshToken()
		store.tokens[hashRTExp] = &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessID,
			TokenHash: hashRTExp,
			FamilyID:  familyID,
			IssuedAt:  time.Now().Add(-48 * time.Hour),
			ExpiresAt: time.Now().Add(-24 * time.Hour),
		}

		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte(`{"refresh_token":"`+rawRTExp+`"}`)))
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "TOKEN_EXPIRED" {
			t.Errorf("expected TOKEN_EXPIRED, got %q", errResp.Error.Code)
		}
	})

	t.Run("replayed refresh token returns 401 SESSION_REVOKED", func(t *testing.T) {
		rawRTRev, hashRTRev, _ := auth.GenerateRefreshToken()
		revTime := time.Now().Add(-1 * time.Hour)
		store.tokens[hashRTRev] = &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessID,
			TokenHash: hashRTRev,
			FamilyID:  familyID,
			IssuedAt:  time.Now().Add(-2 * time.Hour),
			ExpiresAt: time.Now().Add(24 * time.Hour),
			RevokedAt: &revTime,
		}

		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte(`{"refresh_token":"`+rawRTRev+`"}`)))
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "SESSION_REVOKED" {
			t.Errorf("expected SESSION_REVOKED, got %q", errResp.Error.Code)
		}
	})

	t.Run("inactive user returns 401 UNAUTHORIZED", func(t *testing.T) {
		rawRTInact, hashRTInact, _ := auth.GenerateRefreshToken()
		store.tokens[hashRTInact] = &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessID,
			TokenHash: hashRTInact,
			FamilyID:  familyID,
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}

		store.rotateFunc = func(_ context.Context, _ string, _ *uuid.UUID, _, _ time.Duration) (*auth.RefreshTokenRotationResult, error) {
			return nil, auth.ErrUserInactive
		}
		defer func() { store.rotateFunc = nil }()

		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte(`{"refresh_token":"`+rawRTInact+`"}`)))
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}

		var errResp apperr.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Error.Code != "UNAUTHORIZED" {
			t.Errorf("expected UNAUTHORIZED, got %q", errResp.Error.Code)
		}
	})
}
