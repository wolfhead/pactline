package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestParseRepositoryURLNormalizesGitLabReferences(t *testing.T) {
	client := NewClient(nil, time.Second)
	reference, err := client.ParseRepositoryURL(" https://GitLab.EXAMPLE.com/team/platform/repo.git/ ")
	require.NoError(t, err)
	require.Equal(t, domain.RepositoryProviderGitLab, reference.Provider)
	require.Equal(t, "https://gitlab.example.com", reference.Origin)
	require.Equal(t, "team/platform/repo", reference.PathWithNamespace)
	require.Equal(t, "https://gitlab.example.com/team/platform/repo", reference.WebURL)

	for _, raw := range []string{
		"http://gitlab.example.com/team/repo",
		"https://token@example.com/team/repo",
		"https://gitlab.example.com/team/repo?ref=main",
		"https://gitlab.example.com/team/repo/-/tree/main",
		"https://gitlab.example.com/repo",
	} {
		_, err := client.ParseRepositoryURL(raw)
		require.Error(t, err, raw)
		require.Equal(t, ErrorInvalidReference, ErrorCategoryOf(err))
	}
}

func TestParseCodeChangeURLNormalizesGitLabMergeRequest(t *testing.T) {
	client := NewClient(nil, time.Second)
	reference, err := client.ParseCodeChangeURL(
		"https://gitlab.example.com/group/subgroup/repo/-/merge_requests/42",
	)
	require.NoError(t, err)
	require.Equal(t, domain.CodeChangeKindMergeRequest, reference.Kind)
	require.Equal(t, int64(42), reference.ChangeNumber)
	require.Equal(t, "group/subgroup/repo", reference.Repository.PathWithNamespace)

	for _, raw := range []string{
		"https://gitlab.example.com/group/repo/-/issues/42",
		"https://gitlab.example.com/group/repo/-/merge_requests/0",
		"https://gitlab.example.com/group/repo/-/merge_requests/42/diffs",
	} {
		_, err := client.ParseCodeChangeURL(raw)
		require.Error(t, err, raw)
		require.Equal(t, ErrorInvalidReference, ErrorCategoryOf(err))
	}
}

func TestResolveRepositoryUsesEncodedPathAndPrivateToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v4/projects/group%2Fsubgroup%2Frepo", r.URL.EscapedPath())
		require.Equal(t, "secret", r.Header.Get("PRIVATE-TOKEN"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":17,"path_with_namespace":"group/subgroup/repo","web_url":"https://gitlab.example/group/subgroup/repo","default_branch":"main"}`)
	}))
	defer server.Close()
	client := NewClient(server.Client(), time.Second)

	repository, err := client.ResolveRepository(
		context.Background(), domain.RepositoryReference{
			Provider: domain.RepositoryProviderGitLab, Origin: server.URL,
			PathWithNamespace: "group/subgroup/repo",
		}, []byte("secret"), "request-1",
	)
	require.NoError(t, err)
	require.Equal(t, "17", repository.ProviderRepositoryID)
	require.Equal(t, "group/subgroup/repo", repository.PathWithNamespace)
	require.Equal(t, "main", repository.DefaultBranch)
}

func TestGetCodeChangeReturnsConfirmedObservation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v4/projects/17/merge_requests/42", r.URL.Path)
		fmt.Fprint(w, `{
            "id":91,"iid":42,"title":"Implement evidence","state":"opened","draft":false,
            "source_branch":"feature","target_branch":"main","sha":"abc123",
            "merge_commit_sha":null,"merged_at":null,"updated_at":"2026-08-13T08:00:00Z",
            "web_url":"https://gitlab.example/group/repo/-/merge_requests/42"
        }`)
	}))
	defer server.Close()
	client := NewClient(server.Client(), time.Second)
	client.now = func() time.Time { return time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC) }

	codeChange, err := client.GetCodeChange(
		context.Background(), domain.RepositoryReference{Provider: domain.RepositoryProviderGitLab, Origin: server.URL}, "17", domain.CodeChangeKindMergeRequest,
		42, []byte("secret"), "request-2",
	)
	require.NoError(t, err)
	require.Equal(t, "91", codeChange.ProviderChangeID)
	require.Equal(t, domain.CodeChangeObservationConfirmed, codeChange.Observation.Status)
	require.Equal(t, "abc123", codeChange.Observation.HeadSHA)
}

func TestProviderErrorsAreClassified(t *testing.T) {
	for _, testCase := range []struct {
		status   int
		expected ErrorCategory
	}{
		{http.StatusUnauthorized, ErrorUnauthorized},
		{http.StatusForbidden, ErrorUnauthorized},
		{http.StatusNotFound, ErrorNotFound},
		{http.StatusTooManyRequests, ErrorUnreachable},
		{http.StatusBadGateway, ErrorUnreachable},
		{http.StatusBadRequest, ErrorProviderRejected},
	} {
		t.Run(fmt.Sprintf("status_%d", testCase.status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				fmt.Fprint(w, `{"message":"must not be returned"}`)
			}))
			defer server.Close()
			client := NewClient(server.Client(), time.Second)

			_, err := client.ResolveRepository(
				context.Background(), domain.RepositoryReference{
					Provider: domain.RepositoryProviderGitLab, Origin: server.URL, PathWithNamespace: "group/repo",
				}, []byte("secret"), "request-3",
			)
			require.Error(t, err)
			require.Equal(t, testCase.expected, ErrorCategoryOf(err))
			require.NotContains(t, err.Error(), "must not be returned")
		})
	}
}

func TestClientRejectsOversizedAndInvalidJSONResponses(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "invalid_json", body: "not-json"},
		{name: "oversized", body: strings.Repeat("x", maxResponseBodySize+1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, testCase.body)
			}))
			defer server.Close()
			client := NewClient(server.Client(), time.Second)

			_, err := client.ResolveRepository(
				context.Background(), domain.RepositoryReference{
					Provider: domain.RepositoryProviderGitLab, Origin: server.URL, PathWithNamespace: "group/repo",
				}, []byte("secret"), "request-4",
			)
			require.Error(t, err)
			require.Equal(t, ErrorProviderRejected, ErrorCategoryOf(err))
		})
	}
}

func TestClientRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("credential must not follow a cross-origin redirect")
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	client := NewClient(source.Client(), time.Second)

	_, err := client.ResolveRepository(
		context.Background(), domain.RepositoryReference{
			Provider: domain.RepositoryProviderGitLab, Origin: source.URL, PathWithNamespace: "group/repo",
		}, []byte("secret"), "request-5",
	)
	require.Error(t, err)
	require.Equal(t, ErrorUnreachable, ErrorCategoryOf(err))
}

func TestClientHonorsTimeout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	client := NewClient(server.Client(), 10*time.Millisecond)

	_, err := client.ResolveRepository(
		context.Background(), domain.RepositoryReference{
			Provider: domain.RepositoryProviderGitLab, Origin: server.URL, PathWithNamespace: "group/repo",
		}, []byte("secret"), "request-6",
	)
	require.Error(t, err)
	require.Equal(t, ErrorUnreachable, ErrorCategoryOf(err))
}
