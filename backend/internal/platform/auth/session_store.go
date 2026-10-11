package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/postgres"
)

var (
	// ErrSessionNotFound indicates that no session matched the supplied token hash.
	ErrSessionNotFound = errors.New("session not found")

	// ErrUserNotFound indicates that the user account does not exist.
	ErrUserNotFound = errors.New("user account not found")

	// ErrUserInactive indicates that the user account is locked, disabled, or pending.
	ErrUserInactive = errors.New("user account is not active")
)

// Session represents an active or historical user session from identity.session.
type Session struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	TokenHash  string
	IPAddress  *string
	UserAgent  *string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastSeenAt *time.Time
}

// IsRevoked reports whether the session has been explicitly revoked.
func (s *Session) IsRevoked() bool {
	return s.RevokedAt != nil
}

// IsExpired reports whether the session has expired relative to the given time.
func (s *Session) IsExpired(now time.Time) bool {
	return now.After(s.ExpiresAt)
}

// SessionStore defines the contract for session lifecycle tracking and principal resolution.
type SessionStore interface {
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	CreateSession(ctx context.Context, session *Session) error
	RevokeSession(ctx context.Context, sessionID uuid.UUID) error
	RevokeUserSessions(ctx context.Context, tenantID, userID uuid.UUID) error
	TouchSession(ctx context.Context, sessionID uuid.UUID) error
	GetUserPrincipal(ctx context.Context, tenantID, userID uuid.UUID) (*AuthenticatedPrincipal, error)
}

// PgxSessionStore implements SessionStore using PostgreSQL connection pool.
type PgxSessionStore struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	tm     postgres.TxManager
}

// NewPgxSessionStore constructs a new PostgreSQL-backed SessionStore.
func NewPgxSessionStore(pool *pgxpool.Pool, logger *slog.Logger) *PgxSessionStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &PgxSessionStore{
		pool:   pool,
		logger: logger,
		tm:     postgres.NewTxManager(pool, logger),
	}
}

func (s *PgxSessionStore) resolveTenant(ctx context.Context, resolver, tokenHash string) (uuid.UUID, error) {
	var tenant pgtype.UUID
	if err := s.pool.QueryRow(ctx, "SELECT "+resolver+"($1)", tokenHash).Scan(&tenant); err != nil {
		return uuid.Nil, fmt.Errorf("resolve authentication tenant: %w", err)
	}
	if !tenant.Valid {
		return uuid.Nil, pgx.ErrNoRows
	}
	return uuid.UUID(tenant.Bytes), nil
}

func (s *PgxSessionStore) resolveSessionTenant(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	return s.resolveTenant(ctx, "platform.resolve_session_tenant", tokenHash)
}

func (s *PgxSessionStore) resolveRefreshTokenTenant(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	return s.resolveTenant(ctx, "platform.resolve_refresh_token_tenant", tokenHash)
}

func (s *PgxSessionStore) withinTenantTx(ctx context.Context, tenantID uuid.UUID, fn func(context.Context, postgres.DBTX) error) error {
	if tenantID == uuid.Nil {
		return postgres.ErrMissingTenantContext
	}
	if tx, ok := postgres.TxFromContext(ctx); ok {
		pc, hasPrincipal := postgres.FromContext(ctx)
		if !hasPrincipal || pc.TenantID != tenantID {
			return postgres.ErrTenantContextMismatch
		}
		var activeTenant pgtype.UUID
		if err := tx.QueryRow(ctx, `SELECT platform.current_tenant_id()`).Scan(&activeTenant); err != nil {
			return fmt.Errorf("verify active authentication transaction RLS context: %w", err)
		}
		if !activeTenant.Valid {
			return postgres.ErrMissingTenantContext
		}
		if uuid.UUID(activeTenant.Bytes) != tenantID {
			return postgres.ErrTenantContextMismatch
		}
		return fn(ctx, tx)
	}
	if existing, ok := tenantIDFromContext(ctx); ok && existing != tenantID {
		return postgres.ErrTenantContextMismatch
	}

	return s.tm.WithinTx(postgres.WithTenantID(ctx, tenantID), func(txCtx context.Context) error {
		tx, ok := postgres.TxFromContext(txCtx)
		if !ok {
			return errors.New("tenant transaction is missing from context")
		}
		return fn(txCtx, tx)
	})
}

