package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

type repositoryConnectionRepository interface {
	List(context.Context) ([]domain.RepositoryConnection, error)
	Get(context.Context, uuid.UUID) (domain.RepositoryConnection, error)
	Create(context.Context, domain.RepositoryConnection, domain.OperationActor) (domain.RepositoryConnection, error)
	RotateCredential(
		context.Context, uuid.UUID, int64, store.RepositoryConnectionValidation,
		[]byte, string, *time.Time, domain.OperationActor,
	) (domain.RepositoryConnection, error)
	RecordValidation(
		context.Context, uuid.UUID, int64, store.RepositoryConnectionValidation, domain.OperationActor,
	) (domain.RepositoryConnection, error)
	Disable(context.Context, uuid.UUID, int64, domain.OperationActor, time.Time) (domain.RepositoryConnection, error)
	RecordFailure(
		context.Context, *uuid.UUID, uuid.UUID, string, domain.RepositoryProvider,
		string, *string, string, string, time.Time,
	) error
}

type RepositoryConnectionService struct {
	Connections     repositoryConnectionRepository
	Providers       *RepositoryProviderRegistry
	Cipher          *identity.CredentialCipher
	EncryptionKeyID string
	Now             func() time.Time
}

type CreateRepositoryConnection struct {
	Label               string
	Provider            domain.RepositoryProvider
	RepositoryURL       string
	Credential          string
	CredentialExpiresAt *time.Time
}

func (s *RepositoryConnectionService) List(ctx context.Context) ([]domain.RepositoryConnection, error) {
	if s.Connections == nil {
		return nil, domain.ErrIntegrationNotConfigured
	}
	return s.Connections.List(ctx)
}

