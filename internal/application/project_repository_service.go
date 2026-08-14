package application

import (
	"context"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

type ProjectRepositoryService struct {
	Repositories *store.ProjectRepositoryStore
	Providers    *RepositoryProviderRegistry
	Access       *ProjectAccessService
	Now          func() time.Time
}

func (s *ProjectRepositoryService) List(
	ctx context.Context,
	projectNumber int64,
	subject domain.ProjectAccessSubject,
) ([]domain.ProjectRepository, error) {
	project, err := s.Access.RequireProjectByNumber(
		ctx, projectNumber, subject, ProjectPermissionRead,
	)
	if err != nil {
		return nil, err
	}
	return s.Repositories.ListActive(ctx, project.Project.ID)
}

func (s *ProjectRepositoryService) Bind(
	ctx context.Context,
	projectNumber int64,
	expectedProjectVersion int64,
	repositoryURL string,
	provider *domain.RepositoryProvider,
	subject domain.ProjectAccessSubject,
	operation domain.OperationActor,
) (store.ProjectRepositoryMutation, error) {
	project, err := s.Access.RequireProjectByNumber(
		ctx, projectNumber, subject, ProjectPermissionAdmin,
	)
	if err != nil {
		return store.ProjectRepositoryMutation{}, err
	}
	if project.Project.ArchivedAt != nil {
		return store.ProjectRepositoryMutation{}, fmt.Errorf(
			"%w: archived Projects are read-only", domain.ErrConflict,
		)
	}
	reference, err := s.Providers.ParseProjectRepositoryURL(repositoryURL, provider)
	if err != nil {
		return store.ProjectRepositoryMutation{}, err
	}
	return s.Repositories.Bind(
		ctx, project.Project.ID, expectedProjectVersion, reference, operation, s.now(),
	)
}

func (s *ProjectRepositoryService) Unbind(
	ctx context.Context,
	projectNumber int64,
	expectedProjectVersion int64,
	repositoryID uuid.UUID,
	subject domain.ProjectAccessSubject,
	operation domain.OperationActor,
) (store.ProjectRepositoryMutation, error) {
	project, err := s.Access.RequireProjectByNumber(
		ctx, projectNumber, subject, ProjectPermissionAdmin,
	)
	if err != nil {
		return store.ProjectRepositoryMutation{}, err
	}
	if project.Project.ArchivedAt != nil {
		return store.ProjectRepositoryMutation{}, fmt.Errorf(
			"%w: archived Projects are read-only", domain.ErrConflict,
		)
	}
	return s.Repositories.Unbind(
		ctx, project.Project.ID, expectedProjectVersion, repositoryID, operation, s.now(),
	)
}

func (s *ProjectRepositoryService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
