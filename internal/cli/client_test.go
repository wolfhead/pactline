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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestClient(server string) *client {
	return &client{
		server:     server,
		token:      "bb_pat_test-token-never-in-diagnostics",
		clientKind: "test-client",
		sessionID:  "session-test",
		httpClient: &http.Client{},
		verbose:    func(string, ...any) {},
	}
}

func TestOversizedResponseIsNotRetriedAndReturnsStableError(t *testing.T) {
	const bodyMarker = "sensitive-payload-fragment"
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-oversized")
		_, _ = io.WriteString(w, `{"pad":"`+strings.Repeat(bodyMarker, maxResponseBodyBytes/len(bodyMarker)+2)+`"}`)
	}))
	t.Cleanup(server.Close)

	c := newTestClient(server.URL)
	_, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "", false)
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "RESPONSE_TOO_LARGE", apiErr.Code)
	require.Equal(t, http.StatusOK, apiErr.Status)
	require.Equal(t, "req-oversized", apiErr.RequestID)
	require.NotContains(t, err.Error(), c.token)
	require.NotContains(t, err.Error(), bodyMarker)
	// The oversized response is never retried: exactly one request is issued
	// and the error is returned after that first response.
	require.Equal(t, int64(1), requests.Load())
}

func TestResponseAtSizeLimitIsAccepted(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pad":"`+strings.Repeat("x", maxResponseBodyBytes-10)+`"}`)
	}))
	t.Cleanup(server.Close)

	c := newTestClient(server.URL)
	body, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "", false)
	require.NoError(t, err)
	require.Len(t, body, maxResponseBodyBytes)
	require.Equal(t, int64(1), requests.Load())
}

func TestOversizedCLIResponseFailsAfterSingleRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-cli-oversized")
		_, _ = io.WriteString(w, strings.Repeat("z", maxResponseBodyBytes+1))
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", "token-a")

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{
		"--json", "task", "show", "142", "--compact",
	}, strings.NewReader(""), &stdout, &stderr)
	require.Equal(t, 5, code, stderr.String())
	var envelope struct {
		Error APIError `json:"error"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
	require.Equal(t, "RESPONSE_TOO_LARGE", envelope.Error.Code)
	require.Equal(t, "req-cli-oversized", envelope.Error.RequestID)
	require.NotContains(t, stdout.String()+stderr.String(), "token-a")
	require.Equal(t, int64(1), requests.Load())
}
