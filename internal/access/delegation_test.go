package access

import (
	"context"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDelegateServiceIssuesAndAuthenticatesRunBoundCredential(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Name: "Agent User", Active: true}
	run := delegatedTestRun(user.ID, now)
	runs := &delegateRunReader{run: run}
	users := &delegateUserReader{user: user}
	clock := &delegateClock{now: now}
	service := newDelegateTestService(t, runs, users, clock)

	raw, err := service.Issue(context.Background(), run.ID, user.ID)
	require.NoError(t, err)
	require.NotContains(t, raw, user.ID.String())
	require.NotContains(t, raw, run.ID.String())

	principal, err := service.Authenticate(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, AuthenticationMethodAgentDelegate, principal.Method)
	require.Equal(t, user.ID, principal.User.ID)
	require.NotNil(t, principal.AgentRunID)
	require.Equal(t, run.ID, *principal.AgentRunID)
	require.True(t, principal.HasScope(ScopeWorkRead))
	require.True(t, principal.HasScope(ScopeWorkWrite))
}

func TestDelegateServiceRejectsExpiredMismatchedAndNonRunningCredentials(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Name: "Agent User", Active: true}
	run := delegatedTestRun(user.ID, now)
	runs := &delegateRunReader{run: run}
	users := &delegateUserReader{user: user}
	clock := &delegateClock{now: now}
	service := newDelegateTestService(t, runs, users, clock)
	raw, err := service.Issue(context.Background(), run.ID, user.ID)
	require.NoError(t, err)

	clock.now = now.Add(AgentDelegateLifetime)
	_, err = service.Authenticate(context.Background(), raw)
	require.ErrorIs(t, err, ErrAgentDelegateExpired)

	clock.now = now
	runs.run.InitiatingUserID = uuid.New()
	_, err = service.Authenticate(context.Background(), raw)
	require.ErrorIs(t, err, ErrAgentDelegateRun)

	runs.run = run
	runs.run.Status = pactagent.RunWaitingUser
	_, err = service.Authenticate(context.Background(), raw)
	require.ErrorIs(t, err, ErrAgentDelegateRun)
}

func TestDelegateServiceRejectsUnknownKeyAndInactiveUser(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	user := domain.User{ID: uuid.New(), Name: "Agent User", Active: true}
	run := delegatedTestRun(user.ID, now)
	runs := &delegateRunReader{run: run}
	users := &delegateUserReader{user: user}
	clock := &delegateClock{now: now}
	service := newDelegateTestService(t, runs, users, clock)
	raw, err := service.Issue(context.Background(), run.ID, user.ID)
	require.NoError(t, err)

	other, err := NewDelegateService(DelegateConfig{
		ActiveKeyID: "other",
		SigningKeys: map[string][]byte{"other": []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}, runs, users, clock)
	require.NoError(t, err)
	_, err = other.Authenticate(context.Background(), raw)
	require.ErrorIs(t, err, ErrAgentDelegateInvalid)

	users.user.Active = false
	_, err = service.Authenticate(context.Background(), raw)
	require.ErrorIs(t, err, ErrUserInactive)
}

func newDelegateTestService(
	t *testing.T,
	runs AgentRunReader,
	users DelegationUserReader,
	clock Clock,
) *DelegateService {
	t.Helper()
	service, err := NewDelegateService(DelegateConfig{
		ActiveKeyID: "test-key",
		SigningKeys: map[string][]byte{
			"test-key": []byte("0123456789abcdef0123456789abcdef"),
		},
	}, runs, users, clock)
	require.NoError(t, err)
	return service
}

func delegatedTestRun(userID uuid.UUID, now time.Time) pactagent.Run {
	leaseExpiresAt := now.Add(time.Minute)
	return pactagent.Run{
		ID:                  uuid.New(),
		Provider:            "lark",
		TenantID:            "tenant",
		ConversationID:      "conversation",
		TriggerMessageID:    "message",
		ProviderEventID:     "event",
		TriggerOccurredAt:   now,
		InitiatingUserID:    userID,
		InitiatingSubjectID: "open-id",
		Status:              pactagent.RunRunning,
		CommandKind:         pactagent.CommandDirect,
		Model:               "deepseek-v4-pro",
		PromptVersion:       "v1",
		AttemptCount:        1,
		LeaseOwner:          "worker",
		LeaseExpiresAt:      &leaseExpiresAt,
		AvailableAt:         now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

type delegateRunReader struct {
	run pactagent.Run
}

func (r *delegateRunReader) GetRunForDelegate(
	_ context.Context,
	runID, userID uuid.UUID,
) (pactagent.Run, error) {
	if r.run.ID != runID || r.run.InitiatingUserID != userID {
		return pactagent.Run{}, pactagent.ErrAgentRunNotFound
	}
	return r.run, nil
}

type delegateUserReader struct {
	user domain.User
}

func (r *delegateUserReader) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	if r.user.ID != id {
		return domain.User{}, domain.ErrNotFound
	}
	return r.user, nil
}

type delegateClock struct {
	now time.Time
}

func (c *delegateClock) Now() time.Time { return c.now }
