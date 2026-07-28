package identity

import (
	"context"
	"testing"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

type fixedSecrets struct {
	values []string
}

func (f *fixedSecrets) NewSecret() (string, error) {
	value := f.values[0]
	f.values = f.values[1:]
	return value, nil
}

type serviceUsers struct{ user domain.User }

func (s serviceUsers) GetByID(context.Context, uuid.UUID) (domain.User, error) { return s.user, nil }

type serviceSessions struct {
	session       Session
	bundle        SessionBundle
	resolveCalls  int
	touchCalls    int
	logoutCalls   int
	logoutSession uuid.UUID
	logoutErr     error
}

func (s *serviceSessions) CreateSession(_ context.Context, session Session, _ AuditEvent) error {
	s.session = session
	return nil
}

func (s *serviceSessions) ResolveSession(_ context.Context, _ uuid.UUID, secretHash []byte, _ time.Time) (SessionBundle, error) {
	s.resolveCalls++
	if s.bundle.Session.ID == uuid.Nil {
		s.bundle = SessionBundle{Session: s.session, User: domain.User{ID: s.session.UserID, Active: true}}
	}
	if len(secretHash) != len(s.bundle.Session.SecretHash) {
		return SessionBundle{}, ErrSessionInvalid
	}
	return s.bundle, nil
}

func (s *serviceSessions) TouchSession(context.Context, uuid.UUID, time.Time, time.Time) (bool, error) {
	s.touchCalls++
	return true, nil
}

func (s *serviceSessions) LogoutSession(_ context.Context, sessionID uuid.UUID, _ time.Time, _ string) error {
	s.logoutCalls++
	s.logoutSession = sessionID
	return s.logoutErr
}

func TestSessionEnvelopeRejectsBadMACBeforeLookup(t *testing.T) {
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	repository := &serviceSessions{}
	secrets := &fixedSecrets{values: []string{
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
	}}
	service, err := NewService(repository, serviceUsers{user: user}, []byte("01234567890123456789012345678901"), fixedClock{now}, secrets)
	require.NoError(t, err)
	tokens, err := service.IssueSession(context.Background(), user.ID, "request")
	require.NoError(t, err)
	cookie := service.SessionCookieValue(tokens)
	cookie = cookie[:len(cookie)-1] + "x"
	_, _, err = service.Authenticate(context.Background(), cookie)
	require.ErrorIs(t, err, ErrSessionInvalid)
	require.Zero(t, repository.resolveCalls)
}

func TestSessionActorSubjectAndCSRF(t *testing.T) {
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	actor := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleAdmin}
	subject := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	repository := &serviceSessions{}
	secrets := &fixedSecrets{values: []string{
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
	}}
	service, err := NewService(repository, serviceUsers{user: actor}, []byte("01234567890123456789012345678901"), fixedClock{now}, secrets)
	require.NoError(t, err)
	tokens, err := service.IssueSession(context.Background(), actor.ID, "")
	require.NoError(t, err)
	repository.bundle = SessionBundle{
		Session: repository.session, User: actor, Subject: &subject,
		Impersonation: &Impersonation{ID: uuid.New(), SessionID: repository.session.ID, ActorUserID: actor.ID, SubjectUserID: subject.ID},
	}
	requestIdentity, session, err := service.Authenticate(context.Background(), service.SessionCookieValue(tokens))
	require.NoError(t, err)
	require.Equal(t, actor.ID, requestIdentity.Actor.ID)
	require.Equal(t, subject.ID, requestIdentity.Subject.ID)
	require.True(t, requestIdentity.IsImpersonating())
	require.True(t, service.VerifyCSRF(session, tokens.CSRFSecret))
	require.False(t, service.VerifyCSRF(session, tokens.CSRFSecret+"x"))
}

func TestImpersonatingLogoutUsesSingleRepositoryOperation(t *testing.T) {
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	repository := &serviceSessions{}
	service, err := NewService(
		repository,
		serviceUsers{user: domain.User{ID: uuid.New(), Active: true}},
		[]byte("01234567890123456789012345678901"),
		fixedClock{now},
		&fixedSecrets{},
	)
	require.NoError(t, err)
	actor := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleAdmin}
	subject := domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember}
	requestIdentity := RequestIdentity{
		SessionID: uuid.New(), Actor: actor, Subject: subject,
		Impersonation: &Impersonation{ID: uuid.New(), ActorUserID: actor.ID, SubjectUserID: subject.ID},
	}
	require.NoError(t, service.Logout(context.Background(), requestIdentity, "request"))
	require.Equal(t, 1, repository.logoutCalls)
	require.Equal(t, requestIdentity.SessionID, repository.logoutSession)
}

func TestNormalLogoutUsesSingleRepositoryOperation(t *testing.T) {
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	repository := &serviceSessions{}
	service, err := NewService(
		repository,
		serviceUsers{user: domain.User{ID: uuid.New(), Active: true}},
		[]byte("01234567890123456789012345678901"),
		fixedClock{now},
		&fixedSecrets{},
	)
	require.NoError(t, err)
	requestIdentity := RequestIdentity{
		SessionID: uuid.New(),
		Actor:     domain.User{ID: uuid.New(), Active: true, PlatformRole: domain.PlatformRoleMember},
	}
	requestIdentity.Subject = requestIdentity.Actor
	require.NoError(t, service.Logout(context.Background(), requestIdentity, "request"))
	require.Equal(t, 1, repository.logoutCalls)
}
