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
	reviewClaimID     = "4e8c59cf-0af4-4af4-a55d-f2d2f930771c"
	reviewCriterionID = "f2e497d1-c860-482b-918f-c7de8006c788"
)

func configureReviewCLI(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", serverURL)
	t.Setenv("PACTLINE_TOKEN", "token-a")
	t.Setenv("PACTLINE_SESSION_ID", "review-session")
}

func TestTaskClaimReviewUsesExplicitSafetyAssertion(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"id":"task","number":142,"title":"Review me","version":8,"phase":"in_review","activity":"available"}`)
		case http.MethodPost:
			require.Equal(t, `"8"`, r.Header.Get("If-Match"))
			require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
			require.Equal(t, "review-session", r.Header.Get("Pactline-Client-Session-ID"))
			_, _ = io.WriteString(w, `{"task":{"task_number":142,"version":9,"phase":"in_review","activity":"working"},"claim":{"id":"`+reviewClaimID+`","task_number":142,"stage":"review","status":"active","version":1}}`)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)
	configureReviewCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"task", "claim", "142", "--stage", "review", "--task-version", "8",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Zero(t, code, stderr.String())
	require.Equal(t, []string{"GET /api/v1/tasks/142", "POST /api/v1/tasks/142/claims"}, requests)
	require.Contains(t, stdout.String(), "Stage: review")
}

func TestTaskClaimReviewRejectsNonReviewTaskWithoutMutation(t *testing.T) {
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"task","number":142,"title":"Execute me","version":4,"phase":"ready"}`)
	}))
	t.Cleanup(server.Close)
	configureReviewCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"task", "claim", "142", "--stage", "review", "--task-version", "4",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 2, code)
	require.Zero(t, postCount)
	require.Contains(t, stderr.String(), "--stage review requires in_review.available")
}

func TestTaskClaimRejectsUnexpectedReturnedStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"id":"task","number":142,"title":"Review me","version":8,"phase":"in_review","activity":"available"}`)
			return
		}
		_, _ = io.WriteString(w, `{"task":{"task_number":142,"version":9,"phase":"in_review","activity":"working"},"claim":{"id":"`+reviewClaimID+`","task_number":142,"stage":"execution","status":"active","version":1}}`)
	}))
	t.Cleanup(server.Close)
	configureReviewCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"task", "claim", "142", "--stage", "review", "--task-version", "8",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 5, code)
	require.Contains(t, stderr.String(), "server returned an unexpected execution Claim after --stage review")
	require.Contains(t, stderr.String(), "Release Claim "+reviewClaimID)
}

func TestClaimVerifyRecordsServerDerivedReviewAcceptance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/claims/"+reviewClaimID+"/criteria/"+reviewCriterionID+"/checks", r.URL.Path)
		require.Equal(t, `"9"`, r.Header.Get("If-Match"))
		require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, float64(2), body["criterion_revision"])
		require.Equal(t, "passed", body["outcome"])
		require.Equal(t, "Reviewed the frozen delivery and reran focused tests.", body["evidence"])
		require.NotContains(t, body, "purpose")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"check","criterion_id":"`+reviewCriterionID+`","criterion_revision":2,"outcome":"passed","evidence":"Reviewed the frozen delivery and reran focused tests.","purpose":"acceptance","task_claim_id":"`+reviewClaimID+`","review_cycle":1}`)
	}))
	t.Cleanup(server.Close)
	configureReviewCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"claim", "verify", reviewClaimID, reviewCriterionID,
		"--task-version", "9", "--criterion-revision", "2", "--outcome", "passed",
		"--evidence", "Reviewed the frozen delivery and reran focused tests.",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Zero(t, code, stderr.String())
	require.Contains(t, stdout.String(), "Acceptance evidence recorded")
}

func TestReviewOutcomeCommandsUseExplicitClaimVersionAndBody(t *testing.T) {
	tests := []struct {
		name, command, path, message, response, output string
	}{
		{
			name: "request changes", command: "request-changes", path: "request-changes",
			message:  "The error path still lacks coverage.",
			response: `{"task":{"task_number":142,"version":10,"phase":"in_progress","activity":"available"},"claim":{"id":"` + reviewClaimID + `","stage":"review","status":"completed","outcome":"changes_requested"}}`,
			output:   "Changes requested",
		},
		{
			name: "accept task", command: "accept", path: "accept",
			message:  "The acceptance contract is satisfied.",
			response: `{"task":{"task_number":142,"version":10,"phase":"done"},"claim":{"id":"` + reviewClaimID + `","stage":"review","status":"completed","outcome":"task_accepted"}}`,
			output:   "Task accepted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/api/v1/claims/"+reviewClaimID+"/"+test.path, r.URL.Path)
				require.Equal(t, `"9"`, r.Header.Get("If-Match"))
				require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
				require.Equal(t, "review-session", r.Header.Get("Pactline-Client-Session-ID"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, test.message, body["body"])
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.response)
			}))
			t.Cleanup(server.Close)
			configureReviewCLI(t, server.URL)

			var stdout, stderr bytes.Buffer
			code := ExecuteArgs(context.Background(), []string{
				"claim", test.command, reviewClaimID, "--task-version", "9", "--message", test.message,
			}, strings.NewReader(""), &stdout, &stderr)

			require.Zero(t, code, stderr.String())
			require.Contains(t, stdout.String(), test.output)
		})
	}
}

func TestReviewOutcomeRequiresExactlyOneDocumentedContentSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("validation must happen before the request")
	}))
	t.Cleanup(server.Close)
	configureReviewCLI(t, server.URL)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"claim", "accept", reviewClaimID, "--task-version", "9",
	}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 2, code)
	require.Contains(t, stderr.String(), "provide exactly one --message or --file")
}
