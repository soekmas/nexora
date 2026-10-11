package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"example.com/nexora/backend/internal/platform/postgres"
)

var (
	// ErrRefreshTokenNotFound indicates that no refresh token matched the hash.
	ErrRefreshTokenNotFound = errors.New("refresh token not found")

	// ErrRefreshTokenExpired indicates that the refresh token has passed its expiration time.
	ErrRefreshTokenExpired = errors.New("refresh token expired")

	// ErrRefreshTokenRevoked indicates that the refresh token was explicitly revoked.
	ErrRefreshTokenRevoked = errors.New("refresh token revoked")

	// ErrRefreshTokenReplayDetected indicates that an already rotated or revoked token was reused.
	ErrRefreshTokenReplayDetected = errors.New("refresh token replay detected: token already rotated")

	// ErrRefreshTokenTenantMismatch indicates the supplied tenant header does not match the token tenant.
	ErrRefreshTokenTenantMismatch = errors.New("refresh token tenant mismatch")

	// ErrSessionRevoked indicates the underlying session has been revoked.
	ErrSessionRevoked = errors.New("session has been revoked")

	// ErrSessionExpired indicates the underlying session has expired.
	ErrSessionExpired = errors.New("session has expired")
)

// RefreshToken represents a persistent refresh token record from identity.refresh_token.
type RefreshToken struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	UserID            uuid.UUID
	SessionID         uuid.UUID
	TokenHash         string
	FamilyID          uuid.UUID
	IssuedAt          time.Time
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	ReplacedByTokenID *uuid.UUID
	CreatedAt         time.Time
}

// IsRevoked reports whether the refresh token has been revoked.
func (rt *RefreshToken) IsRevoked() bool {
	return rt.RevokedAt != nil
}

// IsExpired reports whether the refresh token is expired relative to the given time.
func (rt *RefreshToken) IsExpired(now time.Time) bool {
	return now.After(rt.ExpiresAt)
}

// RefreshTokenRotationResult holds the outcome of an atomic token rotation transaction.
type RefreshTokenRotationResult struct {
	SessionID        uuid.UUID
	TenantID         uuid.UUID
	UserID           uuid.UUID
	RawAccessToken   string
	AccessTokenHash  string
	RawRefreshToken  string
	RefreshTokenHash string
	AccessExpiresIn  int // in seconds
	Principal        *AuthenticatedPrincipal
}

// RefreshTokenStore defines the contract for persistent refresh-token operations and rotation.
type RefreshTokenStore interface {
	CreateRefreshToken(ctx context.Context, rt *RefreshToken) error
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RotateRefreshToken(ctx context.Context, oldTokenHash string, expectedTenantID *uuid.UUID, accessTTL, refreshTTL time.Duration) (*RefreshTokenRotationResult, error)
	RevokeTokenFamily(ctx context.Context, familyID uuid.UUID) error
}

// CreateRefreshToken persists a new refresh token record in identity.refresh_token.
func (s *PgxSessionStore) CreateRefreshToken(ctx context.Context, rt *RefreshToken) error {
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}
	if rt.IssuedAt.IsZero() {
		rt.IssuedAt = time.Now()
	}
	if rt.CreatedAt.IsZero() {
		rt.CreatedAt = rt.IssuedAt
	}

	query := `
		INSERT INTO identity.refresh_token (
			id, tenant_id, user_id, session_id, token_hash, family_id, issued_at, expires_at, revoked_at, replaced_by_token_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`

	err := s.withinTenantTx(ctx, rt.TenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		_, err := dbtx.Exec(txCtx, query,
			rt.ID,
			rt.TenantID,
			rt.UserID,
			rt.SessionID,
			rt.TokenHash,
			rt.FamilyID,
			rt.IssuedAt,
			rt.ExpiresAt,
			rt.RevokedAt,
			rt.ReplacedByTokenID,
			rt.CreatedAt,
		)
		return err
	})
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}

	return nil
}

