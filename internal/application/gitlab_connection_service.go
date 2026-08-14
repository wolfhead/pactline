package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	gitlabintegration "github.com/wolfhead/pactline/internal/integrations/gitlab"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

type gitLabConnectionProvider interface {
	ResolveProject(
		context.Context, string, string, []byte, string,
	) (domain.GitLabProjectIdentity, error)
}

type gitLabConnectionRepository interface {
	List(context.Context) ([]domain.GitLabConnection, error)
	Get(context.Context, uuid.UUID) (domain.GitLabConnection, error)
	Create(
		context.Context, domain.GitLabConnection, domain.OperationActor,
	) (domain.GitLabConnection, error)
	RotateCredential(
		context.Context, uuid.UUID, int64, store.GitLabConnectionValidation,
		[]byte, string, *time.Time, domain.OperationActor,
	) (domain.GitLabConnection, error)
	RecordValidation(
		context.Context, uuid.UUID, int64, store.GitLabConnectionValidation,
		domain.OperationActor,
	) (domain.GitLabConnection, error)
	Disable(
		context.Context, uuid.UUID, int64, domain.OperationActor, time.Time,
	) (domain.GitLabConnection, error)
	RecordFailure(
		context.Context, *uuid.UUID, uuid.UUID, string, string, *int64, string, string, time.Time,
	) error
}

type GitLabConnectionService struct {
	Connections     gitLabConnectionRepository
	Provider        gitLabConnectionProvider
	Cipher          *identity.CredentialCipher
	EncryptionKeyID string
	Now             func() time.Time
}

type CreateGitLabConnection struct {
	Label               string
	RepositoryURL       string
	Credential          string
	CredentialExpiresAt *time.Time
}

func (s *GitLabConnectionService) List(ctx context.Context) ([]domain.GitLabConnection, error) {
	if s.Connections == nil {
		return nil, domain.ErrIntegrationNotConfigured
	}
	return s.Connections.List(ctx)
}

func (s *GitLabConnectionService) Create(
	ctx context.Context,
	input CreateGitLabConnection,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := s.configured(); err != nil {
		return domain.GitLabConnection{}, err
	}
	label := strings.TrimSpace(input.Label)
	credential := []byte(strings.TrimSpace(input.Credential))
	defer clear(credential)
	if label == "" || len(credential) == 0 {
		return domain.GitLabConnection{}, fmt.Errorf(
			"%w: Connection label and credential are required", domain.ErrInvalidInput,
		)
	}
	reference, err := domain.ParseGitLabRepositoryURL(input.RepositoryURL)
	if err != nil {
		return domain.GitLabConnection{}, err
	}
	now := s.now()
	validation, err := s.resolve(ctx, reference, credential, operation.RequestID, now)
	if err != nil {
		s.recordFailure(ctx, nil, operation, "created", reference.Origin, nil, err, now)
		return domain.GitLabConnection{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(s.EncryptionKeyID, credential)
	if err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("encrypt GitLab credential: %w", err)
	}
	connection := domain.GitLabConnection{
		ID: uuid.New(), Version: 1, Label: label,
		Origin: validation.Reference.Origin, GitLabProjectID: validation.Project.ID,
		PathWithNamespace:    validation.Reference.PathWithNamespace,
		PathLookupKey:        validation.Reference.PathLookupKey,
		CanonicalWebURL:      validation.Reference.WebURL,
		DefaultBranch:        validation.Project.DefaultBranch,
		CredentialCiphertext: ciphertext, EncryptionKeyID: s.EncryptionKeyID,
		CredentialExpiresAt: input.CredentialExpiresAt,
		Status:              domain.GitLabConnectionStatusActive, LastValidatedAt: now,
		CreatedBy: operation.UserID, CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.Connections.Create(ctx, connection, operation)
	if err != nil {
		projectID := validation.Project.ID
		s.recordFailure(ctx, nil, operation, "created", reference.Origin, &projectID, err, now)
		return domain.GitLabConnection{}, err
	}
	return created, nil
}

func (s *GitLabConnectionService) RotateCredential(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	credentialText string,
	expiresAt *time.Time,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := s.configured(); err != nil {
		return domain.GitLabConnection{}, err
	}
	credential := []byte(strings.TrimSpace(credentialText))
	defer clear(credential)
	if len(credential) == 0 {
		return domain.GitLabConnection{}, fmt.Errorf("%w: credential is required", domain.ErrInvalidInput)
	}
	current, err := s.Connections.Get(ctx, id)
	if err != nil {
		return domain.GitLabConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.GitLabConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	reference := domain.GitLabRepositoryReference{
		Origin: current.Origin, PathWithNamespace: current.PathWithNamespace,
		PathLookupKey: current.PathLookupKey, WebURL: current.CanonicalWebURL,
	}
	now := s.now()
	validation, err := s.resolve(ctx, reference, credential, operation.RequestID, now)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "credential_rotated", current.Origin,
			&current.GitLabProjectID, err, now)
		return domain.GitLabConnection{}, err
	}
	if validation.Project.ID != current.GitLabProjectID {
		err := fmt.Errorf("%w: replacement credential resolved a different GitLab project", domain.ErrConflict)
		s.recordFailure(ctx, &id, operation, "credential_rotated", current.Origin,
			&current.GitLabProjectID, err, now)
		return domain.GitLabConnection{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(s.EncryptionKeyID, credential)
	if err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("encrypt GitLab credential: %w", err)
	}
	updated, err := s.Connections.RotateCredential(
		ctx, id, expectedVersion, validation, ciphertext, s.EncryptionKeyID, expiresAt, operation,
	)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "credential_rotated", current.Origin,
			&current.GitLabProjectID, err, now)
		return domain.GitLabConnection{}, err
	}
	return updated, nil
}

