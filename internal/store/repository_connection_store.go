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

const repositoryConnectionColumns = `
	id, version, label, provider, origin, provider_repository_id, path_with_namespace,
	path_lookup_key, canonical_web_url, default_branch,
	credential_ciphertext, encryption_key_id, credential_expires_at,
	status, last_validated_at, created_by, disabled_by, disabled_at,
	created_at, updated_at`

type RepositoryConnectionStore struct{ db *DB }

func NewRepositoryConnectionStore(db *DB) *RepositoryConnectionStore {
	return &RepositoryConnectionStore{db: db}
}

func (s *RepositoryConnectionStore) List(ctx context.Context) ([]domain.RepositoryConnection, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+repositoryConnectionColumns+`
		FROM repository_connections
		ORDER BY status, provider, label, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list Repository Connections: %w", err)
	}
	defer rows.Close()
	connections := []domain.RepositoryConnection{}
	for rows.Next() {
		connection, err := scanRepositoryConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (s *RepositoryConnectionStore) Get(ctx context.Context, id uuid.UUID) (domain.RepositoryConnection, error) {
	connection, err := scanRepositoryConnection(s.db.Pool.QueryRow(ctx, `
		SELECT `+repositoryConnectionColumns+`
		FROM repository_connections WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepositoryConnection{}, domain.ErrNotFound
	}
	return connection, err
}

