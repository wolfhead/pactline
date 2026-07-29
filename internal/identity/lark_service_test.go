package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLarkAuthorizationStateIsOneTimeAndBootstrapRequiresVerifiedEmail(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	repository := &larkServiceRepository{}
	authenticator := &larkAuthenticator{
		principal: AuthenticatedPrincipal{Principal: Principal{
			Key:  PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_admin"},
			Name: "Admin", Email: pointer("admin@example.test"), EmailVerified: true, Active: true,
		}},
	}
	service, err := NewService(repository, larkUsers{}, []byte("01234567890123456789012345678901"),
		fixedLarkClock{now}, &sequenceSecrets{values: []string{"oauth-state", "session-secret", "csrf-secret"}})
	require.NoError(t, err)
	require.NoError(t, service.ConfigureLark(LarkServiceConfig{
		Repository: repository, Authenticator: authenticator, Verifier: larkVerifier{},
		TenantID: "tenant", RedirectURI: "https://app.test/api/auth/lark/callback",
		BootstrapAdminEmail: "admin@example.test",
	}))

	start, err := service.StartAuthorization(context.Background(), AuthorizationLogin, nil)
	require.NoError(t, err)
	require.Contains(t, start.URL, "oauth-state")
	tokens, err := service.CompleteAuthorization(context.Background(), "oauth-state", "code", "request")
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, tokens.SessionID)
	require.NotNil(t, repository.bootstrap)

	_, err = service.CompleteAuthorization(context.Background(), "oauth-state", "code", "request")
	require.ErrorIs(t, err, ErrAuthorizationInvalid)
	require.Equal(t, 1, authenticator.exchanges)
}

func TestLarkBootstrapRejectsUnverifiedEmailGenerically(t *testing.T) {
	now := time.Now().UTC()
	repository := &larkServiceRepository{}
	authenticator := &larkAuthenticator{principal: AuthenticatedPrincipal{Principal: Principal{
		Key:  PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_admin"},
		Name: "Admin", Email: pointer("admin@example.test"), Active: true,
	}}}
	service, err := NewService(repository, larkUsers{}, []byte("01234567890123456789012345678901"),
		fixedLarkClock{now}, &sequenceSecrets{values: []string{"state", "session", "csrf"}})
	require.NoError(t, err)
	require.NoError(t, service.ConfigureLark(LarkServiceConfig{
		Repository: repository, Authenticator: authenticator, Verifier: larkVerifier{},
		TenantID: "tenant", RedirectURI: "https://app.test/callback", BootstrapAdminEmail: "admin@example.test",
	}))
	_, err = service.StartAuthorization(context.Background(), AuthorizationLogin, nil)
	require.NoError(t, err)
	_, err = service.CompleteAuthorization(context.Background(), "state", "secret-code", "")
	require.ErrorIs(t, err, ErrLoginDenied)
	require.Nil(t, repository.bootstrap)
	require.Equal(t, "login_rejected", repository.lastAudit.EventType)
	require.NotContains(t, err.Error(), "admin@example.test")
	require.NotContains(t, err.Error(), "secret-code")
}

func TestLarkExchangeFailureAuditsProviderRequestID(t *testing.T) {
	now := time.Now().UTC()
	repository := &larkServiceRepository{}
	authenticator := &larkAuthenticator{exchangeErr: testCategorizedProviderError{
		category: ProviderUnavailable, requestID: "exchange-request-id",
	}}
	service, err := NewService(repository, larkUsers{}, []byte("01234567890123456789012345678901"),
		fixedLarkClock{now}, &sequenceSecrets{values: []string{"state"}})
	require.NoError(t, err)
	require.NoError(t, service.ConfigureLark(LarkServiceConfig{
		Repository: repository, Authenticator: authenticator, Verifier: larkVerifier{},
		TenantID: "tenant", RedirectURI: "https://app.test/callback",
		BootstrapAdminEmail: "admin@example.test",
	}))
	_, err = service.StartAuthorization(context.Background(), AuthorizationLogin, nil)
	require.NoError(t, err)
	_, err = service.CompleteAuthorization(context.Background(), "state", "secret-code", "app-request-id")
	require.ErrorIs(t, err, ErrLoginDenied)
	require.Equal(t, "login_rejected", repository.lastAudit.EventType)
	require.JSONEq(t,
		`{"category":"provider_exchange","provider_request_id":"exchange-request-id"}`,
		string(repository.lastAudit.Metadata))
}

