package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

// Store defines persistence operations for the Identity domain.
type Store interface {
	CreateUser(ctx context.Context, user *UserAccount) error
	GetUserByID(ctx context.Context, id uuid.UUID) (*UserAccount, error)
	GetUserByUsername(ctx context.Context, username string) (*UserAccount, error)
	UpdateUser(ctx context.Context, user *UserAccount) error
	ListUsers(ctx context.Context, limit, offset int) ([]UserAccount, int64, error)

	RecordLoginAttempt(ctx context.Context, attempt *LoginAttempt) error
	UpdateLoginSuccess(ctx context.Context, userID uuid.UUID, loginAt time.Time) error
	UpdateLoginFailure(ctx context.Context, userID uuid.UUID, lockUntil *time.Time) (int, bool, error)

	GetRoleByID(ctx context.Context, id uuid.UUID) (*Role, error)
	GetRoleByCode(ctx context.Context, code string) (*Role, error)
	ListRoles(ctx context.Context) ([]Role, error)

	AssignUserRole(ctx context.Context, userID, roleID uuid.UUID, assignedBy *uuid.UUID) error
	RevokeUserRole(ctx context.Context, userID, roleID uuid.UUID) error
	GetUserRoles(ctx context.Context, userID uuid.UUID) ([]Role, error)
	GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error)
	ListPermissions(ctx context.Context) ([]Permission, error)

	AssignUserScope(ctx context.Context, scope *UserScope) error
	RevokeUserScope(ctx context.Context, scopeID uuid.UUID) error
	GetUserScopes(ctx context.Context, userID uuid.UUID) ([]UserScope, error)
}

// PgxStore implements Store using PostgreSQL.
type PgxStore struct {
	pool *pgxpool.Pool
}

// NewPgxStore constructs a new PgxStore.
func NewPgxStore(pool *pgxpool.Pool) *PgxStore {
	return &PgxStore{pool: pool}
}

func (s *PgxStore) exec(ctx context.Context) postgres.DBTX {
	return postgres.GetDBTX(ctx, s.pool)
}