func (s *RepositoryConnectionStore) FindActiveByRepository(
	ctx context.Context,
	reference domain.RepositoryReference,
) (domain.RepositoryConnection, error) {
	connection, err := scanRepositoryConnection(s.db.Pool.QueryRow(ctx, `
		SELECT `+repositoryConnectionColumns+`
		FROM repository_connections
		WHERE provider=$1 AND origin=$2 AND path_lookup_key=$3 AND status='active'`,
		reference.Provider, reference.Origin, reference.PathLookupKey,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepositoryConnection{}, domain.ErrNotFound
	}
	return connection, err
}

func (s *RepositoryConnectionStore) Create(
	ctx context.Context,
	connection domain.RepositoryConnection,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	if err := connection.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("begin Repository Connection create: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	created, err := scanRepositoryConnection(tx.QueryRow(ctx, `
		INSERT INTO repository_connections (
			id, version, label, provider, origin, provider_repository_id, path_with_namespace,
			path_lookup_key, canonical_web_url, default_branch,
			credential_ciphertext, encryption_key_id, credential_expires_at,
			status, last_validated_at, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING `+repositoryConnectionColumns,
		connection.ID, connection.Version, connection.Label, connection.Provider, connection.Origin,
		connection.ProviderRepositoryID, connection.PathWithNamespace, connection.PathLookupKey,
		connection.CanonicalWebURL, connection.DefaultBranch, connection.CredentialCiphertext,
		connection.EncryptionKeyID, connection.CredentialExpiresAt, connection.Status,
		connection.LastValidatedAt, connection.CreatedBy, connection.CreatedAt, connection.UpdatedAt,
	))
	if err != nil {
		return domain.RepositoryConnection{}, mapPgError(err)
	}
	if err := insertRepositoryConnectionEvent(
		ctx, tx, created.ID, operation.UserID, "created", "succeeded",
		created.Provider, created.Origin, &created.ProviderRepositoryID, "", operation.RequestID, created.CreatedAt,
	); err != nil {
		return domain.RepositoryConnection{}, err
	}
	newValue, _ := json.Marshal(repositoryConnectionAuditValue(created))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: created.CreatedAt, Actor: operation, EntityType: "repository_connection",
		EntityID: created.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("commit Repository Connection create: %w", err)
	}
	return created, nil
}

func (s *RepositoryConnectionStore) RecordFailure(
	ctx context.Context,
	connectionID *uuid.UUID,
	actorUserID uuid.UUID,
	eventType string,
	provider domain.RepositoryProvider,
	origin string,
	providerRepositoryID *string,
	errorCategory string,
	requestID string,
	now time.Time,
) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO repository_connection_events (
			id, connection_id, actor_user_id, event_type, outcome, provider, origin,
			provider_repository_id, error_category, request_id, created_at
		) VALUES ($1,$2,$3,$4,'failed',$5,$6,$7,$8,$9,$10)`,
		uuid.New(), connectionID, actorUserID, eventType, provider, nullIfEmpty(origin),
		providerRepositoryID, errorCategory, requestID, now,
	)
	if err != nil {
		return fmt.Errorf("record failed Repository Connection event: %w", err)
	}
	return nil
}

type RepositoryConnectionValidation struct {
	Reference  domain.RepositoryReference
	Repository domain.RepositoryIdentity
	At         time.Time
}

func (s *RepositoryConnectionStore) RotateCredential(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	validation RepositoryConnectionValidation,
	ciphertext []byte,
	encryptionKeyID string,
	expiresAt *time.Time,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	return s.updateValidated(ctx, id, expectedVersion, validation, &credentialReplacement{
		Ciphertext: ciphertext, EncryptionKeyID: encryptionKeyID, ExpiresAt: expiresAt,
	}, "credential_rotated", operation)
}

func (s *RepositoryConnectionStore) RecordValidation(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	validation RepositoryConnectionValidation,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	return s.updateValidated(ctx, id, expectedVersion, validation, nil, "validated", operation)
}

type credentialReplacement struct {
	Ciphertext      []byte
	EncryptionKeyID string
	ExpiresAt       *time.Time
}

func (s *RepositoryConnectionStore) updateValidated(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	validation RepositoryConnectionValidation,
	replacement *credentialReplacement,
	eventType string,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("begin Repository Connection %s: %w", eventType, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, err := scanRepositoryConnection(tx.QueryRow(ctx, `
		SELECT `+repositoryConnectionColumns+` FROM repository_connections WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepositoryConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.RepositoryConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.RepositoryConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	if current.Status != domain.RepositoryConnectionStatusActive {
		return domain.RepositoryConnection{}, fmt.Errorf("%w: Repository Connection is disabled", domain.ErrConflict)
	}
	if current.Provider != validation.Reference.Provider ||
		current.ProviderRepositoryID != validation.Repository.ProviderRepositoryID ||
		current.Origin != validation.Reference.Origin {
		return domain.RepositoryConnection{}, fmt.Errorf("%w: Repository Connection repository identity changed", domain.ErrConflict)
	}
	credentialCiphertext := current.CredentialCiphertext
	encryptionKeyID := current.EncryptionKeyID
	expiresAt := current.CredentialExpiresAt
	if replacement != nil {
		if len(replacement.Ciphertext) == 0 || replacement.EncryptionKeyID == "" {
			return domain.RepositoryConnection{}, fmt.Errorf("%w: encrypted credential is required", domain.ErrInvalidInput)
		}
		credentialCiphertext = replacement.Ciphertext
		encryptionKeyID = replacement.EncryptionKeyID
		expiresAt = replacement.ExpiresAt
	}
	updated, err := scanRepositoryConnection(tx.QueryRow(ctx, `
		UPDATE repository_connections SET
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
		RETURNING `+repositoryConnectionColumns,
		id, validation.Reference.PathWithNamespace, validation.Reference.PathLookupKey,
		validation.Reference.WebURL, validation.Repository.DefaultBranch, credentialCiphertext,
		encryptionKeyID, expiresAt, validation.At,
	))
	if err != nil {
		return domain.RepositoryConnection{}, mapPgError(err)
	}
	if err := insertRepositoryConnectionEvent(
		ctx, tx, updated.ID, operation.UserID, eventType, "succeeded", updated.Provider,
		updated.Origin, &updated.ProviderRepositoryID, "", operation.RequestID, validation.At,
	); err != nil {
		return domain.RepositoryConnection{}, err
	}
	oldValue, _ := json.Marshal(repositoryConnectionAuditValue(current))
	newValue, _ := json.Marshal(repositoryConnectionAuditValue(updated))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: validation.At, Actor: operation, EntityType: "repository_connection",
		EntityID: updated.ID, Action: eventType, OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("commit Repository Connection %s: %w", eventType, err)
	}
	return updated, nil
}

func (s *RepositoryConnectionStore) Disable(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	operation domain.OperationActor,
	now time.Time,
) (domain.RepositoryConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("begin Repository Connection disable: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	current, err := scanRepositoryConnection(tx.QueryRow(ctx, `
		SELECT `+repositoryConnectionColumns+` FROM repository_connections WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepositoryConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.RepositoryConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.RepositoryConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	if current.Status != domain.RepositoryConnectionStatusActive {
		return domain.RepositoryConnection{}, fmt.Errorf("%w: Repository Connection is already disabled", domain.ErrConflict)
	}
	updated, err := scanRepositoryConnection(tx.QueryRow(ctx, `
		UPDATE repository_connections SET
			version=version+1, status='disabled', disabled_by=$2, disabled_at=$3, updated_at=$3
		WHERE id=$1
		RETURNING `+repositoryConnectionColumns,
		id, operation.UserID, now,
	))
	if err != nil {
		return domain.RepositoryConnection{}, mapPgError(err)
	}
	if err := insertRepositoryConnectionEvent(
		ctx, tx, updated.ID, operation.UserID, "disabled", "succeeded", updated.Provider,
		updated.Origin, &updated.ProviderRepositoryID, "", operation.RequestID, now,
	); err != nil {
		return domain.RepositoryConnection{}, err
	}
	oldValue, _ := json.Marshal(repositoryConnectionAuditValue(current))
	newValue, _ := json.Marshal(repositoryConnectionAuditValue(updated))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "repository_connection",
		EntityID: updated.ID, Action: "disabled", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("commit Repository Connection disable: %w", err)
	}
	return updated, nil
}

func insertRepositoryConnectionEvent(
	ctx context.Context,
	tx pgx.Tx,
	connectionID uuid.UUID,
	actorUserID uuid.UUID,
	eventType string,
	outcome string,
	provider domain.RepositoryProvider,
	origin string,
	providerRepositoryID *string,
	errorCategory string,
	requestID string,
	now time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO repository_connection_events (
			id, connection_id, actor_user_id, event_type, outcome, provider, origin,
			provider_repository_id, error_category, request_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), connectionID, actorUserID, eventType, outcome, provider, nullIfEmpty(origin),
		providerRepositoryID, nullIfEmpty(errorCategory), requestID, now,
	)
	if err != nil {
		return fmt.Errorf("insert Repository Connection event: %w", err)
	}
	return nil
}

func repositoryConnectionAuditValue(connection domain.RepositoryConnection) map[string]any {
	return map[string]any{
		"label": connection.Label, "provider": connection.Provider, "origin": connection.Origin,
		"provider_repository_id": connection.ProviderRepositoryID,
		"path_with_namespace":    connection.PathWithNamespace,
		"canonical_web_url":      connection.CanonicalWebURL,
		"default_branch":         connection.DefaultBranch,
		"credential_expires_at":  connection.CredentialExpiresAt,
		"status":                 connection.Status, "last_validated_at": connection.LastValidatedAt,
	}
}

type repositoryConnectionScanner interface {
	Scan(dest ...any) error
}

func scanRepositoryConnection(row repositoryConnectionScanner) (domain.RepositoryConnection, error) {
	var connection domain.RepositoryConnection
	err := row.Scan(
		&connection.ID, &connection.Version, &connection.Label, &connection.Provider, &connection.Origin,
		&connection.ProviderRepositoryID, &connection.PathWithNamespace, &connection.PathLookupKey,
		&connection.CanonicalWebURL, &connection.DefaultBranch,
		&connection.CredentialCiphertext, &connection.EncryptionKeyID, &connection.CredentialExpiresAt,
		&connection.Status, &connection.LastValidatedAt, &connection.CreatedBy,
		&connection.DisabledBy, &connection.DisabledAt, &connection.CreatedAt, &connection.UpdatedAt,
	)
	if err != nil {
		return domain.RepositoryConnection{}, err
	}
	return connection, nil
}
