package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/identity"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var primarySeedID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func auditEvent(eventType string, now time.Time) identity.AuditEvent {
	return identity.AuditEvent{
		ID: uuid.New(), EventType: eventType, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	}
}

func TestAuthorizationStateIsHashedExpiredAndOneTime(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	rawState := []byte("raw-authorization-state")
	hash := identity.HashSecret(rawState)
	transaction := identity.AuthorizationTransaction{
		ID: uuid.New(), Purpose: identity.AuthorizationLogin, StateHash: hash[:],
		ExpiresAt: now.Add(time.Minute), CreatedAt: now,
	}
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM authorization_transactions WHERE id=$1`, transaction.ID)
		require.NoError(t, err)
	})

	require.NoError(t, repository.CreateAuthorizationTransaction(ctx, transaction))
	var persisted []byte
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT state_hash FROM authorization_transactions WHERE id=$1`, transaction.ID).Scan(&persisted))
	require.Equal(t, hash[:], persisted)
	require.NotEqual(t, rawState, persisted)

	consumed, err := repository.ConsumeAuthorizationState(ctx, hash[:], now)
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)
	_, err = repository.ConsumeAuthorizationState(ctx, hash[:], now)
	require.ErrorIs(t, err, identity.ErrAuthorizationInvalid)

	expiredHash := identity.HashSecret([]byte("expired-state"))
	expired := identity.AuthorizationTransaction{
		ID: uuid.New(), Purpose: identity.AuthorizationLogin, StateHash: expiredHash[:],
		ExpiresAt: now, CreatedAt: now.Add(-time.Minute),
	}
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `DELETE FROM authorization_transactions WHERE id=$1`, expired.ID)
		require.NoError(t, cleanupErr)
	})
	require.NoError(t, repository.CreateAuthorizationTransaction(ctx, expired))
	_, err = repository.ConsumeAuthorizationState(ctx, expiredHash[:], now)
	require.ErrorIs(t, err, identity.ErrAuthorizationInvalid)
}

func TestInvitationAcceptanceIsAtomicAndCredentialStaysSealed(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	key := identity.PrincipalKey{Provider: "lark", TenantID: "tenant-" + uuid.NewString(), SubjectID: "subject"}
	rawToken := []byte("raw-invitation-token-" + uuid.NewString())
	tokenHash := identity.HashSecret(rawToken)
	invitation := identity.Invitation{
		ID: uuid.New(), Target: key, TargetSnapshot: json.RawMessage(`{"name":"Invitee"}`),
		TokenHash: tokenHash[:], Status: identity.InvitationPending, CreatedByUserID: primarySeedID,
		ExpiresAt: now.Add(identity.InvitationLifetime), CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repository.CreateInvitation(ctx, invitation, auditEvent("invitation_created", now)))

	cipher, err := identity.NewCredentialCipher(map[string][]byte{"test-key": make([]byte, 32)})
	require.NoError(t, err)
	accessCiphertext, err := cipher.Encrypt("test-key", []byte("access-token"))
	require.NoError(t, err)
	refreshCiphertext, err := cipher.Encrypt("test-key", []byte("refresh-token"))
	require.NoError(t, err)
	credential := identity.OAuthCredential{
		AccessTokenCiphertext: accessCiphertext, RefreshTokenCiphertext: refreshCiphertext,
		AccessTokenExpiresAt: now.Add(time.Hour), RefreshTokenExpiresAt: now.Add(24 * time.Hour),
		EncryptionKeyID: "test-key",
	}
	userIDs := []uuid.UUID{uuid.New(), uuid.New()}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupCtx,
			`DELETE FROM identity_audit_events WHERE invitation_id=$1 OR subject_user_id=ANY($2)`, invitation.ID, userIDs)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE user_id=ANY($1)`, userIDs)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM external_identities WHERE user_id=ANY($1)`, userIDs)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM invitations WHERE id=$1`, invitation.ID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=ANY($1)`, userIDs)
		require.NoError(t, cleanupErr)
	})

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, acceptErr := repository.AcceptInvitation(ctx, identity.AcceptInvitationCommand{
				TokenHash:  tokenHash[:],
				Principal:  identity.Principal{Key: key, Name: "Invitee", Active: true},
				Credential: credential, UserID: userIDs[index], UserName: "Invitee",
				Audit: auditEvent("invitation_accepted", now), Now: now,
			})
			results <- acceptErr
		}(i)
	}
	wg.Wait()
	close(results)
	successes, invalid := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, identity.ErrInvitationInvalid):
			invalid++
		default:
			t.Fatalf("unexpected acceptance result: %v", result)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, invalid)

	var externalID uuid.UUID
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT id FROM external_identities WHERE tenant_id=$1 AND subject_id=$2`, key.TenantID, key.SubjectID).
		Scan(&externalID))
	persisted, err := repository.GetCredential(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, "test-key", persisted.EncryptionKeyID)
	require.NotEqual(t, []byte("access-token"), persisted.AccessTokenCiphertext)
	plaintext, err := cipher.Decrypt(persisted.EncryptionKeyID, persisted.AccessTokenCiphertext)
	require.NoError(t, err)
	require.Equal(t, []byte("access-token"), plaintext)

	var storedHash []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT token_hash FROM invitations WHERE id=$1`, invitation.ID).Scan(&storedHash))
	require.Equal(t, tokenHash[:], storedHash)
	require.NotEqual(t, rawToken, storedHash)
}