func (s *GitLabConnectionService) Validate(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	if err := s.configured(); err != nil {
		return domain.GitLabConnection{}, err
	}
	current, err := s.Connections.Get(ctx, id)
	if err != nil {
		return domain.GitLabConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.GitLabConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	credential, err := s.Cipher.Decrypt(current.EncryptionKeyID, current.CredentialCiphertext)
	if err != nil {
		return domain.GitLabConnection{}, fmt.Errorf("decrypt GitLab credential: %w", err)
	}
	defer clear(credential)
	reference := domain.GitLabRepositoryReference{
		Origin: current.Origin, PathWithNamespace: current.PathWithNamespace,
		PathLookupKey: current.PathLookupKey, WebURL: current.CanonicalWebURL,
	}
	now := s.now()
	validation, err := s.resolve(ctx, reference, credential, operation.RequestID, now)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "validated", current.Origin,
			&current.GitLabProjectID, err, now)
		return domain.GitLabConnection{}, err
	}
	if validation.Project.ID != current.GitLabProjectID {
		err := fmt.Errorf("%w: GitLab Connection repository identity changed", domain.ErrConflict)
		s.recordFailure(ctx, &id, operation, "validated", current.Origin,
			&current.GitLabProjectID, err, now)
		return domain.GitLabConnection{}, err
	}
	updated, err := s.Connections.RecordValidation(ctx, id, expectedVersion, validation, operation)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "validated", current.Origin,
			&current.GitLabProjectID, err, now)
		return domain.GitLabConnection{}, err
	}
	return updated, nil
}

func (s *GitLabConnectionService) Disable(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.GitLabConnection{}, err
	}
	if s.Connections == nil {
		return domain.GitLabConnection{}, domain.ErrIntegrationNotConfigured
	}
	return s.Connections.Disable(ctx, id, expectedVersion, operation, s.now())
}

func (s *GitLabConnectionService) resolve(
	ctx context.Context,
	reference domain.GitLabRepositoryReference,
	credential []byte,
	requestID string,
	now time.Time,
) (store.GitLabConnectionValidation, error) {
	project, err := s.Provider.ResolveProject(
		ctx, reference.Origin, reference.PathWithNamespace, credential, requestID,
	)
	if err != nil {
		return store.GitLabConnectionValidation{}, mapGitLabProviderError(err)
	}
	providerReference, err := domain.ParseGitLabRepositoryURL(project.WebURL)
	if err != nil {
		return store.GitLabConnectionValidation{}, fmt.Errorf(
			"%w: GitLab returned an invalid canonical repository URL", domain.ErrProviderRejected,
		)
	}
	if providerReference.Origin != reference.Origin ||
		providerReference.PathLookupKey != reference.PathLookupKey ||
		!strings.EqualFold(project.PathWithNamespace, reference.PathWithNamespace) {
		return store.GitLabConnectionValidation{}, fmt.Errorf(
			"%w: GitLab returned a different repository identity", domain.ErrProviderRejected,
		)
	}
	return store.GitLabConnectionValidation{Reference: providerReference, Project: project, At: now}, nil
}

