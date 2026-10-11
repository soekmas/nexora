package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Default token lifetimes conforming to enterprise security standards.
const (
	DefaultAccessTokenTTL  = 15 * time.Minute
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
)

// RefreshRequest models the payload for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// UserContextResponse models user context inside AuthResponse.
type UserContextResponse struct {
	ID          string   `json:"id"`
	Username    string   `json:"username,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// AuthResponse models the canonical response for POST /auth/refresh.
type AuthResponse struct {
	AccessToken  string              `json:"access_token"`
	RefreshToken string              `json:"refresh_token"`
	TokenType    string              `json:"token_type"`
	ExpiresIn    int                 `json:"expires_in"`
	User         UserContextResponse `json:"user"`
}

// AuthHandler handles HTTP authentication lifecycle operations.
type AuthHandler struct {
	store      auth.RefreshTokenStore
	logger     *slog.Logger
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewAuthHandler constructs an AuthHandler with configured store and token lifetimes.
func NewAuthHandler(store auth.RefreshTokenStore, logger *slog.Logger, accessTTL, refreshTTL time.Duration) *AuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if accessTTL <= 0 {
		accessTTL = DefaultAccessTokenTTL
	}
	if refreshTTL <= 0 {
		refreshTTL = DefaultRefreshTokenTTL
	}
	return &AuthHandler{
		store:      store,
		logger:     logger,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// HandleRefresh handles POST /auth/refresh with single-use rotation, replay detection,
// and tenant scoping.
func (h *AuthHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())

	if h.store == nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError("Authentication store is not configured"))
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid request body"))
		return
	}

	if req.RefreshToken == "" {
		apperr.RespondError(w, reqID, apperr.NewValidationError("refresh_token", "Missing required field"))
		return
	}

	// 1. Validate refresh token format (must begin with nex_rt_ and have valid entropy)
	if !auth.ValidateRefreshTokenFormat(req.RefreshToken) {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Invalid refresh token format"))
		return
	}

	// 2. Validate client-supplied X-Tenant-ID header if present
	var expectedTenantID *uuid.UUID
	if tenantStr := r.Header.Get("X-Tenant-ID"); tenantStr != "" {
		parsedTenantID, err := uuid.Parse(tenantStr)
		if err != nil {
			apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid X-Tenant-ID header format"))
			return
		}
		expectedTenantID = &parsedTenantID
	}

	// 3. Hash raw token for lookup (raw token is never logged or stored)
	tokenHash := auth.HashToken(req.RefreshToken)

	// 4. Perform atomic single-use rotation
	result, err := h.store.RotateRefreshToken(r.Context(), tokenHash, expectedTenantID, h.accessTTL, h.refreshTTL)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrRefreshTokenNotFound):
			apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Invalid or unknown refresh token"))
		case errors.Is(err, auth.ErrRefreshTokenExpired):
			apperr.RespondError(w, reqID, apperr.NewTokenExpiredError("Refresh token has expired"))
		case errors.Is(err, auth.ErrRefreshTokenReplayDetected):
			h.logger.Warn("Refresh token replay attack detected; session and token family revoked",
				slog.String("request_id", reqID))
			apperr.RespondError(w, reqID, apperr.NewSessionRevokedError("Refresh token replay detected; token family has been revoked"))
		case errors.Is(err, auth.ErrRefreshTokenRevoked):
			apperr.RespondError(w, reqID, apperr.NewSessionRevokedError("Refresh token has been revoked"))
		case errors.Is(err, auth.ErrRefreshTokenTenantMismatch):
			apperr.RespondError(w, reqID, apperr.NewTenantMismatchError("Tenant header does not match authenticated session"))
		case errors.Is(err, auth.ErrUserInactive) || errors.Is(err, auth.ErrUserNotFound):
			apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("User account is inactive, locked, or disabled"))
		case errors.Is(err, auth.ErrSessionRevoked):
			apperr.RespondError(w, reqID, apperr.NewSessionRevokedError("Session has been revoked"))
		case errors.Is(err, auth.ErrSessionExpired):
			apperr.RespondError(w, reqID, apperr.NewTokenExpiredError("Session has expired"))
		default:
			h.logger.Error("Token refresh operation failed unexpectedly",
				slog.String("request_id", reqID),
				slog.String("error", err.Error()))
			apperr.RespondError(w, reqID, apperr.NewInternalError("Failed to process token refresh"))
		}
		return
	}

	// 5. Construct canonical AuthResponse
	resp := AuthResponse{
		AccessToken:  result.RawAccessToken,
		RefreshToken: result.RawRefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    result.AccessExpiresIn,
		User: UserContextResponse{
			ID:          result.UserID.String(),
			Username:    result.Principal.Username,
			DisplayName: result.Principal.DisplayName,
			Roles:       result.Principal.Roles,
			Permissions: result.Principal.Permissions,
		},
	}

	RespondJSON(w, http.StatusCreated, resp)
}
