package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/identity"
	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
)

type mockService struct {
	createUserFn       func(ctx context.Context, req identity.CreateUserRequest) (*identity.UserAccount, error)
	getUserFn          func(ctx context.Context, id uuid.UUID) (*identity.UserWithDetails, error)
	updateUserFn       func(ctx context.Context, id uuid.UUID, req identity.UpdateUserRequest) (*identity.UserAccount, error)
	listUsersFn        func(ctx context.Context, limit, offset int) ([]identity.UserAccount, int64, error)
	authenticateUserFn func(ctx context.Context, req identity.LoginRequest) (*identity.UserWithDetails, error)
	changePasswordFn   func(ctx context.Context, id uuid.UUID, req identity.ChangePasswordRequest) error
	listRolesFn        func(ctx context.Context) ([]identity.Role, error)
	assignRoleFn       func(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error
	revokeRoleFn       func(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error
	listPermissionsFn  func(ctx context.Context) ([]identity.Permission, error)
	assignScopeFn      func(ctx context.Context, actorID, targetUserID uuid.UUID, req identity.AssignScopeRequest) (*identity.UserScope, error)
	revokeScopeFn      func(ctx context.Context, actorID, scopeID uuid.UUID) error
	getUserScopesFn    func(ctx context.Context, targetUserID uuid.UUID) ([]identity.UserScope, error)
}

func (m *mockService) CreateUser(ctx context.Context, req identity.CreateUserRequest) (*identity.UserAccount, error) {
	if m.createUserFn != nil {
		return m.createUserFn(ctx, req)
	}
	return nil, nil
}
func (m *mockService) GetUser(ctx context.Context, id uuid.UUID) (*identity.UserWithDetails, error) {
	if m.getUserFn != nil {
		return m.getUserFn(ctx, id)
	}
	return nil, nil
}
func (m *mockService) UpdateUser(ctx context.Context, id uuid.UUID, req identity.UpdateUserRequest) (*identity.UserAccount, error) {
	if m.updateUserFn != nil {
		return m.updateUserFn(ctx, id, req)
	}
	return nil, nil
}
func (m *mockService) ListUsers(ctx context.Context, limit, offset int) ([]identity.UserAccount, int64, error) {
	if m.listUsersFn != nil {
		return m.listUsersFn(ctx, limit, offset)
	}
	return nil, 0, nil
}
func (m *mockService) AuthenticateUser(ctx context.Context, req identity.LoginRequest) (*identity.UserWithDetails, error) {
	if m.authenticateUserFn != nil {
		return m.authenticateUserFn(ctx, req)
	}
	return nil, nil
}
func (m *mockService) ChangePassword(ctx context.Context, id uuid.UUID, req identity.ChangePasswordRequest) error {
	if m.changePasswordFn != nil {
		return m.changePasswordFn(ctx, id, req)
	}
	return nil
}
func (m *mockService) ListRoles(ctx context.Context) ([]identity.Role, error) {
	if m.listRolesFn != nil {
		return m.listRolesFn(ctx)
	}
	return nil, nil
}
func (m *mockService) AssignRole(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error {
	if m.assignRoleFn != nil {
		return m.assignRoleFn(ctx, actorID, targetUserID, roleID)
	}
	return nil
}
func (m *mockService) RevokeRole(ctx context.Context, actorID, targetUserID, roleID uuid.UUID) error {
	if m.revokeRoleFn != nil {
		return m.revokeRoleFn(ctx, actorID, targetUserID, roleID)
	}
	return nil
}
func (m *mockService) ListPermissions(ctx context.Context) ([]identity.Permission, error) {
	if m.listPermissionsFn != nil {
		return m.listPermissionsFn(ctx)
	}
	return nil, nil
}
func (m *mockService) AssignScope(ctx context.Context, actorID, targetUserID uuid.UUID, req identity.AssignScopeRequest) (*identity.UserScope, error) {
	if m.assignScopeFn != nil {
		return m.assignScopeFn(ctx, actorID, targetUserID, req)
	}
	return nil, nil
}
func (m *mockService) RevokeScope(ctx context.Context, actorID, scopeID uuid.UUID) error {
	if m.revokeScopeFn != nil {
		return m.revokeScopeFn(ctx, actorID, scopeID)
	}
	return nil
}
func (m *mockService) GetUserScopes(ctx context.Context, targetUserID uuid.UUID) ([]identity.UserScope, error) {
	if m.getUserScopesFn != nil {
		return m.getUserScopesFn(ctx, targetUserID)
	}
	return nil, nil
}

func TestHandler_Login_MissingTenantHeader(t *testing.T) {
	mockSvc := &mockService{}
	handler := identity.NewHandler(mockSvc, newDiscardLogger())
	router := handler.Routes()

	body, _ := json.Marshal(map[string]string{
		"username": "alice",
		"password": "Password123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No X-Tenant-ID header

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}

	var errResp apperr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Code != "BAD_REQUEST" {
		t.Fatalf("expected error code BAD_REQUEST, got %s", errResp.Error.Code)
	}
}

func TestHandler_Login_SuccessAndFailure(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()

	mockSvc := &mockService{
		authenticateUserFn: func(_ context.Context, req identity.LoginRequest) (*identity.UserWithDetails, error) {
			if req.Username == "alice" && req.Password == "ValidPass123!" {
				return &identity.UserWithDetails{
					User: identity.UserAccount{
						ID:          userID,
						TenantID:    tenantID,
						Username:    "alice",
						DisplayName: "Alice",
						Status:      identity.StatusActive,
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
						Version:     1,
					},
					Roles:       []identity.Role{},
					Permissions: []string{"user.read"},
				}, nil
			}
			return nil, identity.ErrInvalidCredentials
		},
	}

	handler := identity.NewHandler(mockSvc, newDiscardLogger())
	router := handler.Routes()

	// 1. Invalid credentials
	badBody, _ := json.Marshal(map[string]string{
		"username": "alice",
		"password": "WrongPassword!",
	})
	reqBad := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(badBody))
	reqBad.Header.Set("Content-Type", "application/json")
	reqBad.Header.Set("X-Tenant-ID", tenantID.String())

	wBad := httptest.NewRecorder()
	router.ServeHTTP(wBad, reqBad)

	if wBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", wBad.Code)
	}
	var errResp apperr.ErrorResponse
	_ = json.NewDecoder(wBad.Body).Decode(&errResp)
	if errResp.Error.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("expected INVALID_CREDENTIALS, got %s", errResp.Error.Code)
	}

	// 2. Valid credentials
	okBody, _ := json.Marshal(map[string]string{
		"username": "alice",
		"password": "ValidPass123!",
	})
	reqOK := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(okBody))
	reqOK.Header.Set("Content-Type", "application/json")
	reqOK.Header.Set("X-Tenant-ID", tenantID.String())

	wOK := httptest.NewRecorder()
	router.ServeHTTP(wOK, reqOK)

	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wOK.Code)
	}
}

