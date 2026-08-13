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
	project       domain.GitLabProjectIdentity
	err           error
	gotOrigin     string
	gotPath       string
	gotCredential string
}

func (s *gitLabProviderStub) ResolveProject(
	_ context.Context,
	origin string,
	path string,
	credential []byte,
	_ string,
) (domain.GitLabProjectIdentity, error) {
	s.gotOrigin = origin
	s.gotPath = path
	s.gotCredential = string(credential)
	return s.project, s.err
}

type gitLabConnectionRepositoryStub struct {
	current          domain.GitLabConnection
	created          domain.GitLabConnection
	rotated          bool
	failureCategory  string
	failureEventType string
}

func (s *gitLabConnectionRepositoryStub) List(context.Context) ([]domain.GitLabConnection, error) {
	return []domain.GitLabConnection{s.current}, nil
}

func (s *gitLabConnectionRepositoryStub) Get(context.Context, uuid.UUID) (domain.GitLabConnection, error) {
	return s.current, nil
}

func (s *gitLabConnectionRepositoryStub) Create(
	_ context.Context,
	connection domain.GitLabConnection,
	_ domain.OperationActor,
) (domain.GitLabConnection, error) {
	s.created = connection
	return connection, nil
}

func (s *gitLabConnectionRepositoryStub) RotateCredential(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
	_ store.GitLabConnectionValidation,
	_ []byte,
	_ string,
	_ *time.Time,
	_ domain.OperationActor,
) (domain.GitLabConnection, error) {
	s.rotated = true
	return s.current, nil
}

func (s *gitLabConnectionRepositoryStub) RecordValidation(
	context.Context,
	uuid.UUID,
	int64,
	store.GitLabConnectionValidation,
	domain.OperationActor,
) (domain.GitLabConnection, error) {
	return s.current, nil
}

func (s *gitLabConnectionRepositoryStub) Disable(
	context.Context, uuid.UUID, int64, domain.OperationActor, time.Time,
) (domain.GitLabConnection, error) {
	return s.current, nil
}

func (s *gitLabConnectionRepositoryStub) RecordFailure(
	_ context.Context,
	_ *uuid.UUID,
	_ uuid.UUID,
	eventType string,
	_ string,
	_ *int64,
	category string,
	_ string,
	_ time.Time,
) error {
	s.failureEventType = eventType
	s.failureCategory = category
	return nil
}

func TestGitLabConnectionServiceCreatesEncryptedRepositoryConnection(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	provider := &gitLabProviderStub{project: domain.GitLabProjectIdentity{
		ID: 17, PathWithNamespace: "group/repo",
		WebURL: "https://gitlab.example/group/repo", DefaultBranch: "main",
	}}
	repository := &gitLabConnectionRepositoryStub{}
	service := &GitLabConnectionService{
		Connections: repository, Provider: provider, Cipher: cipher,
		EncryptionKeyID: "key-1", Now: func() time.Time { return now },
	}
	actorID := uuid.New()

	created, err := service.Create(context.Background(), CreateGitLabConnection{
		Label: "Repository", RepositoryURL: "https://gitlab.example/group/repo",
		Credential: "plain-secret",
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

func TestGitLabConnectionServiceMapsAndAuditsProviderFailure(t *testing.T) {
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	repository := &gitLabConnectionRepositoryStub{}
	service := &GitLabConnectionService{
		Connections: repository,
		Provider: &gitLabProviderStub{err: &gitlabintegration.ProviderError{
			Category: gitlabintegration.ErrorUnauthorized,
		}},
		Cipher: cipher, EncryptionKeyID: "key-1",
	}

	_, err = service.Create(context.Background(), CreateGitLabConnection{
		Label: "Repository", RepositoryURL: "https://gitlab.example/group/repo",
		Credential: "bad-secret",
	}, domain.SessionOperation(uuid.New(), "request-2"))

	require.ErrorIs(t, err, domain.ErrProviderUnauthorized)
	require.Equal(t, "created", repository.failureEventType)
	require.Equal(t, string(gitlabintegration.ErrorUnauthorized), repository.failureCategory)
}

func TestGitLabConnectionServiceRejectsCredentialForDifferentProject(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	cipher, err := identity.NewCredentialCipher(map[string][]byte{
		"key-1": []byte("0123456789abcdef0123456789abcdef"),
	})
	require.NoError(t, err)
	current := domain.GitLabConnection{
		ID: uuid.New(), Version: 3, Origin: "https://gitlab.example",
		GitLabProjectID: 17, PathWithNamespace: "group/repo", PathLookupKey: "group/repo",
		CanonicalWebURL: "https://gitlab.example/group/repo",
	}
	repository := &gitLabConnectionRepositoryStub{current: current}
	service := &GitLabConnectionService{
		Connections: repository,
		Provider: &gitLabProviderStub{project: domain.GitLabProjectIdentity{
			ID: 18, PathWithNamespace: "group/repo",
			WebURL: "https://gitlab.example/group/repo", DefaultBranch: "main",
		}},
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

func TestGitLabConnectionServiceRequiresEncryptionConfiguration(t *testing.T) {
	service := &GitLabConnectionService{Connections: &gitLabConnectionRepositoryStub{}}
	_, err := service.Create(context.Background(), CreateGitLabConnection{
		Label: "Repository", RepositoryURL: "https://gitlab.example/group/repo", Credential: "secret",
	}, domain.SessionOperation(uuid.New(), "request-4"))
	require.True(t, errors.Is(err, domain.ErrIntegrationNotConfigured))
}
