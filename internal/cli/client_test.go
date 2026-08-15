package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOversizedResponseReturnsStableErrorWithoutRetry(t *testing.T) {
	const (
		token          = "secret-token"
		idempotencyKey = "safe-idempotency-key"
		requestID      = "safe-request-id"
		responseSecret = "secret-response-content"
	)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Content-Length", fmt.Sprint(maxResponseBodyBytes+1))
		_, _ = io.WriteString(w, responseSecret)
		padding := strings.Repeat("x", int(maxResponseBodyBytes+1)-len(responseSecret))
		_, _ = io.WriteString(w, padding)
	}))
	t.Cleanup(server.Close)

	var diagnostics bytes.Buffer
	client := &client{
		server:     server.URL,
		token:      token,
		httpClient: &http.Client{},
		verbose: func(format string, values ...any) {
			_, _ = fmt.Fprintf(&diagnostics, format, values...)
		},
	}

	body, headers, err := client.request(
		context.Background(), http.MethodPost, "/oversized", map[string]string{"value": "request"},
		0, idempotencyKey, true,
	)

	require.Nil(t, body)
	require.Equal(t, requestID, headers.Get("X-Request-ID"))
	require.Equal(t, 1, requestCount)
	var apiError *APIError
	require.True(t, errors.As(err, &apiError))
	require.Equal(t, http.StatusOK, apiError.Status)
	require.Equal(t, "RESPONSE_TOO_LARGE", apiError.Code)
	require.Equal(t, "The server response exceeds the 8 MiB size limit.", apiError.Message)
	require.Equal(t, requestID, apiError.RequestID)
	require.Equal(t, idempotencyKey, apiError.Key)
	require.True(t, client.lastMeta.empty())
	require.NotContains(t, diagnostics.String(), token)
	require.NotContains(t, diagnostics.String(), responseSecret)
}
