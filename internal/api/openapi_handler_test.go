package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIDocumentRequiresAuthentication(t *testing.T) {
	handler, db := newTaskTestServer(t)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(
		unauthenticated,
		httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil),
	)
	require.Equal(t, http.StatusUnauthorized, unauthenticated.Code)

	authenticated := do(t, handler, http.MethodGet, "/api/openapi.yaml", userA, nil)
	require.Equal(t, http.StatusOK, authenticated.Code, authenticated.Body.String())
	require.Equal(t, "application/yaml; charset=utf-8", authenticated.Header().Get("Content-Type"))
	require.Contains(t, authenticated.Body.String(), "openapi: 3.1.1")
	require.Contains(t, authenticated.Body.String(), "/api/v1/tasks:")

	created := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name": "openapi-doc-" + uuid.NewString(), "scopes": []string{"work:read"}, "expires_in_days": 30,
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, created, &issued)
	t.Cleanup(func() { cleanupAPIToken(t, db, issued.ID) })

	bearerRequest := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	bearerRequest.Header.Set("Authorization", "Bearer "+issued.Token)
	bearer := httptest.NewRecorder()
	handler.ServeHTTP(bearer, bearerRequest)
	require.Equal(t, http.StatusOK, bearer.Code, bearer.Body.String())
	require.Contains(t, bearer.Body.String(), "openapi: 3.1.1")
	require.NotEmpty(t, bearer.Header().Get("RateLimit-Limit"))
}

func TestBearerTokensRemainIsolatedFromInternalAPI(t *testing.T) {
	handler, _ := newTaskTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/account/tokens", nil)
	request.Header.Set("Authorization", "Bearer syntactically-valid-but-untrusted")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.False(t, strings.Contains(response.Body.String(), "TOKEN_INVALID"))
}