// CreateUser inserts a new user account.
func (s *PgxStore) CreateUser(ctx context.Context, user *UserAccount) error {
	if user == nil {
		return errors.New("identity: user cannot be nil")
	}
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	if user.Version == 0 {
		user.Version = 1
	}

	const query = `
		INSERT INTO identity.user_account (
			id, tenant_id, username, email, password_hash, display_name,
			status, failed_login_count, locked_until, last_login_at,
			created_at, updated_at, created_by, updated_by, version
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		) RETURNING id, created_at, updated_at, version`

	err := s.exec(ctx).QueryRow(ctx, query,
		user.ID, user.TenantID, user.Username, user.Email, user.PasswordHash, user.DisplayName,
		user.Status, user.FailedLoginCount, user.LockedUntil, user.LastLoginAt,
		user.CreatedAt, user.UpdatedAt, user.CreatedBy, user.UpdatedBy, user.Version,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt, &user.Version)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if strings.Contains(pgErr.ConstraintName, "username") {
				return ErrUsernameExists
			}
			if strings.Contains(pgErr.ConstraintName, "email") {
				return ErrEmailExists
			}
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUserByID fetches a user account by primary key.
func (s *PgxStore) GetUserByID(ctx context.Context, id uuid.UUID) (*UserAccount, error) {
	const query = `
		SELECT id, tenant_id, username, email, password_hash, display_name,
		       status, failed_login_count, locked_until, last_login_at,
		       created_at, updated_at, created_by, updated_by, version
		FROM identity.user_account
		WHERE id = $1`

	var u UserAccount
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&u.ID, &u.TenantID, &u.Username, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.Status, &u.FailedLoginCount, &u.LockedUntil, &u.LastLoginAt,
		&u.CreatedAt, &u.UpdatedAt, &u.CreatedBy, &u.UpdatedBy, &u.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &u, nil
}

// GetUserByUsername fetches a user account by username.
func (s *PgxStore) GetUserByUsername(ctx context.Context, username string) (*UserAccount, error) {
	const query = `
		SELECT id, tenant_id, username, email, password_hash, display_name,
		       status, failed_login_count, locked_until, last_login_at,
		       created_at, updated_at, created_by, updated_by, version
		FROM identity.user_account
		WHERE username = $1`

	var u UserAccount
	err := s.exec(ctx).QueryRow(ctx, query, username).Scan(
		&u.ID, &u.TenantID, &u.Username, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.Status, &u.FailedLoginCount, &u.LockedUntil, &u.LastLoginAt,
		&u.CreatedAt, &u.UpdatedAt, &u.CreatedBy, &u.UpdatedBy, &u.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return &u, nil
}

// UpdateUser performs an optimistic concurrency update on a user account.
func (s *PgxStore) UpdateUser(ctx context.Context, user *UserAccount) error {
	if user == nil {
		return errors.New("identity: user cannot be nil")
	}

	const query = `
		UPDATE identity.user_account
		SET display_name = $1,
		    email = $2,
		    status = $3,
		    password_hash = $4,
		    updated_by = $5,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $6 AND version = $7
		RETURNING version, updated_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		user.DisplayName, user.Email, user.Status, user.PasswordHash, user.UpdatedBy, user.ID, user.Version,
	).Scan(&user.Version, &user.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Check if user exists to distinguish between not found and version conflict
			existing, getErr := s.GetUserByID(ctx, user.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Version != user.Version {
				return fmt.Errorf("user update conflict: current version is %d, expected %d", existing.Version, user.Version)
			}
			return ErrUserNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if strings.Contains(pgErr.ConstraintName, "email") {
				return ErrEmailExists
			}
		}
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// ListUsers queries user accounts with pagination.
func (s *PgxStore) ListUsers(ctx context.Context, limit, offset int) ([]UserAccount, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	err := s.exec(ctx).QueryRow(ctx, `SELECT count(*) FROM identity.user_account`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	const query = `
		SELECT id, tenant_id, username, email, password_hash, display_name,
		       status, failed_login_count, locked_until, last_login_at,
		       created_at, updated_at, created_by, updated_by, version
		FROM identity.user_account
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.exec(ctx).Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []UserAccount
	for rows.Next() {
		var u UserAccount
		if err := rows.Scan(
			&u.ID, &u.TenantID, &u.Username, &u.Email, &u.PasswordHash, &u.DisplayName,
			&u.Status, &u.FailedLoginCount, &u.LockedUntil, &u.LastLoginAt,
			&u.CreatedAt, &u.UpdatedAt, &u.CreatedBy, &u.UpdatedBy, &u.Version,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
	}

	return users, total, nil
}

// RecordLoginAttempt inserts an authentication attempt into identity.login_attempt.
func (s *PgxStore) RecordLoginAttempt(ctx context.Context, attempt *LoginAttempt) error {
	if attempt == nil {
		return errors.New("identity: attempt cannot be nil")
	}
	if attempt.OccurredAt.IsZero() {
		attempt.OccurredAt = time.Now().UTC()
	}

	const query = `
		INSERT INTO identity.login_attempt (
			tenant_id, user_id, username, success, failure_reason,
			ip_address, user_agent, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`

	err := s.exec(ctx).QueryRow(ctx, query,
		attempt.TenantID, attempt.UserID, attempt.Username, attempt.Success,
		attempt.FailureReason, attempt.IPAddress, attempt.UserAgent, attempt.OccurredAt,
	).Scan(&attempt.ID)

	if err != nil {
		return fmt.Errorf("record login attempt: %w", err)
	}
	return nil
}

// UpdateLoginSuccess resets failed login count, clears lockout, and updates last_login_at.
func (s *PgxStore) UpdateLoginSuccess(ctx context.Context, userID uuid.UUID, loginAt time.Time) error {
	const query = `
		UPDATE identity.user_account
		SET failed_login_count = 0,
		    locked_until = NULL,
		    last_login_at = $1,
		    status = CASE WHEN status = 'LOCKED' THEN 'ACTIVE' ELSE status END,
		    updated_at = now()
		WHERE id = $2`

	tag, err := s.exec(ctx).Exec(ctx, query, loginAt, userID)
	if err != nil {
		return fmt.Errorf("update login success: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdateLoginFailure increments failed login count, locking the account if threshold is met.
func (s *PgxStore) UpdateLoginFailure(ctx context.Context, userID uuid.UUID, lockUntil *time.Time) (int, bool, error) {
	const query = `
		UPDATE identity.user_account
		SET failed_login_count = failed_login_count + 1,
		    locked_until = CASE
		        WHEN (failed_login_count + 1) >= $1 THEN COALESCE($2, now() + interval '15 minutes')
		        ELSE locked_until
		    END,
		    status = CASE
		        WHEN (failed_login_count + 1) >= $1 THEN 'LOCKED'
		        ELSE status
		    END,
		    updated_at = now()
		WHERE id = $3
		RETURNING failed_login_count, (failed_login_count >= $1)`

	var newCount int
	var isLocked bool
	err := s.exec(ctx).QueryRow(ctx, query, MaxFailedLoginAttempts, lockUntil, userID).Scan(&newCount, &isLocked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, ErrUserNotFound
		}
		return 0, false, fmt.Errorf("update login failure: %w", err)
	}
	return newCount, isLocked, nil
}

// GetRoleByID fetches a role by ID.
func (s *PgxStore) GetRoleByID(ctx context.Context, id uuid.UUID) (*Role, error) {
	const query = `
		SELECT id, tenant_id, code, name, description, is_system,
		       created_at, updated_at, created_by, updated_by, version
		FROM identity.role
		WHERE id = $1`

	var r Role
	err := s.exec(ctx).QueryRow(ctx, query, id).Scan(
		&r.ID, &r.TenantID, &r.Code, &r.Name, &r.Description, &r.IsSystem,
		&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, fmt.Errorf("get role by id: %w", err)
	}
	return &r, nil
}

// GetRoleByCode fetches a role by code.
func (s *PgxStore) GetRoleByCode(ctx context.Context, code string) (*Role, error) {
	const query = `
		SELECT id, tenant_id, code, name, description, is_system,
		       created_at, updated_at, created_by, updated_by, version
		FROM identity.role
		WHERE code = $1`

	var r Role
	err := s.exec(ctx).QueryRow(ctx, query, code).Scan(
		&r.ID, &r.TenantID, &r.Code, &r.Name, &r.Description, &r.IsSystem,
		&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, fmt.Errorf("get role by code: %w", err)
	}
	return &r, nil
}

// ListRoles queries all roles for the current tenant.
func (s *PgxStore) ListRoles(ctx context.Context) ([]Role, error) {
	const query = `
		SELECT id, tenant_id, code, name, description, is_system,
		       created_at, updated_at, created_by, updated_by, version
		FROM identity.role
		ORDER BY is_system DESC, code ASC`

	rows, err := s.exec(ctx).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.Code, &r.Name, &r.Description, &r.IsSystem,
			&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
		); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// AssignUserRole assigns a role to a user.
func (s *PgxStore) AssignUserRole(ctx context.Context, userID, roleID uuid.UUID, assignedBy *uuid.UUID) error {
	const query = `
		INSERT INTO identity.user_role (user_id, role_id, assigned_at, assigned_by)
		VALUES ($1, $2, now(), $3)`

	_, err := s.exec(ctx).Exec(ctx, query, userID, roleID, assignedBy)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrUserRoleExists
		}
		return fmt.Errorf("assign user role: %w", err)
	}
	return nil
}

// RevokeUserRole revokes a role from a user.
func (s *PgxStore) RevokeUserRole(ctx context.Context, userID, roleID uuid.UUID) error {
	const query = `
		DELETE FROM identity.user_role
		WHERE user_id = $1 AND role_id = $2`

	tag, err := s.exec(ctx).Exec(ctx, query, userID, roleID)
	if err != nil {
		return fmt.Errorf("revoke user role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserRoleNotFound
	}
	return nil
}

// GetUserRoles returns all roles assigned to a user.
func (s *PgxStore) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]Role, error) {
	const query = `
		SELECT r.id, r.tenant_id, r.code, r.name, r.description, r.is_system,
		       r.created_at, r.updated_at, r.created_by, r.updated_by, r.version
		FROM identity.role r
		JOIN identity.user_role ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY r.code ASC`

	rows, err := s.exec(ctx).Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.Code, &r.Name, &r.Description, &r.IsSystem,
			&r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &r.Version,
		); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GetUserPermissions aggregates all distinct permission codes assigned to a user via roles.
func (s *PgxStore) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const query = `
		SELECT DISTINCT p.code
		FROM identity.permission p
		JOIN identity.role_permission rp ON rp.permission_id = p.id
		JOIN identity.user_role ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1
		ORDER BY p.code ASC`

	rows, err := s.exec(ctx).Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("get user permissions: %w", err)
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, code)
	}
	return perms, rows.Err()
}