func (s *RepositoryConnectionService) Create(
	ctx context.Context,
	input CreateRepositoryConnection,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := s.configured(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	label := strings.TrimSpace(input.Label)
	credential := []byte(strings.TrimSpace(input.Credential))
	defer clear(credential)
	if label == "" || len(credential) == 0 || !input.Provider.Valid() {
		return domain.RepositoryConnection{}, fmt.Errorf(
			"%w: provider, Connection label, and credential are required", domain.ErrInvalidInput,
		)
	}
	reference, err := s.Providers.ParseRepositoryURL(input.Provider, input.RepositoryURL)
	if err != nil {
		return domain.RepositoryConnection{}, err
	}
	now := s.now()
	validation, err := s.resolve(ctx, reference, credential, operation.RequestID, now)
	if err != nil {
		s.recordFailure(ctx, nil, operation, "created", reference.Provider, reference.Origin, nil, err, now)
		return domain.RepositoryConnection{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(s.EncryptionKeyID, credential)
	if err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("encrypt repository credential: %w", err)
	}
	connection := domain.RepositoryConnection{
		ID: uuid.New(), Version: 1, Label: label, Provider: reference.Provider,
		Origin:               validation.Reference.Origin,
		ProviderRepositoryID: validation.Repository.ProviderRepositoryID,
		PathWithNamespace:    validation.Reference.PathWithNamespace,
		PathLookupKey:        validation.Reference.PathLookupKey,
		CanonicalWebURL:      validation.Reference.WebURL,
		DefaultBranch:        validation.Repository.DefaultBranch,
		CredentialCiphertext: ciphertext, EncryptionKeyID: s.EncryptionKeyID,
		CredentialExpiresAt: input.CredentialExpiresAt,
		Status:              domain.RepositoryConnectionStatusActive, LastValidatedAt: now,
		CreatedBy: operation.UserID, CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.Connections.Create(ctx, connection, operation)
	if err != nil {
		providerRepositoryID := validation.Repository.ProviderRepositoryID
		s.recordFailure(ctx, nil, operation, "created", reference.Provider, reference.Origin, &providerRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	return created, nil
}

func (s *RepositoryConnectionService) RotateCredential(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	credentialText string,
	expiresAt *time.Time,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := s.configured(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	credential := []byte(strings.TrimSpace(credentialText))
	defer clear(credential)
	if len(credential) == 0 {
		return domain.RepositoryConnection{}, fmt.Errorf("%w: credential is required", domain.ErrInvalidInput)
	}
	current, err := s.Connections.Get(ctx, id)
	if err != nil {
		return domain.RepositoryConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.RepositoryConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	reference := repositoryReference(current)
	now := s.now()
	validation, err := s.resolve(ctx, reference, credential, operation.RequestID, now)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "credential_rotated", current.Provider, current.Origin,
			&current.ProviderRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	if validation.Repository.ProviderRepositoryID != current.ProviderRepositoryID {
		err := fmt.Errorf("%w: replacement credential resolved a different repository", domain.ErrConflict)
		s.recordFailure(ctx, &id, operation, "credential_rotated", current.Provider, current.Origin,
			&current.ProviderRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(s.EncryptionKeyID, credential)
	if err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("encrypt repository credential: %w", err)
	}
	updated, err := s.Connections.RotateCredential(
		ctx, id, expectedVersion, validation, ciphertext, s.EncryptionKeyID, expiresAt, operation,
	)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "credential_rotated", current.Provider, current.Origin,
			&current.ProviderRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	return updated, nil
}

func (s *RepositoryConnectionService) Validate(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if err := s.configured(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	current, err := s.Connections.Get(ctx, id)
	if err != nil {
		return domain.RepositoryConnection{}, err
	}
	if current.Version != expectedVersion {
		return domain.RepositoryConnection{}, domain.VersionConflictError{CurrentVersion: current.Version}
	}
	credential, err := s.Cipher.Decrypt(current.EncryptionKeyID, current.CredentialCiphertext)
	if err != nil {
		return domain.RepositoryConnection{}, fmt.Errorf("decrypt repository credential: %w", err)
	}
	defer clear(credential)
	now := s.now()
	validation, err := s.resolve(ctx, repositoryReference(current), credential, operation.RequestID, now)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "validated", current.Provider, current.Origin,
			&current.ProviderRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	if validation.Repository.ProviderRepositoryID != current.ProviderRepositoryID {
		err := fmt.Errorf("%w: Repository Connection identity changed", domain.ErrConflict)
		s.recordFailure(ctx, &id, operation, "validated", current.Provider, current.Origin,
			&current.ProviderRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	updated, err := s.Connections.RecordValidation(ctx, id, expectedVersion, validation, operation)
	if err != nil {
		s.recordFailure(ctx, &id, operation, "validated", current.Provider, current.Origin,
			&current.ProviderRepositoryID, err, now)
		return domain.RepositoryConnection{}, err
	}
	return updated, nil
}

func (s *RepositoryConnectionService) Disable(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	if err := operation.Validate(); err != nil {
		return domain.RepositoryConnection{}, err
	}
	if s.Connections == nil {
		return domain.RepositoryConnection{}, domain.ErrIntegrationNotConfigured
	}
	return s.Connections.Disable(ctx, id, expectedVersion, operation, s.now())
}

func (s *RepositoryConnectionService) resolve(
	ctx context.Context,
	reference domain.RepositoryReference,
	credential []byte,
	requestID string,
	now time.Time,
) (store.RepositoryConnectionValidation, error) {
	provider, err := s.Providers.Provider(reference.Provider)
	if err != nil {
		return store.RepositoryConnectionValidation{}, err
	}
	repository, err := provider.ResolveRepository(ctx, reference, credential, requestID)
	if err != nil {
		return store.RepositoryConnectionValidation{}, mapRepositoryProviderError(reference.Provider, err)
	}
	providerReference, err := provider.ParseRepositoryURL(repository.WebURL)
	if err != nil {
		return store.RepositoryConnectionValidation{}, fmt.Errorf(
			"%w: provider returned an invalid canonical repository URL", domain.ErrProviderRejected,
		)
	}
	if providerReference.Provider != reference.Provider || providerReference.Origin != reference.Origin ||
		providerReference.PathLookupKey != reference.PathLookupKey ||
		!strings.EqualFold(repository.PathWithNamespace, reference.PathWithNamespace) {
		return store.RepositoryConnectionValidation{}, fmt.Errorf(
			"%w: provider returned a different repository identity", domain.ErrProviderRejected,
		)
	}
	return store.RepositoryConnectionValidation{Reference: providerReference, Repository: repository, At: now}, nil
}

func (s *RepositoryConnectionService) validateExisting(
	ctx context.Context,
	connection domain.RepositoryConnection,
	requestID string,
) (store.RepositoryConnectionValidation, error) {
	if err := s.configured(); err != nil {
		return store.RepositoryConnectionValidation{}, err
	}
	if connection.Status != domain.RepositoryConnectionStatusActive {
		return store.RepositoryConnectionValidation{}, fmt.Errorf(
			"%w: Repository Connection is disabled", domain.ErrConflict,
		)
	}
	credential, err := s.Cipher.Decrypt(connection.EncryptionKeyID, connection.CredentialCiphertext)
	if err != nil {
		return store.RepositoryConnectionValidation{}, fmt.Errorf("decrypt repository credential: %w", err)
	}
	defer clear(credential)
	validation, err := s.resolve(ctx, repositoryReference(connection), credential, requestID, s.now())
	if err != nil {
		return store.RepositoryConnectionValidation{}, err
	}
	if validation.Repository.ProviderRepositoryID != connection.ProviderRepositoryID {
		return store.RepositoryConnectionValidation{}, fmt.Errorf(
			"%w: Repository Connection identity changed", domain.ErrConflict,
		)
	}
	return validation, nil
}

func repositoryReference(connection domain.RepositoryConnection) domain.RepositoryReference {
	return domain.RepositoryReference{
		Provider: connection.Provider, Origin: connection.Origin,
		PathWithNamespace: connection.PathWithNamespace, PathLookupKey: connection.PathLookupKey,
		WebURL: connection.CanonicalWebURL,
	}
}

func (s *RepositoryConnectionService) configured() error {
	if s.Connections == nil || s.Providers == nil || s.Cipher == nil || strings.TrimSpace(s.EncryptionKeyID) == "" {
		return domain.ErrIntegrationNotConfigured
	}
	return nil
}

func (s *RepositoryConnectionService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *RepositoryConnectionService) recordFailure(
	ctx context.Context,
	connectionID *uuid.UUID,
	operation domain.OperationActor,
	eventType string,
	provider domain.RepositoryProvider,
	origin string,
	providerRepositoryID *string,
	failure error,
	now time.Time,
) {
	category := repositoryIntegrationFailureCategory(failure)
	if err := s.Connections.RecordFailure(
		ctx, connectionID, operation.UserID, eventType, provider, origin, providerRepositoryID,
		category, operation.RequestID, now,
	); err != nil {
		slog.ErrorContext(ctx, "record Repository Connection failure audit",
			"event_type", eventType, "provider", provider, "origin", origin,
			"request_id", operation.RequestID, "error", err)
	}
}
