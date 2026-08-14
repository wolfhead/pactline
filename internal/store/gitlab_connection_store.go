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

const gitLabConnectionColumns = `
	id, version, label, origin, gitlab_project_id, path_with_namespace,
	path_lookup_key, canonical_web_url, default_branch,
	credential_ciphertext, encryption_key_id, credential_expires_at,
	status, last_validated_at, created_by, disabled_by, disabled_at,
	created_at, updated_at`

type GitLabConnectionStore struct{ db *DB }

func NewGitLabConnectionStore(db *DB) *GitLabConnectionStore {
	return &GitLabConnectionStore{db: db}
}

func (s *GitLabConnectionStore) List(ctx context.Context) ([]domain.GitLabConnection, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+gitLabConnectionColumns+`
		FROM gitlab_connections
		ORDER BY status, label, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list GitLab Connections: %w", err)
	}
	defer rows.Close()
	connections := []domain.GitLabConnection{}
	for rows.Next() {
		connection, err := scanGitLabConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (s *GitLabConnectionStore) Get(ctx context.Context, id uuid.UUID) (domain.GitLabConnection, error) {
	connection, err := scanGitLabConnection(s.db.Pool.QueryRow(ctx, `
		SELECT `+gitLabConnectionColumns+`
		FROM gitlab_connections WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitLabConnection{}, domain.ErrNotFound
	}
	return connection, err
}

