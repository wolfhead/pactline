package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/store"

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

func TestInvitationAuthorizationStateIsInvalidatedByResendLinkRotationAndRevoke(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, action := range []string{"resend_or_link_rotation", "revoke"} {
		t.Run(action, func(t *testing.T) {
			tokenHash := identity.HashSecret([]byte("invitation-token-" + uuid.NewString()))
			invitation := identity.Invitation{
				ID: uuid.New(),
				Target: identity.PrincipalKey{
					Provider: "lark", TenantID: "tenant-" + uuid.NewString(),
					SubjectID: "subject-" + uuid.NewString(),
				},
				TargetSnapshot:  json.RawMessage(`{"name":"Invitee"}`),
				TokenHash:       tokenHash[:],
				Status:          identity.InvitationPending,
				CreatedByUserID: primarySeedID,
				ExpiresAt:       now.Add(identity.InvitationLifetime),
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			require.NoError(t, repository.CreateInvitation(ctx, invitation, auditEvent("invitation_created", now)))
			stateHash := identity.HashSecret([]byte("oauth-state-" + uuid.NewString()))
			transaction := identity.AuthorizationTransaction{
				ID: uuid.New(), Purpose: identity.AuthorizationInvitation,
				StateHash: stateHash[:], InvitationID: &invitation.ID,
				InvitationTokenHash: tokenHash[:],
				ExpiresAt:           now.Add(10 * time.Minute), CreatedAt: now,
			}
			require.NoError(t, repository.CreateAuthorizationTransaction(ctx, transaction))
			t.Cleanup(func() {
				cleanupCtx := context.Background()
				_, cleanupErr := db.Pool.Exec(cleanupCtx,
					`DELETE FROM authorization_transactions WHERE id=$1`, transaction.ID)
				require.NoError(t, cleanupErr)
				_, cleanupErr = db.Pool.Exec(cleanupCtx,
					`DELETE FROM identity_audit_events WHERE invitation_id=$1`, invitation.ID)
				require.NoError(t, cleanupErr)
				_, cleanupErr = db.Pool.Exec(cleanupCtx,
					`DELETE FROM invitations WHERE id=$1`, invitation.ID)
				require.NoError(t, cleanupErr)
			})

			switch action {
			case "resend_or_link_rotation":
				rotatedHash := identity.HashSecret([]byte("rotated-token-" + uuid.NewString()))
				_, err := repository.RotateInvitation(
					ctx, invitation.ID, primarySeedID, rotatedHash[:], now.Add(identity.InvitationLifetime))
				require.NoError(t, err)
			case "revoke":
				audit := auditEvent("invitation_revoked", now)
				audit.InvitationID = &invitation.ID
				require.NoError(t, repository.RevokeInvitation(ctx, invitation.ID, now, audit))
			}

			_, err := repository.ConsumeAuthorizationState(ctx, stateHash[:], now.Add(time.Second))
			require.ErrorIs(t, err, identity.ErrAuthorizationInvalid)
		})
	}
}

func TestConsumedInvitationAuthorizationRejectsRotatedTokenVersion(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, testCase := range []struct {
		name   string
		rotate bool
		valid  bool
	}{
		{name: "unchanged token succeeds", valid: true},
		{name: "rotation after state consumption fails", rotate: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokenHash := identity.HashSecret([]byte("invitation-token-" + uuid.NewString()))
			key := identity.PrincipalKey{
				Provider: "lark", TenantID: "tenant-" + uuid.NewString(),
				SubjectID: "subject-" + uuid.NewString(),
			}
			invitation := identity.Invitation{
				ID: uuid.New(), Target: key, TargetSnapshot: json.RawMessage(`{"name":"Invitee"}`),
				TokenHash: tokenHash[:], Status: identity.InvitationPending,
				CreatedByUserID: primarySeedID, ExpiresAt: now.Add(identity.InvitationLifetime),
				CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, repository.CreateInvitation(
				ctx, invitation, auditEvent("invitation_created", now),
			))
			stateHash := identity.HashSecret([]byte("oauth-state-" + uuid.NewString()))
			transaction := identity.AuthorizationTransaction{
				ID: uuid.New(), Purpose: identity.AuthorizationInvitation,
				StateHash: stateHash[:], InvitationID: &invitation.ID,
				InvitationTokenHash: tokenHash[:],
				ExpiresAt:           now.Add(10 * time.Minute), CreatedAt: now,
			}
			require.NoError(t, repository.CreateAuthorizationTransaction(ctx, transaction))
			userID := uuid.New()
			t.Cleanup(func() {
				cleanupCtx := context.Background()
				_, cleanupErr := db.Pool.Exec(cleanupCtx,
					`DELETE FROM authorization_transactions WHERE id=$1`, transaction.ID)
				require.NoError(t, cleanupErr)
				_, cleanupErr = db.Pool.Exec(cleanupCtx,
					`DELETE FROM identity_audit_events WHERE invitation_id=$1 OR subject_user_id=$2`,
					invitation.ID, userID)
				require.NoError(t, cleanupErr)
				_, cleanupErr = db.Pool.Exec(cleanupCtx,
					`DELETE FROM external_identities WHERE user_id=$1`, userID)
				require.NoError(t, cleanupErr)
				_, cleanupErr = db.Pool.Exec(cleanupCtx,
					`DELETE FROM invitations WHERE id=$1`, invitation.ID)
				require.NoError(t, cleanupErr)
				_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
				require.NoError(t, cleanupErr)
			})

			consumed, err := repository.ConsumeAuthorizationState(ctx, stateHash[:], now.Add(time.Second))
			require.NoError(t, err)
			require.Equal(t, tokenHash[:], consumed.InvitationTokenHash)
			if testCase.rotate {
				rotatedHash := identity.HashSecret([]byte("rotated-token-" + uuid.NewString()))
				_, err = repository.RotateInvitation(
					ctx, invitation.ID, primarySeedID, rotatedHash[:],
					now.Add(identity.InvitationLifetime),
				)
				require.NoError(t, err)
			}

			_, err = repository.AcceptInvitation(ctx, identity.AcceptInvitationCommand{
				InvitationID: invitation.ID, InvitationTokenHash: consumed.InvitationTokenHash,
				Principal: identity.Principal{Key: key, Name: "Invitee", Active: true},
				Credential: identity.OAuthCredential{
					AccessTokenCiphertext:  []byte("sealed-access"),
					RefreshTokenCiphertext: []byte("sealed-refresh"),
					AccessTokenExpiresAt:   now.Add(time.Hour),
					RefreshTokenExpiresAt:  now.Add(24 * time.Hour),
					EncryptionKeyID:        "test",
				},
				UserID: userID, UserName: "Invitee",
				Audit: auditEvent("invitation_accepted", now.Add(2*time.Second)),
				Now:   now.Add(2 * time.Second),
			})
			if testCase.valid {
				require.NoError(t, err)
				var platformRole string
				require.NoError(t, db.Pool.QueryRow(ctx,
					`SELECT platform_role FROM users WHERE id=$1`, userID).Scan(&platformRole))
				require.Equal(t, "MEMBER", platformRole)
				return
			}
			require.ErrorIs(t, err, identity.ErrInvitationInvalid)
			var userCount int
			require.NoError(t, db.Pool.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE id=$1`, userID).Scan(&userCount))
			require.Zero(t, userCount)
		})
	}
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
	_, accepted, err := repository.FindExternalIdentity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, domain.AccessStatusPending, accepted.AccessStatus)
}

func TestPendingAccessRegistrationAndApprovalAreTransactional(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	var previousRole domain.PlatformRole
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT platform_role FROM users WHERE id=$1`, primarySeedID).Scan(&previousRole))
	_, err := db.Pool.Exec(ctx, `UPDATE users SET platform_role='ADMIN' WHERE id=$1`, primarySeedID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`UPDATE users SET platform_role=$1 WHERE id=$2`, previousRole, primarySeedID)
		require.NoError(t, cleanupErr)
	})
	userID, sessionID := uuid.New(), uuid.New()
	key := identity.PrincipalKey{
		Provider: "lark", TenantID: "tenant-" + uuid.NewString(), SubjectID: "subject",
	}
	sessionHash := identity.HashSecret([]byte("pending-session-secret"))
	csrfHash := identity.HashSecret([]byte("pending-csrf-secret"))
	command := identity.RegisterAccessRequestCommand{
		Principal: identity.Principal{Key: key, Name: "Pending Member", Active: true},
		Credential: identity.OAuthCredential{
			AccessTokenCiphertext: []byte("sealed-access"), RefreshTokenCiphertext: []byte("sealed-refresh"),
			AccessTokenExpiresAt: now.Add(time.Hour), RefreshTokenExpiresAt: now.Add(24 * time.Hour),
			EncryptionKeyID: "test-key",
		},
		UserID: userID, UserName: "Pending Member",
		Session: identity.Session{
			ID: sessionID, UserID: userID, SecretHash: sessionHash[:], CSRFSecretHash: csrfHash[:],
			CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(2 * time.Hour),
		},
		Audit: auditEvent("access_requested", now), Now: now,
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, err := db.Pool.Exec(cleanupCtx, `DELETE FROM outbox_events WHERE aggregate_id=$1`, userID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(cleanupCtx, `DELETE FROM identity_audit_events WHERE subject_user_id=$1`, userID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE user_id=$1`, userID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(cleanupCtx, `DELETE FROM external_identities WHERE user_id=$1`, userID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
		require.NoError(t, err)
	})

	created, err := repository.RegisterAccessRequest(ctx, command)
	require.NoError(t, err)
	require.Equal(t, domain.AccessStatusPending, created.AccessStatus)
	var requestedRecipient uuid.UUID
	var requestedPayload []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT recipient_id, payload FROM outbox_events
		WHERE aggregate_id=$1 AND event_type=$2`, userID, events.AccessRequested).
		Scan(&requestedRecipient, &requestedPayload))
	require.Equal(t, primarySeedID, requestedRecipient)
	var requestEvent events.AccessRequestedPayload
	require.NoError(t, json.Unmarshal(requestedPayload, &requestEvent))
	require.Equal(t, userID, requestEvent.RequesterID)
	require.Equal(t, "Pending Member", requestEvent.RequesterName)
	bundle, err := repository.ResolveSession(ctx, sessionID, sessionHash[:], now)
	require.NoError(t, err)
	require.Equal(t, domain.AccessStatusPending, bundle.User.AccessStatus)

	require.NoError(t, repository.SetUserAccessStatus(
		ctx, userID, primarySeedID, domain.AccessStatusRejected, now.Add(time.Minute), "request-reject",
	))
	rejected, err := store.NewUserStore(db).GetByID(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, domain.AccessStatusRejected, rejected.AccessStatus)
	require.NoError(t, repository.SetUserAccessStatus(
		ctx, userID, primarySeedID, domain.AccessStatusApproved, now.Add(2*time.Minute), "request-approve",
	))
	approved, err := store.NewUserStore(db).GetByID(ctx, userID)
	require.NoError(t, err)
	require.True(t, approved.CanUseApplication())
	var approvedRecipient uuid.UUID
	var approvedPayload []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT recipient_id, payload FROM outbox_events
		WHERE aggregate_id=$1 AND event_type=$2`, userID, events.AccessApproved).
		Scan(&approvedRecipient, &approvedPayload))
	require.Equal(t, userID, approvedRecipient)
	var approvalEvent events.AccessApprovedPayload
	require.NoError(t, json.Unmarshal(approvedPayload, &approvalEvent))
	require.Equal(t, userID, approvalEvent.UserID)
	require.Equal(t, primarySeedID, approvalEvent.ApprovedByID)
	require.ErrorIs(t, repository.SetUserAccessStatus(
		ctx, userID, primarySeedID, domain.AccessStatusRejected, now.Add(3*time.Minute), "request-regress",
	), domain.ErrConflict)
}

func TestCredentialRefreshLockedReusesConcurrentRotation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID, externalID := uuid.New(), uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id,name,email,platform_role,active)
		VALUES ($1,'Refresh User',$2,'MEMBER',true)`,
		userID, userID.String()+"@example.test")
	require.NoError(t, err)
	expired := identity.OAuthCredential{
		AccessTokenCiphertext: []byte("expired-access"), RefreshTokenCiphertext: []byte("refresh"),
		AccessTokenExpiresAt: now.Add(-time.Minute), RefreshTokenExpiresAt: now.Add(time.Hour),
		EncryptionKeyID: "test",
	}
	external := identity.ExternalIdentity{
		ID: externalID, UserID: userID,
		Key: identity.PrincipalKey{
			Provider: "lark", TenantID: "tenant-" + uuid.NewString(), SubjectID: "subject",
		},
		ProviderProfile: json.RawMessage(`{"name":"Refresh User"}`),
		CreatedAt:       now, UpdatedAt: now,
	}
	audit := auditEvent("identity_bound", now)
	audit.SubjectUserID = &userID
	require.NoError(t, repository.BindExternalIdentity(ctx, external, expired, audit))
	externalUsers, err := repository.ListExternalIdentityUsers(ctx, "lark")
	require.NoError(t, err)
	foundExternalUser := false
	for _, candidate := range externalUsers {
		if candidate.ID == userID {
			foundExternalUser = true
			require.Equal(t, "Refresh User", candidate.Name)
			require.True(t, candidate.CanUseApplication())
		}
	}
	require.True(t, foundExternalUser)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupCtx,
			`DELETE FROM identity_audit_events WHERE subject_user_id=$1`, userID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx,
			`DELETE FROM external_identities WHERE id=$1`, externalID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
		require.NoError(t, cleanupErr)
	})

	fresh := identity.OAuthCredential{
		AccessTokenCiphertext: []byte("fresh-access"), RefreshTokenCiphertext: []byte("fresh-refresh"),
		AccessTokenExpiresAt: now.Add(time.Hour), RefreshTokenExpiresAt: now.Add(24 * time.Hour),
		EncryptionKeyID: "test",
	}
	var providerRefreshes atomic.Int64
	start := make(chan struct{})
	results := make(chan identity.OAuthCredential, 2)
	errorsChannel := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			credential, refreshErr := repository.RefreshCredentialLocked(
				ctx, externalID, func(current identity.OAuthCredential) (identity.OAuthCredential, error) {
					if now.Before(current.AccessTokenExpiresAt) {
						return current, nil
					}
					providerRefreshes.Add(1)
					return fresh, nil
				})
			results <- credential
			errorsChannel <- refreshErr
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsChannel)
	for refreshErr := range errorsChannel {
		require.NoError(t, refreshErr)
	}
	for credential := range results {
		require.Equal(t, fresh.AccessTokenCiphertext, credential.AccessTokenCiphertext)
		require.Equal(t, fresh.RefreshTokenCiphertext, credential.RefreshTokenCiphertext)
	}
	require.EqualValues(t, 1, providerRefreshes.Load())
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

	require.NoError(t, repository.DeactivateUser(ctx, userID, primarySeedID, "provider_invalid", "provider-request-id"))
	var active bool
	var revokedAt *time.Time
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT u.active, s.revoked_at FROM users u JOIN sessions s ON s.user_id=u.id
		WHERE u.id=$1 AND s.id=$2`, userID, session.ID).Scan(&active, &revokedAt))
	require.False(t, active)
	require.NotNil(t, revokedAt)
	var deactivationMetadata []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT metadata FROM identity_audit_events
		WHERE event_type='user_deactivated' AND subject_user_id=$1
		ORDER BY occurred_at DESC,id DESC LIMIT 1`, userID).Scan(&deactivationMetadata))
	require.JSONEq(t,
		`{"reason":"provider_invalid","provider_request_id":"provider-request-id"}`,
		string(deactivationMetadata))
	_, err = repository.ResolveSession(ctx, session.ID, sessionSecret[:], now.Add(9*time.Minute))
	require.ErrorIs(t, err, identity.ErrUserInactive)
}

