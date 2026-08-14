package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	gitlabintegration "github.com/wolfhead/pactline/internal/integrations/gitlab"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type gitLabProviderStub struct {
	repository    domain.RepositoryIdentity
	err           error
	gotOrigin     string
	gotPath       string
	gotCredential string
}

func (*gitLabProviderStub) Provider() domain.RepositoryProvider {
	return domain.RepositoryProviderGitLab
}

func (*gitLabProviderStub) ParseRepositoryURL(raw string) (domain.RepositoryReference, error) {
	return gitlabintegration.NewClient(nil, time.Second).ParseRepositoryURL(raw)
}

func (*gitLabProviderStub) ParseCodeChangeURL(raw string) (domain.CodeChangeReference, error) {
	return gitlabintegration.NewClient(nil, time.Second).ParseCodeChangeURL(raw)
}

func (s *gitLabProviderStub) ResolveRepository(
	_ context.Context,
	reference domain.RepositoryReference,
	credential []byte,
	_ string,
) (domain.RepositoryIdentity, error) {
	s.gotOrigin = reference.Origin
	s.gotPath = reference.PathWithNamespace
	s.gotCredential = string(credential)
	return s.repository, s.err
}

func (*gitLabProviderStub) GetCodeChange(
	context.Context, domain.RepositoryReference, string, domain.CodeChangeKind, int64, []byte, string,
) (domain.CodeChange, error) {
	return domain.CodeChange{}, errors.New("not implemented")
}

type repositoryConnectionRepositoryStub struct {
	current          domain.RepositoryConnection
	created          domain.RepositoryConnection
	rotated          bool
	failureCategory  string
	failureEventType string
}

func (s *repositoryConnectionRepositoryStub) List(context.Context) ([]domain.RepositoryConnection, error) {
	return []domain.RepositoryConnection{s.current}, nil
}

func (s *repositoryConnectionRepositoryStub) Get(context.Context, uuid.UUID) (domain.RepositoryConnection, error) {
	return s.current, nil
}

func (s *repositoryConnectionRepositoryStub) Create(
	_ context.Context,
	connection domain.RepositoryConnection,
	_ domain.OperationActor,
) (domain.RepositoryConnection, error) {
	s.created = connection
	return connection, nil
}

func (s *repositoryConnectionRepositoryStub) RotateCredential(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
	_ store.RepositoryConnectionValidation,
	_ []byte,
	_ string,
	_ *time.Time,
	_ domain.OperationActor,
) (domain.RepositoryConnection, error) {
	s.rotated = true
	return s.current, nil
}

func (s *repositoryConnectionRepositoryStub) RecordValidation(
	context.Context,
	uuid.UUID,
	int64,
	store.RepositoryConnectionValidation,
	domain.OperationActor,
) (domain.RepositoryConnection, error) {
	return s.current, nil
}

func (s *repositoryConnectionRepositoryStub) Disable(
	context.Context, uuid.UUID, int64, domain.OperationActor, time.Time,
) (domain.RepositoryConnection, error) {
	return s.current, nil
}

func (s *repositoryConnectionRepositoryStub) RecordFailure(
	_ context.Context,
	_ *uuid.UUID,
	_ uuid.UUID,
	eventType string,
	_ domain.RepositoryProvider,
	_ string,
	_ *string,
	category string,
	_ string,
	_ time.Time,
) error {
	s.failureEventType = eventType
	s.failureCategory = category
	return nil
}

