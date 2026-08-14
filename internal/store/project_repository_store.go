package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ProjectRepositoryMutation struct {
	Repository     domain.ProjectRepository
	ProjectVersion int64
}

type ProjectRepositoryStore struct{ db *DB }

func NewProjectRepositoryStore(db *DB) *ProjectRepositoryStore {
	return &ProjectRepositoryStore{db: db}
}

func (s *ProjectRepositoryStore) ListActive(
	ctx context.Context,
	projectID uuid.UUID,
) ([]domain.ProjectRepository, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT
			repository.id, repository.project_id, repository.provider, repository.origin,
			repository.path_with_namespace, repository.path_lookup_key, repository.canonical_web_url,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at
		FROM project_repositories repository
		WHERE repository.project_id=$1 AND repository.unbound_at IS NULL
		ORDER BY repository.provider, repository.origin, repository.path_lookup_key, repository.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list Project repositories: %w", err)
	}
	defer rows.Close()
	result := []domain.ProjectRepository{}
	for rows.Next() {
		item, err := scanProjectRepository(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *ProjectRepositoryStore) FindActiveByReference(
	ctx context.Context,
	projectID uuid.UUID,
	reference domain.RepositoryReference,
) (domain.ProjectRepository, error) {
	item, err := scanProjectRepository(s.db.Pool.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.provider, repository.origin,
			repository.path_with_namespace, repository.path_lookup_key, repository.canonical_web_url,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at
		FROM project_repositories repository
		WHERE repository.project_id=$1 AND repository.unbound_at IS NULL
		  AND repository.provider=$2 AND repository.origin=$3 AND repository.path_lookup_key=$4`,
		projectID, reference.Provider, reference.Origin, reference.PathLookupKey,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectRepository{}, domain.ErrNotFound
	}
	return item, err
}

func (s *ProjectRepositoryStore) Bind(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	reference domain.RepositoryReference,
	operation domain.OperationActor,
	now time.Time,
) (ProjectRepositoryMutation, error) {
	if err := operation.Validate(); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if err := reference.Validate(); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("begin Project repository bind: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockVersion(ctx, tx, "projects", projectID)
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if projectVersion != expectedProjectVersion {
		return ProjectRepositoryMutation{}, domain.VersionConflictError{CurrentVersion: projectVersion}
	}
	var archivedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT archived_at FROM projects WHERE id=$1`, projectID).Scan(&archivedAt); err != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("read Project archive state: %w", err)
	}
	if archivedAt != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("%w: archived Projects are read-only", domain.ErrConflict)
	}
	repository := domain.ProjectRepository{
		ID: uuid.New(), ProjectID: projectID,
		Provider: reference.Provider, Origin: reference.Origin,
		PathWithNamespace: reference.PathWithNamespace, PathLookupKey: reference.PathLookupKey,
		CanonicalWebURL: reference.WebURL,
		BoundBy:         operation.UserID, BoundAt: now,
	}
	if err := repository.Validate(); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_repositories (
			id, project_id, provider, origin, path_with_namespace, path_lookup_key,
			canonical_web_url, bound_by, bound_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		repository.ID, repository.ProjectID, repository.Provider, repository.Origin,
		repository.PathWithNamespace, repository.PathLookupKey, repository.CanonicalWebURL,
		repository.BoundBy, repository.BoundAt,
	)
	if err != nil {
		return ProjectRepositoryMutation{}, mapPgError(err)
	}
	projectVersion, err = incrementVersion(ctx, tx, "projects", projectID, projectVersion)
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if err := insertProjectRepositoryActivity(
		ctx, tx, projectID, operation, "project_repository_bound", repository.CanonicalWebURL,
		"", repository.CanonicalWebURL,
	); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	newValue, _ := json.Marshal(projectRepositoryAuditValue(repository))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "project_repository",
		EntityID: repository.ID, Action: "bound", NewValue: newValue,
	}); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("commit Project repository bind: %w", err)
	}
	return ProjectRepositoryMutation{
		Repository:     repository,
		ProjectVersion: projectVersion,
	}, nil
}

