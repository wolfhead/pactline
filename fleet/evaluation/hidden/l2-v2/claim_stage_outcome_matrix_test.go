package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/wolfhead/pactline/internal/domain"
)

func TestFleetHiddenClaimStageOutcomeMatrix(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	userID := uuid.New()
	newClaim := func(t *testing.T, stage domain.TaskClaimStage) domain.TaskStageClaim {
		claim, err := domain.NewTaskStageClaim(
			uuid.New(), 900, stage, domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
			domain.SessionOperation(userID, "fleet-hidden"), "fleet-hidden", uuid.NewString(), now,
		)
		require.NoError(t, err)
		return claim
	}
	all := []domain.TaskClaimOutcome{
		domain.TaskClaimOutcomeExecutionCompleted, domain.TaskClaimOutcomeTaskAccepted,
		domain.TaskClaimOutcomeChangesRequested, domain.TaskClaimOutcomeNeedsResolution,
		domain.TaskClaimOutcomeVoluntarilyReleased, domain.TaskClaimOutcomeDeadlineElapsed,
		domain.TaskClaimOutcomeTaskCancelled,
	}
	for _, stage := range []domain.TaskClaimStage{domain.TaskClaimStageExecution, domain.TaskClaimStageReview} {
		for _, outcome := range all {
			stage, outcome := stage, outcome
			t.Run(string(stage)+"/"+string(outcome), func(t *testing.T) {
				claim := newClaim(t, stage)
				err := claim.Complete(outcome, now)
				allowed := outcome == domain.TaskClaimOutcomeNeedsResolution || outcome == domain.TaskClaimOutcomeVoluntarilyReleased ||
					outcome == domain.TaskClaimOutcomeDeadlineElapsed || outcome == domain.TaskClaimOutcomeTaskCancelled ||
					(stage == domain.TaskClaimStageExecution && outcome == domain.TaskClaimOutcomeExecutionCompleted) ||
					(stage == domain.TaskClaimStageReview && (outcome == domain.TaskClaimOutcomeTaskAccepted || outcome == domain.TaskClaimOutcomeChangesRequested))
				if allowed { require.NoError(t, err) } else { require.ErrorIs(t, err, domain.ErrConflict) }
			})
		}
	}
}
