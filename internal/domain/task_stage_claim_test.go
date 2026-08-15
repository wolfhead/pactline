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

	testCases := []struct {
		stage        domain.TaskClaimStage
		outcome      domain.TaskClaimOutcome
		wantStatus   domain.StageClaimStatus
		wantConflict bool
	}{
		{domain.TaskClaimStageExecution, domain.TaskClaimOutcomeExecutionCompleted, domain.StageClaimStatusCompleted, false},
		{domain.TaskClaimStageExecution, domain.TaskClaimOutcomeTaskAccepted, domain.StageClaimStatusActive, true},
		{domain.TaskClaimStageExecution, domain.TaskClaimOutcomeChangesRequested, domain.StageClaimStatusActive, true},
		{domain.TaskClaimStageReview, domain.TaskClaimOutcomeExecutionCompleted, domain.StageClaimStatusActive, true},
		{domain.TaskClaimStageReview, domain.TaskClaimOutcomeTaskAccepted, domain.StageClaimStatusCompleted, false},
		{domain.TaskClaimStageReview, domain.TaskClaimOutcomeChangesRequested, domain.StageClaimStatusCompleted, false},
	}

	sharedOutcomes := []struct {
		outcome domain.TaskClaimOutcome
		status  domain.StageClaimStatus
	}{
		{domain.TaskClaimOutcomeNeedsResolution, domain.StageClaimStatusReleased},
		{domain.TaskClaimOutcomeVoluntarilyReleased, domain.StageClaimStatusReleased},
		{domain.TaskClaimOutcomeDeadlineElapsed, domain.StageClaimStatusExpired},
		{domain.TaskClaimOutcomeTaskCancelled, domain.StageClaimStatusCancelled},
	}
	for _, stage := range []domain.TaskClaimStage{domain.TaskClaimStageExecution, domain.TaskClaimStageReview} {
		for _, shared := range sharedOutcomes {
			testCases = append(testCases, struct {
				stage        domain.TaskClaimStage
				outcome      domain.TaskClaimOutcome
				wantStatus   domain.StageClaimStatus
				wantConflict bool
			}{stage, shared.outcome, shared.status, false})
		}
	}

	for _, testCase := range testCases {
		t.Run(string(testCase.stage)+"/"+string(testCase.outcome), func(t *testing.T) {
			claim := newClaim(testCase.stage)
			err := claim.Complete(testCase.outcome, now)
			if testCase.wantConflict {
				require.ErrorIs(t, err, domain.ErrConflict)
				require.Equal(t, domain.StageClaimStatusActive, claim.Status)
				require.Empty(t, claim.Outcome)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.wantStatus, claim.Status)
			require.NoError(t, claim.Validate())
		})
	}

	invalidPersistedClaim := newClaim(domain.TaskClaimStageReview)
	require.NoError(t, invalidPersistedClaim.Complete(domain.TaskClaimOutcomeChangesRequested, now))
	invalidPersistedClaim.Stage = domain.TaskClaimStageExecution
	require.ErrorIs(t, invalidPersistedClaim.Validate(), domain.ErrInvalidInput)
}