func TestCreateSessionRejectsInactiveUserInRepositoryTransaction(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID, sessionID, auditID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id,name,email,platform_role,active)
		VALUES ($1,'Inactive Session User',$2,'MEMBER',false)`,
		userID, userID.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupCtx, `DELETE FROM identity_audit_events WHERE id=$1`, auditID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE id=$1`, sessionID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
		require.NoError(t, cleanupErr)
	})
	sessionHash, csrfHash := identity.HashSecret([]byte("session")), identity.HashSecret([]byte("csrf"))
	err = repository.CreateSession(ctx, identity.Session{
		ID: sessionID, UserID: userID, SecretHash: sessionHash[:], CSRFSecretHash: csrfHash[:],
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}, identity.AuditEvent{
		ID: auditID, EventType: "session_created", SubjectUserID: &userID,
		SessionID: &sessionID, Metadata: json.RawMessage(`{}`), OccurredAt: now,
	})
	require.ErrorIs(t, err, identity.ErrUserInactive)
	var sessionCount, auditCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id=$1`, sessionID).Scan(&sessionCount))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM identity_audit_events WHERE id=$1`, auditID).Scan(&auditCount))
	require.Zero(t, sessionCount)
	require.Zero(t, auditCount)
}

func TestLogoutSessionAtomicallyEndsImpersonationAndAuditsOwner(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repository := store.NewIdentityStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	subjectID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id,name,email,platform_role,active)
		VALUES ($1,'Logout Subject',$2,'MEMBER',true)`,
		subjectID, subjectID.String()+"@example.test")
	require.NoError(t, err)
	sessionIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	impersonationIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for _, sessionID := range sessionIDs {
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO sessions
				(id,user_id,secret_hash,csrf_secret_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at)
			VALUES ($1,$2,$3,$4,$5,$5,$6,$7)`,
			sessionID, primarySeedID, []byte("session-hash"), []byte("csrf-hash"), now,
			now.Add(24*time.Hour), now.Add(30*24*time.Hour))
		require.NoError(t, err)
	}
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO impersonations (id,session_id,actor_user_id,subject_user_id,started_at)
		VALUES ($1,$2,$3,$4,$5)`,
		impersonationIDs[0], sessionIDs[0], primarySeedID, subjectID, now)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO impersonations (id,session_id,actor_user_id,subject_user_id,started_at,ended_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		impersonationIDs[1], sessionIDs[2], primarySeedID, subjectID, now.Add(-time.Hour), now.Add(-time.Minute))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupCtx, `DELETE FROM identity_audit_events WHERE session_id=ANY($1)`, sessionIDs)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM impersonations WHERE session_id=ANY($1)`, sessionIDs)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE id=ANY($1)`, sessionIDs)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, subjectID)
		require.NoError(t, cleanupErr)
	})

	impersonatingRequestID := "logout-impersonating-" + uuid.NewString()
	require.NoError(t, repository.LogoutSession(ctx, sessionIDs[0], now, impersonatingRequestID))
	var revokedAt, endedAt *time.Time
	var revokeReason *string
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT s.revoked_at, s.revoke_reason, i.ended_at
		FROM sessions s JOIN impersonations i ON i.session_id=s.id
		WHERE s.id=$1`, sessionIDs[0]).Scan(&revokedAt, &revokeReason, &endedAt))
	require.NotNil(t, revokedAt)
	require.Equal(t, "logout", *revokeReason)
	require.NotNil(t, endedAt)
	rows, err := db.Pool.Query(ctx, `
		SELECT event_type, actor_user_id, subject_user_id
		FROM identity_audit_events
		WHERE request_id=$1 ORDER BY event_type`, impersonatingRequestID)
	require.NoError(t, err)
	defer rows.Close()
	type auditIdentity struct {
		eventType string
		actorID   uuid.UUID
		subjectID uuid.UUID
	}
	var audits []auditIdentity
	for rows.Next() {
		var audit auditIdentity
		require.NoError(t, rows.Scan(&audit.eventType, &audit.actorID, &audit.subjectID))
		audits = append(audits, audit)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []auditIdentity{
		{eventType: "impersonation_ended", actorID: primarySeedID, subjectID: subjectID},
		{eventType: "session_revoked", actorID: primarySeedID, subjectID: primarySeedID},
	}, audits)

	normalRequestID := "logout-normal-" + uuid.NewString()
	require.NoError(t, repository.LogoutSession(ctx, sessionIDs[1], now, normalRequestID))
	endedRequestID := "logout-ended-" + uuid.NewString()
	require.NoError(t, repository.LogoutSession(ctx, sessionIDs[2], now, endedRequestID))
	for _, requestID := range []string{normalRequestID, endedRequestID} {
		var sessionAudits, impersonationAudits int
		require.NoError(t, db.Pool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE event_type='session_revoked'),
			       count(*) FILTER (WHERE event_type='impersonation_ended')
			FROM identity_audit_events WHERE request_id=$1`, requestID).
			Scan(&sessionAudits, &impersonationAudits))
		require.Equal(t, 1, sessionAudits)
		require.Zero(t, impersonationAudits)
	}
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
	wrongActor := impersonation
	wrongActor.ID = uuid.New()
	wrongActor.ActorUserID = memberID
	wrongActor.SubjectUserID = primarySeedID
	err = repository.StartImpersonation(ctx, wrongActor, auditEvent("impersonation_started", now))
	require.ErrorIs(t, err, identity.ErrImpersonationDenied)

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

	for index, status := range []identity.DeliveryStatus{identity.DeliveryFailed, identity.DeliveryDelivered} {
		category := identity.ProviderUnavailable
		delivery := identity.InvitationDelivery{
			ID: uuid.New(), InvitationID: invitationID, Channel: identity.DeliveryProviderDM,
			Status: status, ErrorCategory: &category, AttemptedAt: now.Add(time.Duration(index) * time.Second),
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
	latest, err := repository.ListLatestInvitationDeliveries(ctx)
	require.NoError(t, err)
	require.Equal(t, identity.DeliveryDelivered, latest[invitationID].Status)
}