func (s *GitLabConnectionStore) FindActiveByRepository(
	ctx context.Context,
	reference domain.GitLabRepositoryReference,
) (domain.GitLabConnection, error) {
	connection, err := scanGitLabConnection(s.db.Pool.QueryRow(ctx, `
		SELECT `+gitLabConnectionColumns+`
		FROM gitlab_connections
		WHERE origin=$1 AND path_lookup_key=$2 AND status='active'`,
		reference.Origin, reference.PathLookupKey,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitLabConnection{}, domain.ErrNotFound
	}
	return connection, err
}

func (s *GitLabConnectionStore) Create(
	ctx context.Context,
	connection domain.GitLabConnection,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	if err := connection.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("begin GitLab Connection create: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	created, err := scanGitLabConnection(tx.QueryRow(ctx, `
		INSERT INTO gitlab_connections (
			id, version, label, origin, gitlab_project_id, path_with_namespace,
			path_lookup_key, canonical_web_url, default_branch,
			credential_ciphertext, encryption_key_id, credential_expires_at,
			status, last_validated_at, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING `+gitLabConnectionColumns,
		connection.ID, connection.Version, connection.Label, connection.Origin,
		connection.GitLabProjectID, connection.PathWithNamespace, connection.PathLookupKey,
		connection.CanonicalWebURL, connection.DefaultBranch, connection.CredentialCiphertext,
		connection.EncryptionKeyID, connection.CredentialExpiresAt, connection.Status,
		connection.LastValidatedAt, connection.CreatedBy, connection.CreatedAt, connection.UpdatedAt,
	))
	if err != nil {
		return domain.GitLabConnection{}, mapPgError(err)
	}
	if err := insertGitLabConnectionEvent(
		ctx, tx, created.ID, operation.UserID, "created", "succeeded",
		created.Origin, &created.GitLabProjectID, "", operation.RequestID, created.CreatedAt,
	); err != nil {
		return domain.GitLabConnection{}, err
	}
	newValue, _ := json.Marshal(gitLabConnectionAuditValue(created))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: created.CreatedAt, Actor: operation, EntityType: "gitlab_connection",
		EntityID: created.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("commit GitLab Connection create: %w", err)
	}
	return created, nil
}

func (s *GitLabConnectionStore) RecordFailure(
	ctx context.Context,
	connectionID *uuid.UUID,
	actorUserID uuid.UUID,
	eventType string,
	origin string,
	gitLabProjectID *int64,
	errorCategory string,
	requestID string,
	now time.Time,
) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO gitlab_connection_events (
			id, connection_id, actor_user_id, event_type, outcome, origin,
			gitlab_project_id, error_category, request_id, created_at
		) VALUES ($1,$2,$3,$4,'failed',$5,$6,$7,$8,$9)`,
		uuid.New(), connectionID, actorUserID, eventType, nullIfEmpty(origin),
		gitLabProjectID, errorCategory, requestID, now,
	)
	if err != nil {
		return fmt.Errorf("record failed GitLab Connection event: %w", err)
	}
	return nil
}

type GitLabConnectionValidation struct {
	Reference domain.GitLabRepositoryReference
	Project   domain.GitLabProjectIdentity
	At        time.Time
}

func (s *GitLabConnectionStore) RotateCredential(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	validation GitLabConnectionValidation,
	ciphertext []byte,
	encryptionKeyID string,
	expiresAt *time.Time,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	return s.updateValidated(ctx, id, expectedVersion, validation, &credentialReplacement{
		Ciphertext: ciphertext, EncryptionKeyID: encryptionKeyID, ExpiresAt: expiresAt,
	}, "credential_rotated", operation)
}

func (s *GitLabConnectionStore) RecordValidation(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	validation GitLabConnectionValidation,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	return s.updateValidated(ctx, id, expectedVersion, validation, nil, "validated", operation)
}

type credentialReplacement struct {
	Ciphertext      []byte
	EncryptionKeyID string
	ExpiresAt       *time.Time
}

func (s *GitLabConnectionStore) updateValidated(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	validation GitLabConnectionValidation,
	replacement *credentialReplacement,
	eventType string,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("begin GitLab Connection %s: %w", eventType, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, err := scanGitLabConnection(tx.QueryRow(ctx, `
		SELECT `+gitLabConnectionColumns+` FROM gitlab_connections WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitLabConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.GitLabConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.GitLabConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	if current.Status != domain.GitLabConnectionStatusActive {
		return domain.GitLabConnection{}, fmt.Errorf("%w: GitLab Connection is disabled", domain.ErrConflict)
	}
	if current.GitLabProjectID != validation.Project.ID || current.Origin != validation.Reference.Origin {
		return domain.GitLabConnection{}, fmt.Errorf("%w: GitLab Connection repository identity changed", domain.ErrConflict)
	}
	credentialCiphertext := current.CredentialCiphertext
	encryptionKeyID := current.EncryptionKeyID
	expiresAt := current.CredentialExpiresAt
	if replacement != nil {
		if len(replacement.Ciphertext) == 0 || replacement.EncryptionKeyID == "" {
			return domain.GitLabConnection{}, fmt.Errorf("%w: encrypted credential is required", domain.ErrInvalidInput)
		}
		credentialCiphertext = replacement.Ciphertext
		encryptionKeyID = replacement.EncryptionKeyID
		expiresAt = replacement.ExpiresAt
	}
	updated, err := scanGitLabConnection(tx.QueryRow(ctx, `
		UPDATE gitlab_connections SET
			version=version+1,
			path_with_namespace=$2,
			path_lookup_key=$3,
			canonical_web_url=$4,
			default_branch=$5,
			credential_ciphertext=$6,
			encryption_key_id=$7,
			credential_expires_at=$8,
			last_validated_at=$9,
			updated_at=$9
		WHERE id=$1
		RETURNING `+gitLabConnectionColumns,
		id, validation.Reference.PathWithNamespace, validation.Reference.PathLookupKey,
		validation.Reference.WebURL, validation.Project.DefaultBranch, credentialCiphertext,
		encryptionKeyID, expiresAt, validation.At,
	))
	if err != nil {
		return domain.GitLabConnection{}, mapPgError(err)
	}
	if err := insertGitLabConnectionEvent(
		ctx, tx, updated.ID, operation.UserID, eventType, "succeeded", updated.Origin,
		&updated.GitLabProjectID, "", operation.RequestID, validation.At,
	); err != nil {
		return domain.GitLabConnection{}, err
	}
	oldValue, _ := json.Marshal(gitLabConnectionAuditValue(current))
	newValue, _ := json.Marshal(gitLabConnectionAuditValue(updated))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: validation.At, Actor: operation, EntityType: "gitlab_connection",
		EntityID: updated.ID, Action: eventType, OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("commit GitLab Connection %s: %w", eventType, err)
	}
	return updated, nil
}

func (s *GitLabConnectionStore) Disable(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	operation domain.OperationActor,
	now time.Time,
) (domain.GitLabConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("begin GitLab Connection disable: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, err := scanGitLabConnection(tx.QueryRow(ctx, `
		SELECT `+gitLabConnectionColumns+` FROM gitlab_connections WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GitLabConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.GitLabConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.GitLabConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	if current.Status != domain.GitLabConnectionStatusActive {
		return domain.GitLabConnection{}, fmt.Errorf("%w: GitLab Connection is already disabled", domain.ErrConflict)
	}
	var activeBindings int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM project_repositories
		WHERE connection_id=$1 AND unbound_at IS NULL`, id).Scan(&activeBindings); err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("count active GitLab Connection bindings: %w", err)
	}
	if activeBindings > 0 {
		return domain.GitLabConnection{}, fmt.Errorf(
			"%w: GitLab Connection has active Project bindings", domain.ErrConflict,
		)
	}
	updated, err := scanGitLabConnection(tx.QueryRow(ctx, `
		UPDATE gitlab_connections SET
			version=version+1, status='disabled', disabled_by=$2, disabled_at=$3, updated_at=$3
		WHERE id=$1
		RETURNING `+gitLabConnectionColumns,
		id, operation.UserID, now,
	))
	if err != nil {
		return domain.GitLabConnection{}, mapPgError(err)
	}
	if err := insertGitLabConnectionEvent(
		ctx, tx, updated.ID, operation.UserID, "disabled", "succeeded", updated.Origin,
		&updated.GitLabProjectID, "", operation.RequestID, now,
	); err != nil {
		return domain.GitLabConnection{}, err
	}
	oldValue, _ := json.Marshal(gitLabConnectionAuditValue(current))
	newValue, _ := json.Marshal(gitLabConnectionAuditValue(updated))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "gitlab_connection",
		EntityID: updated.ID, Action: "disabled", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("commit GitLab Connection disable: %w", err)
	}
	return updated, nil
}

func insertGitLabConnectionEvent(
	ctx context.Context,
	tx pgx.Tx,
	connectionID uuid.UUID,
	actorUserID uuid.UUID,
	eventType string,
	outcome string,
	origin string,
	gitLabProjectID *int64,
	errorCategory string,
	requestID string,
	now time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO gitlab_connection_events (
			id, connection_id, actor_user_id, event_type, outcome, origin,
			gitlab_project_id, error_category, request_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		uuid.New(), connectionID, actorUserID, eventType, outcome, nullIfEmpty(origin),
		gitLabProjectID, nullIfEmpty(errorCategory), requestID, now,
	)
	if err != nil {
		return fmt.Errorf("insert GitLab Connection event: %w", err)
	}
	return nil
}

func gitLabConnectionAuditValue(connection domain.GitLabConnection) map[string]any {
	return map[string]any{
		"label": connection.Label, "origin": connection.Origin,
		"gitlab_project_id":     connection.GitLabProjectID,
		"path_with_namespace":   connection.PathWithNamespace,
		"canonical_web_url":     connection.CanonicalWebURL,
		"default_branch":        connection.DefaultBranch,
		"credential_expires_at": connection.CredentialExpiresAt,
		"status":                connection.Status, "last_validated_at": connection.LastValidatedAt,
	}
}

type gitLabConnectionScanner interface {
	Scan(dest ...any) error
}

func scanGitLabConnection(row gitLabConnectionScanner) (domain.GitLabConnection, error) {
	var connection domain.GitLabConnection
	err := row.Scan(
		&connection.ID, &connection.Version, &connection.Label, &connection.Origin,
		&connection.GitLabProjectID, &connection.PathWithNamespace, &connection.PathLookupKey,
		&connection.CanonicalWebURL, &connection.DefaultBranch,
		&connection.CredentialCiphertext, &connection.EncryptionKeyID, &connection.CredentialExpiresAt,
		&connection.Status, &connection.LastValidatedAt, &connection.CreatedBy,
		&connection.DisabledBy, &connection.DisabledAt, &connection.CreatedAt, &connection.UpdatedAt,
	)
	if err != nil {
		return domain.GitLabConnection{}, err
	}
	return connection, nil
}
