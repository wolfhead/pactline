package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"bountyboard/internal/domain"

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
	require.NotContains(t, err.Error(), "admin@example.test")
	require.NotContains(t, err.Error(), "secret-code")
}

type larkServiceRepository struct {
	authorization *AuthorizationTransaction
	bootstrap     *BootstrapAdminCommand
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
	return ExternalIdentity{}, ErrCredentialNotFound
}
func (r *larkServiceRepository) RefreshCredentialLocked(context.Context, uuid.UUID, func(OAuthCredential) (OAuthCredential, error)) (OAuthCredential, error) {
	return OAuthCredential{}, errors.New("unexpected refresh")
}
func (r *larkServiceRepository) RecordProviderVerification(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (r *larkServiceRepository) RecordProviderFailure(context.Context, uuid.UUID, time.Time, AuditEvent) error {
	return nil
}
func (r *larkServiceRepository) DeactivateUser(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (r *larkServiceRepository) AcceptInvitation(context.Context, AcceptInvitationCommand) (domain.User, error) {
	return domain.User{}, ErrInvitationInvalid
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
	principal AuthenticatedPrincipal
	exchanges int
}

func (a *larkAuthenticator) StartAuthorization(_ context.Context, request AuthorizationRequest) (AuthorizationStart, error) {
	return AuthorizationStart{URL: "https://accounts.test/authorize?state=" + request.State}, nil
}
func (a *larkAuthenticator) ExchangeAuthorizationCode(context.Context, string) (AuthenticatedPrincipal, error) {
	a.exchanges++
	return a.principal, nil
}
func (a *larkAuthenticator) RefreshCredential(context.Context, OAuthCredential) (RefreshedCredential, error) {
	return RefreshedCredential{}, errors.New("unexpected refresh")
}

type larkVerifier struct{}

func (larkVerifier) VerifyPrincipal(context.Context, OAuthCredential, PrincipalKey) (VerificationResult, error) {
	return VerificationResult{State: VerificationValid}, nil
}

type larkUsers struct{}

func (larkUsers) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	return domain.User{ID: id, Active: true}, nil
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
