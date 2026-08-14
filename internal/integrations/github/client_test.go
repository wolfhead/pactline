package github

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

func TestParseRepositoryURLNormalizesGitHubReferences(t *testing.T) {
	client := NewClient(nil, time.Second)
	reference, err := client.ParseRepositoryURL(" https://GitHub.COM/Owner/Repo.git/ ")
	require.NoError(t, err)
	require.Equal(t, domain.RepositoryProviderGitHub, reference.Provider)
	require.Equal(t, "https://github.com", reference.Origin)
	require.Equal(t, "Owner/Repo", reference.PathWithNamespace)
	require.Equal(t, "owner/repo", reference.PathLookupKey)
	require.Equal(t, "https://github.com/Owner/Repo", reference.WebURL)

	for _, raw := range []string{
		"http://github.com/owner/repo",
		"https://token@example.test/owner/repo",
		"https://github.com/owner/repo?tab=readme",
		"https://github.com/owner/repo/tree/main",
		"https://github.com/repo",
		"https://github.com/owner/repo/extra",
	} {
		_, err := client.ParseRepositoryURL(raw)
		require.Error(t, err, raw)
		require.Equal(t, ErrorInvalidReference, ErrorCategoryOf(err))
	}
}

func TestParseCodeChangeURLNormalizesGitHubPullRequest(t *testing.T) {
	client := NewClient(nil, time.Second)
	reference, err := client.ParseCodeChangeURL("https://github.example/Owner/Repo/pull/42")
	require.NoError(t, err)
	require.Equal(t, domain.CodeChangeKindPullRequest, reference.Kind)
	require.Equal(t, int64(42), reference.ChangeNumber)
	require.Equal(t, "Owner/Repo", reference.Repository.PathWithNamespace)

	for _, raw := range []string{
		"https://github.com/owner/repo/issues/42",
		"https://github.com/owner/repo/pull/0",
		"https://github.com/owner/repo/pull/42/files",
		"https://github.com/owner/repo/pull/42?diff=split",
	} {
		_, err := client.ParseCodeChangeURL(raw)
		require.Error(t, err, raw)
		require.Equal(t, ErrorInvalidReference, ErrorCategoryOf(err))
	}
}

func TestAPIEndpointsSupportGitHubDotComAndEnterprise(t *testing.T) {
	reference := domain.RepositoryReference{Origin: "https://github.com", PathWithNamespace: "owner/repo"}
	endpoint, err := repositoryEndpoint(reference)
	require.NoError(t, err)
	require.Equal(t, "https://api.github.com/repos/owner/repo", endpoint)

	reference.Origin = "https://github.example"
	endpoint, err = pullRequestEndpoint(reference, 9)
	require.NoError(t, err)
	require.Equal(t, "https://github.example/api/v3/repos/owner/repo/pulls/9", endpoint)
}

func TestResolveRepositoryUsesGitHubHeaders(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/repos/Owner/Repo", r.URL.Path)
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		require.Equal(t, "2022-11-28", r.Header.Get("X-GitHub-Api-Version"))
		require.Equal(t, "Pactline/1.0", r.Header.Get("User-Agent"))
		fmt.Fprintf(w, `{"id":17,"full_name":"Owner/Repo","html_url":"%s/Owner/Repo","default_branch":"main"}`, serverURL(r))
	}))
	defer server.Close()
	client := NewClient(server.Client(), time.Second)
	reference := domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "Owner/Repo"}

	repository, err := client.ResolveRepository(context.Background(), reference, []byte("secret"), "request-1")
	require.NoError(t, err)
	require.Equal(t, "17", repository.ProviderRepositoryID)
	require.Equal(t, "Owner/Repo", repository.PathWithNamespace)
	require.Equal(t, "main", repository.DefaultBranch)
}

func TestGetCodeChangeReturnsConfirmedObservation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/repos/Owner/Repo/pulls/42", r.URL.Path)
		fmt.Fprintf(w, `{
            "id":91,"number":42,"title":"Implement evidence","state":"closed","draft":false,
            "head":{"ref":"feature","sha":"abc123"},
            "base":{"ref":"main","repo":{"id":17,"full_name":"Owner/Repo"}},
            "merge_commit_sha":"def456","merged_at":"2026-08-14T08:30:00Z",
            "updated_at":"2026-08-14T08:45:00Z","html_url":"%s/Owner/Repo/pull/42"
        }`, serverURL(r))
	}))
	defer server.Close()
	client := NewClient(server.Client(), time.Second)
	client.now = func() time.Time { return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC) }
	reference := domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "Owner/Repo"}

	codeChange, err := client.GetCodeChange(context.Background(), reference, "17", domain.CodeChangeKindPullRequest, 42, []byte("secret"), "request-2")
	require.NoError(t, err)
	require.Equal(t, "91", codeChange.ProviderChangeID)
	require.Equal(t, domain.CodeChangeStateMerged, codeChange.Observation.State)
	require.Equal(t, "abc123", codeChange.Observation.HeadSHA)
}

