package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

type ProjectPermission string

const (
	ProjectPermissionRead  ProjectPermission = "read"
	ProjectPermissionWrite ProjectPermission = "write"
	ProjectPermissionAdmin ProjectPermission = "admin"
)

type ProjectAccessService struct {
	Projects    *store.ProjectStore
	Tasks       *store.TaskStore
	Memberships *store.ProjectMembershipStore
}

func (s *ProjectAccessService) RequireProjectByNumber(
	ctx context.Context,
	number int64,
	subject domain.ProjectAccessSubject,
	permission ProjectPermission,
) (store.ProjectWithRelations, error) {
	project, err := s.Projects.GetByNumber(ctx, number)
	if err != nil {
		return store.ProjectWithRelations{}, err
	}
	if err := s.RequireProject(ctx, project, subject, permission); err != nil {
		return store.ProjectWithRelations{}, err
	}
	return project, nil
}

func (s *ProjectAccessService) RequireProjectByID(
	ctx context.Context,
	id uuid.UUID,
	subject domain.ProjectAccessSubject,
	permission ProjectPermission,
) error {
	project, err := s.Projects.GetByID(ctx, id)
	if err != nil {
		return err
	}
	return s.RequireProject(ctx, project, subject, permission)
}

func (s *ProjectAccessService) RequireProject(
	ctx context.Context,
	project store.ProjectWithRelations,
	subject domain.ProjectAccessSubject,
	permission ProjectPermission,
) error {
	if subject.UserID == uuid.Nil {
		return domain.ErrForbidden
	}
	if !subject.IsPlatformAdministrator() {
		membership, err := s.Memberships.Get(ctx, project.Project.ID, subject.UserID)
		if errors.Is(err, domain.ErrNotFound) {
			slog.Warn("project access hidden from non-member",
				"project_number", project.Project.Number,
				"subject_user_id", subject.UserID,
				"permission", permission,
			)
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("authorize project membership: %w", err)
		}
		if permission == ProjectPermissionAdmin && membership.Role != domain.ProjectRoleAdmin {
			slog.Warn("project administrator access rejected",
				"project_number", project.Project.Number,
				"subject_user_id", subject.UserID,
			)
			return domain.ErrForbidden
		}
	}
	if permission == ProjectPermissionWrite && project.Project.ArchivedAt != nil {
		return fmt.Errorf("%w: archived projects are read-only", domain.ErrConflict)
	}
	return nil
}

func (s *ProjectAccessService) RequireTaskByNumber(
	ctx context.Context,
	number int64,
	subject domain.ProjectAccessSubject,
	permission ProjectPermission,
) (store.TaskWithRelations, error) {
	task, err := s.Tasks.GetByNumber(ctx, number)
	if err != nil {
		return store.TaskWithRelations{}, err
	}
	project, err := s.Projects.GetByNumber(ctx, task.Project.Number)
	if err != nil {
		return store.TaskWithRelations{}, err
	}
	if err := s.RequireProject(ctx, project, subject, permission); err != nil {
		return store.TaskWithRelations{}, err
	}
	return task, nil
}
