package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const fleetHiddenResponseLimit = 8 * 1024 * 1024

func TestFleetHiddenResponseLimitAndRedaction(t *testing.T) {
	t.Parallel()
	credential := "fleet-hidden-bearer-token"
	tests := []struct {
		name       string
		status     int
		body       []byte
		wantCode   string
		wantLength int
	}{
		{name: "exact limit succeeds", status: http.StatusOK, body: append(append([]byte{'"'}, bytes.Repeat([]byte{'a'}, fleetHiddenResponseLimit-2)...), '"'), wantLength: fleetHiddenResponseLimit},
		{name: "one byte over fails", status: http.StatusOK, body: append(append([]byte{'"'}, bytes.Repeat([]byte{'b'}, fleetHiddenResponseLimit-1)...), '"'), wantCode: "RESPONSE_TOO_LARGE"},
		{name: "oversized malformed error is redacted", status: http.StatusInternalServerError, body: append([]byte("not-json "+credential+" "), bytes.Repeat([]byte{'x'}, fleetHiddenResponseLimit)...), wantCode: "RESPONSE_TOO_LARGE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write(test.body)
			}))
			t.Cleanup(server.Close)
			client := &client{server: server.URL, token: credential, clientKind: "fleet-hidden", sessionID: "hidden", httpClient: server.Client(), verbose: func(string, ...any) {}}
			body, _, err := client.request(context.Background(), http.MethodGet, "/response", nil, 0, "", false)
			if test.wantCode == "" {
				require.NoError(t, err)
				require.Len(t, body, test.wantLength)
				return
			}
			require.Error(t, err)
			require.Nil(t, body)
			var apiError *APIError
			require.True(t, errors.As(err, &apiError))
			require.Equal(t, test.wantCode, apiError.Code)
			require.NotContains(t, err.Error(), credential)
			require.False(t, strings.Contains(err.Error(), "not-json"))
		})
	}
}
