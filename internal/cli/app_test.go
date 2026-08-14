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

func TestTaskClaimRejectsReviewBeforeCreatingClaim(t *testing.T) {
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
	require.Contains(t, stderr.String(), "does not claim Task review work")
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