func (s *GitLabConnectionService) validateExisting(
	ctx context.Context,
	connection domain.GitLabConnection,
	requestID string,
) (store.GitLabConnectionValidation, error) {
	if err := s.configured(); err != nil {
		return store.GitLabConnectionValidation{}, err
	}
	if connection.Status != domain.GitLabConnectionStatusActive {
		return store.GitLabConnectionValidation{}, fmt.Errorf(
			"%w: GitLab Connection is disabled", domain.ErrConflict,
		)
	}
	credential, err := s.Cipher.Decrypt(connection.EncryptionKeyID, connection.CredentialCiphertext)
	if err != nil {
		return store.GitLabConnectionValidation{}, fmt.Errorf("decrypt GitLab credential: %w", err)
	}
	defer clear(credential)
	reference := domain.GitLabRepositoryReference{
		Origin: connection.Origin, PathWithNamespace: connection.PathWithNamespace,
		PathLookupKey: connection.PathLookupKey, WebURL: connection.CanonicalWebURL,
	}
	validation, err := s.resolve(ctx, reference, credential, requestID, s.now())
	if err != nil {
		return store.GitLabConnectionValidation{}, err
	}
	if validation.Project.ID != connection.GitLabProjectID {
		return store.GitLabConnectionValidation{}, fmt.Errorf(
			"%w: GitLab Connection repository identity changed", domain.ErrConflict,
		)
	}
	return validation, nil
}

func (s *GitLabConnectionService) configured() error {
	if s.Connections == nil || s.Provider == nil || s.Cipher == nil || strings.TrimSpace(s.EncryptionKeyID) == "" {
		return domain.ErrIntegrationNotConfigured
	}
	return nil
}

func (s *GitLabConnectionService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *GitLabConnectionService) recordFailure(
	ctx context.Context,
	connectionID *uuid.UUID,
	operation domain.OperationActor,
	eventType string,
	origin string,
	projectID *int64,
	failure error,
	now time.Time,
) {
	category := integrationFailureCategory(failure)
	if err := s.Connections.RecordFailure(
		ctx, connectionID, operation.UserID, eventType, origin, projectID,
		category, operation.RequestID, now,
	); err != nil {
		slog.ErrorContext(ctx, "record GitLab Connection failure audit",
			"event_type", eventType, "origin", origin, "request_id", operation.RequestID, "error", err)
	}
}

func mapGitLabProviderError(err error) error {
	switch gitlabintegration.ErrorCategoryOf(err) {
	case gitlabintegration.ErrorInvalidReference:
		return fmt.Errorf("%w: GitLab reference is invalid", domain.ErrInvalidInput)
	case gitlabintegration.ErrorNotFound:
		return fmt.Errorf("%w: GitLab repository was not found", domain.ErrNotFound)
	case gitlabintegration.ErrorUnauthorized:
		return fmt.Errorf("%w: GitLab rejected the credential", domain.ErrProviderUnauthorized)
	case gitlabintegration.ErrorUnreachable:
		return fmt.Errorf("%w: GitLab is unavailable", domain.ErrProviderUnavailable)
	default:
		return fmt.Errorf("%w: GitLab rejected the request", domain.ErrProviderRejected)
	}
}

func integrationFailureCategory(err error) string {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		return string(gitlabintegration.ErrorInvalidReference)
	case errors.Is(err, domain.ErrNotFound):
		return string(gitlabintegration.ErrorNotFound)
	case errors.Is(err, domain.ErrProviderUnauthorized):
		return string(gitlabintegration.ErrorUnauthorized)
	case errors.Is(err, domain.ErrProviderUnavailable):
		return string(gitlabintegration.ErrorUnreachable)
	case errors.Is(err, domain.ErrProviderRejected):
		return string(gitlabintegration.ErrorProviderRejected)
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrVersionConflict):
		return "conflict"
	default:
		return "internal"
	}
}
