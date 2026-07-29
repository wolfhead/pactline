package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	baseapi "bountyboard/internal/api"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestV1TransportUsesApplicationAuthenticationAndScopes(t *testing.T) {
	handler, db := newTaskTestServer(t)

	session := do(t, handler, http.MethodGet, "/api/v1/me", userA, nil)
	require.Equal(t, http.StatusOK, session.Code, session.Body.String())
	var sessionPrincipal struct {
		AuthenticationMethod string `json:"authentication_method"`
	}
	decodeJSON(t, session, &sessionPrincipal)
	require.Equal(t, "session", sessionPrincipal.AuthenticationMethod)

	created := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name":   "v1-transport-" + uuid.NewString(),
		"scopes": []string{"work:read"}, "expires_in_days": 30,
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, created, &issued)
	t.Cleanup(func() { cleanupAPIToken(t, db, issued.ID) })

	bearerRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	bearerRequest.Header.Set("Authorization", "Bearer "+issued.Token)
	bearer := httptest.NewRecorder()
	handler.ServeHTTP(bearer, bearerRequest)
	require.Equal(t, http.StatusOK, bearer.Code, bearer.Body.String())
	require.NotEmpty(t, bearer.Header().Get("RateLimit-Limit"))
	var bearerPrincipal struct {
		AuthenticationMethod string   `json:"authentication_method"`
		Scopes               []string `json:"scopes"`
	}
	require.NoError(t, json.NewDecoder(bearer.Body).Decode(&bearerPrincipal))
	require.Equal(t, "api_token", bearerPrincipal.AuthenticationMethod)
	require.Equal(t, []string{"work:read"}, bearerPrincipal.Scopes)
	var successfulReadAudits int
	require.NoError(t, db.Pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM api_request_audit_events WHERE request_id=$1`,
		bearer.Header().Get("X-Request-ID"),
	).Scan(&successfulReadAudits))
	require.Zero(t, successfulReadAudits)

	mutationRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/labels",
		bytes.NewBufferString(`{"name":"blocked"}`),
	)
	mutationRequest.Header.Set("Authorization", "Bearer "+issued.Token)
	mutationRequest.Header.Set("Content-Type", "application/json")
	mutationRequest.Header.Set("Idempotency-Key", uuid.NewString())
	mutation := httptest.NewRecorder()
	handler.ServeHTTP(mutation, mutationRequest)
	require.Equal(t, http.StatusForbidden, mutation.Code, mutation.Body.String())
	var problem baseapi.Problem
	require.NoError(t, json.NewDecoder(mutation.Body).Decode(&problem))
	require.Equal(t, "INSUFFICIENT_SCOPE", problem.Code)
	var auditedRoute string
	require.NoError(t, db.Pool.QueryRow(
		context.Background(),
		`SELECT route_pattern FROM api_request_audit_events WHERE request_id=$1`,
		mutation.Header().Get("X-Request-ID"),
	).Scan(&auditedRoute))
	require.Equal(t, "/api/v1/labels", auditedRoute)
}
