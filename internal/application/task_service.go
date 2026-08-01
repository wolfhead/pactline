package application

import (
	"context"
	"fmt"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

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
	parentNumber *int64,
	dependencyNumbers []int64,
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
	if parentNumber != nil {
		parent, err := s.Tasks.GetByNumber(ctx, *parentNumber)
		if err != nil {
			return store.TaskWithRelations{}, err
		}
		task.ParentTaskID = &parent.Task.ID
	}
	dependencyIDs, err := s.resolveTaskNumbers(ctx, dependencyNumbers)
	if err != nil {
		return store.TaskWithRelations{}, err
	}
	return s.Tasks.CreateWithOperation(ctx, task, labelIDs, dependencyIDs, actor)
}

type TaskAssociationPatch struct {
	ProjectNumberSet bool
	ProjectNumber    *int64
	MilestoneSet     bool
	MilestoneID      *uuid.UUID
}

type TaskRelationshipPatch struct {
	ParentSet         bool
	ParentNumber      *int64
	DependenciesSet   bool
	DependencyNumbers []int64
}

func (s *TaskService) Update(
	ctx context.Context,
	number, expectedVersion int64,
	patch domain.TaskPatch,
	association TaskAssociationPatch,
	relationships TaskRelationshipPatch,
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
		patch.ProjectID = &projectID
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
	if relationships.ParentSet {
		patch.ParentSet = true
		if relationships.ParentNumber != nil {
			parent, err := s.Tasks.GetByNumber(ctx, *relationships.ParentNumber)
			if err != nil {
				return store.TaskWithRelations{}, err
			}
			patch.ParentTaskID = &parent.Task.ID
		}
	}
	if relationships.DependenciesSet {
		dependencyIDs, err := s.resolveTaskNumbers(ctx, relationships.DependencyNumbers)
		if err != nil {
			return store.TaskWithRelations{}, err
		}
		patch.DependenciesSet = true
		patch.DependencyIDs = dependencyIDs
	}
	return s.Tasks.UpdateVersionedWithOperation(
		ctx, number, expectedVersion, patch, actor,
	)
}

func (s *TaskService) resolveTaskNumbers(
	ctx context.Context,
	numbers []int64,
) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(numbers))
	seen := make(map[int64]struct{}, len(numbers))
	for _, number := range numbers {
		if _, duplicate := seen[number]; duplicate {
			return nil, fmt.Errorf("%w: duplicate task number %d", domain.ErrInvalidInput, number)
		}
		seen[number] = struct{}{}
		task, err := s.Tasks.GetByNumber(ctx, number)
		if err != nil {
			return nil, err
		}
		ids = append(ids, task.Task.ID)
	}
	return ids, nil
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
	replyToCommentID *uuid.UUID,
	mentionedUserIDs []uuid.UUID,
	actor domain.OperationActor,
) (store.CommentCreation, error) {
	task, err := s.Tasks.GetByNumber(ctx, number)
	if err != nil {
		return store.CommentCreation{}, err
	}
	return s.Comments.CreateVersionedThreadedWithOperation(
		ctx, task.Task.ID, expectedTaskVersion, authorID, body,
		replyToCommentID, mentionedUserIDs, actor,
	)
}

func (s *TaskService) UpdateComment(
	ctx context.Context,
	number int64,
	id uuid.UUID,
	expectedVersion int64,
	body string,
	mentionedUserIDs []uuid.UUID,
	actor domain.OperationActor,
) (domain.Comment, error) {
	task, err := s.Tasks.GetByNumber(ctx, number)
	if err != nil {
		return domain.Comment{}, err
	}
	return s.Comments.UpdateVersionedMentionedWithOperation(
		ctx, task.Task.ID, id, expectedVersion, body, mentionedUserIDs, actor,
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