// ListPermissions queries all catalog permissions.
func (s *PgxStore) ListPermissions(ctx context.Context) ([]Permission, error) {
	const query = `
		SELECT id, code, name, module, action
		FROM identity.permission
		ORDER BY module ASC, action ASC`

	rows, err := s.exec(ctx).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	defer rows.Close()

	var perms []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Module, &p.Action); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// AssignUserScope binds a user to an organizational boundary.
func (s *PgxStore) AssignUserScope(ctx context.Context, scope *UserScope) error {
	if scope == nil {
		return errors.New("identity: scope cannot be nil")
	}
	if scope.ID == uuid.Nil {
		scope.ID = uuid.New()
	}
	if scope.CreatedAt.IsZero() {
		scope.CreatedAt = time.Now().UTC()
	}

	const query = `
		INSERT INTO identity.user_scope (
			id, tenant_id, user_id, company_id, region_id, store_id,
			warehouse_id, terminal_id, scope_type, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`

	err := s.exec(ctx).QueryRow(ctx, query,
		scope.ID, scope.TenantID, scope.UserID, scope.CompanyID, scope.RegionID,
		scope.StoreID, scope.WarehouseID, scope.TerminalID, scope.ScopeType, scope.CreatedAt,
	).Scan(&scope.ID, &scope.CreatedAt)

	if err != nil {
		return fmt.Errorf("assign user scope: %w", err)
	}
	return nil
}

