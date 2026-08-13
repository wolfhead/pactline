package domain_test

import (
	"errors"
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAcceptanceCriterionHasExactlyOneOwner(t *testing.T) {
	milestoneID := uuid.New()
	taskID := uuid.New()
	for _, criterion := range []domain.AcceptanceCriterion{
		{Criterion: "Observable", VerificationInstructions: "Run the check"},
		{MilestoneID: &milestoneID, TaskID: &taskID, Criterion: "Observable", VerificationInstructions: "Run the check"},
	} {
		if err := criterion.Validate(); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("Validate() error = %v, want ErrInvalidInput", err)
		}
	}

	for _, criterion := range []domain.AcceptanceCriterion{
		{MilestoneID: &milestoneID, Criterion: "Observable", VerificationInstructions: "Run the check", Revision: 1},
		{TaskID: &taskID, Criterion: "Observable", VerificationInstructions: "Run the check", Revision: 1},
	} {
		if err := criterion.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil", err)
		}
	}
}

func TestAcceptanceCriterionSemanticEditAdvancesRevision(t *testing.T) {
	taskID := uuid.New()
	criterion := domain.AcceptanceCriterion{
		TaskID:                   &taskID,
		Criterion:                "Latency is below 250ms",
		VerificationInstructions: "Run the benchmark",
		Revision:                 1,
	}
	criterion.Edit("Latency is below 200ms", "Run the benchmark")
	if criterion.Revision != 2 {
		t.Fatalf("revision = %d, want 2", criterion.Revision)
	}
	criterion.Move(2)
	if criterion.Revision != 2 {
		t.Fatalf("revision after reorder = %d, want 2", criterion.Revision)
	}
}

func TestAcceptanceCheckRequiresEvidenceAndCurrentRevision(t *testing.T) {
	userID := uuid.New()
	criterion := domain.AcceptanceCriterion{ID: uuid.New(), Revision: 2}
	check := domain.AcceptanceCheck{
		CriterionID:       criterion.ID,
		CriterionRevision: 1,
		Outcome:           domain.AcceptanceOutcomePassed,
		Evidence:          "benchmark output",
		Checker:           domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
	}
	if err := check.ValidateAgainst(criterion); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale ValidateAgainst() error = %v, want ErrConflict", err)
	}
	check.CriterionRevision = 2
	check.Evidence = " "
	if err := check.ValidateAgainst(criterion); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("blank evidence ValidateAgainst() error = %v, want ErrInvalidInput", err)
	}
}

func TestAcceptanceWaiverDoesNotDependOnActorType(t *testing.T) {
	criterion := domain.AcceptanceCriterion{ID: uuid.New(), Revision: 1}
	check := domain.AcceptanceCheck{
		CriterionID:       criterion.ID,
		CriterionRevision: 1,
		Outcome:           domain.AcceptanceOutcomeWaived,
		Evidence:          "Risk accepted",
		Checker:           domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex"},
	}
	require.NoError(t, check.ValidateAgainst(criterion))
}

func TestTaskCompletionRequiresConfiguredAcceptanceToBeSatisfied(t *testing.T) {
	task := domain.Task{}
	if err := task.ValidateCompletion(domain.TaskCompletionReadiness{}); err != nil {
		t.Fatalf("task without criteria should complete: %v", err)
	}
	if err := task.ValidateCompletion(domain.TaskCompletionReadiness{
		ActiveCriteria: 1, UnsatisfiedCriteria: 1,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unsatisfied completion error = %v, want ErrConflict", err)
	}
	if err := task.ValidateCompletion(domain.TaskCompletionReadiness{
		ActiveCriteria: 2, UnsatisfiedCriteria: 0,
	}); err != nil {
		t.Fatalf("satisfied task should complete: %v", err)
	}
}

func TestTaskAcceptancePurposeFollowsClaimStageNotActorType(t *testing.T) {
	criterion := domain.AcceptanceCriterion{ID: uuid.New(), Revision: 2}
	claimID := uuid.New()
	cycle := int64(3)
	userID := uuid.New()

	execution := domain.AcceptanceCheck{
		CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
		Outcome: domain.AcceptanceOutcomePassed, Evidence: "Focused test passed",
		Checker:     domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
		Purpose:     domain.AcceptanceCheckPurposeExecutionVerification,
		TaskClaimID: &claimID, TaskReviewCycle: &cycle,
	}
	require.NoError(t, execution.ValidateForTaskClaim(
		criterion, claimID, domain.TaskClaimStageExecution, cycle,
	))
	require.False(t, execution.SatisfiesTaskReview(cycle),
		"a human execution check is still self-verification")

	agentAcceptance := execution
	agentAcceptance.Checker = domain.Actor{Type: domain.ActorTypeAgent, Ref: "review-agent"}
	agentAcceptance.Purpose = domain.AcceptanceCheckPurposeAcceptance
	require.NoError(t, agentAcceptance.ValidateForTaskClaim(
		criterion, claimID, domain.TaskClaimStageReview, cycle,
	))
	require.True(t, agentAcceptance.SatisfiesTaskReview(cycle),
		"an Agent review check may satisfy acceptance")
	require.False(t, agentAcceptance.SatisfiesTaskReview(cycle+1))
}
