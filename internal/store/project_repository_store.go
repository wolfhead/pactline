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

type ProjectRepositoryWithConnection struct {
	Repository domain.ProjectRepository
	Connection domain.RepositoryConnection
}

type ProjectRepositoryMutation struct {
	Repository     ProjectRepositoryWithConnection
	ProjectVersion int64
}

type ProjectRepositoryStore struct{ db *DB }

func NewProjectRepositoryStore(db *DB) *ProjectRepositoryStore {
	return &ProjectRepositoryStore{db: db}
}

func (s *ProjectRepositoryStore) ListActive(
	ctx context.Context,
	projectID uuid.UUID,
) ([]ProjectRepositoryWithConnection, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT
			repository.id, repository.project_id, repository.connection_id,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
			`+prefixedRepositoryConnectionColumns("connection")+`
		FROM project_repositories repository
		JOIN repository_connections connection ON connection.id=repository.connection_id
		WHERE repository.project_id=$1 AND repository.unbound_at IS NULL
		ORDER BY connection.path_lookup_key, repository.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list Project repositories: %w", err)
	}
	defer rows.Close()
	result := []ProjectRepositoryWithConnection{}
	for rows.Next() {
		item, err := scanProjectRepositoryWithConnection(rows)
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
) (ProjectRepositoryWithConnection, error) {
	item, err := scanProjectRepositoryWithConnection(s.db.Pool.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.connection_id,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
			`+prefixedRepositoryConnectionColumns("connection")+`
		FROM project_repositories repository
		JOIN repository_connections connection ON connection.id=repository.connection_id
		WHERE repository.project_id=$1 AND repository.unbound_at IS NULL
		  AND connection.status='active' AND connection.provider=$2
		  AND connection.origin=$3 AND connection.path_lookup_key=$4`,
		projectID, reference.Provider, reference.Origin, reference.PathLookupKey,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRepositoryWithConnection{}, domain.ErrNotFound
	}
	return item, err
}

func (s *ProjectRepositoryStore) Bind(
	ctx context.Context,
	projectID uuid.UUID,
	expectedProjectVersion int64,
	connectionID uuid.UUID,
	operation domain.OperationActor,
	now time.Time,
) (ProjectRepositoryMutation, error) {
	if err := operation.Validate(); err != nil {
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
	connection, err := scanRepositoryConnection(tx.QueryRow(ctx, `
		SELECT `+repositoryConnectionColumns+`
		FROM repository_connections WHERE id=$1 FOR SHARE`, connectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRepositoryMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if connection.Status != domain.RepositoryConnectionStatusActive {
		return ProjectRepositoryMutation{}, fmt.Errorf("%w: Repository Connection is disabled", domain.ErrConflict)
	}
	repository := domain.ProjectRepository{
		ID: uuid.New(), ProjectID: projectID, ConnectionID: connectionID,
		BoundBy: operation.UserID, BoundAt: now,
	}
	if err := repository.Validate(); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_repositories (
			id, project_id, connection_id, bound_by, bound_at
		) VALUES ($1,$2,$3,$4,$5)`,
		repository.ID, repository.ProjectID, repository.ConnectionID,
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
		ctx, tx, projectID, operation, "project_repository_bound", connection.CanonicalWebURL,
		"", connection.CanonicalWebURL,
	); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	newValue, _ := json.Marshal(projectRepositoryAuditValue(repository, connection))
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
		Repository:     ProjectRepositoryWithConnection{Repository: repository, Connection: connection},
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
	item, err := scanProjectRepositoryWithConnection(tx.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.connection_id,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
			`+prefixedRepositoryConnectionColumns("connection")+`
		FROM project_repositories repository
		JOIN repository_connections connection ON connection.id=repository.connection_id
		WHERE repository.id=$1 AND repository.project_id=$2
		FOR UPDATE OF repository`, repositoryID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRepositoryMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if !item.Repository.Active() {
		return ProjectRepositoryMutation{}, fmt.Errorf("%w: Project repository is already unbound", domain.ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		UPDATE project_repositories
		SET unbound_by=$2, unbound_at=$3
		WHERE id=$1`, repositoryID, operation.UserID, now)
	if err != nil {
		return ProjectRepositoryMutation{}, mapPgError(err)
	}
	item.Repository.UnboundBy = &operation.UserID
	item.Repository.UnboundAt = &now
	projectVersion, err = incrementVersion(ctx, tx, "projects", projectID, projectVersion)
	if err != nil {
		return ProjectRepositoryMutation{}, err
	}
	if err := insertProjectRepositoryActivity(
		ctx, tx, projectID, operation, "project_repository_unbound",
		item.Connection.CanonicalWebURL, item.Connection.CanonicalWebURL, "",
	); err != nil {
		return ProjectRepositoryMutation{}, err
	}
	oldValue, _ := json.Marshal(projectRepositoryAuditValue(
		domain.ProjectRepository{
			ID: item.Repository.ID, ProjectID: item.Repository.ProjectID,
			ConnectionID: item.Repository.ConnectionID, BoundBy: item.Repository.BoundBy,
			BoundAt: item.Repository.BoundAt,
		}, item.Connection,
	))
	newValue, _ := json.Marshal(projectRepositoryAuditValue(item.Repository, item.Connection))
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

func projectRepositoryAuditValue(
	repository domain.ProjectRepository,
	connection domain.RepositoryConnection,
) map[string]any {
	return map[string]any{
		"project_id": repository.ProjectID, "connection_id": repository.ConnectionID,
		"provider": connection.Provider, "provider_repository_id": connection.ProviderRepositoryID,
		"canonical_web_url": connection.CanonicalWebURL,
		"bound_at":          repository.BoundAt, "unbound_at": repository.UnboundAt,
	}
}

func prefixedRepositoryConnectionColumns(alias string) string {
	return alias + `.id, ` + alias + `.version, ` + alias + `.label, ` + alias + `.origin, ` +
		alias + `.provider, ` + alias + `.provider_repository_id, ` + alias + `.path_with_namespace, ` +
		alias + `.path_lookup_key, ` + alias + `.canonical_web_url, ` +
		alias + `.default_branch, ` + alias + `.credential_ciphertext, ` +
		alias + `.encryption_key_id, ` + alias + `.credential_expires_at, ` +
		alias + `.status, ` + alias + `.last_validated_at, ` + alias + `.created_by, ` +
		alias + `.disabled_by, ` + alias + `.disabled_at, ` + alias + `.created_at, ` +
		alias + `.updated_at`
}

type projectRepositoryScanner interface {
	Scan(dest ...any) error
}

func scanProjectRepositoryWithConnection(row projectRepositoryScanner) (ProjectRepositoryWithConnection, error) {
	var item ProjectRepositoryWithConnection
	err := row.Scan(
		&item.Repository.ID, &item.Repository.ProjectID, &item.Repository.ConnectionID,
		&item.Repository.BoundBy, &item.Repository.BoundAt,
		&item.Repository.UnboundBy, &item.Repository.UnboundAt,
		&item.Connection.ID, &item.Connection.Version, &item.Connection.Label,
		&item.Connection.Origin, &item.Connection.Provider, &item.Connection.ProviderRepositoryID,
		&item.Connection.PathWithNamespace, &item.Connection.PathLookupKey,
		&item.Connection.CanonicalWebURL, &item.Connection.DefaultBranch,
		&item.Connection.CredentialCiphertext, &item.Connection.EncryptionKeyID,
		&item.Connection.CredentialExpiresAt, &item.Connection.Status,
		&item.Connection.LastValidatedAt, &item.Connection.CreatedBy,
		&item.Connection.DisabledBy, &item.Connection.DisabledAt,
		&item.Connection.CreatedAt, &item.Connection.UpdatedAt,
	)
	if err != nil {
		return ProjectRepositoryWithConnection{}, err
	}
	return item, nil
}