func TestInvitationCreationDeliveryRotationAndAcceptanceState(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	adminID := uuid.New()
	repository := &larkServiceRepository{
		external: ExternalIdentity{
			UserID: adminID,
			EncryptedCredential: OAuthCredential{
				EncryptionKeyID: "sealed", AccessTokenExpiresAt: now.Add(time.Hour),
				RefreshTokenExpiresAt: now.Add(24 * time.Hour),
			},
		},
		invitations: map[uuid.UUID]Invitation{},
	}
	principal := Principal{
		Key:  PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_member"},
		Name: "Member", Active: true,
	}
	directory := &larkDirectory{principal: principal}
	notifier := &larkNotifier{}
	service, err := NewService(repository, larkUsers{}, []byte("01234567890123456789012345678901"),
		fixedLarkClock{now}, &sequenceSecrets{values: []string{"first-token", "second-token", "oauth-state"}})
	require.NoError(t, err)
	authenticator := &larkAuthenticator{}
	require.NoError(t, service.ConfigureLark(LarkServiceConfig{
		Repository: repository, Authenticator: authenticator, Verifier: larkVerifier{},
		Directory: directory, Notifier: notifier, AppBaseURL: "https://app.test",
		TenantID: "tenant", RedirectURI: "https://app.test/callback", BootstrapAdminEmail: "admin@example.test",
	}))
	admin := domain.User{ID: adminID, Active: true, PlatformRole: domain.PlatformRoleAdmin}

	created, err := service.CreateInvitation(context.Background(), admin, "ou_member", "request")
	require.NoError(t, err)
	require.Equal(t, DeliveryDelivered, created.Delivery.Status)
	require.Equal(t, "https://app.test/invite#first-token", notifier.links[0])
	require.False(t, VerifySecret([]byte(notifier.links[0]), created.Invitation.TokenHash))
	require.True(t, VerifySecret([]byte("first-token"), created.Invitation.TokenHash))
	listed, err := service.ListInvitations(context.Background(), admin)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, DeliveryDelivered, listed[0].Delivery.Status)

	resent, err := service.ResendInvitation(context.Background(), admin, created.Invitation.ID)
	require.NoError(t, err)
	require.Equal(t, "https://app.test/invite#second-token", notifier.links[1])
	require.False(t, VerifySecret([]byte("first-token"), resent.Invitation.TokenHash))
	require.True(t, VerifySecret([]byte("second-token"), resent.Invitation.TokenHash))

	_, err = service.AcceptInvitationToken(context.Background(), "first-token")
	require.ErrorIs(t, err, ErrInvitationInvalid)
	start, err := service.AcceptInvitationToken(context.Background(), "second-token")
	require.NoError(t, err)
	require.Contains(t, start.URL, "oauth-state")
}

func TestInvitationOAuthAcceptanceRequiresValidatedTokenVersion(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 30, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name   string
		rotate bool
	}{
		{name: "unchanged token succeeds"},
		{name: "rotation after state consumption fails", rotate: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rawToken := "invitation-token"
			tokenHash := HashSecret([]byte(rawToken))
			invitation := Invitation{
				ID: uuid.New(),
				Target: PrincipalKey{
					Provider: "lark", TenantID: "tenant", SubjectID: "ou_member",
				},
				TokenHash: tokenHash[:], Status: InvitationPending,
				ExpiresAt: now.Add(InvitationLifetime), CreatedAt: now, UpdatedAt: now,
			}
			repository := &larkServiceRepository{
				invitations: map[uuid.UUID]Invitation{invitation.ID: invitation},
			}
			authenticator := &larkAuthenticator{principal: AuthenticatedPrincipal{
				Principal: Principal{Key: invitation.Target, Name: "Member", Active: true},
			}}
			if testCase.rotate {
				authenticator.onExchange = func() {
					rotated := repository.invitations[invitation.ID]
					rotatedHash := HashSecret([]byte("rotated-token"))
					rotated.TokenHash = rotatedHash[:]
					repository.invitations[invitation.ID] = rotated
				}
			}
			service, err := NewService(
				repository, larkUsers{}, []byte("01234567890123456789012345678901"),
				fixedLarkClock{now},
				&sequenceSecrets{values: []string{"oauth-state", "session-secret", "csrf-secret"}},
			)
			require.NoError(t, err)
			require.NoError(t, service.ConfigureLark(LarkServiceConfig{
				Repository: repository, Authenticator: authenticator, Verifier: larkVerifier{},
				TenantID: "tenant", RedirectURI: "https://app.test/callback",
				BootstrapAdminEmail: "admin@example.test",
			}))

			_, err = service.AcceptInvitationToken(context.Background(), rawToken)
			require.NoError(t, err)
			require.Equal(t, tokenHash[:], repository.authorization.InvitationTokenHash)
			_, err = service.CompleteAuthorization(
				context.Background(), "oauth-state", "code", "request-id",
			)
			require.NotNil(t, repository.authorization.ConsumedAt)
			if testCase.rotate {
				require.ErrorIs(t, err, ErrLoginDenied)
				require.Nil(t, repository.acceptedInvitation)
				require.Equal(t, "login_rejected", repository.lastAudit.EventType)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, repository.acceptedInvitation)
			require.Equal(t, tokenHash[:], repository.acceptedInvitation.InvitationTokenHash)
		})
	}
}