func TestSessionRollingProviderFailureRevocationAndDeactivation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id,name,email,platform_role,active) VALUES ($1,'Session User',$2,'MEMBER',true)`,
		userID, userID.String()+"@example.test")
	require.NoError(t, err)
	sessionSecret := identity.HashSecret([]byte("session-secret"))
	csrfSecret := identity.HashSecret([]byte("csrf-secret"))
	session := identity.Session{
		ID: uuid.New(), UserID: userID, SecretHash: sessionSecret[:], CSRFSecretHash: csrfSecret[:],
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(24 * time.Hour),
		AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	sessionAudit := auditEvent("session_created", now)
	sessionAudit.SubjectUserID = &userID
	sessionAudit.SessionID = &session.ID
	require.NoError(t, repository.CreateSession(ctx, session, sessionAudit))
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupCtx,
			`DELETE FROM identity_audit_events WHERE subject_user_id=$1 OR session_id=$2`, userID, session.ID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE user_id=$1`, userID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
		require.NoError(t, cleanupErr)
	})

	bundle, err := repository.ResolveSession(ctx, session.ID, sessionSecret[:], now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, userID, bundle.User.ID)
	wrongHash := identity.HashSecret([]byte("wrong"))
	_, err = repository.ResolveSession(ctx, session.ID, wrongHash[:], now)
	require.ErrorIs(t, err, identity.ErrSessionInvalid)

	changed, err := repository.TouchSession(ctx, session.ID, now.Add(time.Minute), now.Add(25*time.Hour))
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repository.TouchSession(ctx, session.ID, now.Add(6*time.Minute), now.Add(30*time.Hour))
	require.NoError(t, err)
	require.True(t, changed)

	failureAudit := auditEvent("provider_verification_failed", now.Add(7*time.Minute))
	failureAudit.SessionID = &session.ID
	require.NoError(t, repository.RecordProviderFailure(ctx, session.ID, now.Add(7*time.Minute), failureAudit))
	var failureSince *time.Time
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT provider_failure_since FROM sessions WHERE id=$1`, session.ID).Scan(&failureSince))
	require.NotNil(t, failureSince)
	require.NoError(t, repository.RecordProviderVerification(ctx, session.ID, now.Add(8*time.Minute)))
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT provider_failure_since FROM sessions WHERE id=$1`, session.ID).Scan(&failureSince))
	require.Nil(t, failureSince)

	require.NoError(t, repository.DeactivateUser(ctx, userID, primarySeedID, "provider_invalid"))
	var active bool
	var revokedAt *time.Time
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT u.active, s.revoked_at FROM users u JOIN sessions s ON s.user_id=u.id
		WHERE u.id=$1 AND s.id=$2`, userID, session.ID).Scan(&active, &revokedAt))
	require.False(t, active)
	require.NotNil(t, revokedAt)
	_, err = repository.ResolveSession(ctx, session.ID, sessionSecret[:], now.Add(9*time.Minute))
	require.ErrorIs(t, err, identity.ErrSessionRevoked)
}

func TestImpersonationDeliveryAndAuditAreAppendOnly(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	memberID := uuid.New()
	sessionID := uuid.New()
	invitationID := uuid.New()
	previousRole := domain.PlatformRoleMember
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT platform_role FROM users WHERE id=$1`, primarySeedID).Scan(&previousRole))
	_, err := db.Pool.Exec(ctx, `UPDATE users SET platform_role='ADMIN' WHERE id=$1`, primarySeedID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO users (id,name,email,platform_role,active)
		VALUES ($1,'Impersonated Member',$2,'MEMBER',true)`,
		memberID, memberID.String()+"@example.test")
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO sessions
			(id,user_id,secret_hash,csrf_secret_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at)
		VALUES ($1,$2,$3,$4,$5,$5,$6,$7)`,
		sessionID, primarySeedID, []byte("session-hash"), []byte("csrf-hash"), now,
		now.Add(24*time.Hour), now.Add(30*24*time.Hour))
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO invitations
			(id,provider,tenant_id,target_subject_id,target_snapshot,token_hash,status,created_by_user_id,expires_at,created_at,updated_at)
		VALUES ($1,'lark',$2,'delivery-subject','{}',$3,'pending',$4,$5,$6,$6)`,
		invitationID, "tenant-"+uuid.NewString(), []byte(uuid.NewString()), primarySeedID,
		now.Add(24*time.Hour), now)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupCtx,
			`DELETE FROM identity_audit_events WHERE session_id=$1 OR invitation_id=$2 OR subject_user_id=$3`,
			sessionID, invitationID, memberID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM invitation_deliveries WHERE invitation_id=$1`, invitationID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM impersonations WHERE session_id=$1`, sessionID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM invitations WHERE id=$1`, invitationID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE id=$1`, sessionID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, memberID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx,
			`UPDATE users SET platform_role=$1 WHERE id=$2`, previousRole, primarySeedID)
		require.NoError(t, cleanupErr)
	})

	impersonation := identity.Impersonation{
		ID: uuid.New(), SessionID: sessionID, ActorUserID: primarySeedID,
		SubjectUserID: memberID, StartedAt: now,
	}
	startAudit := auditEvent("impersonation_started", now)
	startAudit.SessionID, startAudit.ActorUserID, startAudit.SubjectUserID = &sessionID, &primarySeedID, &memberID
	require.NoError(t, repository.StartImpersonation(ctx, impersonation, startAudit))
	second := impersonation
	second.ID = uuid.New()
	err = repository.StartImpersonation(ctx, second, auditEvent("impersonation_started", now))
	require.ErrorIs(t, err, identity.ErrImpersonationActive)
	current, err := repository.CurrentImpersonation(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, memberID, current.SubjectUserID)

	endAudit := auditEvent("impersonation_ended", now.Add(time.Minute))
	endAudit.SessionID, endAudit.ActorUserID, endAudit.SubjectUserID = &sessionID, &primarySeedID, &memberID
	require.NoError(t, repository.EndImpersonation(ctx, sessionID, now.Add(time.Minute), endAudit))
	current, err = repository.CurrentImpersonation(ctx, sessionID)
	require.NoError(t, err)
	require.Nil(t, current)

	for _, status := range []identity.DeliveryStatus{identity.DeliveryFailed, identity.DeliveryDelivered} {
		category := identity.ProviderUnavailable
		delivery := identity.InvitationDelivery{
			ID: uuid.New(), InvitationID: invitationID, Channel: identity.DeliveryProviderDM,
			Status: status, ErrorCategory: &category, AttemptedAt: now,
		}
		require.NoError(t, repository.RecordDelivery(ctx, delivery))
	}
	require.NoError(t, repository.AppendAudit(ctx, identity.AuditEvent{
		ID: uuid.New(), EventType: "delivery_observed", InvitationID: &invitationID,
		Metadata: json.RawMessage(`{"result":"recorded"}`), OccurredAt: now,
	}))
	var deliveries, audits int
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM invitation_deliveries WHERE invitation_id=$1`, invitationID).Scan(&deliveries))
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM identity_audit_events WHERE session_id=$1 OR invitation_id=$2`, sessionID, invitationID).Scan(&audits))
	require.Equal(t, 2, deliveries)
	require.GreaterOrEqual(t, audits, 3)
}
