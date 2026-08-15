package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const clientTestToken = "bb_pat_cli-test-token"

// paddedJSONDocument returns a valid JSON object of exactly size bytes:
// {"ok":true,"pad":"<inner><filler>"}, with a uniform filler character.
func paddedJSONDocument(size int, inner string) []byte {
	const head = `{"ok":true,"pad":"`
	const tail = `"}`
	required := len(head) + len(inner) + len(tail)
	if size < required {
		panic("paddedJSONDocument: size too small")
	}
	payload := head + inner + strings.Repeat("x", size-required) + tail
	if len(payload) != size {
		panic("paddedJSONDocument: size math")
	}
	return []byte(payload)
}

func newTestClient(t *testing.T, handler http.Handler, verbose func(string, ...any)) *client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	if verbose == nil {
		verbose = func(string, ...any) {}
	}
	return &client{
		server:     server.URL,
		token:      clientTestToken,
		clientKind: "cli-test",
		sessionID:  "cli-test-session",
		httpClient: server.Client(),
		verbose:    verbose,
	}
}

func TestRequestAtExactByteLimitSucceeds(t *testing.T) {
	payload := paddedJSONDocument(MaxResponseBytes, "")
	require.Len(t, payload, MaxResponseBytes)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+clientTestToken, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}), nil)

	body, _, err := client.request(context.Background(), http.MethodGet, "/api/v1/tasks/1", nil, 0, "", false)
	require.NoError(t, err)
	require.Equal(t, payload, []byte(body))
}

func TestRequestOneByteOverLimitFailsStablyWithoutPartialJSON(t *testing.T) {
	payload := paddedJSONDocument(MaxResponseBytes, "")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte(" "))
	}), nil)

	body, _, err := client.request(context.Background(), http.MethodGet, "/api/v1/tasks/1", nil, 0, "", false)
	require.Error(t, err)
	require.Nil(t, body, "oversized responses must not return partial JSON")
	require.ErrorIs(t, err, ErrResponseTooLarge)
	var apiError *APIError
	require.ErrorAs(t, err, &apiError)
	require.Equal(t, "RESPONSE_TOO_LARGE", apiError.Code)
	require.Equal(t, fmt.Sprintf("response body exceeds the %d-byte limit", MaxResponseBytes), apiError.Message)

	// The oversized-response error must be deterministic across repeats.
	body, _, err = client.request(context.Background(), http.MethodGet, "/api/v1/tasks/1", nil, 0, "", false)
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.Nil(t, body)
	var repeat *APIError
	require.ErrorAs(t, err, &repeat)
	require.Equal(t, apiError.Message, repeat.Message)
	require.Equal(t, apiError.Code, repeat.Code)
}

func TestOversizedErrorResponseNeverLeaksTokenOrBodyContent(t *testing.T) {
	const marker = "SENSITIVE_ERROR_BODY_MARKER"
	prefix := `{"code":"LEAK","detail":"` + clientTestToken + " " + marker + `"`
	payload := append([]byte(prefix), bytes.Repeat([]byte("x"), MaxResponseBytes-len(prefix)+1)...)
	require.Greater(t, len(payload), MaxResponseBytes)

	var logs []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-oversized")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(payload)
	}), func(format string, values ...any) {
		logs = append(logs, fmt.Sprintf(format, values...))
	})

	body, _, err := client.request(context.Background(), http.MethodGet, "/api/v1/me", nil, 0, "", false)
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.Nil(t, body)
	for _, diagnostic := range append([]string{err.Error()}, logs...) {
		require.NotContains(t, diagnostic, clientTestToken)
		require.NotContains(t, diagnostic, marker)
	}
}

func TestMalformedErrorResponseFallsBackToStatusWithoutBodyContent(t *testing.T) {
	const marker = "SENSITIVE_ERROR_BODY_MARKER"
	cases := map[string]string{
		"truncated json":  `{"code":"LEAK","detail":"` + clientTestToken + " " + marker + `"`,
		"trailing data":   `{"code":"LEAK","detail":"` + clientTestToken + " " + marker + `"}` + "garbage",
		"non-object json": `"` + clientTestToken + " " + marker + `"`,
		"not json":        "<html>" + clientTestToken + " " + marker + "</html>",
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(payload))
			}), nil)

			body, _, err := client.request(context.Background(), http.MethodGet, "/api/v1/me", nil, 0, "", false)
			require.Error(t, err)
			require.Nil(t, body)
			var apiError *APIError
			require.ErrorAs(t, err, &apiError)
			require.Equal(t, "HTTP_ERROR", apiError.Code)
			require.Equal(t, "502 Bad Gateway", apiError.Message)
			require.NotContains(t, err.Error(), clientTestToken)
			require.NotContains(t, err.Error(), marker)
		})
	}
}

func TestWellFormedProblemDocumentStillDrivesTheError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"ACTIVE_CLAIM","detail":"Task already has a Claim","request_id":"req-42"}`))
	}), nil)

	_, _, err := client.request(context.Background(), http.MethodGet, "/api/v1/tasks/1", nil, 0, "", false)
	var apiError *APIError
	require.ErrorAs(t, err, &apiError)
	require.Equal(t, "ACTIVE_CLAIM", apiError.Code)
	require.Equal(t, "Task already has a Claim", apiError.Message)
	require.Equal(t, "req-42", apiError.RequestID)
}

func TestNetworkErrorRedactsTokenAndURLUserinfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := strings.Replace(server.URL, "http://", "http://"+clientTestToken+"@", 1)
	server.Close()

	client := &client{
		server:     closedURL,
		token:      clientTestToken,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		verbose:    func(string, ...any) {},
	}
	_, _, err := client.request(context.Background(), http.MethodGet, "/api/v1/me", nil, 0, "", false)
	require.Error(t, err)
	var apiError *APIError
	require.ErrorAs(t, err, &apiError)
	require.Equal(t, "NETWORK_ERROR", apiError.Code)
	require.NotContains(t, err.Error(), clientTestToken)
}