func TestUserLifecycleAndImpersonationUseRealAdminActor(t *testing.T) {
	now := time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)
	admin := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleAdmin}
	member := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	users := lifecycleUsers{users: map[uuid.UUID]domain.User{admin.ID: admin, member.ID: member}}
	repository := &larkServiceRepository{}
	service, err := NewService(repository, users, []byte("01234567890123456789012345678901"),
		fixedLarkClock{now}, &sequenceSecrets{})
	require.NoError(t, err)

	require.NoError(t, service.SetUserActive(context.Background(), admin, member.ID, false))
	require.Equal(t, member.ID, repository.deactivated)
	require.Equal(t, admin.ID, repository.lifecycleActor)
	require.ErrorIs(t, service.SetUserActive(context.Background(), admin, admin.ID, false), domain.ErrForbidden)

	current := RequestIdentity{SessionID: uuid.New(), Actor: admin, Subject: admin}
	require.NoError(t, service.StartImpersonation(context.Background(), current, member.ID, "request"))
	require.NotNil(t, repository.impersonation)
	require.Equal(t, admin.ID, repository.impersonation.ActorUserID)
	require.Equal(t, member.ID, repository.impersonation.SubjectUserID)

	current.Subject = member
	current.Impersonation = repository.impersonation
	require.NoError(t, service.RecordImpersonationWriteRejected(
		context.Background(), current, "PATCH", "/api/v1/tasks/{number}", "request"))
	require.Equal(t, "impersonation_write_rejected", repository.lastAudit.EventType)
	require.JSONEq(t, `{"method":"PATCH","route":"/api/v1/tasks/{number}"}`, string(repository.lastAudit.Metadata))
	require.NoError(t, service.EndImpersonation(context.Background(), current, "request"))
	require.True(t, repository.impersonationEnded)
}