// GetSessionByTokenHash retrieves a session by its deterministic SHA-256 token hash.
func (s *PgxSessionStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	tenantID, err := s.resolveSessionTenant(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	query := `
		SELECT id, tenant_id, user_id, token_hash, host(ip_address), user_agent, issued_at, expires_at, revoked_at, last_seen_at
		FROM identity.session
		WHERE token_hash = $1
	`

	var sess Session
	var ipAddr *string
	err = s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		return dbtx.QueryRow(txCtx, query, tokenHash).Scan(
			&sess.ID,
			&sess.TenantID,
			&sess.UserID,
			&sess.TokenHash,
			&ipAddr,
			&sess.UserAgent,
			&sess.IssuedAt,
			&sess.ExpiresAt,
			&sess.RevokedAt,
			&sess.LastSeenAt,
		)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("query session by token hash: %w", err)
	}

	sess.IPAddress = ipAddr
	return &sess, nil
}

// CreateSession persists a new session in identity.session.
func (s *PgxSessionStore) CreateSession(ctx context.Context, sess *Session) error {
	if sess.ID == uuid.Nil {
		sess.ID = uuid.New()
	}
	if sess.IssuedAt.IsZero() {
		sess.IssuedAt = time.Now()
	}

	query := `
		INSERT INTO identity.session (id, tenant_id, user_id, token_hash, ip_address, user_agent, issued_at, expires_at, revoked_at, last_seen_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, $6, $7, $8, $9, $10)
	`

	var ipStr string
	if sess.IPAddress != nil {
		ipStr = *sess.IPAddress
	}

	err := s.withinTenantTx(ctx, sess.TenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		_, err := dbtx.Exec(txCtx, query,
			sess.ID,
			sess.TenantID,
			sess.UserID,
			sess.TokenHash,
			ipStr,
			sess.UserAgent,
			sess.IssuedAt,
			sess.ExpiresAt,
			sess.RevokedAt,
			sess.LastSeenAt,
		)
		return err
	})
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	return nil
}

// RevokeSession invalidates a session by setting revoked_at to current timestamp.
func (s *PgxSessionStore) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	query := `
		UPDATE identity.session
		SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`

	tenantID, ok := tenantIDFromContext(ctx)
	if !ok {
		return postgres.ErrMissingTenantContext
	}
	err := s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		_, err := dbtx.Exec(txCtx, query, sessionID)
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	return nil
}

// RevokeUserSessions revokes all active sessions belonging to the given user account.
func (s *PgxSessionStore) RevokeUserSessions(ctx context.Context, tenantID, userID uuid.UUID) error {
	query := `
		UPDATE identity.session
		SET revoked_at = now()
		WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL
	`

	err := s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		_, err := dbtx.Exec(txCtx, query, tenantID, userID)
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke all user sessions: %w", err)
	}

	return nil
}

// TouchSession updates the last_seen_at timestamp on the session if at least 30 seconds
// have elapsed since the last update, or if last_seen_at is NULL. This condition ensures
// cluster-wide write amplification protection across multiple API instances without
// requiring read-modify-write transactions.
func (s *PgxSessionStore) TouchSession(ctx context.Context, sessionID uuid.UUID) error {
	query := `
		UPDATE identity.session
		SET last_seen_at = now()
		WHERE id = $1 AND (last_seen_at IS NULL OR last_seen_at < now() - interval '30 seconds');
	`

	tenantID, ok := tenantIDFromContext(ctx)
	if !ok {
		return postgres.ErrMissingTenantContext
	}
	err := s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		_, err := dbtx.Exec(txCtx, query, sessionID)
		return err
	})
	if err != nil {
		return fmt.Errorf("touch session last_seen_at: %w", err)
	}

	return nil
}

