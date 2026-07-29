package domain_test

import (
	"errors"
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
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

func TestOnlyHumanUsersMayWaiveAcceptance(t *testing.T) {
	criterion := domain.AcceptanceCriterion{ID: uuid.New(), Revision: 1}
	check := domain.AcceptanceCheck{
		CriterionID:       criterion.ID,
		CriterionRevision: 1,
		Outcome:           domain.AcceptanceOutcomeWaived,
		Evidence:          "Risk accepted",
		Checker:           domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex"},
	}
	if err := check.ValidateAgainst(criterion); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("agent waiver error = %v, want ErrForbidden", err)
	}
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
