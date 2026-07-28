package api

import (
	"time"

	"bountyboard/internal/application"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type projectView struct {
	ID                uuid.UUID            `json:"id"`
	Number            int64                `json:"number"`
	Name              string               `json:"name"`
	Outcome           string               `json:"outcome"`
	Description       string               `json:"description"`
	Owner             domain.UserRef       `json:"owner"`
	Creator           domain.UserRef       `json:"creator"`
	Status            domain.ProjectStatus `json:"status"`
	TargetDate        *string              `json:"target_date"`
	CompletedAt       *time.Time           `json:"completed_at"`
	CancelledAt       *time.Time           `json:"cancelled_at"`
	ArchivedAt        *time.Time           `json:"archived_at"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	CompletedTasks    int                  `json:"completed_tasks"`
	EligibleTasks     int                  `json:"eligible_tasks"`
	ActiveCriteria    int                  `json:"active_criteria"`
	SatisfiedCriteria int                  `json:"satisfied_criteria"`
}

func newProjectView(value store.ProjectWithRelations) projectView {
	var targetDate *string
	if value.Project.TargetDate != nil {
		formatted := value.Project.TargetDate.Format("2006-01-02")
		targetDate = &formatted
	}
	return projectView{
		ID: value.Project.ID, Number: value.Project.Number, Name: value.Project.Name,
		Outcome: value.Project.Outcome, Description: value.Project.Description,
		Owner: value.Owner, Creator: value.Creator, Status: value.Project.Status,
		TargetDate: targetDate, CompletedAt: value.Project.CompletedAt,
		CancelledAt: value.Project.CancelledAt, ArchivedAt: value.Project.ArchivedAt,
		CreatedAt: value.Project.CreatedAt, UpdatedAt: value.Project.UpdatedAt,
		CompletedTasks: value.CompletedTasks, EligibleTasks: value.EligibleTasks,
		ActiveCriteria: value.ActiveCriteria, SatisfiedCriteria: value.SatisfiedCriteria,
	}
}

type milestoneView struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Outcome     string                 `json:"outcome"`
	Description string                 `json:"description"`
	Status      domain.MilestoneStatus `json:"status"`
	TargetDate  *string                `json:"target_date"`
	Position    int                    `json:"position"`
	CompletedAt *time.Time             `json:"completed_at"`
	CancelledAt *time.Time             `json:"cancelled_at"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Criteria    []criterionView        `json:"acceptance_criteria"`
}

func newMilestoneView(value domain.Milestone, criteria []store.CriterionWithCurrentCheck) milestoneView {
	var targetDate *string
	if value.TargetDate != nil {
		formatted := value.TargetDate.Format("2006-01-02")
		targetDate = &formatted
	}
	return milestoneView{
		ID: value.ID, Name: value.Name, Outcome: value.Outcome,
		Description: value.Description, Status: value.Status, TargetDate: targetDate,
		Position: value.Position, CompletedAt: value.CompletedAt,
		CancelledAt: value.CancelledAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		Criteria: newCriterionViews(criteria),
	}
}

type acceptanceCheckView struct {
	ID                uuid.UUID                `json:"id"`
	CriterionRevision int                      `json:"criterion_revision"`
	Outcome           domain.AcceptanceOutcome `json:"outcome"`
	Evidence          string                   `json:"evidence"`
	CheckerType       domain.ActorType         `json:"checker_type"`
	CheckedByUserID   *uuid.UUID               `json:"checked_by_user_id"`
	CheckerRef        string                   `json:"checker_ref,omitempty"`
	CheckedAt         time.Time                `json:"checked_at"`
}

type criterionView struct {
	ID                       uuid.UUID            `json:"id"`
	Criterion                string               `json:"criterion"`
	VerificationInstructions string               `json:"verification_instructions"`
	Revision                 int                  `json:"revision"`
	Position                 int                  `json:"position"`
	CurrentCheck             *acceptanceCheckView `json:"current_check"`
}

func newCriterionView(value store.CriterionWithCurrentCheck) criterionView {
	view := criterionView{
		ID: value.Criterion.ID, Criterion: value.Criterion.Criterion,
		VerificationInstructions: value.Criterion.VerificationInstructions,
		Revision:                 value.Criterion.Revision, Position: value.Criterion.Position,
	}
	if value.CurrentCheck != nil {
		view.CurrentCheck = &acceptanceCheckView{
			ID: value.CurrentCheck.ID, CriterionRevision: value.CurrentCheck.CriterionRevision,
			Outcome: value.CurrentCheck.Outcome, Evidence: value.CurrentCheck.Evidence,
			CheckerType:     value.CurrentCheck.Checker.Type,
			CheckedByUserID: value.CurrentCheck.Checker.UserID,
			CheckerRef:      value.CurrentCheck.Checker.Ref, CheckedAt: value.CurrentCheck.CheckedAt,
		}
	}
	return view
}

func newCriterionViews(values []store.CriterionWithCurrentCheck) []criterionView {
	out := make([]criterionView, len(values))
	for i, value := range values {
		out[i] = newCriterionView(value)
	}
	return out
}

type projectDetailView struct {
	Project            projectView           `json:"project"`
	AcceptanceCriteria []criterionView       `json:"acceptance_criteria"`
	Milestones         []milestoneView       `json:"milestones"`
	Tasks              []taskView            `json:"tasks"`
	Activity           []projectActivityView `json:"activity"`
}

type projectActivityView struct {
	ID          uuid.UUID  `json:"id"`
	MilestoneID *uuid.UUID `json:"milestone_id"`
	ActorID     uuid.UUID  `json:"actor_id"`
	Action      string     `json:"action"`
	Reason      *string    `json:"reason"`
	OldValue    *string    `json:"old_value"`
	NewValue    *string    `json:"new_value"`
	CreatedAt   time.Time  `json:"created_at"`
}

func newProjectDetailView(value application.ProjectDetail) projectDetailView {
	milestones := make([]milestoneView, len(value.Milestones))
	for i, milestone := range value.Milestones {
		milestones[i] = newMilestoneView(milestone, value.MilestoneCriteria[milestone.ID])
	}
	tasks := make([]taskView, len(value.Tasks))
	for i, task := range value.Tasks {
		tasks[i] = newTaskView(task)
	}
	activity := make([]projectActivityView, len(value.Activity))
	for i, item := range value.Activity {
		activity[i] = projectActivityView{
			ID: item.ID, MilestoneID: item.MilestoneID, ActorID: item.ActorID,
			Action: item.Action, Reason: item.Reason, OldValue: item.OldValue,
			NewValue: item.NewValue, CreatedAt: item.CreatedAt,
		}
	}
	return projectDetailView{
		Project: newProjectView(value.Project), AcceptanceCriteria: newCriterionViews(value.ProjectCriteria),
		Milestones: milestones, Tasks: tasks, Activity: activity,
	}
}