func TestCredentialRefreshTransientUsesProviderGrace(t *testing.T) {
	now := time.Date(2026, 7, 28, 14, 0, 0, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	repository := &larkServiceRepository{
		external: ExternalIdentity{
			ID: uuid.New(), UserID: user.ID,
			Key: PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_member"},
			EncryptedCredential: OAuthCredential{
				AccessTokenExpiresAt: now.Add(-time.Minute), RefreshTokenExpiresAt: now.Add(time.Hour),
			},
		},
	}
	service, err := NewService(repository, lifecycleUsers{users: map[uuid.UUID]domain.User{user.ID: user}},
		[]byte("01234567890123456789012345678901"), fixedLarkClock{now}, &sequenceSecrets{})
	require.NoError(t, err)
	service.authenticator = &larkAuthenticator{refreshErr: testCategorizedProviderError{
		category: ProviderUnavailable, requestID: "refresh-request-id",
	}}
	service.verifier = larkVerifier{}
	bundle := SessionBundle{User: user, Session: Session{ID: uuid.New(), UserID: user.ID}}

	require.NoError(t, service.revalidate(context.Background(), &bundle, now))
	require.Equal(t, 1, repository.providerFailures)
	require.JSONEq(t,
		`{"category":"unavailable","provider_request_id":"refresh-request-id"}`,
		string(repository.providerFailureAudit.Metadata))

	failureSince := now.Add(-ProviderTransientGrace - time.Second)
	bundle.Session.ProviderFailureSince = &failureSince
	require.ErrorIs(t, service.revalidate(context.Background(), &bundle, now), ErrProviderTransient)
}

func TestConcurrentRevalidationRefreshesOnceAndVerifiesReturnedCredential(t *testing.T) {
	now := time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	repository := &larkServiceRepository{
		external: ExternalIdentity{
			ID: uuid.New(), UserID: user.ID,
			Key: PrincipalKey{Provider: "lark", TenantID: "tenant", SubjectID: "ou_member"},
			EncryptedCredential: OAuthCredential{
				AccessTokenExpiresAt: now.Add(-time.Minute), RefreshTokenExpiresAt: now.Add(time.Hour),
			},
		},
	}
	refreshed := OAuthCredential{
		AccessTokenCiphertext: []byte("fresh-access"), RefreshTokenCiphertext: []byte("fresh-refresh"),
		AccessTokenExpiresAt: now.Add(time.Hour), RefreshTokenExpiresAt: now.Add(24 * time.Hour),
		EncryptionKeyID: "test",
	}
	authenticator := &larkAuthenticator{refreshedCredential: refreshed}
	verifier := &freshCredentialVerifier{now: now}
	service, err := NewService(repository, lifecycleUsers{users: map[uuid.UUID]domain.User{user.ID: user}},
		[]byte("01234567890123456789012345678901"), fixedLarkClock{now}, &sequenceSecrets{})
	require.NoError(t, err)
	service.authenticator = authenticator
	service.verifier = verifier

	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			bundle := SessionBundle{User: user, Session: Session{ID: uuid.New(), UserID: user.ID}}
			results <- service.revalidate(context.Background(), &bundle, now)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	require.EqualValues(t, 1, authenticator.refreshCalls.Load())
	require.EqualValues(t, 2, verifier.calls.Load())
	require.Equal(t, uuid.Nil, repository.deactivated)
}

func TestTokenOwnerRevalidationUsesVerificationWindowAndTransientGrace(t *testing.T) {
	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	credential := OAuthCredential{
		AccessTokenExpiresAt:  now.Add(time.Hour),
		RefreshTokenExpiresAt: now.Add(24 * time.Hour),
	}

	t.Run("recent verification avoids provider call", func(t *testing.T) {
		recent := now.Add(-10 * time.Minute)
		repository := &larkServiceRepository{external: ExternalIdentity{
			ID: uuid.New(), UserID: user.ID, LastVerifiedAt: &recent,
			EncryptedCredential: credential,
		}}
		verifier := &resultVerifier{result: VerificationResult{State: VerificationValid}}
		service, err := NewService(repository, lifecycleUsers{}, []byte("01234567890123456789012345678901"),
			fixedLarkClock{now}, &sequenceSecrets{})
		require.NoError(t, err)
		service.identity, service.verifier = repository, verifier

		require.NoError(t, service.VerifyTokenOwner(context.Background(), user))
		require.Zero(t, verifier.calls.Load())
		require.Zero(t, repository.tokenVerifications)
	})

	t.Run("due verification is persisted", func(t *testing.T) {
		stale := now.Add(-20 * time.Minute)
		repository := &larkServiceRepository{external: ExternalIdentity{
			ID: uuid.New(), UserID: user.ID, LastVerifiedAt: &stale,
			EncryptedCredential: credential,
		}}
		verifier := &resultVerifier{result: VerificationResult{State: VerificationValid}}
		service, err := NewService(repository, lifecycleUsers{}, []byte("01234567890123456789012345678901"),
			fixedLarkClock{now}, &sequenceSecrets{})
		require.NoError(t, err)
		service.identity, service.verifier = repository, verifier

		require.NoError(t, service.VerifyTokenOwner(context.Background(), user))
		require.EqualValues(t, 1, verifier.calls.Load())
		require.Equal(t, 1, repository.tokenVerifications)
	})

	for _, test := range []struct {
		name         string
		lastVerified time.Time
		want         error
	}{
		{name: "transient inside grace", lastVerified: now.Add(-30 * time.Minute)},
		{name: "transient outside grace", lastVerified: now.Add(-2 * time.Hour), want: ErrProviderTransient},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &larkServiceRepository{external: ExternalIdentity{
				ID: uuid.New(), UserID: user.ID, LastVerifiedAt: &test.lastVerified,
				EncryptedCredential: credential,
			}}
			verifier := &resultVerifier{result: VerificationResult{
				State: VerificationTransient, Category: ProviderUnavailable,
			}}
			service, err := NewService(repository, lifecycleUsers{}, []byte("01234567890123456789012345678901"),
				fixedLarkClock{now}, &sequenceSecrets{})
			require.NoError(t, err)
			service.identity, service.verifier = repository, verifier

			err = service.VerifyTokenOwner(context.Background(), user)
			require.ErrorIs(t, err, test.want)
		})
	}
}