// RevokeUserScope deletes a user scope binding.
func (s *PgxStore) RevokeUserScope(ctx context.Context, scopeID uuid.UUID) error {
	const query = `
		DELETE FROM identity.user_scope
		WHERE id = $1`

	tag, err := s.exec(ctx).Exec(ctx, query, scopeID)
	if err != nil {
		return fmt.Errorf("revoke user scope: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("scope not found")
	}
	return nil
}

// GetUserScopes returns all scopes bound to a user.
func (s *PgxStore) GetUserScopes(ctx context.Context, userID uuid.UUID) ([]UserScope, error) {
	const query = `
		SELECT id, tenant_id, user_id, company_id, region_id, store_id,
		       warehouse_id, terminal_id, scope_type, created_at
		FROM identity.user_scope
		WHERE user_id = $1
		ORDER BY created_at ASC`

	rows, err := s.exec(ctx).Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("get user scopes: %w", err)
	}
	defer rows.Close()

	var scopes []UserScope
	for rows.Next() {
		var sc UserScope
		if err := rows.Scan(
			&sc.ID, &sc.TenantID, &sc.UserID, &sc.CompanyID, &sc.RegionID,
			&sc.StoreID, &sc.WarehouseID, &sc.TerminalID, &sc.ScopeType, &sc.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user scope: %w", err)
		}
		scopes = append(scopes, sc)
	}
	return scopes, rows.Err()
}