func TestHandler_CreateUser_AuthorizationAndCreation(t *testing.T) {
	tenantID := uuid.New()
	creatorID := uuid.New()
	createdUserID := uuid.New()

	mockSvc := &mockService{
		createUserFn: func(_ context.Context, req identity.CreateUserRequest) (*identity.UserAccount, error) {
			return &identity.UserAccount{
				ID:          createdUserID,
				TenantID:    tenantID,
				Username:    req.Username,
				DisplayName: req.DisplayName,
				Status:      identity.StatusActive,
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
				Version:     1,
			}, nil
		},
	}

	handler := identity.NewHandler(mockSvc, newDiscardLogger())
	router := handler.Routes()

	body, _ := json.Marshal(map[string]string{
		"username":     "new_user",
		"display_name": "New User",
		"password":     "ValidPassword123!",
	})

	// 1. Unauthenticated request -> 401
	reqUnauth := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
	wUnauth := httptest.NewRecorder()
	router.ServeHTTP(wUnauth, reqUnauth)
	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", wUnauth.Code)
	}

	// 2. Authenticated but lacks user.create permission -> 403
	principalNoPerm := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      creatorID,
		Permissions: []string{"user.read"}, // missing user.create
	}
	ctxNoPerm := auth.WithPrincipal(context.Background(), principalNoPerm)
	reqNoPerm := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body)).WithContext(ctxNoPerm)
	wNoPerm := httptest.NewRecorder()
	router.ServeHTTP(wNoPerm, reqNoPerm)
	if wNoPerm.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", wNoPerm.Code)
	}

	// 3. Authenticated with user.create permission -> 201 Created
	principalWithPerm := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      creatorID,
		Permissions: []string{"user.create"},
	}
	ctxWithPerm := auth.WithPrincipal(context.Background(), principalWithPerm)
	reqWithPerm := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body)).WithContext(ctxWithPerm)
	wWithPerm := httptest.NewRecorder()
	router.ServeHTTP(wWithPerm, reqWithPerm)
	if wWithPerm.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", wWithPerm.Code, wWithPerm.Body.String())
	}
}

func TestHandler_AssignRole_SegregationOfDutiesDenied(t *testing.T) {
	tenantID := uuid.New()
	actorID := uuid.New()
	adminRoleID := uuid.New()

	mockSvc := &mockService{
		assignRoleFn: func(_ context.Context, aID, targetUserID, _ uuid.UUID) error {
			if aID == targetUserID {
				return identity.ErrSelfRoleAssignmentDenied
			}
			return nil
		},
	}

	handler := identity.NewHandler(mockSvc, newDiscardLogger())
	router := handler.Routes()

	principal := &auth.AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      actorID,
		Permissions: []string{"user.role.assign"},
	}
	ctx := auth.WithPrincipal(context.Background(), principal)

	body, _ := json.Marshal(map[string]uuid.UUID{
		"role_id": adminRoleID,
	})

	// Actor assigning to self -> 403 SoD violation
	req := httptest.NewRequest(http.MethodPost, "/users/"+actorID.String()+"/roles", bytes.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for SoD violation, got %d", w.Code)
	}

	var errResp apperr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "SOD_VIOLATION" {
		t.Fatalf("expected SOD_VIOLATION code, got %s", errResp.Error.Code)
	}
}