type larkServiceRepository struct {
	authorization        *AuthorizationTransaction
	bootstrap            *BootstrapAdminCommand
	external             ExternalIdentity
	invitations          map[uuid.UUID]Invitation
	deliveries           []InvitationDelivery
	deactivated          uuid.UUID
	lifecycleActor       uuid.UUID
	impersonation        *Impersonation
	impersonationEnded   bool
	lastAudit            AuditEvent
	providerFailures     int
	tokenVerifications   int
	providerFailureAudit AuditEvent
	acceptedInvitation   *AcceptInvitationCommand
	refreshMu            sync.Mutex
}

func (r *larkServiceRepository) CreateAuthorizationTransaction(_ context.Context, transaction AuthorizationTransaction) error {
	r.authorization = &transaction
	return nil
}

func (r *larkServiceRepository) ConsumeAuthorizationState(_ context.Context, hash []byte, now time.Time) (AuthorizationTransaction, error) {
	if r.authorization == nil || r.authorization.ConsumedAt != nil ||
		!VerifySecret([]byte("oauth-state"), hash) && !VerifySecret([]byte("state"), hash) ||
		!now.Before(r.authorization.ExpiresAt) {
		return AuthorizationTransaction{}, ErrAuthorizationInvalid
	}
	consumed := now
	r.authorization.ConsumedAt = &consumed
	return *r.authorization, nil
}

func (r *larkServiceRepository) BootstrapAdmin(_ context.Context, command BootstrapAdminCommand) (domain.User, error) {
	r.bootstrap = &command
	return domain.User{ID: command.Session.UserID, Active: true, PlatformRole: domain.PlatformRoleAdmin}, nil
}

