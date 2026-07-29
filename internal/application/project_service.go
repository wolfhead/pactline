package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

type ProjectService struct {
	Projects   *store.ProjectStore
	Milestones *store.MilestoneStore
	Acceptance *store.AcceptanceStore
	Tasks      *store.TaskStore
}

type ProjectDetail struct {
	Project           store.ProjectWithRelations
	Milestones        []domain.Milestone
	MilestoneCriteria map[uuid.UUID][]store.CriterionWithCurrentCheck
	Tasks             []store.TaskWithRelations
	Activity          []domain.ProjectActivity
}

func (s *ProjectService) GetDetail(ctx context.Context, number int64) (ProjectDetail, error) {
	project, err := s.Projects.GetByNumber(ctx, number)
	if err != nil {
		return ProjectDetail{}, err
	}
	milestones, err := s.Milestones.List(ctx, project.Project.ID)
	if err != nil {
		return ProjectDetail{}, err
	}
	milestoneCriteria := make(map[uuid.UUID][]store.CriterionWithCurrentCheck, len(milestones))
	for _, milestone := range milestones {
		criteria, err := s.Acceptance.ListForMilestone(ctx, milestone.ID)
		if err != nil {
			return ProjectDetail{}, err
		}
		milestoneCriteria[milestone.ID] = criteria
	}
	tasks := make([]store.TaskWithRelations, 0, 200)
	cursor := ""
	for {
		taskResult, err := s.Tasks.List(ctx, store.TaskListFilter{
			ProjectID: &project.Project.ID,
			Archived:  "all",
			Sort:      "number",
			Order:     "asc",
			Cursor:    cursor,
			Limit:     200,
		})
		if err != nil {
			return ProjectDetail{}, err
		}
		tasks = append(tasks, taskResult.Items...)
		if !taskResult.HasMore {
			break
		}
		cursor = taskResult.NextCursor
	}
	activity, err := s.Projects.ListActivity(ctx, project.Project.ID)
	if err != nil {
		return ProjectDetail{}, err
	}
	return ProjectDetail{
		Project:           project,
		Milestones:        milestones,
		MilestoneCriteria: milestoneCriteria,
		Tasks:             tasks,
		Activity:          activity,
	}, nil
}

func (s *ProjectService) ResolveTaskAssociation(
	ctx context.Context,
	projectNumber *int64,
	milestoneID *uuid.UUID,
) (uuid.UUID, *uuid.UUID, error) {
	if projectNumber == nil {
		return uuid.Nil, nil, fmt.Errorf("%w: task project is required", domain.ErrInvalidInput)
	}
	project, err := s.Projects.GetByNumber(ctx, *projectNumber)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if project.Project.ArchivedAt != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: archived Projects cannot accept task associations", domain.ErrConflict)
	}
	projectID := project.Project.ID
	if milestoneID != nil {
		belongs, err := s.Milestones.BelongsToProject(ctx, *milestoneID, projectID)
		if err != nil {
			return uuid.Nil, nil, fmt.Errorf("validate milestone project: %w", err)
		}
		if !belongs {
			return uuid.Nil, nil, fmt.Errorf("%w: milestone does not belong to project", domain.ErrInvalidInput)
		}
	}
	return projectID, milestoneID, nil
}

func (s *ProjectService) ApplyProjectLifecycle(
	ctx context.Context,
	number int64,
	action store.ProjectLifecycleAction,
	actor domain.Actor,
	reason string,
) (store.ProjectWithRelations, error) {
	if !actor.IsHuman() || actor.UserID == nil {
		return store.ProjectWithRelations{}, fmt.Errorf(
			"%w: project lifecycle actions require a human user", domain.ErrForbidden,
		)
	}
	return s.ApplyProjectLifecycleWithOperation(
		ctx, number, action, domain.SessionOperation(*actor.UserID, "internal"), reason,
	)
}

func (s *ProjectService) ApplyProjectLifecycleWithOperation(
	ctx context.Context,
	number int64,
	action store.ProjectLifecycleAction,
	actor domain.OperationActor,
	reason string,
) (store.ProjectWithRelations, error) {
	slog.Info("project lifecycle requested", "project_number", number, "action", action)
	userID := actor.UserID
	project, err := s.Projects.ApplyLifecycleWithOperation(
		ctx, number, action,
		domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
		reason, actor,
	)
	if err != nil {
		slog.Warn("project lifecycle rejected", "project_number", number, "action", action, "error", err)
		return store.ProjectWithRelations{}, err
	}
	slog.Info("Project archive action applied", "project_number", number, "action", action, "archived", project.Project.ArchivedAt != nil)
	return project, nil
}

func (s *ProjectService) ApplyMilestoneLifecycle(
	ctx context.Context,
	projectNumber int64,
	milestoneID uuid.UUID,
	action store.MilestoneLifecycleAction,
	actor domain.Actor,
	reason string,
) (domain.Milestone, error) {
	if !actor.IsHuman() || actor.UserID == nil {
		return domain.Milestone{}, fmt.Errorf(
			"%w: milestone lifecycle actions require a human user", domain.ErrForbidden,
		)
	}
	return s.ApplyMilestoneLifecycleWithOperation(
		ctx, projectNumber, milestoneID, action,
		domain.SessionOperation(*actor.UserID, "internal"), reason,
	)
}

func (s *ProjectService) ApplyMilestoneLifecycleWithOperation(
	ctx context.Context,
	projectNumber int64,
	milestoneID uuid.UUID,
	action store.MilestoneLifecycleAction,
	actor domain.OperationActor,
	reason string,
) (domain.Milestone, error) {
	projectID, err := s.Projects.ResolveProjectID(ctx, projectNumber)
	if err != nil {
		return domain.Milestone{}, err
	}
	slog.Info("milestone lifecycle requested", "project_number", projectNumber, "milestone_id", milestoneID, "action", action)
	milestone, err := s.Milestones.ApplyLifecycleWithOperation(
		ctx, projectID, milestoneID, action, actor, reason)
	if err != nil {
		slog.Warn("milestone lifecycle rejected", "project_number", projectNumber, "milestone_id", milestoneID, "action", action, "error", err)
		return domain.Milestone{}, err
	}
	return milestone, nil
}
