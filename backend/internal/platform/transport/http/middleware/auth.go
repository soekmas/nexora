package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/postgres"
)

var (
	sessionTouchMap      sync.Map
	sessionTouchInterval = 30 * time.Second
)

func shouldTouchSession(sessID uuid.UUID) bool {
	now := time.Now()
	if val, ok := sessionTouchMap.Load(sessID); ok {
		if lastTouch, ok := val.(time.Time); ok && now.Sub(lastTouch) < sessionTouchInterval {
			return false
		}
	}
	sessionTouchMap.Store(sessID, now)
	return true
}

// Authenticate constructs a middleware that validates incoming Bearer tokens,
// checks session expiration and revocation, enforces tenant and organizational scope
// boundaries, and binds the AuthenticatedPrincipal to both the request context and
// the PostgreSQL RLS PrincipalContext.
func Authenticate(store auth.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				// No authorization header provided. Pass through for public or unauthenticated endpoints.
				// Protected endpoints will be caught by RequireAuth.
				next.ServeHTTP(w, r)
				return
			}

			reqID := GetRequestID(r.Context())

			rawToken, err := auth.ParseBearerToken(authHeader)
			if err != nil {
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Invalid bearer token format"))
				return
			}

			tokenHash := auth.HashToken(rawToken)
			session, err := store.GetSessionByTokenHash(r.Context(), tokenHash)
			if err != nil {
				if errors.Is(err, auth.ErrSessionNotFound) {
					apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Invalid or unknown session token"))
					return
				}
				apperr.RespondError(w, reqID, apperr.NewInternalError("Failed to verify session"))
				return
			}

			// Validate session revocation
			if session.RevokedAt != nil {
				apperr.RespondError(w, reqID, apperr.NewSessionRevokedError("Session has been revoked"))
				return
			}

			// Validate session expiry
			if time.Now().After(session.ExpiresAt) {
				apperr.RespondError(w, reqID, apperr.NewTokenExpiredError("Session token has expired"))
				return
			}

			// Validate client-supplied X-Tenant-ID header if present
			if clientTenantStr := r.Header.Get("X-Tenant-ID"); clientTenantStr != "" {
				clientTenantID, parseErr := uuid.Parse(clientTenantStr)
				if parseErr != nil || clientTenantID != session.TenantID {
					apperr.RespondError(w, reqID, apperr.NewTenantMismatchError("Tenant header does not match authenticated session"))
					return
				}
			}

			// Resolve full principal (user account, roles, permissions, scopes)
			principal, err := store.GetUserPrincipal(r.Context(), session.TenantID, session.UserID)
			if err != nil {
				if errors.Is(err, auth.ErrUserNotFound) || errors.Is(err, auth.ErrUserInactive) {
					apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("User account is inactive, locked, or disabled"))
					return
				}
				apperr.RespondError(w, reqID, apperr.NewInternalError("Failed to resolve user authorization context"))
				return
			}

			// Clone principal for request-scoped isolation across concurrent requests
			reqPrincipal := principal.Clone()
			reqPrincipal.SessionID = session.ID

			// Validate client-supplied organizational headers against authorized scopes
			if err := validateAndBindOrganizationalHeaders(r, reqPrincipal); err != nil {
				apperr.RespondError(w, reqID, err)
				return
			}

			// Touch session last_seen_at asynchronously with debounce to reduce write amplification
			if shouldTouchSession(session.ID) {
				go func(sessID, tenantID uuid.UUID) {
					touchCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					touchCtx = postgres.WithTenantID(touchCtx, tenantID)
					_ = store.TouchSession(touchCtx, sessID)
				}(session.ID, session.TenantID)
			}

			// Bind principal to context and PostgreSQL transaction RLS context
			ctx := auth.WithPrincipal(r.Context(), reqPrincipal)
			ctx = postgres.WithPrincipalContext(ctx, reqPrincipal.ToPrincipalContext())

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth constructs an HTTP middleware that denies unauthenticated requests with HTTP 401.
func RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission constructs an HTTP middleware that denies access with HTTP 403
// if the authenticated principal lacks the required permission.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal == nil {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
				return
			}

			if authErr := auth.EvaluatePermission(principal, permission); authErr != nil {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, authErr)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermission constructs an HTTP middleware that denies access with HTTP 403
// unless the authenticated principal possesses at least one of the specified permissions.
func RequireAnyPermission(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal == nil {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
				return
			}

			if !principal.HasAnyPermission(permissions...) {
				reqID := GetRequestID(r.Context())
				err := apperr.NewForbiddenError("Missing required permissions")
				err.Detail.Details = map[string]any{
					"required_permissions": permissions,
				}
				apperr.RespondError(w, reqID, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole constructs an HTTP middleware that requires the principal to have the given role.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal == nil {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
				return
			}

			if !principal.HasRole(role) {
				reqID := GetRequestID(r.Context())
				err := apperr.NewForbiddenError(fmt.Sprintf("User does not have role %q", role))
				err.Detail.Details = map[string]any{
					"required_role": role,
				}
				apperr.RespondError(w, reqID, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireScope constructs an HTTP middleware that validates the target resource ID
// against the principal's authorized organization scopes (e.g., STORE, WAREHOUSE, TERMINAL).
func RequireScope(scopeType string, getTargetID func(*http.Request) uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal == nil {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
				return
			}

			targetID := getTargetID(r)
			if authErr := auth.EvaluateScope(principal, scopeType, targetID); authErr != nil {
				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, authErr)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func validateAndBindOrganizationalHeaders(r *http.Request, p *auth.AuthenticatedPrincipal) *apperr.AppError {
	if headerVal := r.Header.Get("X-Company-ID"); headerVal != "" {
		id, err := uuid.Parse(headerVal)
		if err != nil || !p.HasScope("COMPANY", id) {
			return apperr.NewScopeAccessDeniedError("Access denied to requested company scope", map[string]any{
				"scope_type": "COMPANY",
				"target_id":  headerVal,
			})
		}
		p.ActiveCompanyID = id
	}

	if headerVal := r.Header.Get("X-Store-ID"); headerVal != "" {
		id, err := uuid.Parse(headerVal)
		if err != nil || !p.HasScope("STORE", id) {
			return apperr.NewScopeAccessDeniedError("Access denied to requested store scope", map[string]any{
				"scope_type": "STORE",
				"target_id":  headerVal,
			})
		}
		p.ActiveStoreID = id
	}

	if headerVal := r.Header.Get("X-Warehouse-ID"); headerVal != "" {
		id, err := uuid.Parse(headerVal)
		if err != nil || !p.HasScope("WAREHOUSE", id) {
			return apperr.NewScopeAccessDeniedError("Access denied to requested warehouse scope", map[string]any{
				"scope_type": "WAREHOUSE",
				"target_id":  headerVal,
			})
		}
		p.ActiveWarehouseID = id
	}

	if headerVal := r.Header.Get("X-Terminal-ID"); headerVal != "" {
		id, err := uuid.Parse(headerVal)
		if err != nil || !p.HasScope("TERMINAL", id) {
			return apperr.NewScopeAccessDeniedError("Access denied to requested terminal scope", map[string]any{
				"scope_type": "TERMINAL",
				"target_id":  headerVal,
			})
		}
		p.ActiveTerminalID = id
	}

	return nil
}
