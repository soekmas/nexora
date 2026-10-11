package identity

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/postgres"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Handler exposes Identity capabilities via HTTP endpoints.
type Handler struct {
	service Service
	logger  *slog.Logger
}

// NewHandler constructs a new Identity HTTP Handler.
func NewHandler(service Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Routes sets up Chi routing for Identity endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Public authentication route (tenant context set via X-Tenant-ID header)
	r.Post("/login", h.handleLogin)

	// User management routes
	r.Group(func(protected chi.Router) {
		protected.Post("/users", h.handleCreateUser)
		protected.Get("/users", h.handleListUsers)
		protected.Get("/users/{id}", h.handleGetUser)
		protected.Patch("/users/{id}", h.handleUpdateUser)
		protected.Post("/users/{id}/password", h.handleChangePassword)

		// Role assignments
		protected.Get("/users/{id}/roles", h.handleGetUserRoles)
		protected.Post("/users/{id}/roles", h.handleAssignRole)
		protected.Delete("/users/{id}/roles/{roleId}", h.handleRevokeRole)

		// Scope assignments
		protected.Get("/users/{id}/scopes", h.handleGetUserScopes)
		protected.Post("/users/{id}/scopes", h.handleAssignScope)
		protected.Delete("/users/{id}/scopes/{scopeId}", h.handleRevokeScope)

		// Role and permission catalog discovery
		protected.Get("/roles", h.handleListRoles)
		protected.Get("/permissions", h.handleListPermissions)
	})

	return r
}

func (h *Handler) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func checkPermission(principal *auth.AuthenticatedPrincipal, perms ...string) *apperr.AppError {
	if principal == nil {
		return apperr.NewUnauthorizedError("Authentication required")
	}
	for _, p := range perms {
		if principal.HasPermission(p) {
			return nil
		}
	}
	return auth.EvaluatePermission(principal, perms[0])
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())

	// Resolve tenant context: either from authenticated principal or X-Tenant-ID header
	ctx := r.Context()
	var tenantID uuid.UUID
	if principal, ok := auth.PrincipalFromContext(ctx); ok && principal.TenantID != uuid.Nil {
		tenantID = principal.TenantID
	} else if tenantHdr := r.Header.Get("X-Tenant-ID"); tenantHdr != "" {
		parsed, err := uuid.Parse(tenantHdr)
		if err == nil {
			tenantID = parsed
			ctx = postgres.WithTenantID(ctx, tenantID)
		}
	}

	if tenantID == uuid.Nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Missing required X-Tenant-ID header"))
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		apperr.RespondError(w, reqID, apperr.NewValidationError("username/password", "Username and password are required"))
		return
	}

	clientIP := r.RemoteAddr
	userAgent := r.UserAgent()
	req.IPAddress = &clientIP
	req.UserAgent = &userAgent

	user, err := h.service.AuthenticateUser(ctx, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"user":        user.User,
		"roles":       user.Roles,
		"permissions": user.Permissions,
		"scopes":      user.Scopes,
	})
}

func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.create", "identity:user:create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	req.CreatedBy = &principal.UserID

	user, err := h.service.CreateUser(ctx, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, user)
}

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.read", "identity:user:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	user, err := h.service.GetUser(ctx, targetID)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, user)
}

func (h *Handler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.update", "identity:user:update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}
	req.UpdatedBy = &principal.UserID

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	updated, err := h.service.UpdateUser(ctx, targetID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.read", "identity:user:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	users, total, err := h.service.ListUsers(ctx, limit, offset)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  users,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	// User can change their own password, or user with identity:user:update can change it
	if principal.UserID != targetID {
		if permErr := checkPermission(principal, "user.update", "identity:user:update"); permErr != nil {
			apperr.RespondError(w, reqID, permErr)
			return
		}
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}
	req.UpdatedBy = &principal.UserID

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	if err := h.service.ChangePassword(ctx, targetID, req); err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGetUserRoles(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.read", "identity:user:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	user, err := h.service.GetUser(ctx, targetID)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, user.Roles)
}

func (h *Handler) handleAssignRole(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.role.assign", "identity:role:assign"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var body struct {
		RoleID uuid.UUID `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	if err := h.service.AssignRole(ctx, principal.UserID, targetID, body.RoleID); err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.role.assign", "identity:role:assign"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	roleIDStr := chi.URLParam(r, "roleId")
	roleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("roleId", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	if err := h.service.RevokeRole(ctx, principal.UserID, targetID, roleID); err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGetUserScopes(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.read", "identity:user:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	scopes, err := h.service.GetUserScopes(ctx, targetID)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, scopes)
}

func (h *Handler) handleAssignScope(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.scope.manage", "identity:scope:manage"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req AssignScopeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	scope, err := h.service.AssignScope(ctx, principal.UserID, targetID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, scope)
}

func (h *Handler) handleRevokeScope(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.scope.manage", "identity:scope:manage"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	scopeIDStr := chi.URLParam(r, "scopeId")
	scopeID, err := uuid.Parse(scopeIDStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("scopeId", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	if err := h.service.RevokeScope(ctx, principal.UserID, scopeID); err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListRoles(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.read", "identity:user:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	roles, err := h.service.ListRoles(ctx)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, roles)
}

func (h *Handler) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "user.read", "identity:user:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	perms, err := h.service.ListPermissions(ctx)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, perms)
}