func (r *larkServiceRepository) LoginExternal(context.Context, LoginCommand) (domain.User, error) {
	return domain.User{}, ErrLoginDenied
}
func (r *larkServiceRepository) GetExternalIdentityForUser(context.Context, uuid.UUID) (ExternalIdentity, error) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	if r.external.UserID == uuid.Nil {
		return ExternalIdentity{}, ErrCredentialNotFound
	}
	return r.external, nil
}
func (r *larkServiceRepository) RefreshCredentialLocked(
	_ context.Context,
	_ uuid.UUID,
	refresh func(OAuthCredential) (OAuthCredential, error),
) (OAuthCredential, error) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	updated, err := refresh(r.external.EncryptedCredential)
	if err == nil {
		r.external.EncryptedCredential = updated
	}
	return updated, err
}
func (r *larkServiceRepository) RecordProviderVerification(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (r *larkServiceRepository) RecordTokenProviderVerification(context.Context, uuid.UUID, time.Time) error {
	r.tokenVerifications++
	return nil
}
func (r *larkServiceRepository) RecordProviderFailure(
	_ context.Context,
	_ uuid.UUID,
	_ time.Time,
	audit AuditEvent,
) error {
	r.providerFailures++
	r.providerFailureAudit = audit
	return nil
}
func (r *larkServiceRepository) DeactivateUser(
	_ context.Context,
	userID, actorID uuid.UUID,
	_, _ string,
) error {
	r.deactivated, r.lifecycleActor = userID, actorID
	return nil
}
func (r *larkServiceRepository) AcceptInvitation(
	_ context.Context,
	command AcceptInvitationCommand,
) (domain.User, error) {
	invitation, ok := r.invitations[command.InvitationID]
	if !ok || !InvitationMatches(invitation, command.Principal.Key, command.Now) ||
		len(command.InvitationTokenHash) == 0 ||
		subtle.ConstantTimeCompare(invitation.TokenHash, command.InvitationTokenHash) != 1 {
		return domain.User{}, ErrInvitationInvalid
	}
	r.acceptedInvitation = &command
	return domain.User{
		ID: command.UserID, Name: command.UserName, Email: command.UserEmail,
		PlatformRole: domain.PlatformRoleMember, Active: true,
	}, nil
}
func (r *larkServiceRepository) CreateInvitation(_ context.Context, invitation Invitation, _ AuditEvent) error {
	if r.invitations == nil {
		r.invitations = map[uuid.UUID]Invitation{}
	}
	for _, existing := range r.invitations {
		if existing.Target == invitation.Target && existing.Status == InvitationPending {
			return ErrInvitationConflict
		}
	}
	r.invitations[invitation.ID] = invitation
	return nil
}
func (r *larkServiceRepository) ListInvitations(context.Context) ([]Invitation, error) {
	var invitations []Invitation
	for _, invitation := range r.invitations {
		invitations = append(invitations, invitation)
	}
	return invitations, nil
}
func (r *larkServiceRepository) ListLatestInvitationDeliveries(context.Context) (map[uuid.UUID]InvitationDelivery, error) {
	deliveries := make(map[uuid.UUID]InvitationDelivery)
	for _, delivery := range r.deliveries {
		deliveries[delivery.InvitationID] = delivery
	}
	return deliveries, nil
}
func (r *larkServiceRepository) ExpireInvitations(_ context.Context, now time.Time) (int64, error) {
	var count int64
	for id, invitation := range r.invitations {
		if invitation.Status == InvitationPending && !now.Before(invitation.ExpiresAt) {
			invitation.Status = InvitationExpired
			r.invitations[id] = invitation
			count++
		}
	}
	return count, nil
}
func (r *larkServiceRepository) GetInvitation(context.Context, uuid.UUID) (Invitation, error) {
	return Invitation{}, ErrInvitationInvalid
}
func (r *larkServiceRepository) GetInvitationByTokenHash(_ context.Context, hash []byte, now time.Time) (Invitation, error) {
	for _, invitation := range r.invitations {
		if string(invitation.TokenHash) == string(hash) && invitation.Status == InvitationPending && now.Before(invitation.ExpiresAt) {
			return invitation, nil
		}
	}
	return Invitation{}, ErrInvitationInvalid
}
func (r *larkServiceRepository) RotateInvitation(_ context.Context, id, _ uuid.UUID, hash []byte, expiresAt time.Time) (Invitation, error) {
	invitation, ok := r.invitations[id]
	if !ok {
		return Invitation{}, ErrInvitationInvalid
	}
	invitation.TokenHash = append([]byte(nil), hash...)
	invitation.ExpiresAt = expiresAt
	r.invitations[id] = invitation
	return invitation, nil
}
func (r *larkServiceRepository) RevokeInvitation(context.Context, uuid.UUID, time.Time, AuditEvent) error {
	return ErrInvitationInvalid
}
func (r *larkServiceRepository) RecordDelivery(_ context.Context, delivery InvitationDelivery) error {
	r.deliveries = append(r.deliveries, delivery)
	return nil
}
func (r *larkServiceRepository) ReactivateUser(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (r *larkServiceRepository) StartImpersonation(_ context.Context, value Impersonation, _ AuditEvent) error {
	r.impersonation = &value
	return nil
}
func (r *larkServiceRepository) EndImpersonation(context.Context, uuid.UUID, time.Time, AuditEvent) error {
	r.impersonationEnded = true
	return nil
}
func (r *larkServiceRepository) AppendAudit(_ context.Context, audit AuditEvent) error {
	r.lastAudit = audit
	return nil
}
func (r *larkServiceRepository) FindExternalIdentity(context.Context, PrincipalKey) (ExternalIdentity, domain.User, error) {
	return ExternalIdentity{}, domain.User{}, domain.ErrNotFound
}
func (r *larkServiceRepository) CreateSession(context.Context, Session, AuditEvent) error { return nil }
func (r *larkServiceRepository) ResolveSession(context.Context, uuid.UUID, []byte, time.Time) (SessionBundle, error) {
	return SessionBundle{}, ErrSessionInvalid
}
func (r *larkServiceRepository) TouchSession(context.Context, uuid.UUID, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (r *larkServiceRepository) LogoutSession(context.Context, uuid.UUID, time.Time, string) error {
	return nil
}

type larkAuthenticator struct {
	principal           AuthenticatedPrincipal
	exchanges           int
	exchangeErr         error
	onExchange          func()
	refreshErr          error
	refreshedCredential OAuthCredential
	refreshCalls        atomic.Int64
}

func (a *larkAuthenticator) StartAuthorization(_ context.Context, request AuthorizationRequest) (AuthorizationStart, error) {
	return AuthorizationStart{URL: "https://accounts.test/authorize?state=" + request.State}, nil
}
func (a *larkAuthenticator) ExchangeAuthorizationCode(context.Context, string) (AuthenticatedPrincipal, error) {
	a.exchanges++
	if a.exchangeErr != nil {
		return AuthenticatedPrincipal{}, a.exchangeErr
	}
	if a.onExchange != nil {
		a.onExchange()
	}
	return a.principal, nil
}
func (a *larkAuthenticator) RefreshCredential(context.Context, OAuthCredential) (RefreshedCredential, error) {
	a.refreshCalls.Add(1)
	if a.refreshErr != nil {
		return RefreshedCredential{}, a.refreshErr
	}
	if !a.refreshedCredential.AccessTokenExpiresAt.IsZero() {
		return RefreshedCredential{Credential: a.refreshedCredential}, nil
	}
	return RefreshedCredential{}, errors.New("unexpected refresh")
}

type larkVerifier struct{}

func (larkVerifier) VerifyPrincipal(context.Context, OAuthCredential, PrincipalKey) (VerificationResult, error) {
	return VerificationResult{State: VerificationValid}, nil
}

type freshCredentialVerifier struct {
	now   time.Time
	calls atomic.Int64
}

type resultVerifier struct {
	result VerificationResult
	err    error
	calls  atomic.Int64
}

func (v *resultVerifier) VerifyPrincipal(context.Context, OAuthCredential, PrincipalKey) (VerificationResult, error) {
	v.calls.Add(1)
	return v.result, v.err
}

type testCategorizedProviderError struct {
	category  ProviderErrorCategory
	requestID string
}

func (e testCategorizedProviderError) Error() string { return "provider failure" }
func (e testCategorizedProviderError) ProviderCategory() ProviderErrorCategory {
	return e.category
}
func (e testCategorizedProviderError) ProviderRequestID() string { return e.requestID }

func (v *freshCredentialVerifier) VerifyPrincipal(
	_ context.Context,
	credential OAuthCredential,
	_ PrincipalKey,
) (VerificationResult, error) {
	v.calls.Add(1)
	if !v.now.Before(credential.AccessTokenExpiresAt) {
		return VerificationResult{State: VerificationInvalid, Category: ProviderAuthorizationRevoked}, nil
	}
	return VerificationResult{State: VerificationValid}, nil
}

type larkDirectory struct{ principal Principal }

func (d *larkDirectory) SearchPrincipals(context.Context, OAuthCredential, string, int) ([]Principal, error) {
	return []Principal{d.principal}, nil
}
func (d *larkDirectory) GetPrincipal(context.Context, OAuthCredential, string) (Principal, error) {
	return d.principal, nil
}

type larkNotifier struct{ links []string }

func (n *larkNotifier) SendInvitation(_ context.Context, _ PrincipalKey, link string) (DeliveryReceipt, error) {
	n.links = append(n.links, link)
	return DeliveryReceipt{ProviderReference: "message"}, nil
}

type larkUsers struct{}

func (larkUsers) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	return domain.User{ID: id, Active: true}, nil
}
func (larkUsers) ListAll(context.Context) ([]domain.User, error) { return nil, nil }

type lifecycleUsers struct{ users map[uuid.UUID]domain.User }

func (u lifecycleUsers) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	user, ok := u.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}
func (u lifecycleUsers) ListAll(context.Context) ([]domain.User, error) {
	var users []domain.User
	for _, user := range u.users {
		users = append(users, user)
	}
	return users, nil
}

type fixedLarkClock struct{ now time.Time }

func (c fixedLarkClock) Now() time.Time { return c.now }

type sequenceSecrets struct {
	values []string
	index  int
}

func (s *sequenceSecrets) NewSecret() (string, error) {
	if s.index >= len(s.values) {
		return "", errors.New("no secret available")
	}
	value := s.values[s.index]
	s.index++
	return value, nil
}

func pointer(value string) *string { return &value }
