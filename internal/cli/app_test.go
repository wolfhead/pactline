package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHelpIsSelfExplainingAndHasNoUnplannedCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code)
	require.Contains(t, stdout.String(), "Quick start:")
	require.Contains(t, stdout.String(), "Claim ID")
	require.NotContains(t, stdout.String(), "completion")

	stdout.Reset()
	code = ExecuteArgs(context.Background(), []string{"help", "identity"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code)
	require.Contains(t, stdout.String(), "Client Session ID is only audit")
	require.Contains(t, stdout.String(), "different Token cannot")

	stdout.Reset()
	code = ExecuteArgs(context.Background(), []string{"claim", "complete", "--help"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code)
	require.Contains(t, stdout.String(), "in_review.available")
	require.Contains(t, stdout.String(), "--task-version")

	stdout.Reset()
	code = ExecuteArgs(context.Background(), []string{"claim", "accept", "--help"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code)
	require.Contains(t, stdout.String(), "moves the Task to done")
	require.Contains(t, stdout.String(), "--task-version")

	stdout.Reset()
	code = ExecuteArgs(context.Background(), []string{"help", "workflow"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code)
	require.Contains(t, stdout.String(), "Review workflow")
	require.Contains(t, stdout.String(), "claim request-changes")
	require.Contains(t, stdout.String(), "claim accept")
}

func TestJSONFailureIsOneDocumentAndVerboseStaysOnStderr(t *testing.T) {
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"--json", "--verbose", "task", "list"}, strings.NewReader(""), &stdout, &stderr)
	require.Equal(t, 2, code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.Equal(t, false, envelope["ok"])
	require.NotContains(t, stdout.String(), "[verbose]")
}

func TestUnknownCommandAndWrongArgumentCountAreUsageErrors(t *testing.T) {
	for _, arguments := range [][]string{{"unknown"}, {"version", "extra"}} {
		var stdout, stderr bytes.Buffer
		code := ExecuteArgs(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr)
		require.Equal(t, 2, code)
		require.Contains(t, stderr.String(), "Error [USAGE]")
	}
}

func TestConfigPersistsSecretSecurelyAndNeverPrintsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pactline", "config.json")
	t.Setenv("PACTLINE_CONFIG", path)
	secret := "bb_pat_super-secret"
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"config", "set", "--server", "https://pactline.example", "--token-stdin"}, strings.NewReader(secret), &stdout, &stderr)
	require.Zero(t, code)
	require.NotContains(t, stdout.String()+stderr.String(), secret)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	stdout.Reset()
	code = ExecuteArgs(context.Background(), []string{"config", "show"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code)
	require.Contains(t, stdout.String(), "Token: configured")
	require.NotContains(t, stdout.String(), secret)
}

