package domain_test

import (
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskClaimEligibilityRequiresAssignedAgentReadyTodo(t *testing.T) {
	userID := uuid.New()
	tokenID := uuid.New()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	base := domain.Task{
		ID: uuid.New(), Number: 42, Status: domain.TaskStatusTodo,
		ExecutionMode: domain.TaskExecutionModeAgentAllowed, AssigneeID: &userID,
	}
	actor := domain.OperationActor{
		UserID: userID, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Codex", RequestID: "request",
	}

	claim, err := domain.NewTaskClaim(base, actor, "codex", "thread-1", now)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStatusActive, claim.Status)
	require.Equal(t, now.Add(domain.TaskClaimActiveLifetime), claim.ExpiresAt)

	notReady := base
	notReady.ExecutionMode = domain.TaskExecutionModeHumanOnly
	_, err = domain.NewTaskClaim(notReady, actor, "codex", "thread-1", now)
	require.ErrorIs(t, err, domain.ErrConflict)

	inProgress := base
	inProgress.Status = domain.TaskStatusInProgress
	_, err = domain.NewTaskClaim(inProgress, actor, "codex", "thread-1", now)
	require.ErrorIs(t, err, domain.ErrConflict)

	otherUser := uuid.New()
	unassigned := base
	unassigned.AssigneeID = nil
	_, err = domain.NewTaskClaim(unassigned, actor, "codex", "thread-1", now)
	require.ErrorIs(t, err, domain.ErrForbidden)
	assignedElsewhere := base
	assignedElsewhere.AssigneeID = &otherUser
	_, err = domain.NewTaskClaim(assignedElsewhere, actor, "codex", "thread-1", now)
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestTaskClaimLifecycleUsesBoundedActiveAndWaitingPeriods(t *testing.T) {
	userID := uuid.New()
	tokenID := uuid.New()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	task := domain.Task{
		ID: uuid.New(), Number: 42, Status: domain.TaskStatusTodo,
		ExecutionMode: domain.TaskExecutionModeAgentAllowed, AssigneeID: &userID,
	}
	claim, err := domain.NewTaskClaim(task, domain.OperationActor{
		UserID: userID, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Codex", RequestID: "request",
	}, "codex", "thread-1", now)
	require.NoError(t, err)

	require.True(t, claim.OwnedBy(userID, "codex", "thread-1"))
	require.False(t, claim.OwnedBy(userID, "codex", "thread-2"))

	waitedAt := now.Add(time.Hour)
	require.NoError(t, claim.WaitForHuman(waitedAt))
	require.Equal(t, domain.TaskClaimStatusWaitingHuman, claim.Status)
	require.Equal(t, waitedAt.Add(domain.TaskClaimWaitingLifetime), claim.ExpiresAt)
	require.Error(t, claim.Extend(waitedAt))

	resumedAt := waitedAt.Add(2 * time.Hour)
	require.NoError(t, claim.Resume(resumedAt))
	require.Equal(t, domain.TaskClaimStatusActive, claim.Status)
	require.Equal(t, resumedAt.Add(domain.TaskClaimActiveLifetime), claim.ExpiresAt)

	extendedAt := resumedAt.Add(time.Hour)
	require.NoError(t, claim.Extend(extendedAt))
	require.Equal(t, extendedAt.Add(domain.TaskClaimActiveLifetime), claim.ExpiresAt)

	submittedAt := extendedAt.Add(time.Hour)
	require.NoError(t, claim.Submit(submittedAt))
	require.Equal(t, domain.TaskClaimStatusSubmitted, claim.Status)
	require.Equal(t, &submittedAt, claim.CompletedAt)
	require.ErrorIs(t, claim.Release("late", submittedAt), domain.ErrConflict)
}

func TestTaskClaimExpiryAndRelease(t *testing.T) {
	claim := newDomainClaim(t)
	require.ErrorIs(t, claim.Expire(claim.ExpiresAt.Add(-time.Second)), domain.ErrConflict)
	require.NoError(t, claim.Expire(claim.ExpiresAt))
	require.Equal(t, domain.TaskClaimStatusExpired, claim.Status)

	released := newDomainClaim(t)
	now := released.CreatedAt.Add(time.Hour)
	require.NoError(t, released.Release("environment unavailable", now))
	require.Equal(t, domain.TaskClaimStatusReleased, released.Status)
	require.Equal(t, "environment unavailable", released.TerminalReason)
}

func TestTaskClaimMessageSeparatesAgentInteractionRoles(t *testing.T) {
	userID := uuid.New()
	tokenID := uuid.New()
	now := time.Now().UTC()
	base := domain.TaskClaimMessage{
		ID: uuid.New(), ClaimID: uuid.New(), TaskID: uuid.New(),
		Kind: domain.TaskClaimMessageQuestion, Body: "Which API behavior is intended?",
		Author:    domain.Actor{Type: domain.ActorTypeAgent, Ref: "Codex"},
		RequestID: "request", APITokenID: &tokenID, TokenName: "Codex",
		CreatedAt: now,
	}
	require.NoError(t, base.Validate())

	answer := base
	answer.ID = uuid.New()
	answer.Kind = domain.TaskClaimMessageAnswer
	answer.Author = domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}
	answer.APITokenID = nil
	answer.TokenName = ""
	require.NoError(t, answer.Validate())

	invalidAgentAnswer := base
	invalidAgentAnswer.Kind = domain.TaskClaimMessageAnswer
	require.ErrorIs(t, invalidAgentAnswer.Validate(), domain.ErrForbidden)

	invalidHumanProgress := answer
	invalidHumanProgress.Kind = domain.TaskClaimMessageProgress
	require.ErrorIs(t, invalidHumanProgress.Validate(), domain.ErrForbidden)
}

func newDomainClaim(t *testing.T) domain.TaskClaim {
	t.Helper()
	userID := uuid.New()
	tokenID := uuid.New()
	claim, err := domain.NewTaskClaim(domain.Task{
		ID: uuid.New(), Number: 42, Status: domain.TaskStatusTodo,
		ExecutionMode: domain.TaskExecutionModeAgentAllowed, AssigneeID: &userID,
	}, domain.OperationActor{
		UserID: userID, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Codex", RequestID: "request",
	}, "codex", uuid.NewString(), time.Now().UTC())
	require.NoError(t, err)
	return claim
}
