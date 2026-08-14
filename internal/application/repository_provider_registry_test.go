package application

import (
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	githubintegration "github.com/wolfhead/pactline/internal/integrations/github"
	gitlabintegration "github.com/wolfhead/pactline/internal/integrations/gitlab"

	"github.com/stretchr/testify/require"
)

func TestRepositoryProviderRegistryReturnsAllSyntacticCandidates(t *testing.T) {
	registry, err := NewRepositoryProviderRegistry(
		gitlabintegration.NewClient(nil, time.Second),
		githubintegration.NewClient(nil, time.Second),
	)
	require.NoError(t, err)

	candidates, err := registry.RepositoryURLCandidates("https://code.example/owner/repo")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, domain.RepositoryProviderGitHub, candidates[0].Provider)
	require.Equal(t, domain.RepositoryProviderGitLab, candidates[1].Provider)

	changes, err := registry.CodeChangeURLCandidates("https://code.example/owner/repo/pull/42")
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, domain.RepositoryProviderGitHub, changes[0].Repository.Provider)
}

func TestRepositoryProviderRegistryRejectsURLsWithoutCandidate(t *testing.T) {
	registry, err := NewRepositoryProviderRegistry(
		gitlabintegration.NewClient(nil, time.Second),
		githubintegration.NewClient(nil, time.Second),
	)
	require.NoError(t, err)

	_, err = registry.CodeChangeURLCandidates("https://code.example/owner/repo/issues/42")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}
