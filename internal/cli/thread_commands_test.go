package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	threadTaskNumber = "142"
	mainThreadID     = "5c7ea4a2-9ef8-4a0e-9fb7-f4f40c7b9232"
	issueThreadID    = "76a195c0-4c0f-4f04-ab4e-7ae53cf4ef34"
	threadItemID     = "03bb1743-a1d5-47e8-ae79-c459a43af90c"
	threadReplyID    = "d6ac82d9-93ae-4106-8ac4-3a27e3624200"
	mentionedUserID  = "07884986-d4f3-45b8-ae8a-a3eda776af5b"
)

func configureThreadCLI(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", serverURL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	t.Setenv("PACTLINE_SESSION_ID", "thread-session")
}

func TestTaskThreadsAndThreadItemsUseBoundedExplicitTargets(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/tasks/142/threads":
			_, _ = io.WriteString(w, `{"items":[{"id":"`+mainThreadID+`","task_id":"task-id","role":"main","version":1,"created_at":"2026-08-14T00:00:00Z","updated_at":"2026-08-14T00:00:00Z"},{"id":"`+issueThreadID+`","task_id":"task-id","role":"issue","issue_type":"decision_required","issue_status":"open","opened_from_phase":"in_progress","version":2,"created_at":"2026-08-14T00:01:00Z","updated_at":"2026-08-14T00:02:00Z"}]}`)
		case "/api/v1/threads/" + issueThreadID + "/items":
			require.Equal(t, "2", r.URL.Query().Get("limit"))
			require.Equal(t, "next page", r.URL.Query().Get("cursor"))
			_, _ = io.WriteString(w, `{"items":[{"id":"`+threadItemID+`","thread_id":"`+issueThreadID+`","kind":"message","author":{"type":"agent","ref":"api-token/worker"},"body":"Please choose the release strategy.","mentioned_user_ids":[],"version":1,"created_at":"2026-08-14T00:02:00Z","updated_at":"2026-08-14T00:02:00Z"}],"next_cursor":"later"}`)
		default:
			t.Fatalf("unexpected request %s", r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)
	configureThreadCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"task", "threads", threadTaskNumber}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Main Thread")
	require.Contains(t, stdout.String(), "decision_required")
	require.Contains(t, stdout.String(), issueThreadID)

	stdout.Reset()
	stderr.Reset()
	code = ExecuteArgs(context.Background(), []string{
		"--json", "thread", "items", issueThreadID, "--limit", "2", "--cursor", "next page",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	var envelope struct {
		Data struct {
			Items      []threadItem `json:"items"`
			NextCursor string       `json:"next_cursor"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.Len(t, envelope.Data.Items, 1)
	require.Equal(t, threadItemID, envelope.Data.Items[0].ID)
	require.Equal(t, "later", envelope.Data.NextCursor)
	require.Equal(t, []string{
		"GET /api/v1/tasks/142/threads",
		"GET /api/v1/threads/" + issueThreadID + "/items?cursor=next+page&limit=2",
	}, requests)
}

func TestThreadItemRenderingCoversEmptyTombstoneAndStructuredResolution(t *testing.T) {
	var output bytes.Buffer
	printThreadItem(&output, threadItem{
		ID: threadItemID, Kind: "message", Author: actorSummary{Type: "user", UserID: mentionedUserID},
		Body: "removed", Version: 2, DeletedAt: "2026-08-14T00:03:00Z",
	})
	require.Contains(t, output.String(), "[deleted]")
	require.NotContains(t, output.String(), "removed")

	output.Reset()
	printThreadItem(&output, threadItem{
		ID: threadItemID, Kind: "issue_resolution", Author: actorSummary{Type: "agent", Ref: "api-token/resolver"},
		IssueResolution: json.RawMessage(`{"issue_type":"decision_required","request":"Choose a rollout","resolution":"Use staged rollout"}`),
		Version:         1,
	})
	require.Contains(t, output.String(), "Issue resolved (decision_required)")
	require.Contains(t, output.String(), "Request: Choose a rollout")
	require.Contains(t, output.String(), "Resolution: Use staged rollout")

	output.Reset()
	printThread(&output, taskThread{ID: mainThreadID, Role: "main", Version: 1})
	require.Contains(t, output.String(), "Main Thread")
}

func TestThreadPostCarriesReplyMentionsProvenanceAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/threads/"+issueThreadID+"/items", r.URL.Path)
		require.Equal(t, "thread-session", r.Header.Get("Pactline-Client-Session-ID"))
		require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
		var body struct {
			Kind             string   `json:"kind"`
			Body             string   `json:"body"`
			ReplyToItemID    string   `json:"reply_to_item_id"`
			MentionedUserIDs []string `json:"mentioned_user_ids"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "message", body.Kind)
		require.Equal(t, "Decision context", body.Body)
		require.Equal(t, threadReplyID, body.ReplyToItemID)
		require.Equal(t, []string{mentionedUserID}, body.MentionedUserIDs)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"`+threadItemID+`","thread_id":"`+issueThreadID+`","kind":"message","author":{"type":"agent","ref":"api-token/worker"},"body":"Decision context","reply_to_item_id":"`+threadReplyID+`","mentioned_user_ids":["`+mentionedUserID+`"],"version":1,"created_at":"2026-08-14T00:02:00Z","updated_at":"2026-08-14T00:02:00Z"}`)
	}))
	t.Cleanup(server.Close)
	configureThreadCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"thread", "post", issueThreadID, "--message", "Decision context",
		"--reply-to", threadReplyID, "--mention", mentionedUserID, "--mention", mentionedUserID,
	}, strings.NewReader(""), &stdout, &stderr)

	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Thread message posted")
	require.Contains(t, stdout.String(), threadItemID)
}

func TestThreadEditAndDeleteUseItemVersion(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		require.Equal(t, `"3"`, r.Header.Get("If-Match"))
		require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPatch {
			var body struct {
				Body             string   `json:"body"`
				MentionedUserIDs []string `json:"mentioned_user_ids"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "Updated context", body.Body)
			require.Empty(t, body.MentionedUserIDs)
			_, _ = io.WriteString(w, `{"id":"`+threadItemID+`","thread_id":"`+issueThreadID+`","kind":"message","author":{"type":"agent","ref":"api-token/worker"},"body":"Updated context","mentioned_user_ids":[],"version":4,"created_at":"2026-08-14T00:02:00Z","updated_at":"2026-08-14T00:03:00Z"}`)
			return
		}
		require.Equal(t, http.MethodDelete, r.Method)
		_, _ = io.WriteString(w, `{"id":"`+threadItemID+`","thread_id":"`+issueThreadID+`","kind":"message","author":{"type":"agent","ref":"api-token/worker"},"mentioned_user_ids":[],"version":4,"created_at":"2026-08-14T00:02:00Z","updated_at":"2026-08-14T00:03:00Z","deleted_at":"2026-08-14T00:03:00Z"}`)
	}))
	t.Cleanup(server.Close)
	configureThreadCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"thread", "edit", threadItemID, "--item-version", "3", "--message", "Updated context",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Thread message updated")

	stdout.Reset()
	stderr.Reset()
	code = ExecuteArgs(context.Background(), []string{
		"thread", "delete", threadItemID, "--item-version", "3",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Thread message deleted")
	require.Equal(t, []string{
		"PATCH /api/v1/thread-items/" + threadItemID,
		"DELETE /api/v1/thread-items/" + threadItemID,
	}, requests)
}

func TestIssueResolveUsesTaskAndThreadVersionsWithoutClaim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/tasks/142/issues/"+issueThreadID+"/resolve", r.URL.Path)
		require.Equal(t, `"7"`, r.Header.Get("If-Match"))
		require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
		var body struct {
			ThreadVersion int64  `json:"thread_version"`
			Resolution    string `json:"resolution"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, int64(2), body.ThreadVersion)
		require.Equal(t, "Use the staged rollout.", body.Resolution)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task":{"task_number":142,"version":8,"phase":"in_progress","activity":"available"},"issue":{"id":"`+issueThreadID+`","task_id":"task-id","role":"issue","issue_type":"decision_required","issue_status":"resolved","resolved_by":{"type":"agent","ref":"api-token/resolver"},"version":3,"created_at":"2026-08-14T00:01:00Z","updated_at":"2026-08-14T00:04:00Z","resolved_at":"2026-08-14T00:04:00Z"}}`)
	}))
	t.Cleanup(server.Close)
	configureThreadCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"issue", "resolve", threadTaskNumber, issueThreadID,
		"--task-version", "7", "--thread-version", "2", "--message", "Use the staged rollout.",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Issue resolved")
	require.Contains(t, stdout.String(), "in_progress.available")
	require.Contains(t, stdout.String(), "Task version: 8")
	require.Contains(t, stdout.String(), "Issue version: 3")
	require.Contains(t, stdout.String(), "Resolved by: agent/api-token/resolver")
}

func TestThreadAndIssueValidationHappensBeforeMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("validation must happen before the request")
	}))
	t.Cleanup(server.Close)
	configureThreadCLI(t, server.URL)

	for _, arguments := range [][]string{
		{"thread", "items", "not-a-uuid"},
		{"thread", "post", issueThreadID, "--message", "x", "--reply-to", "bad"},
		{"thread", "edit", threadItemID, "--item-version", "0", "--message", "x"},
		{"issue", "resolve", threadTaskNumber, issueThreadID, "--task-version", "7", "--thread-version", "0", "--message", "x"},
	} {
		var stdout, stderr bytes.Buffer
		code := ExecuteArgs(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr)
		require.Equal(t, 2, code, "%v: %s", arguments, stderr.String())
	}
}
