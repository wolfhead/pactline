package domain_test

import (
	"errors"
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestTaskLifecycleValidCombinations(t *testing.T) {
	t.Parallel()
	valid := []domain.TaskLifecycle{
		{Phase: domain.TaskPhaseBacklog},
		{Phase: domain.TaskPhaseReady},
		{Phase: domain.TaskPhaseInProgress, Activity: domain.TaskActivityAvailable},
		{Phase: domain.TaskPhaseInProgress, Activity: domain.TaskActivityWorking},
		{Phase: domain.TaskPhaseInProgress, Activity: domain.TaskActivityNeedsResolution},
		{Phase: domain.TaskPhaseInReview, Activity: domain.TaskActivityAvailable, ReviewCycle: 1},
		{Phase: domain.TaskPhaseInReview, Activity: domain.TaskActivityWorking, ReviewCycle: 1},
		{Phase: domain.TaskPhaseInReview, Activity: domain.TaskActivityNeedsResolution, ReviewCycle: 1},
		{Phase: domain.TaskPhaseDone, ReviewCycle: 1},
		{Phase: domain.TaskPhaseCancelled},
		{Phase: domain.TaskPhaseCancelled, ReviewCycle: 3},
	}
	for _, state := range valid {
		state := state
		t.Run(string(state.Phase)+"_"+string(state.Activity), func(t *testing.T) {
			t.Parallel()
			require.NoError(t, state.Validate())
		})
	}

	invalid := []domain.TaskLifecycle{
		{},
		{Phase: domain.TaskPhase("unknown")},
		{Phase: domain.TaskPhaseBacklog, Activity: domain.TaskActivityAvailable},
		{Phase: domain.TaskPhaseReady, Activity: domain.TaskActivityWorking},
		{Phase: domain.TaskPhaseInProgress},
		{Phase: domain.TaskPhaseInReview, Activity: domain.TaskActivityState("unknown")},
		{Phase: domain.TaskPhaseDone, Activity: domain.TaskActivityAvailable},
		{Phase: domain.TaskPhaseCancelled, ReviewCycle: -1},
	}
	for _, state := range invalid {
		require.ErrorIs(t, state.Validate(), domain.ErrInvalidInput, "%+v", state)
	}
}

func TestTaskLifecycleHappyPathAndReviewCycle(t *testing.T) {
	t.Parallel()
	state := domain.NewTaskLifecycle()
	require.Equal(t, domain.TaskPhaseBacklog, state.Phase)
	require.NoError(t, state.MarkReady(false, false))
	require.True(t, state.Claimable())

	stage, err := state.Claim(false)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStageExecution, stage)
	require.Equal(t, domain.TaskLifecycle{
		Phase: domain.TaskPhaseInProgress, Activity: domain.TaskActivityWorking,
	}, state)

	require.NoError(t, state.CompleteExecution())
	require.Equal(t, int64(1), state.ReviewCycle)
	require.Equal(t, domain.TaskActivityAvailable, state.Activity)

	stage, err = state.Claim(false)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStageReview, stage)
	require.NoError(t, state.RequestChanges())
	require.Equal(t, int64(1), state.ReviewCycle)

	stage, err = state.Claim(false)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStageExecution, stage)
	require.NoError(t, state.CompleteExecution())
	require.Equal(t, int64(2), state.ReviewCycle)

	stage, err = state.Claim(false)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStageReview, stage)
	require.NoError(t, state.Accept(domain.TaskCompletionReadiness{}))
	require.Equal(t, domain.TaskPhaseDone, state.Phase)
	require.Empty(t, state.Activity)
}

func TestTaskLifecycleResolutionEndsOwnershipAndPreservesPhase(t *testing.T) {
	t.Parallel()
	for _, phase := range []domain.TaskPhase{
		domain.TaskPhaseInProgress,
		domain.TaskPhaseInReview,
	} {
		stage := domain.TaskClaimStageExecution
		cycle := int64(0)
		if phase == domain.TaskPhaseInReview {
			stage = domain.TaskClaimStageReview
			cycle = 1
		}
		state := domain.TaskLifecycle{
			Phase: phase, Activity: domain.TaskActivityWorking, ReviewCycle: cycle,
		}
		require.NoError(t, state.RequestResolution(stage))
		require.Equal(t, phase, state.Phase)
		require.Equal(t, domain.TaskActivityNeedsResolution, state.Activity)
		require.False(t, state.Claimable())

		require.NoError(t, state.ResolveIssue())
		require.Equal(t, phase, state.Phase)
		require.Equal(t, domain.TaskActivityAvailable, state.Activity)
		require.True(t, state.Claimable())
	}
}

func TestTaskLifecycleRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()
	state := domain.NewTaskLifecycle()
	require.ErrorIs(t, state.MarkReady(true, false), domain.ErrConflict)
	require.ErrorIs(t, state.MarkReady(false, true), domain.ErrConflict)
	require.ErrorIs(t, state.CompleteExecution(), domain.ErrConflict)
	require.ErrorIs(t, state.ResolveIssue(), domain.ErrConflict)

	require.NoError(t, state.MarkReady(false, false))
	_, err := state.Claim(false)
	require.NoError(t, err)
	require.ErrorIs(t, state.Release(domain.TaskClaimStageReview), domain.ErrConflict)
	require.ErrorIs(t, state.Accept(domain.TaskCompletionReadiness{}), domain.ErrConflict)

	require.NoError(t, state.Cancel())
	require.ErrorIs(t, state.Cancel(), domain.ErrConflict)
	_, err = state.Claim(false)
	require.True(t, errors.Is(err, domain.ErrConflict))
}

func TestTaskLifecycleAcceptanceKeepsStateWhenReadinessFails(t *testing.T) {
	t.Parallel()
	state := domain.TaskLifecycle{
		Phase: domain.TaskPhaseInReview, Activity: domain.TaskActivityWorking, ReviewCycle: 2,
	}
	err := state.Accept(domain.TaskCompletionReadiness{
		ActiveCriteria: 1, UnsatisfiedCriteria: 1,
	})
	require.ErrorIs(t, err, domain.ErrConflict)
	require.Equal(t, domain.TaskPhaseInReview, state.Phase)
	require.Equal(t, domain.TaskActivityWorking, state.Activity)
}
