package auth_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/postgres"
)

func TestRefreshToken_Integration_LifecycleAndRotation(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	adminTM := postgres.NewTxManager(adminPool, newDiscardLogger())
	store := auth.NewPgxSessionStore(appPool, newDiscardLogger())

	tenantID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	familyID := uuid.New()

	// Seed tenant, user, and session
	if err := adminTM.WithinUnscopedTx(ctx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)

		_, err := tx.Exec(txCtx, `
			INSERT INTO identity.user_account (id, tenant_id, username, display_name, status)
			VALUES ($1, $2, 'rotation_tester', 'Rotation Tester', 'ACTIVE')
		`, userID, tenantID)
		if err != nil {
			return err
		}

		rawAccess, accessHash, _ := auth.GenerateToken()
		_, err = tx.Exec(txCtx, `
			INSERT INTO identity.session (id, tenant_id, user_id, token_hash, issued_at, expires_at)
			VALUES ($1, $2, $3, $4, now(), now() + interval '15 minutes')
		`, sessionID, tenantID, userID, accessHash)
		_ = rawAccess
		return err
	}); err != nil {
		t.Fatalf("failed to seed user and session: %v", err)
	}

	_, hashRT1, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}

	rt1 := &auth.RefreshToken{
		ID:        uuid.New(),
		TenantID:  tenantID,
		UserID:    userID,
		SessionID: sessionID,
		TokenHash: hashRT1,
		FamilyID:  familyID,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	if err := store.CreateRefreshToken(ctx, rt1); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	t.Run("successful atomic rotation issues new tokens and updates session", func(t *testing.T) {
		result, err := store.RotateRefreshToken(ctx, hashRT1, &tenantID, 15*time.Minute, 30*24*time.Hour)
		if err != nil {
			t.Fatalf("RotateRefreshToken failed: %v", err)
		}

		if result.RawAccessToken == "" || result.RawRefreshToken == "" {
			t.Errorf("expected non-empty tokens in rotation result")
		}
		if result.SessionID != sessionID {
			t.Errorf("expected sessionID %s, got %s", sessionID, result.SessionID)
		}

		// Verify old token is now revoked and replaced_by_token_id is set
		oldToken, err := store.GetRefreshTokenByHash(ctx, hashRT1)
		if err != nil {
			t.Fatalf("GetRefreshTokenByHash failed: %v", err)
		}
		if oldToken.RevokedAt == nil {
			t.Errorf("expected old refresh token to be revoked")
		}
		if oldToken.ReplacedByTokenID == nil {
			t.Errorf("expected replaced_by_token_id to be set on old token")
		}

		// Verify new refresh token exists in DB in the SAME family
		newToken, err := store.GetRefreshTokenByHash(ctx, result.RefreshTokenHash)
		if err != nil {
			t.Fatalf("new refresh token not found in DB: %v", err)
		}
		if newToken.FamilyID != familyID {
			t.Errorf("expected new token familyID %s, got %s", familyID, newToken.FamilyID)
		}
		if newToken.RevokedAt != nil {
			t.Errorf("new refresh token should not be revoked")
		}

		// Verify session token_hash was updated to new access token hash
		sess, err := store.GetSessionByTokenHash(ctx, result.AccessTokenHash)
		if err != nil {
			t.Fatalf("session not found with new access token hash: %v", err)
		}
		if sess.ID != sessionID {
			t.Errorf("session ID mismatch: %s vs %s", sessionID, sess.ID)
		}
	})

	t.Run("replay detection revokes entire family and session", func(t *testing.T) {
		// Attempting to rotate the ALREADY revoked rt1 must trigger replay detection
		result, err := store.RotateRefreshToken(ctx, hashRT1, &tenantID, 15*time.Minute, 30*24*time.Hour)
		if result != nil {
			t.Errorf("expected nil result on replay, got: %v", result)
		}
		if !errors.Is(err, auth.ErrRefreshTokenReplayDetected) {
			t.Fatalf("expected ErrRefreshTokenReplayDetected, got: %v", err)
		}

		// Verify session is now revoked
		var sessRevokedAt *time.Time
		err = appPool.QueryRow(ctx, `SELECT revoked_at FROM identity.session WHERE id = $1`, sessionID).Scan(&sessRevokedAt)
		if err != nil {
			t.Fatalf("query session revoked_at failed: %v", err)
		}
		if sessRevokedAt == nil {
			t.Errorf("expected session to be revoked after replay detection")
		}
	})

	t.Run("cross-tenant refresh attempt rejected", func(t *testing.T) {
		otherTenantID := uuid.New()
		_, hashRTX, _ := auth.GenerateRefreshToken()
		_ = store.CreateRefreshToken(ctx, &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessionID,
			TokenHash: hashRTX,
			FamilyID:  uuid.New(),
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		})

		_, err := store.RotateRefreshToken(ctx, hashRTX, &otherTenantID, 15*time.Minute, 30*24*time.Hour)
		if !errors.Is(err, auth.ErrRefreshTokenTenantMismatch) {
			t.Fatalf("expected ErrRefreshTokenTenantMismatch, got: %v", err)
		}
	})

	t.Run("expired refresh token rejected", func(t *testing.T) {
		_, hashRTExp, _ := auth.GenerateRefreshToken()
		_ = store.CreateRefreshToken(ctx, &auth.RefreshToken{
			ID:        uuid.New(),
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessionID,
			TokenHash: hashRTExp,
			FamilyID:  uuid.New(),
			IssuedAt:  time.Now().Add(-48 * time.Hour),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		})

		_, err := store.RotateRefreshToken(ctx, hashRTExp, &tenantID, 15*time.Minute, 30*24*time.Hour)
		if !errors.Is(err, auth.ErrRefreshTokenExpired) {
			t.Fatalf("expected ErrRefreshTokenExpired, got: %v", err)
		}
	})
}