func TestRepositoryConnectionServiceCreatesEncryptedRepositoryConnection(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	provider := &gitLabProviderStub{repository: domain.RepositoryIdentity{
		ProviderRepositoryID: "17", PathWithNamespace: "group/repo",
		WebURL: "https://gitlab.example/group/repo", DefaultBranch: "main",
	}}
	repository := &repositoryConnectionRepositoryStub{}
	registry, err := NewRepositoryProviderRegistry(provider)
	require.NoError(t, err)
	service := &RepositoryConnectionService{
		Connections: repository, Providers: registry, Cipher: cipher,
		EncryptionKeyID: "key-1", Now: func() time.Time { return now },
	}
	actorID := uuid.New()

	created, err := service.Create(context.Background(), CreateRepositoryConnection{
		Label: "Repository", Provider: domain.RepositoryProviderGitLab,
		RepositoryURL: "https://gitlab.example/group/repo",
		Credential:    "plain-secret",
	}, domain.SessionOperation(actorID, "request-1"))

	require.NoError(t, err)
	require.Equal(t, "plain-secret", provider.gotCredential)
	require.Equal(t, "https://gitlab.example", provider.gotOrigin)
	require.Equal(t, "group/repo", provider.gotPath)
	require.NotEqual(t, []byte("plain-secret"), repository.created.CredentialCiphertext)
	plaintext, err := cipher.Decrypt(created.EncryptionKeyID, created.CredentialCiphertext)
	require.NoError(t, err)
	require.Equal(t, "plain-secret", string(plaintext))
}

func TestRepositoryConnectionServiceMapsAndAuditsProviderFailure(t *testing.T) {
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	repository := &repositoryConnectionRepositoryStub{}
	provider := &gitLabProviderStub{err: &gitlabintegration.ProviderError{
		Category: gitlabintegration.ErrorUnauthorized,
	}}
	registry, err := NewRepositoryProviderRegistry(provider)
	require.NoError(t, err)
	service := &RepositoryConnectionService{
		Connections: repository, Providers: registry,
		Cipher: cipher, EncryptionKeyID: "key-1",
	}

	_, err = service.Create(context.Background(), CreateRepositoryConnection{
		Label: "Repository", Provider: domain.RepositoryProviderGitLab,
		RepositoryURL: "https://gitlab.example/group/repo",
		Credential:    "bad-secret",
	}, domain.SessionOperation(uuid.New(), "request-2"))

	require.ErrorIs(t, err, domain.ErrProviderUnauthorized)
	require.Equal(t, "created", repository.failureEventType)
	require.Equal(t, string(gitlabintegration.ErrorUnauthorized), repository.failureCategory)
}

func TestRepositoryConnectionServiceRejectsCredentialForDifferentRepository(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	current := domain.RepositoryConnection{
		ID: uuid.New(), Version: 3, Provider: domain.RepositoryProviderGitLab,
		Origin: "https://gitlab.example", ProviderRepositoryID: "17",
		PathWithNamespace: "group/repo", PathLookupKey: "group/repo",
		CanonicalWebURL: "https://gitlab.example/group/repo",
	}
	repository := &repositoryConnectionRepositoryStub{current: current}
	provider := &gitLabProviderStub{repository: domain.RepositoryIdentity{
		ProviderRepositoryID: "18", PathWithNamespace: "group/repo",
		WebURL: "https://gitlab.example/group/repo", DefaultBranch: "main",
	}}
	registry, err := NewRepositoryProviderRegistry(provider)
	require.NoError(t, err)
	service := &RepositoryConnectionService{
		Connections: repository, Providers: registry,
		Cipher: cipher, EncryptionKeyID: "key-1", Now: func() time.Time { return now },
	}

	_, err = service.RotateCredential(
		context.Background(), current.ID, 3, "new-secret", nil,
		domain.SessionOperation(uuid.New(), "request-3"),
	)

	require.ErrorIs(t, err, domain.ErrConflict)
	require.False(t, repository.rotated)
	require.Equal(t, "credential_rotated", repository.failureEventType)
	require.Equal(t, "conflict", repository.failureCategory)
}

func TestRepositoryConnectionServiceRequiresEncryptionConfiguration(t *testing.T) {
	service := &RepositoryConnectionService{Connections: &repositoryConnectionRepositoryStub{}}
	_, err := service.Create(context.Background(), CreateRepositoryConnection{
		Label: "Repository", Provider: domain.RepositoryProviderGitLab,
		RepositoryURL: "https://gitlab.example/group/repo", Credential: "secret",
	}, domain.SessionOperation(uuid.New(), "request-4"))
	require.True(t, errors.Is(err, domain.ErrIntegrationNotConfigured))
}
