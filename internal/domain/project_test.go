package domain_test

import (
	"errors"
	"testing"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
)

func TestNewProjectRequiresOwnerNameAndOutcome(t *testing.T) {
	userID := uuid.New()
	tests := []struct {
		name    string
		project domain.Project
	}{
		{"missing owner", domain.Project{Name: "Launch", Outcome: "Customers can use it", CreatorID: userID}},
		{"blank name", domain.Project{Name: " ", Outcome: "Customers can use it", OwnerID: userID, CreatorID: userID}},
		{"blank outcome", domain.Project{Name: "Launch", Outcome: " ", OwnerID: userID, CreatorID: userID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.project.Validate(); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("Validate() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestProjectLifecycleEnforcesReadiness(t *testing.T) {
	project := domain.Project{Status: domain.ProjectStatusPlanned}
	if err := project.Activate(domain.ProjectReadiness{}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Activate() error = %v, want ErrConflict", err)
	}
	if err := project.Activate(domain.ProjectReadiness{ActiveCriteria: 1}); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if project.Status != domain.ProjectStatusActive {
		t.Fatalf("status = %q, want active", project.Status)
	}
	if err := project.Complete(domain.ProjectReadiness{
		ActiveCriteria:      1,
		UnsatisfiedCriteria: 1,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() error = %v, want ErrConflict", err)
	}
	if err := project.Complete(domain.ProjectReadiness{ActiveCriteria: 1, OpenMilestones: 1}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() with open milestone error = %v, want ErrConflict", err)
	}
	if err := project.Complete(domain.ProjectReadiness{ActiveCriteria: 1, UnfinishedTasks: 1}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() with unfinished task error = %v, want ErrConflict", err)
	}
	if err := project.Complete(domain.ProjectReadiness{ActiveCriteria: 1}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestProjectReopenRequiresHumanReason(t *testing.T) {
	project := domain.Project{Status: domain.ProjectStatusCompleted}
	userID := uuid.New()
	if err := project.Reopen(domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex"}, "New scope"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("agent Reopen() error = %v, want ErrForbidden", err)
	}
	if err := project.Reopen(domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, " "); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("blank reason Reopen() error = %v, want ErrInvalidInput", err)
	}
	if err := project.Reopen(domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, "New scope"); err != nil {
		t.Fatalf("Reopen() error = %v", err)
	}
}

func TestMilestoneCompletionRequiresAcceptanceAndResolvedTasks(t *testing.T) {
	milestone := domain.Milestone{Status: domain.MilestoneStatusOpen}
	if err := milestone.Complete(domain.MilestoneReadiness{}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() error = %v, want ErrConflict", err)
	}
	if err := milestone.Complete(domain.MilestoneReadiness{ActiveCriteria: 1, UnfinishedTasks: 1}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() with unfinished tasks error = %v, want ErrConflict", err)
	}
	if err := milestone.Complete(domain.MilestoneReadiness{ActiveCriteria: 1}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}