func TestRefreshToken_Integration_ConcurrentRotationRace(t *testing.T) {
	adminPool, appPool := connectLivePool(t)
	if adminPool == nil || appPool == nil {
		return
	}
	defer adminPool.Close()
	defer appPool.Close()

	ctx := context.Background()
	adminTM := postgres.NewTxManager(adminPool, newDiscardLogger())
	store := auth.NewPgxSessionStore(appPool, newDiscardLogger())

	tenantID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	familyID := uuid.New()

	// Seed user and session
	if err := adminTM.WithinUnscopedTx(ctx, func(txCtx context.Context) error {
		tx, _ := postgres.TxFromContext(txCtx)

		_, err := tx.Exec(txCtx, `
			INSERT INTO identity.user_account (id, tenant_id, username, display_name, status)
			VALUES ($1, $2, 'race_tester', 'Race Tester', 'ACTIVE')
		`, userID, tenantID)
		if err != nil {
			return err
		}

		_, accessHash, _ := auth.GenerateToken()
		_, err = tx.Exec(txCtx, `
			INSERT INTO identity.session (id, tenant_id, user_id, token_hash, issued_at, expires_at)
			VALUES ($1, $2, $3, $4, now(), now() + interval '1 hour')
		`, sessionID, tenantID, userID, accessHash)
		return err
	}); err != nil {
		t.Fatalf("failed to seed user/session: %v", err)
	}

	_, tokenHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}

	if err := store.CreateRefreshToken(ctx, &auth.RefreshToken{
		ID:        uuid.New(),
		TenantID:  tenantID,
		UserID:    userID,
		SessionID: sessionID,
		TokenHash: tokenHash,
		FamilyID:  familyID,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	// Launch 10 concurrent rotation attempts on the exact same token hash
	const concurrency = 10
	var wg sync.WaitGroup
	var successCount int64
	var replayCount int64

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier

			res, err := store.RotateRefreshToken(ctx, tokenHash, &tenantID, 15*time.Minute, 30*24*time.Hour)
			if err == nil && res != nil {
				atomic.AddInt64(&successCount, 1)
			} else if errors.Is(err, auth.ErrRefreshTokenReplayDetected) {
				atomic.AddInt64(&replayCount, 1)
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	// Exactly 1 rotation MUST succeed; all others must be detected as replay
	if successCount != 1 {
		t.Fatalf("expected exactly 1 rotation to succeed, got %d", successCount)
	}
	if replayCount != concurrency-1 {
		t.Errorf("expected %d replay detections, got %d", concurrency-1, replayCount)
	}
}