func TestGetCodeChangeMapsOpenState(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
            "id":91,"number":42,"title":"Open change","state":"open","draft":true,
            "head":{"ref":"feature","sha":"abc123"},
            "base":{"ref":"main","repo":{"id":17,"full_name":"Owner/Repo"}},
            "merge_commit_sha":null,"merged_at":null,
            "updated_at":"2026-08-14T08:45:00Z","html_url":"%s/Owner/Repo/pull/42"
        }`, serverURL(r))
	}))
	defer server.Close()
	client := NewClient(server.Client(), time.Second)
	reference := domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "Owner/Repo"}
	codeChange, err := client.GetCodeChange(context.Background(), reference, "17", domain.CodeChangeKindPullRequest, 42, []byte("secret"), "request-open")
	require.NoError(t, err)
	require.Equal(t, domain.CodeChangeStateOpened, codeChange.Observation.State)
	require.True(t, codeChange.Observation.Draft)
}

func TestGetCodeChangeRejectsRepositoryIdentityMismatch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
            "id":91,"number":42,"title":"Wrong repository","state":"open","draft":false,
            "head":{"ref":"feature","sha":"abc123"},
            "base":{"ref":"main","repo":{"id":18,"full_name":"other/repo"}},
            "merge_commit_sha":null,"merged_at":null,"updated_at":"2026-08-14T08:45:00Z",
            "html_url":"%s/other/repo/pull/42"
        }`, serverURL(r))
	}))
	defer server.Close()
	client := NewClient(server.Client(), time.Second)
	reference := domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "Owner/Repo"}

	_, err := client.GetCodeChange(context.Background(), reference, "17", domain.CodeChangeKindPullRequest, 42, []byte("secret"), "request-3")
	require.Error(t, err)
	require.Equal(t, ErrorProviderRejected, ErrorCategoryOf(err))
}

func TestProviderErrorsAreClassified(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		status   int
		headers  map[string]string
		expected ErrorCategory
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, expected: ErrorUnauthorized},
		{name: "forbidden", status: http.StatusForbidden, expected: ErrorUnauthorized},
		{name: "rate_limited_forbidden", status: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0"}, expected: ErrorUnreachable},
		{name: "not_found", status: http.StatusNotFound, expected: ErrorNotFound},
		{name: "too_many_requests", status: http.StatusTooManyRequests, expected: ErrorUnreachable},
		{name: "bad_gateway", status: http.StatusBadGateway, expected: ErrorUnreachable},
		{name: "bad_request", status: http.StatusBadRequest, expected: ErrorProviderRejected},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for name, value := range testCase.headers {
					w.Header().Set(name, value)
				}
				w.WriteHeader(testCase.status)
				fmt.Fprint(w, `{"message":"must not be returned"}`)
			}))
			defer server.Close()
			client := NewClient(server.Client(), time.Second)
			_, err := client.ResolveRepository(context.Background(), domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "owner/repo"}, []byte("secret"), "request-4")
			require.Error(t, err)
			require.Equal(t, testCase.expected, ErrorCategoryOf(err))
			require.NotContains(t, err.Error(), "must not be returned")
		})
	}
}

func TestClientRejectsOversizedAndInvalidJSONResponses(t *testing.T) {
	for _, body := range []string{"not-json", strings.Repeat("x", maxResponseBodySize+1)} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
		client := NewClient(server.Client(), time.Second)
		_, err := client.ResolveRepository(context.Background(), domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "owner/repo"}, []byte("secret"), "request-5")
		server.Close()
		require.Error(t, err)
		require.Equal(t, ErrorProviderRejected, ErrorCategoryOf(err))
	}
}

func TestClientRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("credential must not follow a cross-origin redirect")
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	client := NewClient(source.Client(), time.Second)
	_, err := client.ResolveRepository(context.Background(), domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: source.URL, PathWithNamespace: "owner/repo"}, []byte("secret"), "request-6")
	require.Error(t, err)
	require.Equal(t, ErrorUnreachable, ErrorCategoryOf(err))
}

func TestClientHonorsTimeout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { time.Sleep(100 * time.Millisecond); fmt.Fprint(w, `{}`) }))
	defer server.Close()
	client := NewClient(server.Client(), 10*time.Millisecond)
	_, err := client.ResolveRepository(context.Background(), domain.RepositoryReference{Provider: domain.RepositoryProviderGitHub, Origin: server.URL, PathWithNamespace: "owner/repo"}, []byte("secret"), "request-7")
	require.Error(t, err)
	require.Equal(t, ErrorUnreachable, ErrorCategoryOf(err))
}

func serverURL(r *http.Request) string { return "https://" + r.Host }
