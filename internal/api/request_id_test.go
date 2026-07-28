package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddlewarePreservesValidID(t *testing.T) {
	const valid = "agent-run_2026-07-28"
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, valid, requestID(r))
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	request.Header.Set("X-Request-ID", valid)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, valid, response.Header().Get("X-Request-ID"))
}

func TestRequestIDMiddlewareReplacesInvalidOrMissingID(t *testing.T) {
	for _, incoming := range []string{"", "contains a newline\nAuthorization: Bearer secret"} {
		t.Run(incoming, func(t *testing.T) {
			var accepted string
			handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				accepted = requestID(r)
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header["X-Request-Id"] = []string{incoming}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			_, err := uuid.Parse(accepted)
			require.NoError(t, err)
			require.Equal(t, accepted, response.Header().Get("X-Request-ID"))
		})
	}
}
