package domain_test

import (
	"testing"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestParseGitLabRepositoryURL(t *testing.T) {
	reference, err := domain.ParseGitLabRepositoryURL(" https://GitLab.EXAMPLE.com/team/platform/repo.git/ ")
	require.NoError(t, err)
	require.Equal(t, "https://gitlab.example.com", reference.Origin)
	require.Equal(t, "team/platform/repo", reference.PathWithNamespace)
	require.Equal(t, "team/platform/repo", reference.PathLookupKey)
	require.Equal(t, "https://gitlab.example.com/team/platform/repo", reference.WebURL)
}

func TestParseGitLabRepositoryURLRejectsSubpagesAndUnsafeURLParts(t *testing.T) {
	for _, raw := range []string{
		"http://gitlab.example.com/team/repo",
		"https://token@example.com/team/repo",
		"https://gitlab.example.com/team/repo?ref=main",
		"https://gitlab.example.com/team/repo#readme",
		"https://gitlab.example.com/team/repo/-/tree/main",
		"https://gitlab.example.com/repo",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := domain.ParseGitLabRepositoryURL(raw)
			require.ErrorIs(t, err, domain.ErrInvalidInput)
		})
	}
}

func TestParseGitLabMergeRequestURL(t *testing.T) {
	reference, err := domain.ParseGitLabMergeRequestURL(
		"https://gitlab.example.com/group/subgroup/repo/-/merge_requests/42",
	)
	require.NoError(t, err)
	require.Equal(t, int64(42), reference.IID)
	require.Equal(t, "group/subgroup/repo", reference.Repository.PathWithNamespace)
	require.Equal(t,
		"https://gitlab.example.com/group/subgroup/repo/-/merge_requests/42",
		reference.WebURL,
	)
}

func TestParseGitLabMergeRequestURLRejectsNonMRPages(t *testing.T) {
	for _, raw := range []string{
		"https://gitlab.example.com/group/repo/-/issues/42",
		"https://gitlab.example.com/group/repo/-/merge_requests/0",
		"https://gitlab.example.com/group/repo/-/merge_requests/42/diffs",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := domain.ParseGitLabMergeRequestURL(raw)
			require.ErrorIs(t, err, domain.ErrInvalidInput)
		})
	}
}