// GetUserPrincipal loads user account details, active status, roles, permissions,
// and user scopes from the identity schema.
func (s *PgxSessionStore) GetUserPrincipal(ctx context.Context, tenantID, userID uuid.UUID) (*AuthenticatedPrincipal, error) {
	var principal *AuthenticatedPrincipal
	err := s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		var err error
		principal, err = s.getUserPrincipal(txCtx, dbtx, tenantID, userID)
		return err
	})
	return principal, err
}

func (s *PgxSessionStore) getUserPrincipal(ctx context.Context, dbtx postgres.DBTX, tenantID, userID uuid.UUID) (*AuthenticatedPrincipal, error) {
	// 1. Verify user account exists and is ACTIVE
	userQuery := `
		SELECT username, display_name, status
		FROM identity.user_account
		WHERE tenant_id = $1 AND id = $2
	`

	var username, displayName, status string
	err := dbtx.QueryRow(ctx, userQuery, tenantID, userID).Scan(&username, &displayName, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user account: %w", err)
	}

	if status != "ACTIVE" {
		return nil, ErrUserInactive
	}

	// 2. Fetch assigned roles
	rolesQuery := `
		SELECT r.code
		FROM identity.user_role ur
		JOIN identity.role r ON ur.role_id = r.id
		WHERE ur.user_id = $1
	`
	roleRows, err := dbtx.Query(ctx, rolesQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("query user roles: %w", err)
	}
	defer roleRows.Close()

	var roles []string
	for roleRows.Next() {
		var roleCode string
		if scanErr := roleRows.Scan(&roleCode); scanErr != nil {
			return nil, fmt.Errorf("scan user role: %w", scanErr)
		}
		roles = append(roles, roleCode)
	}
	if err := roleRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user roles: %w", err)
	}

	// 3. Fetch aggregated permissions across all roles
	permsQuery := `
		SELECT DISTINCT p.code
		FROM identity.user_role ur
		JOIN identity.role_permission rp ON ur.role_id = rp.role_id
		JOIN identity.permission p ON rp.permission_id = p.id
		WHERE ur.user_id = $1
	`
	permRows, err := dbtx.Query(ctx, permsQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("query role permissions: %w", err)
	}
	defer permRows.Close()

	var permissions []string
	for permRows.Next() {
		var permCode string
		if scanErr := permRows.Scan(&permCode); scanErr != nil {
			return nil, fmt.Errorf("scan permission: %w", scanErr)
		}
		permissions = append(permissions, permCode)
	}
	if err := permRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role permissions: %w", err)
	}

	// 4. Fetch assigned scopes
	scopesQuery := `
		SELECT scope_type, company_id, region_id, store_id, warehouse_id, terminal_id
		FROM identity.user_scope
		WHERE tenant_id = $1 AND user_id = $2
	`
	scopeRows, err := dbtx.Query(ctx, scopesQuery, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("query user scopes: %w", err)
	}
	defer scopeRows.Close()

	var scopes []UserScope
	for scopeRows.Next() {
		var us UserScope
		if scanErr := scopeRows.Scan(&us.Type, &us.CompanyID, &us.RegionID, &us.StoreID, &us.WarehouseID, &us.TerminalID); scanErr != nil {
			return nil, fmt.Errorf("scan user scope: %w", scanErr)
		}
		scopes = append(scopes, us)
	}
	if err := scopeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user scopes: %w", err)
	}

	return &AuthenticatedPrincipal{
		TenantID:    tenantID,
		UserID:      userID,
		Username:    username,
		DisplayName: displayName,
		Roles:       roles,
		Permissions: permissions,
		Scopes:      scopes,
	}, nil
}

func tenantIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	pc, ok := postgres.FromContext(ctx)
	return pc.TenantID, ok && pc.TenantID != uuid.Nil
}