// GetRefreshTokenByHash retrieves a refresh token by its SHA-256 digest.
func (s *PgxSessionStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	tenantID, err := s.resolveRefreshTokenTenant(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}

	query := `
		SELECT id, tenant_id, user_id, session_id, token_hash, family_id, issued_at, expires_at, revoked_at, replaced_by_token_id, created_at
		FROM identity.refresh_token
		WHERE token_hash = $1
	`

	var rt RefreshToken
	err = s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		return dbtx.QueryRow(txCtx, query, tokenHash).Scan(
			&rt.ID,
			&rt.TenantID,
			&rt.UserID,
			&rt.SessionID,
			&rt.TokenHash,
			&rt.FamilyID,
			&rt.IssuedAt,
			&rt.ExpiresAt,
			&rt.RevokedAt,
			&rt.ReplacedByTokenID,
			&rt.CreatedAt,
		)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, fmt.Errorf("query refresh token: %w", err)
	}

	return &rt, nil
}

// RotateRefreshToken performs atomic single-use rotation with replay detection.
func (s *PgxSessionStore) RotateRefreshToken(
	ctx context.Context,
	oldTokenHash string,
	expectedTenantID *uuid.UUID,
	accessTTL, refreshTTL time.Duration,
) (*RefreshTokenRotationResult, error) {
	tenantID, err := s.resolveRefreshTokenTenant(ctx, oldTokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	if expectedTenantID != nil && tenantID != *expectedTenantID {
		return nil, ErrRefreshTokenTenantMismatch
	}

	var result *RefreshTokenRotationResult
	var committedError error
	err = s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {

		// 1. Lock and fetch old refresh token row
		query := `
		SELECT id, tenant_id, user_id, session_id, token_hash, family_id, issued_at, expires_at, revoked_at, replaced_by_token_id, created_at
		FROM identity.refresh_token
		WHERE token_hash = $1
		FOR UPDATE
	`

		var oldRT RefreshToken
		err := dbtx.QueryRow(txCtx, query, oldTokenHash).Scan(
			&oldRT.ID,
			&oldRT.TenantID,
			&oldRT.UserID,
			&oldRT.SessionID,
			&oldRT.TokenHash,
			&oldRT.FamilyID,
			&oldRT.IssuedAt,
			&oldRT.ExpiresAt,
			&oldRT.RevokedAt,
			&oldRT.ReplacedByTokenID,
			&oldRT.CreatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrRefreshTokenNotFound
			}
			return fmt.Errorf("query old refresh token: %w", err)
		}

		// 2. Replay detection: If token was already revoked, immediately revoke family and session
		if oldRT.RevokedAt != nil {
			if _, err := dbtx.Exec(txCtx, `UPDATE identity.refresh_token SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, oldRT.FamilyID); err != nil {
				return fmt.Errorf("revoke replayed refresh token family: %w", err)
			}
			if _, err := dbtx.Exec(txCtx, `UPDATE identity.session SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, oldRT.SessionID); err != nil {
				return fmt.Errorf("revoke replayed refresh token session: %w", err)
			}
			committedError = ErrRefreshTokenReplayDetected
			return nil
		}

		now := time.Now()

		// 3. Expiration check
		if now.After(oldRT.ExpiresAt) {
			return ErrRefreshTokenExpired
		}

		// 4. Tenant isolation check
		if oldRT.TenantID != tenantID {
			return errors.New("refresh token tenant resolver mismatch")
		}

		// 5. User Account status verification (must be ACTIVE)
		var userStatus string
		err = dbtx.QueryRow(txCtx, `SELECT status FROM identity.user_account WHERE id = $1 AND tenant_id = $2`, oldRT.UserID, oldRT.TenantID).Scan(&userStatus)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUserNotFound
			}
			return fmt.Errorf("query user account status: %w", err)
		}
		if userStatus != "ACTIVE" {
			if _, err := dbtx.Exec(txCtx, `UPDATE identity.refresh_token SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, oldRT.FamilyID); err != nil {
				return fmt.Errorf("revoke inactive user's refresh tokens: %w", err)
			}
			if _, err := dbtx.Exec(txCtx, `UPDATE identity.session SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, oldRT.SessionID); err != nil {
				return fmt.Errorf("revoke inactive user's session: %w", err)
			}
			committedError = ErrUserInactive
			return nil
		}

		// 6. Session validity check
		var sessRevokedAt *time.Time
		var sessExpiresAt time.Time
		err = dbtx.QueryRow(txCtx, `SELECT revoked_at, expires_at FROM identity.session WHERE id = $1 FOR UPDATE`, oldRT.SessionID).Scan(&sessRevokedAt, &sessExpiresAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSessionNotFound
			}
			return fmt.Errorf("query session: %w", err)
		}
		if sessRevokedAt != nil {
			return ErrSessionRevoked
		}
		if now.After(sessExpiresAt) {
			return ErrSessionExpired
		}

		// 7. Generate new cryptographic tokens
		rawAccessToken, accessTokenHash, err := GenerateToken()
		if err != nil {
			return fmt.Errorf("generate access token: %w", err)
		}

		rawRefreshToken, refreshTokenHash, err := GenerateRefreshToken()
		if err != nil {
			return fmt.Errorf("generate refresh token: %w", err)
		}

		newRTID := uuid.New()
		newRTExpiresAt := now.Add(refreshTTL)
		newAccessExpiresAt := now.Add(accessTTL)

		// 8. Insert new refresh token into family
		_, err = dbtx.Exec(txCtx, `
		INSERT INTO identity.refresh_token (
			id, tenant_id, user_id, session_id, token_hash, family_id, issued_at, expires_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $7
		)
	`, newRTID, oldRT.TenantID, oldRT.UserID, oldRT.SessionID, refreshTokenHash, oldRT.FamilyID, now, newRTExpiresAt)
		if err != nil {
			return fmt.Errorf("insert rotated refresh token: %w", err)
		}

		// 9. Atomically revoke old refresh token, linking replacement ID
		tag, err := dbtx.Exec(txCtx, `
		UPDATE identity.refresh_token
		SET revoked_at = now(), replaced_by_token_id = $1
		WHERE id = $2 AND revoked_at IS NULL
	`, newRTID, oldRT.ID)
		if err != nil {
			return fmt.Errorf("revoke old token: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrRefreshTokenReplayDetected
		}

		// 10. Update session access token hash and expiry
		_, err = dbtx.Exec(txCtx, `
		UPDATE identity.session
		SET token_hash = $1, expires_at = $2, last_seen_at = $3
		WHERE id = $4
	`, accessTokenHash, newAccessExpiresAt, now, oldRT.SessionID)
		if err != nil {
			return fmt.Errorf("update session token hash: %w", err)
		}

		principal, err := s.getUserPrincipal(txCtx, dbtx, oldRT.TenantID, oldRT.UserID)
		if err != nil {
			return fmt.Errorf("resolve user principal before token rotation commit: %w", err)
		}
		principal.SessionID = oldRT.SessionID

		result = &RefreshTokenRotationResult{
			SessionID:        oldRT.SessionID,
			TenantID:         oldRT.TenantID,
			UserID:           oldRT.UserID,
			RawAccessToken:   rawAccessToken,
			AccessTokenHash:  accessTokenHash,
			RawRefreshToken:  rawRefreshToken,
			RefreshTokenHash: refreshTokenHash,
			AccessExpiresIn:  int(accessTTL.Seconds()),
			Principal:        principal,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if committedError != nil {
		return nil, committedError
	}
	return result, nil
}

// RevokeTokenFamily revokes all tokens in the given family.
func (s *PgxSessionStore) RevokeTokenFamily(ctx context.Context, familyID uuid.UUID) error {
	query := `
		UPDATE identity.refresh_token
		SET revoked_at = now()
		WHERE family_id = $1 AND revoked_at IS NULL
	`
	tenantID, ok := tenantIDFromContext(ctx)
	if !ok {
		return postgres.ErrMissingTenantContext
	}
	err := s.withinTenantTx(ctx, tenantID, func(txCtx context.Context, dbtx postgres.DBTX) error {
		_, err := dbtx.Exec(txCtx, query, familyID)
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke token family: %w", err)
	}
	return nil
}
