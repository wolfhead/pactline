package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdminUserLifecycleAndReadOnlyImpersonationHTTPPolicy(t *testing.T) {
	handler, db := newTaskTestServer(t)
	ctx := context.Background()
	testCutoff := time.Now().UTC()
	adminID, memberID := uuid.MustParse(userA), uuid.MustParse(userB)
	type originalUserState struct {
		role   string
		active bool
	}
	original := map[uuid.UUID]originalUserState{}
	for _, id := range []uuid.UUID{adminID, memberID} {
		var state originalUserState
		require.NoError(t, db.Pool.QueryRow(ctx,
			`SELECT platform_role, active FROM users WHERE id=$1`, id).
			Scan(&state.role, &state.active))
		original[id] = state
	}
	_, err := db.Pool.Exec(ctx, `UPDATE users SET platform_role='ADMIN' WHERE id=$1`, adminID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `
			DELETE FROM identity_audit_events
			WHERE occurred_at >= $1 AND (actor_user_id=$2 OR subject_user_id=$2 OR subject_user_id=$3)`,
			testCutoff, adminID, memberID)
		require.NoError(t, cleanupErr)
		for id, state := range original {
			_, cleanupErr = db.Pool.Exec(context.Background(),
				`UPDATE users SET platform_role=$2, active=$3 WHERE id=$1`,
				id, state.role, state.active)
			require.NoError(t, cleanupErr)
		}
	})

	memberCookies, _ := developmentLogin(t, handler, userB)
	memberSessionID := uuid.MustParse(strings.Split(memberCookies[0].Value, ".")[0])
	adminCookies, adminCSRF := developmentLogin(t, handler, userA)

	listRequest := authenticatedHTTPTestRequest(http.MethodGet, "/api/admin/users", nil, adminCookies, adminCSRF)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())

	unknownFieldRequest := authenticatedHTTPTestRequest(http.MethodPatch, "/api/admin/users/"+userB,
		[]byte(`{"active":false,"platform_role":"ADMIN"}`), adminCookies, adminCSRF)
	unknownFieldResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownFieldResponse, unknownFieldRequest)
	require.Equal(t, http.StatusBadRequest, unknownFieldResponse.Code)

	deactivateRequest := authenticatedHTTPTestRequest(http.MethodPatch, "/api/admin/users/"+userB,
		[]byte(`{"active":false}`), adminCookies, adminCSRF)
	deactivateResponse := httptest.NewRecorder()
	handler.ServeHTTP(deactivateResponse, deactivateRequest)
	require.Equal(t, http.StatusNoContent, deactivateResponse.Code, deactivateResponse.Body.String())
	var revoked bool
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1`, memberSessionID).Scan(&revoked))
	require.True(t, revoked)

	reactivateRequest := authenticatedHTTPTestRequest(http.MethodPatch, "/api/admin/users/"+userB,
		[]byte(`{"active":true}`), adminCookies, adminCSRF)
	reactivateResponse := httptest.NewRecorder()
	handler.ServeHTTP(reactivateResponse, reactivateRequest)
	require.Equal(t, http.StatusNoContent, reactivateResponse.Code)
	var activeSessions int
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, memberID).Scan(&activeSessions))
	require.Zero(t, activeSessions)

	selfRequest := authenticatedHTTPTestRequest(http.MethodPatch, "/api/admin/users/"+userA,
		[]byte(`{"active":false}`), adminCookies, adminCSRF)
	selfResponse := httptest.NewRecorder()
	handler.ServeHTTP(selfResponse, selfRequest)
	require.Equal(t, http.StatusForbidden, selfResponse.Code)

	startRequest := authenticatedHTTPTestRequest(http.MethodPost, "/api/admin/impersonation",
		[]byte(`{"user_id":"`+userB+`"}`), adminCookies, adminCSRF)
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, startRequest)
	require.Equal(t, http.StatusNoContent, startResponse.Code, startResponse.Body.String())

	meRequest := authenticatedHTTPTestRequest(http.MethodGet, "/api/me", nil, adminCookies, adminCSRF)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, meRequest)
	require.Equal(t, http.StatusOK, meResponse.Code, meResponse.Body.String())
	var me struct {
		Actor struct {
			ID uuid.UUID `json:"id"`
		} `json:"actor"`
		Subject struct {
			ID uuid.UUID `json:"id"`
		} `json:"subject"`
	}
	require.NoError(t, json.Unmarshal(meResponse.Body.Bytes(), &me))
	require.Equal(t, adminID, me.Actor.ID)
	require.Equal(t, memberID, me.Subject.ID)

	adminReadRequest := authenticatedHTTPTestRequest(http.MethodGet, "/api/admin/users", nil, adminCookies, adminCSRF)
	adminReadResponse := httptest.NewRecorder()
	handler.ServeHTTP(adminReadResponse, adminReadRequest)
	require.Equal(t, http.StatusForbidden, adminReadResponse.Code)

	rejectionRequestID := "impersonation-rejection-" + uuid.NewString()
	writeRequest := authenticatedHTTPTestRequest(http.MethodPatch, "/api/v1/tasks/123?raw=must-not-be-audited",
		[]byte(`{"title":"must-not-be-read"}`), adminCookies, adminCSRF)
	bodyProbe := &readProbe{}
	writeRequest.Body = io.NopCloser(bodyProbe)
	writeRequest.Header.Set("X-Request-Id", rejectionRequestID)
	writeResponse := httptest.NewRecorder()
	handler.ServeHTTP(writeResponse, writeRequest)
	require.Equal(t, http.StatusForbidden, writeResponse.Code)
	var metadata []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT metadata FROM identity_audit_events
		WHERE event_type='impersonation_write_rejected' AND request_id=$1`, rejectionRequestID).Scan(&metadata))
	require.JSONEq(t, `{"method":"PATCH","route":"/api/v1/tasks/{number}"}`, string(metadata))
	require.NotContains(t, string(metadata), "must-not-be-audited")
	require.NotContains(t, string(metadata), "must-not-be-read")
	require.Zero(t, bodyProbe.reads)

	exitRequest := authenticatedHTTPTestRequest(http.MethodDelete, "/api/admin/impersonation", nil, adminCookies, adminCSRF)
	exitResponse := httptest.NewRecorder()
	handler.ServeHTTP(exitResponse, exitRequest)
	require.Equal(t, http.StatusNoContent, exitResponse.Code, exitResponse.Body.String())
	adminSessionID := uuid.MustParse(strings.Split(adminCookies[0].Value, ".")[0])
	var starts, ends int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE event_type='impersonation_started'),
		       count(*) FILTER (WHERE event_type='impersonation_ended')
		FROM identity_audit_events WHERE session_id=$1`, adminSessionID).Scan(&starts, &ends))
	require.Equal(t, 1, starts)
	require.Equal(t, 1, ends)

	logoutCookies, logoutCSRF := developmentLogin(t, handler, userA)
	secondStart := authenticatedHTTPTestRequest(http.MethodPost, "/api/admin/impersonation",
		[]byte(`{"user_id":"`+userB+`"}`), logoutCookies, logoutCSRF)
	secondStartResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondStartResponse, secondStart)
	require.Equal(t, http.StatusNoContent, secondStartResponse.Code)
	logoutRequest := authenticatedHTTPTestRequest(http.MethodPost, "/api/auth/logout", nil, logoutCookies, logoutCSRF)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	require.Equal(t, http.StatusNoContent, logoutResponse.Code)
}

type readProbe struct{ reads int }

func (r *readProbe) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}
