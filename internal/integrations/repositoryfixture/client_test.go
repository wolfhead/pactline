package repositoryfixture

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	githubintegration "github.com/wolfhead/pactline/internal/integrations/github"
	gitlabintegration "github.com/wolfhead/pactline/internal/integrations/gitlab"

	"github.com/stretchr/testify/require"
)

func TestFixtureClientsResolveRepositoriesAndCodeChanges(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		provider      domain.RepositoryProvider
		delegate      ProviderClient
		repositoryURL string
		codeChangeURL string
		repositoryID  string
		kind          domain.CodeChangeKind
		changeNumber  int64
		changeID      string
		headSHA       string
	}{
		{
			name: "github", provider: domain.RepositoryProviderGitHub,
			delegate:      githubintegration.NewClient(nil, time.Second),
			repositoryURL: GitHubOrigin + "/" + RepositoryPath,
			codeChangeURL: GitHubOrigin + "/" + RepositoryPath + "/pull/42",
			repositoryID:  GitHubRepositoryID, kind: domain.CodeChangeKindPullRequest,
			changeNumber: GitHubChangeNumber, changeID: GitHubChangeID,
			headSHA: "1111111111111111111111111111111111111111",
		},
		{
			name: "gitlab", provider: domain.RepositoryProviderGitLab,
			delegate:      gitlabintegration.NewClient(nil, time.Second),
			repositoryURL: GitLabOrigin + "/" + RepositoryPath,
			codeChangeURL: GitLabOrigin + "/" + RepositoryPath + "/-/merge_requests/43",
			repositoryID:  GitLabRepositoryID, kind: domain.CodeChangeKindMergeRequest,
			changeNumber: GitLabChangeNumber, changeID: GitLabChangeID,
			headSHA: "2222222222222222222222222222222222222222",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := New(testCase.provider, testCase.delegate)
			require.NoError(t, err)
			client.now = func() time.Time { return time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC) }

			reference, err := client.ParseRepositoryURL(testCase.repositoryURL)
			require.NoError(t, err)
			repository, err := client.ResolveRepository(
				context.Background(), reference, []byte(SyntheticCredential), "request-repository",
			)
			require.NoError(t, err)
			require.Equal(t, testCase.repositoryID, repository.ProviderRepositoryID)
			require.Equal(t, RepositoryPath, repository.PathWithNamespace)
			require.Equal(t, testCase.repositoryURL, repository.WebURL)

			changeReference, err := client.ParseCodeChangeURL(testCase.codeChangeURL)
			require.NoError(t, err)
			change, err := client.GetCodeChange(
				context.Background(), changeReference.Repository, repository.ProviderRepositoryID,
				changeReference.Kind, changeReference.ChangeNumber,
				[]byte(SyntheticCredential), "request-change",
			)
			require.NoError(t, err)
			require.Equal(t, testCase.provider, change.Provider)
			require.Equal(t, testCase.kind, change.Kind)
			require.Equal(t, testCase.changeNumber, change.ChangeNumber)
			require.Equal(t, testCase.changeID, change.ProviderChangeID)
			require.Equal(t, testCase.codeChangeURL, change.WebURL)
			require.Equal(t, testCase.headSHA, change.Observation.HeadSHA)
			require.Equal(t, domain.CodeChangeStateOpened, change.Observation.State)
		})
	}
}

func TestFixtureClientClassifiesCredentialAndIdentityErrors(t *testing.T) {
	client, err := New(
		domain.RepositoryProviderGitHub,
		githubintegration.NewClient(nil, time.Second),
	)
	require.NoError(t, err)
	reference, err := client.ParseRepositoryURL(GitHubOrigin + "/" + RepositoryPath)
	require.NoError(t, err)

	_, err = client.ResolveRepository(context.Background(), reference, []byte("wrong"), "request-1")
	require.Equal(t, ErrorUnauthorized, errorCategory(err))
	require.NotContains(t, err.Error(), "wrong")
	require.NotContains(t, err.Error(), SyntheticCredential)

	unknown := reference
	unknown.PathLookupKey = "pactline/missing"
	_, err = client.ResolveRepository(context.Background(), unknown, []byte(SyntheticCredential), "request-2")
	require.Equal(t, ErrorNotFound, errorCategory(err))

	_, err = client.GetCodeChange(
		context.Background(), reference, GitHubRepositoryID,
		domain.CodeChangeKindPullRequest, 999, []byte(SyntheticCredential), "request-3",
	)
	require.Equal(t, ErrorNotFound, errorCategory(err))

	_, err = client.GetCodeChange(
		context.Background(), reference, GitHubRepositoryID,
		domain.CodeChangeKindMergeRequest, GitHubChangeNumber,
		[]byte(SyntheticCredential), "request-4",
	)
	require.Equal(t, ErrorInvalidReference, errorCategory(err))
}

func TestFixtureClientDelegatesOrdinaryOrigins(t *testing.T) {
	delegate := &delegateStub{provider: domain.RepositoryProviderGitHub}
	client, err := New(domain.RepositoryProviderGitHub, delegate)
	require.NoError(t, err)
	reference := domain.RepositoryReference{
		Provider: domain.RepositoryProviderGitHub, Origin: "https://github.com",
		PathWithNamespace: "example/repository", PathLookupKey: "example/repository",
		WebURL: "https://github.com/example/repository",
	}

	_, err = client.ResolveRepository(context.Background(), reference, []byte("credential"), "request-5")
	require.ErrorIs(t, err, errDelegated)
	require.True(t, delegate.resolved)

	_, err = client.GetCodeChange(
		context.Background(), reference, "17", domain.CodeChangeKindPullRequest,
		42, []byte("credential"), "request-6",
	)
	require.ErrorIs(t, err, errDelegated)
	require.True(t, delegate.readChange)
}

func TestFixtureClientRejectsMismatchedDelegate(t *testing.T) {
	_, err := New(
		domain.RepositoryProviderGitHub,
		gitlabintegration.NewClient(nil, time.Second),
	)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func errorCategory(err error) ErrorCategory {
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		return providerError.Category
	}
	return ""
}

var errDelegated = errors.New("delegated")

type delegateStub struct {
	provider   domain.RepositoryProvider
	resolved   bool
	readChange bool
}

func (s *delegateStub) Provider() domain.RepositoryProvider { return s.provider }

func (*delegateStub) ParseRepositoryURL(string) (domain.RepositoryReference, error) {
	return domain.RepositoryReference{}, errDelegated
}

func (*delegateStub) ParseCodeChangeURL(string) (domain.CodeChangeReference, error) {
	return domain.CodeChangeReference{}, errDelegated
}

func (s *delegateStub) ResolveRepository(
	context.Context, domain.RepositoryReference, []byte, string,
) (domain.RepositoryIdentity, error) {
	s.resolved = true
	return domain.RepositoryIdentity{}, errDelegated
}

func (s *delegateStub) GetCodeChange(
	context.Context, domain.RepositoryReference, string, domain.CodeChangeKind, int64, []byte, string,
) (domain.CodeChange, error) {
	s.readChange = true
	return domain.CodeChange{}, errDelegated
}