func TestConfigRefusesInsecureExistingDirectoryWithoutChangingItsMode(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(directory, 0o755))
	require.NoError(t, os.Chmod(directory, 0o755))
	path := filepath.Join(directory, "config.json")
	t.Setenv("PACTLINE_CONFIG", path)
	var stdout, stderr bytes.Buffer

	code := ExecuteArgs(context.Background(), []string{
		"config", "set", "--server", "https://pactline.example", "--token-stdin",
	}, strings.NewReader("secret"), &stdout, &stderr)

	require.Equal(t, 2, code)
	info, err := os.Stat(directory)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDifferentSessionCanContinueExplicitClaim(t *testing.T) {
	const claimID = "4e8c59cf-0af4-4af4-a55d-f2d2f930771c"
	var sessions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer token-a", r.Header.Get("Authorization"))
		sessions = append(sessions, r.Header.Get("Pactline-Client-Session-ID"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task":{"task_number":142,"version":4,"phase":"in_progress","activity":"working"},"claim":{"id":"`+claimID+`","task_number":142,"stage":"execution","status":"active","version":1},"progress":{"id":"progress"}}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")

	for _, session := range []string{"session-a", "session-b"} {
		var stdout, stderr bytes.Buffer
		code := ExecuteArgs(context.Background(), []string{"--session-id", session, "claim", "progress", claimID, "--message", "continued"}, strings.NewReader(""), &stdout, &stderr)
		require.Zero(t, code, stderr.String())
		require.Contains(t, stdout.String(), "Progress recorded")
	}
	require.Equal(t, []string{"session-a", "session-b"}, sessions)
}

func TestTaskClaimDefaultsToExecutionAndRejectsReviewBeforeCreatingClaim(t *testing.T) {
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			postCount++
		}
		_, _ = io.WriteString(w, `{"id":"task","number":142,"title":"Review me","version":4,"phase":"in_review","activity":"available"}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	t.Setenv("PACTLINE_SESSION_ID", "session-a")

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"task", "claim", "142", "--task-version", "4"}, strings.NewReader(""), &stdout, &stderr)
	require.Equal(t, 2, code)
	require.Zero(t, postCount)
	require.Contains(t, stderr.String(), "--stage execution does not match")
}

func TestAPIProblemRetainsMachineCodeAndRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"ACTIVE_CLAIM","detail":"Task already has a Claim","request_id":"req-42"}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"--json", "task", "list"}, strings.NewReader(""), &stdout, &stderr)
	require.Equal(t, 4, code)
	var envelope struct {
		Error APIError `json:"error"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.Equal(t, "ACTIVE_CLAIM", envelope.Error.Code)
	require.Equal(t, "req-42", envelope.Error.RequestID)
}

func TestContentRequiresOneExplicitSource(t *testing.T) {
	_, err := content("inline", "file", strings.NewReader(""), "message")
	require.Error(t, err)
	value, err := content("", "-", strings.NewReader("from stdin"), "message")
	require.NoError(t, err)
	require.Equal(t, "from stdin", value)
}

func TestCapabilitiesIsOfflineAndStable(t *testing.T) {
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"--json", "capabilities"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Protocol   int      `json:"protocol"`
			CLIVersion string   `json:"cli_version"`
			Features   []string `json:"features"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.True(t, envelope.OK)
	require.Equal(t, 1, envelope.Data.Protocol)
	require.Equal(t, Version, envelope.Data.CLIVersion)
	require.Equal(t, []string{
		"bounded_work_packets", "claim_progress", "claim_release", "execution_claims",
		"execution_completion", "execution_verification", "gitlab_merge_request_links",
		"repeatable_submission", "resolution_request", "review_acceptance",
		"review_claims", "review_request_changes", "success_metadata", "task_acceptance",
	}, envelope.Data.Features)
}

func TestJSONSuccessIncludesAvailableResponseMetadata(t *testing.T) {
	const claimID = "4e8c59cf-0af4-4af4-a55d-f2d2f930771c"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "fixed-key", r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-success")
		w.Header().Set("ETag", `"7"`)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	t.Setenv("PACTLINE_SESSION_ID", "session-a")
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"--json", "--idempotency-key", "fixed-key", "claim", "progress", claimID, "--message", "working",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	var envelope struct {
		Meta responseMeta `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.Equal(t, responseMeta{RequestID: "req-success", ETag: `"7"`, IdempotencyKey: "fixed-key"}, envelope.Meta)
}

func TestTaskListUsesServerClaimableStageFilters(t *testing.T) {
	var requests []*http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Clone(r.Context()))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/me" {
			_, _ = io.WriteString(w, `{"subject":{"id":"6a214c32-788d-423b-81cf-dac976e9c686"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"task", "list", "--project", "12", "--limit", "7"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Len(t, requests, 2)
	execution := requests[1].URL.Query()
	require.Equal(t, "execution", execution.Get("claimable_stage"))
	require.Equal(t, "6a214c32-788d-423b-81cf-dac976e9c686", execution.Get("assignee"))
	require.Equal(t, "12", execution.Get("project_number"))
	require.Equal(t, "7", execution.Get("limit"))
	require.Equal(t, "number", execution.Get("sort"))
	require.Equal(t, "asc", execution.Get("order"))

	requests = nil
	stdout.Reset()
	stderr.Reset()
	code = ExecuteArgs(context.Background(), []string{"task", "list", "--stage", "review"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Len(t, requests, 1)
	review := requests[0].URL.Query()
	require.Equal(t, "review", review.Get("claimable_stage"))
	require.Empty(t, review.Get("assignee"))
}

func TestCompactClaimShowUsesOneBoundedEndpoint(t *testing.T) {
	const claimID = "4e8c59cf-0af4-4af4-a55d-f2d2f930771c"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "/api/v1/claims/"+claimID+"/work-packet", r.URL.Path)
		require.Equal(t, "3", r.URL.Query().Get("thread_items_limit"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task":{"number":142,"title":"Compact","version":4,"phase":"in_progress","context":"ctx","expected_result":"done"},"claim":{"id":"`+claimID+`","stage":"execution","status":"active"},"criteria":[],"delivery":{"active_links":[]},"main_thread":{"thread":{},"items":[],"total_count":0,"returned_count":0,"truncated":false},"resolved_issue_thread_count":0}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"claim", "show", claimID, "--compact", "--thread-items-limit", "3"}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Equal(t, 1, requests)
	require.Contains(t, stdout.String(), "Main Thread: 0/0 items")
}

func TestClaimMergeRequestLinkUsesExplicitClaimAndVersion(t *testing.T) {
	const claimID = "4e8c59cf-0af4-4af4-a55d-f2d2f930771c"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/claims/"+claimID+"/merge-requests", r.URL.Path)
		require.Equal(t, `"9"`, r.Header.Get("If-Match"))
		require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "https://gitlab.example/team/repo/-/merge_requests/42", body["merge_request_url"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	t.Setenv("PACTLINE_SESSION_ID", "session-a")
	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"claim", "mr", "link", claimID,
		"--url", "https://gitlab.example/team/repo/-/merge_requests/42", "--task-version", "9",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Merge Request linked")
}

func TestClaimMergeRequestListAndUnlinkUseClaimAssociation(t *testing.T) {
	const claimID = "4e8c59cf-0af4-4af4-a55d-f2d2f930771c"
	const linkID = "f2e497d1-c860-482b-918f-c7de8006c788"
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/claims/"+claimID:
			_, _ = io.WriteString(w, `{"id":"`+claimID+`","task_number":142,"stage":"execution","status":"active","version":1}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/142/merge-requests":
			_, _ = io.WriteString(w, `{"active_links":[{"id":"`+linkID+`","web_url":"https://gitlab.example/team/repo/-/merge_requests/42","latest_observation":{"state":"opened"}}]}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/claims/"+claimID+"/merge-requests/"+linkID:
			require.Equal(t, `"9"`, r.Header.Get("If-Match"))
			require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	t.Setenv("PACTLINE_SESSION_ID", "session-a")

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"claim", "mr", "list", claimID}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), linkID)
	require.Equal(t, []string{
		"GET /api/v1/claims/" + claimID,
		"GET /api/v1/tasks/142/merge-requests",
	}, paths)

	paths = nil
	stdout.Reset()
	stderr.Reset()
	code = ExecuteArgs(context.Background(), []string{
		"claim", "mr", "unlink", claimID, linkID, "--task-version", "9",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Equal(t, []string{"DELETE /api/v1/claims/" + claimID + "/merge-requests/" + linkID}, paths)
}
