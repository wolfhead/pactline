package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubReadinessPinger struct {
	err error
}

func (p stubReadinessPinger) Ping(context.Context) error {
	return p.err
}

func TestOperationalEndpointsReportProcessAndDatabaseHealth(t *testing.T) {
	handler := withOperationalEndpoints(http.NotFoundHandler(), stubReadinessPinger{})

	liveness := httptest.NewRecorder()
	handler.ServeHTTP(liveness, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, liveness.Code)
	require.Equal(t, "ok\n", liveness.Body.String())
	require.Equal(t, "no-store", liveness.Header().Get("Cache-Control"))

	readiness := httptest.NewRecorder()
	handler.ServeHTTP(readiness, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, readiness.Code)
	require.Equal(t, "ready\n", readiness.Body.String())
}

func TestReadinessFailsWithoutExposingDatabaseDetails(t *testing.T) {
	handler := withOperationalEndpoints(
		http.NotFoundHandler(),
		stubReadinessPinger{err: errors.New("database host and credentials")},
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, "not ready\n", response.Body.String())
	require.NotContains(t, response.Body.String(), "database host")
}

func TestOperationalHandlerPreservesApplicationRoutes(t *testing.T) {
	application := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := withOperationalEndpoints(application, stubReadinessPinger{})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	require.Equal(t, http.StatusTeapot, response.Code)
}
