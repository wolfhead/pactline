package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testToken = "bb_pat_test-secret-token"

func newTestClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &client{
		server:     server.URL,
		token:      testToken,
		clientKind: "pactline-cli",
		sessionID:  "test-session",
		httpClient: server.Client(),
		verbose:    func(string, ...any) {},
	}
}

func oversizedBody(marker string) []byte {
	body := make([]byte, 0, maxResponseBodySize+1)
	for len(body) < maxResponseBodySize+1 {
		body = append(body, marker...)
	}
	return body[:maxResponseBodySize+1]
}

func TestRequestAcceptsResponseAtExactByteLimit(t *testing.T) {
	const marker = "EXACT-LIMIT-BODY-MARKER"
	payload := []byte(`{"ok":true,"message":"` + marker + `"}`)
	body := append(append([]byte{}, payload...), bytes.Repeat([]byte(" "), maxResponseBodySize-len(payload))...)
	require.Len(t, body, maxResponseBodySize)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-exact-limit")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	c := &client{
		server: server.URL, token: testToken, clientKind: "pactline-cli", sessionID: "test-session",
		httpClient: server.Client(), verbose: func(string, ...any) {},
	}

	raw, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/me", nil, 0, "", false)

	require.NoError(t, err)
	require.Len(t, raw, maxResponseBodySize)
	require.True(t, json.Valid(raw))
	require.Contains(t, string(raw), marker)
	require.Equal(t, "req-exact-limit", c.lastMeta.RequestID)
}

func TestRequestRejectsResponseOneByteOverLimitWithoutPartialJSON(t *testing.T) {
	const marker = "BODY-CONTENT-MARKER-"
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-oversized")
		_, _ = w.Write(oversizedBody(marker))
	})

	raw, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "key-1", false)

	require.Error(t, err)
	require.Nil(t, raw, "an oversized response must not be returned as partial JSON")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "RESPONSE_TOO_LARGE", apiErr.Code)
	require.Equal(t, fmt.Sprintf("The response exceeds the configured %d byte limit.", maxResponseBodySize), apiErr.Message)
	require.Equal(t, http.StatusOK, apiErr.Status)
	require.Equal(t, "req-oversized", apiErr.RequestID)
	require.Equal(t, "key-1", apiErr.Key)
	require.NotContains(t, err.Error(), marker)
}

func TestRequestRejectsOversizedErrorResponseWithoutLeakingBodyOrToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := []byte(`{"detail":"credential ` + testToken + ` BODY-LEAK-MARKER"} `)
		body = append(body, bytes.Repeat([]byte(" "), maxResponseBodySize+1-len(body))...)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(body)
	})

	raw, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "", false)

	require.Error(t, err)
	require.Nil(t, raw)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "RESPONSE_TOO_LARGE", apiErr.Code)
	require.Equal(t, http.StatusInternalServerError, apiErr.Status)
	require.NotContains(t, err.Error(), c.token)
	require.NotContains(t, err.Error(), "BODY-LEAK-MARKER")
}

func TestRequestDoesNotExposeBodyOrTokenForMalformedErrorResponse(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "partial_json_with_trailing_garbage", body: `{"detail":"credential ` + testToken + ` BODY-LEAK-MARKER"}` + " trailing-garbage"},
		{name: "plain_text", body: "not-json " + testToken + " BODY-LEAK-MARKER"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, testCase.body)
			})

			raw, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "", false)

			require.Error(t, err)
			require.Nil(t, raw)
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, "HTTP_ERROR", apiErr.Code)
			require.Equal(t, "502 Bad Gateway", apiErr.Message)
			require.NotContains(t, err.Error(), testToken)
			require.NotContains(t, err.Error(), "BODY-LEAK-MARKER")
		})
	}
}

func TestRequestSurfacesWellFormedProblemDetails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-header-123")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"code":"INVALID_REQUEST","detail":"Task version mismatch","request_id":"req-body-456"}`)
	})

	_, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "", false)

	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "INVALID_REQUEST", apiErr.Code)
	require.Equal(t, "Task version mismatch", apiErr.Message)
	require.Equal(t, "req-body-456", apiErr.RequestID)
}

func TestRequestRedactsTokenFromWellFormedErrorDetails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":"credential `+testToken+` was rejected"}`)
	})

	_, _, err := c.request(context.Background(), http.MethodGet, "/api/v1/tasks/142", nil, 0, "", false)

	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.NotContains(t, err.Error(), testToken)
	require.Contains(t, err.Error(), "credential [redacted] was rejected")
}

func TestCLIReportsOversizedResponseWithoutLeakingBodyOrToken(t *testing.T) {
	const marker = "BODY-CONTENT-MARKER-"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := []byte(`{"detail":"credential ` + testToken + ` ` + marker + `"} `)
		body = append(body, bytes.Repeat([]byte(" "), maxResponseBodySize+1-len(body))...)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("PACTLINE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("PACTLINE_SERVER", server.URL)
	t.Setenv("PACTLINE_TOKEN", testToken)

	var stdout, stderr bytes.Buffer
	code := ExecuteArgs(context.Background(), []string{"--verbose", "task", "show", "142"}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 5, code)
	require.Contains(t, stderr.String(), "Error [RESPONSE_TOO_LARGE]")
	require.Contains(t, stderr.String(), "byte limit")
	require.Contains(t, stderr.String(), "[verbose] response rejected")
	require.NotContains(t, stdout.String()+stderr.String(), testToken)
	require.NotContains(t, stdout.String()+stderr.String(), marker)
}
