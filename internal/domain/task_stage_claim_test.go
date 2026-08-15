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

	successCases := []struct {
		name    string
		stage   domain.TaskClaimStage
		outcome domain.TaskClaimOutcome
		allowed bool
	}{
		{name: "execution completes execution", stage: domain.TaskClaimStageExecution, outcome: domain.TaskClaimOutcomeExecutionCompleted, allowed: true},
		{name: "execution rejects acceptance", stage: domain.TaskClaimStageExecution, outcome: domain.TaskClaimOutcomeTaskAccepted},
		{name: "execution rejects changes requested", stage: domain.TaskClaimStageExecution, outcome: domain.TaskClaimOutcomeChangesRequested},
		{name: "review rejects execution completion", stage: domain.TaskClaimStageReview, outcome: domain.TaskClaimOutcomeExecutionCompleted},
		{name: "review accepts task", stage: domain.TaskClaimStageReview, outcome: domain.TaskClaimOutcomeTaskAccepted, allowed: true},
		{name: "review requests changes", stage: domain.TaskClaimStageReview, outcome: domain.TaskClaimOutcomeChangesRequested, allowed: true},
	}
	for _, tc := range successCases {
		t.Run(tc.name, func(t *testing.T) {
			claim := newClaim(tc.stage)
			err := claim.Complete(tc.outcome, now)
			if !tc.allowed {
				require.ErrorIs(t, err, domain.ErrConflict)
				require.Equal(t, domain.StageClaimStatusActive, claim.Status)
				return
			}
			require.NoError(t, err)
			require.Equal(t, domain.StageClaimStatusCompleted, claim.Status)
			require.ErrorIs(t, claim.Complete(tc.outcome, now), domain.ErrConflict)
		})
	}

	sharedOutcomes := []struct {
		name    string
		outcome domain.TaskClaimOutcome
		status  domain.StageClaimStatus
	}{
		{name: "needs resolution", outcome: domain.TaskClaimOutcomeNeedsResolution, status: domain.StageClaimStatusReleased},
		{name: "voluntarily released", outcome: domain.TaskClaimOutcomeVoluntarilyReleased, status: domain.StageClaimStatusReleased},
		{name: "deadline elapsed", outcome: domain.TaskClaimOutcomeDeadlineElapsed, status: domain.StageClaimStatusExpired},
		{name: "task cancelled", outcome: domain.TaskClaimOutcomeTaskCancelled, status: domain.StageClaimStatusCancelled},
	}
	for _, stage := range []domain.TaskClaimStage{domain.TaskClaimStageExecution, domain.TaskClaimStageReview} {
		for _, tc := range sharedOutcomes {
			t.Run(string(stage)+" "+tc.name, func(t *testing.T) {
				claim := newClaim(stage)
				require.NoError(t, claim.Complete(tc.outcome, now))
				require.Equal(t, tc.status, claim.Status)
			})
		}
	}
}
