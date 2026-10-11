package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/postgres"
)

type principalCtxKey struct{}

var principalKey = principalCtxKey{}

// UserScope models an organization boundary assignment from identity.user_scope.
type UserScope struct {
	Type        string
	CompanyID   *uuid.UUID
	RegionID    *uuid.UUID
	StoreID     *uuid.UUID
	WarehouseID *uuid.UUID
	TerminalID  *uuid.UUID
}

// AuthenticatedPrincipal encapsulates the security identity, roles, permissions,
// and organization scopes of an authenticated user for the current request.
type AuthenticatedPrincipal struct {
	SessionID   uuid.UUID
	TenantID    uuid.UUID
	UserID      uuid.UUID
	Username    string
	DisplayName string
	Roles       []string
	Permissions []string
	Scopes      []UserScope

	// Active context boundaries validated against Scopes
	ActiveCompanyID   uuid.UUID
	ActiveStoreID     uuid.UUID
	ActiveWarehouseID uuid.UUID
	ActiveTerminalID  uuid.UUID
}

// Clone returns an isolated deep copy of AuthenticatedPrincipal to prevent concurrent request mutations.
func (p *AuthenticatedPrincipal) Clone() *AuthenticatedPrincipal {
	if p == nil {
		return nil
	}

	cp := *p
	if len(p.Roles) > 0 {
		cp.Roles = make([]string, len(p.Roles))
		copy(cp.Roles, p.Roles)
	}
	if len(p.Permissions) > 0 {
		cp.Permissions = make([]string, len(p.Permissions))
		copy(cp.Permissions, p.Permissions)
	}
	if len(p.Scopes) > 0 {
		cp.Scopes = make([]UserScope, len(p.Scopes))
		copy(cp.Scopes, p.Scopes)
	}
	return &cp
}

// HasPermission checks if the principal possesses the requested permission.
// It supports exact matching and full wildcard ("*").
func (p *AuthenticatedPrincipal) HasPermission(permission string) bool {
	if p == nil || permission == "" {
		return false
	}

	for _, perm := range p.Permissions {
		if perm == "*" || perm == permission {
			return true
		}
	}
	return false
}

// HasAnyPermission checks if the principal possesses at least one of the specified permissions.
func (p *AuthenticatedPrincipal) HasAnyPermission(permissions ...string) bool {
	if p == nil || len(permissions) == 0 {
		return false
	}

	for _, perm := range permissions {
		if p.HasPermission(perm) {
			return true
		}
	}
	return false
}

// HasRole checks if the principal is assigned the specified role code.
func (p *AuthenticatedPrincipal) HasRole(role string) bool {
	if p == nil || role == "" {
		return false
	}

	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasScope evaluates whether the principal's authorized scopes grant access to the specified target.
func (p *AuthenticatedPrincipal) HasScope(scopeType string, id uuid.UUID) bool {
	if p == nil || id == uuid.Nil {
		return false
	}

	for _, s := range p.Scopes {
		switch scopeType {
		case "COMPANY":
			if s.CompanyID != nil && *s.CompanyID == id {
				return true
			}
		case "REGION":
			if s.RegionID != nil && *s.RegionID == id {
				return true
			}
		case "STORE":
			if s.StoreID != nil && *s.StoreID == id {
				return true
			}
		case "WAREHOUSE":
			if s.WarehouseID != nil && *s.WarehouseID == id {
				return true
			}
		case "TERMINAL":
			if s.TerminalID != nil && *s.TerminalID == id {
				return true
			}
		}
	}
	return false
}

// ToPrincipalContext converts the AuthenticatedPrincipal into the postgres.PrincipalContext
// required for RLS parameter binding during database transactions.
func (p *AuthenticatedPrincipal) ToPrincipalContext() postgres.PrincipalContext {
	if p == nil {
		return postgres.PrincipalContext{}
	}

	return postgres.PrincipalContext{
		TenantID:    p.TenantID,
		UserID:      p.UserID,
		CompanyID:   p.ActiveCompanyID,
		StoreID:     p.ActiveStoreID,
		WarehouseID: p.ActiveWarehouseID,
		TerminalID:  p.ActiveTerminalID,
	}
}

// WithPrincipal stores the AuthenticatedPrincipal in the context.
func WithPrincipal(ctx context.Context, p *AuthenticatedPrincipal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFromContext retrieves the AuthenticatedPrincipal from the context if present.
func PrincipalFromContext(ctx context.Context) (*AuthenticatedPrincipal, bool) {
	if ctx == nil {
		return nil, false
	}
	p, ok := ctx.Value(principalKey).(*AuthenticatedPrincipal)
	return p, ok && p != nil
}

// MustPrincipalFromContext retrieves the AuthenticatedPrincipal from context or returns an error.
func MustPrincipalFromContext(ctx context.Context) (*AuthenticatedPrincipal, error) {
	p, ok := PrincipalFromContext(ctx)
	if !ok || p == nil {
		return nil, errors.New("authenticated principal not found in context")
	}
	return p, nil
}
