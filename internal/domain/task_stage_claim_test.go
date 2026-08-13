package domain_test

import (
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskStageClaimSupportsHumanAndAgentProvenance(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 5, 0, 0, 0, time.UTC)
	userID := uuid.New()
	human, err := domain.NewTaskStageClaim(
		uuid.New(), 42, domain.TaskClaimStageExecution,
		domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
		domain.SessionOperation(userID, "human-claim"), "browser", "session-a", now,
	)
	require.NoError(t, err)
	require.Equal(t, domain.StageClaimStatusActive, human.Status)

	tokenID := uuid.New()
	agent, err := domain.NewTaskStageClaim(
		uuid.New(), 43, domain.TaskClaimStageReview,
		domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex/thread-a"},
		domain.OperationActor{
			UserID: userID, AuthMethod: domain.AuthenticationMethodAPIToken,
			TokenID: &tokenID, TokenName: "executor", RequestID: "agent-claim",
		},
		"codex", "thread-a", now,
	)
	require.NoError(t, err)
	require.Equal(t, domain.StageClaimStatusActive, agent.Status)
}

func TestTaskStageClaimOutcomeMustMatchStage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 13, 5, 0, 0, 0, time.UTC)
	userID := uuid.New()
	newClaim := func(stage domain.TaskClaimStage) domain.TaskStageClaim {
		claim, err := domain.NewTaskStageClaim(
			uuid.New(), 42, stage,
			domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
			domain.SessionOperation(userID, "claim"), "browser", uuid.NewString(), now,
		)
		require.NoError(t, err)
		return claim
	}

	execution := newClaim(domain.TaskClaimStageExecution)
	require.ErrorIs(t, execution.Complete(domain.TaskClaimOutcomeTaskAccepted, now), domain.ErrConflict)
	require.NoError(t, execution.Complete(domain.TaskClaimOutcomeWorkSubmitted, now))
	require.Equal(t, domain.StageClaimStatusCompleted, execution.Status)
	require.ErrorIs(t, execution.Complete(domain.TaskClaimOutcomeWorkSubmitted, now), domain.ErrConflict)

	review := newClaim(domain.TaskClaimStageReview)
	require.ErrorIs(t, review.Complete(domain.TaskClaimOutcomeWorkSubmitted, now), domain.ErrConflict)
	require.NoError(t, review.Complete(domain.TaskClaimOutcomeChangesRequested, now))
	require.Equal(t, domain.StageClaimStatusCompleted, review.Status)

	blocked := newClaim(domain.TaskClaimStageExecution)
	require.NoError(t, blocked.Complete(domain.TaskClaimOutcomeNeedsResolution, now))
	require.Equal(t, domain.StageClaimStatusReleased, blocked.Status)
}
