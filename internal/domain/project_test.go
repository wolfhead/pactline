package domain_test

import (
	"errors"
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
)

func TestProjectValidationRequiresDurableWorkspaceIdentity(t *testing.T) {
	userID := uuid.New()
	tests := []struct {
		name    string
		project domain.Project
	}{
		{
			name:    "missing owner",
			project: domain.Project{Name: "Task Manager", CreatorID: userID},
		},
		{
			name:    "missing creator",
			project: domain.Project{Name: "Task Manager", OwnerID: userID},
		},
		{
			name:    "blank name",
			project: domain.Project{Name: " ", OwnerID: userID, CreatorID: userID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.project.Validate(); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("Validate() error = %v, want ErrInvalidInput", err)
			}
		})
	}

	valid := domain.Project{Name: "Task Manager", OwnerID: userID, CreatorID: userID}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProjectArchiveRequiresConcludedWork(t *testing.T) {
	tests := []struct {
		name      string
		readiness domain.ProjectArchiveReadiness
	}{
		{
			name:      "planned or active milestone",
			readiness: domain.ProjectArchiveReadiness{OpenMilestones: 1},
		},
		{
			name:      "unfinished task",
			readiness: domain.ProjectArchiveReadiness{UnfinishedTasks: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := domain.Project{}
			if err := project.Archive(tt.readiness); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("Archive() error = %v, want ErrConflict", err)
			}
			if project.ArchivedAt != nil {
				t.Fatal("Archive() changed ArchivedAt after a rejected transition")
			}
		})
	}

	project := domain.Project{}
	if err := project.Archive(domain.ProjectArchiveReadiness{}); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if project.ArchivedAt == nil {
		t.Fatal("Archive() did not set ArchivedAt")
	}
	project.Restore()
	if project.ArchivedAt != nil {
		t.Fatal("Restore() did not clear ArchivedAt")
	}
}

func TestMilestoneValidationRequiresDeliveryIdentity(t *testing.T) {
	userID := uuid.New()
	projectID := uuid.New()
	tests := []struct {
		name      string
		milestone domain.Milestone
	}{
		{
			name: "missing project",
			milestone: domain.Milestone{
				Name: "Project center", Outcome: "Project-first workflow is usable",
				OwnerID: userID, Status: domain.MilestoneStatusPlanned,
			},
		},
		{
			name: "missing owner",
			milestone: domain.Milestone{
				ProjectID: projectID, Name: "Project center",
				Outcome: "Project-first workflow is usable", Status: domain.MilestoneStatusPlanned,
			},
		},
		{
			name: "invalid status",
			milestone: domain.Milestone{
				ProjectID: projectID, Name: "Project center",
				Outcome: "Project-first workflow is usable", OwnerID: userID, Status: "open",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.milestone.Validate(); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("Validate() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestMilestoneActivationRequiresAcceptance(t *testing.T) {
	milestone := domain.Milestone{Status: domain.MilestoneStatusPlanned}
	if err := milestone.Activate(domain.MilestoneReadiness{}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Activate() error = %v, want ErrConflict", err)
	}
	if err := milestone.Activate(domain.MilestoneReadiness{ActiveCriteria: 1}); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if milestone.Status != domain.MilestoneStatusActive {
		t.Fatalf("status = %q, want active", milestone.Status)
	}
}

func TestMilestoneCompletionRequiresAcceptanceAndResolvedTasks(t *testing.T) {
	milestone := domain.Milestone{Status: domain.MilestoneStatusActive}
	if err := milestone.Complete(domain.MilestoneReadiness{}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() error = %v, want ErrConflict", err)
	}
	if err := milestone.Complete(domain.MilestoneReadiness{ActiveCriteria: 1, UnfinishedTasks: 1}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Complete() with unfinished tasks error = %v, want ErrConflict", err)
	}
	if err := milestone.Complete(domain.MilestoneReadiness{ActiveCriteria: 1}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if milestone.Status != domain.MilestoneStatusCompleted || milestone.CompletedAt == nil {
		t.Fatalf("milestone = %#v, want completed milestone", milestone)
	}
}

func TestMilestoneCancellationSupportsPlannedAndActive(t *testing.T) {
	for _, status := range []domain.MilestoneStatus{
		domain.MilestoneStatusPlanned,
		domain.MilestoneStatusActive,
	} {
		t.Run(string(status), func(t *testing.T) {
			milestone := domain.Milestone{Status: status}
			if err := milestone.Cancel(domain.MilestoneReadiness{UnfinishedTasks: 1}); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("Cancel() error = %v, want ErrConflict", err)
			}
			if err := milestone.Cancel(domain.MilestoneReadiness{}); err != nil {
				t.Fatalf("Cancel() error = %v", err)
			}
			if milestone.Status != domain.MilestoneStatusCancelled || milestone.CancelledAt == nil {
				t.Fatalf("milestone = %#v, want cancelled milestone", milestone)
			}
		})
	}
}

func TestMilestoneReopenRequiresHumanReason(t *testing.T) {
	milestone := domain.Milestone{Status: domain.MilestoneStatusCompleted}
	userID := uuid.New()
	if err := milestone.Reopen(domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex"}, "New scope"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("agent Reopen() error = %v, want ErrForbidden", err)
	}
	if err := milestone.Reopen(domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, " "); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("blank reason Reopen() error = %v, want ErrInvalidInput", err)
	}
	if err := milestone.Reopen(domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, "Scope changed"); err != nil {
		t.Fatalf("Reopen() error = %v", err)
	}
	if milestone.Status != domain.MilestoneStatusActive {
		t.Fatalf("status = %q, want active", milestone.Status)
	}
}
