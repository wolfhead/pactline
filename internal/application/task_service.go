package application

import (
	"context"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

// TaskService coordinates task aggregate use cases. Persistence methods own
// their database transactions; this service resolves human-facing task and
// project identifiers and keeps that orchestration out of HTTP adapters.
type TaskService struct {
	Tasks    *store.TaskStore
	Comments *store.CommentStore
	Projects *ProjectService
}

func (s *TaskService) Create(
	ctx context.Context,
	task domain.Task,
	labelIDs []uuid.UUID,
	projectNumber *int64,
	milestoneID *uuid.UUID,
	actor domain.OperationActor,
) (store.TaskWithRelations, error) {
	projectID, resolvedMilestoneID, err := s.Projects.ResolveTaskAssociation(
		ctx, projectNumber, milestoneID,
	)
	if err != nil {
		return store.TaskWithRelations{}, err
	}
	task.ProjectID = projectID
	task.MilestoneID = resolvedMilestoneID
	return s.Tasks.CreateWithOperation(ctx, task, labelIDs, actor)
}

type TaskAssociationPatch struct {
	ProjectNumberSet bool
	ProjectNumber    *int64
	MilestoneSet     bool
	MilestoneID      *uuid.UUID
}

func (s *TaskService) Update(
	ctx context.Context,
	number, expectedVersion int64,
	patch domain.TaskPatch,
	association TaskAssociationPatch,
	actor domain.OperationActor,
) (store.TaskWithRelations, error) {
	if association.ProjectNumberSet {
		projectID, milestoneID, err := s.Projects.ResolveTaskAssociation(
			ctx, association.ProjectNumber, association.MilestoneID,
		)
		if err != nil {
			return store.TaskWithRelations{}, err
		}
		patch.ProjectSet = true
		patch.ProjectID = projectID
		if association.MilestoneSet {
			patch.MilestoneSet = true
			patch.MilestoneID = milestoneID
		}
	} else if association.MilestoneSet {
		patch.MilestoneSet = true
		if association.MilestoneID != nil {
			current, err := s.Tasks.GetByNumber(ctx, number)
			if err != nil {
				return store.TaskWithRelations{}, err
			}
			if current.Project == nil {
				return store.TaskWithRelations{}, domain.ErrInvalidInput
			}
			projectNumber := current.Project.Number
			_, milestoneID, err := s.Projects.ResolveTaskAssociation(
				ctx, &projectNumber, association.MilestoneID,
			)
			if err != nil {
				return store.TaskWithRelations{}, err
			}
			patch.MilestoneID = milestoneID
		}
	}
	return s.Tasks.UpdateVersionedWithOperation(
		ctx, number, expectedVersion, patch, actor,
	)
}

func (s *TaskService) SetArchived(
	ctx context.Context,
	number, expectedVersion int64,
	archived bool,
	actor domain.OperationActor,
) (store.TaskWithRelations, error) {
	return s.Tasks.SetArchivedVersionedWithOperation(
		ctx, number, expectedVersion, archived, actor,
	)
}

func (s *TaskService) CreateComment(
	ctx context.Context,
	number, expectedTaskVersion int64,
	authorID uuid.UUID,
	body string,
	actor domain.OperationActor,
) (store.CommentCreation, error) {
	task, err := s.Tasks.GetByNumber(ctx, number)
	if err != nil {
		return store.CommentCreation{}, err
	}
	return s.Comments.CreateVersionedWithOperation(
		ctx, task.Task.ID, expectedTaskVersion, authorID, body, actor,
	)
}

func (s *TaskService) UpdateComment(
	ctx context.Context,
	number int64,
	id uuid.UUID,
	expectedVersion int64,
	body string,
	actor domain.OperationActor,
) (domain.Comment, error) {
	task, err := s.Tasks.GetByNumber(ctx, number)
	if err != nil {
		return domain.Comment{}, err
	}
	return s.Comments.UpdateVersionedWithOperation(
		ctx, task.Task.ID, id, expectedVersion, body, actor,
	)
}

func (s *TaskService) DeleteComment(
	ctx context.Context,
	number int64,
	id uuid.UUID,
	expectedVersion int64,
	actor domain.OperationActor,
) error {
	task, err := s.Tasks.GetByNumber(ctx, number)
	if err != nil {
		return err
	}
	return s.Comments.DeleteVersionedWithOperation(
		ctx, task.Task.ID, id, expectedVersion, actor,
	)
}