func (s *ProjectRepositoryStore) Unbind(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	repositoryID uuid.UUID,
	operation domain.OperationActor,
	now time.Time,
) (ProjectRepositoryMutation, error) {
	if err := operation.Validate(); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("begin Project repository unbind: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	projectVersion, err := lockVersion(ctx, tx, "projects", projectID)
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if projectVersion != expectedProjectVersion {
		return ProjectRepositoryMutation{}, domain.VersionConflictError{CurrentVersion: projectVersion}
	}
	var archivedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT archived_at FROM projects WHERE id=$1`, projectID).Scan(&archivedAt); err != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("read Project archive state: %w", err)
	}
	if archivedAt != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("%w: archived Projects are read-only", domain.ErrConflict)
	}
	item, err := scanProjectRepository(tx.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.provider, repository.origin,
			repository.path_with_namespace, repository.path_lookup_key, repository.canonical_web_url,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at
		FROM project_repositories repository
		WHERE repository.id=$1 AND repository.project_id=$2
		FOR UPDATE OF repository`, repositoryID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRepositoryMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if !item.Active() {
		return ProjectRepositoryMutation{}, fmt.Errorf("%w: Project repository is already unbound", domain.ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		UPDATE project_repositories
		SET unbound_by=$2, unbound_at=$3
		WHERE id=$1`, repositoryID, operation.UserID, now)
	if err != nil {
		return ProjectRepositoryMutation{}, mapPgError(err)
	}
	item.UnboundBy = &operation.UserID
	item.UnboundAt = &now
	projectVersion, err = incrementVersion(ctx, tx, "projects", projectID, projectVersion)
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if err := insertProjectRepositoryActivity(
		ctx, tx, projectID, operation, "project_repository_unbound",
		item.CanonicalWebURL, item.CanonicalWebURL, "",
	); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	oldValue, _ := json.Marshal(projectRepositoryAuditValue(
		domain.ProjectRepository{
			ID: item.ID, ProjectID: item.ProjectID, Provider: item.Provider, Origin: item.Origin,
			PathWithNamespace: item.PathWithNamespace, PathLookupKey: item.PathLookupKey,
			CanonicalWebURL: item.CanonicalWebURL, BoundBy: item.BoundBy, BoundAt: item.BoundAt,
		},
	))
	newValue, _ := json.Marshal(projectRepositoryAuditValue(item))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "project_repository",
		EntityID: repositoryID, Action: "unbound", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectRepositoryMutation{}, fmt.Errorf("commit Project repository unbind: %w", err)
	}
	return ProjectRepositoryMutation{Repository: item, ProjectVersion: projectVersion}, nil
}

func insertProjectRepositoryActivity(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	operation domain.OperationActor,
	action string,
	reason string,
	oldValue string,
	newValue string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO project_activity (
			id, project_id, actor_id, action, reason, old_value, new_value,
			request_id, auth_method, api_token_id, token_name_snapshot, agent_run_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		uuid.New(), projectID, operation.UserID, action, reason,
		nullIfEmpty(oldValue), nullIfEmpty(newValue), operation.RequestID,
		operation.AuthMethod, operation.TokenID, nullIfEmpty(operation.TokenName), operation.AgentRunID,
	)
	if err != nil {
		return fmt.Errorf("insert Project repository activity: %w", err)
	}
	return nil
}

func projectRepositoryAuditValue(repository domain.ProjectRepository) map[string]any {
	return map[string]any{
		"project_id": repository.ProjectID, "provider": repository.Provider,
		"origin": repository.Origin, "path_with_namespace": repository.PathWithNamespace,
		"canonical_web_url": repository.CanonicalWebURL,
		"bound_at":          repository.BoundAt, "unbound_at": repository.UnboundAt,
	}
}

type projectRepositoryScanner interface {
	Scan(dest ...any) error
}

func scanProjectRepository(row projectRepositoryScanner) (domain.ProjectRepository, error) {
	var repository domain.ProjectRepository
	err := row.Scan(
		&repository.ID, &repository.ProjectID, &repository.Provider, &repository.Origin,
		&repository.PathWithNamespace, &repository.PathLookupKey, &repository.CanonicalWebURL,
		&repository.BoundBy, &repository.BoundAt, &repository.UnboundBy, &repository.UnboundAt,
	)
	if err != nil {
		return domain.ProjectRepository{}, err
	}
	return repository, nil
}
