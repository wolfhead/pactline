package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnversionedWorkRoutesAreRemoved(t *testing.T) {
	handler, _ := newTaskTestServer(t)
	for _, path := range []string{
		"/api/tasks",
		"/api/tasks/123",
		"/api/projects",
		"/api/projects/123",
		"/api/labels",
		"/api/users",
		"/api/acceptance-criteria/00000000-0000-0000-0000-000000000001",
	} {
		response := do(t, handler, http.MethodGet, path, userA, nil)
		require.Equal(t, http.StatusNotFound, response.Code, "%s: %s", path, response.Body.String())
	}
}

func TestSupportedRouteBoundariesRemainMounted(t *testing.T) {
	handler, _ := newTaskTestServer(t)

	for _, path := range []string{
		"/api/v1/users",
		"/api/me",
		"/api/account/tokens",
		"/api/legacy/bounties",
	} {
		response := do(t, handler, http.MethodGet, path, userA, nil)
		require.NotEqual(t, http.StatusNotFound, response.Code, "%s: %s", path, response.Body.String())
	}

	adminResponse := do(t, handler, http.MethodGet, "/api/admin/users", userA, nil)
	require.NotEqual(t, http.StatusNotFound, adminResponse.Code, adminResponse.Body.String())
}
