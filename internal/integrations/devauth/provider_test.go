package devauth

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeUsers struct {
	user domain.User
	err  error
}

func (f fakeUsers) GetByID(context.Context, uuid.UUID) (domain.User, error) {
	return f.user, f.err
}

type fakeSessions struct {
	called bool
}

func (f *fakeSessions) IssueSession(_ context.Context, userID uuid.UUID, _ string) (identity.SessionTokens, error) {
	f.called = true
	return identity.SessionTokens{SessionID: userID}, nil
}

func TestDevelopmentProviderIssuesNormalSessionForActiveUser(t *testing.T) {
	userID := uuid.New()
	issuer := &fakeSessions{}
	provider := New(fakeUsers{user: domain.User{ID: userID, Active: true}}, issuer)
	tokens, err := provider.Authenticate(context.Background(), userID, "request-id")
	require.NoError(t, err)
	require.True(t, issuer.called)
	require.Equal(t, userID, tokens.SessionID)
}

func TestDevelopmentProviderRejectsInactiveUser(t *testing.T) {
	issuer := &fakeSessions{}
	provider := New(fakeUsers{user: domain.User{ID: uuid.New(), Active: false}}, issuer)
	_, err := provider.Authenticate(context.Background(), uuid.New(), "request-id")
	require.ErrorIs(t, err, identity.ErrUserInactive)
	require.False(t, issuer.called)
}
