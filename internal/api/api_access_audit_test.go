package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recordingAccessAuditStore struct {
	event access.RequestAuditEvent
	err   error
}

func (s *recordingAccessAuditStore) RecordAccessAudit(_ context.Context, event access.RequestAuditEvent) error {
	s.event = event
	return s.err
}

func TestAccessAuditRecordsMetadataWithoutRequestSecrets(t *testing.T) {
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	userID, tokenID := uuid.New(), uuid.New()
	audits := &recordingAccessAuditStore{}
	tokens := &fakeBearerAuthenticator{principal: access.Principal{
		User:    domain.User{ID: userID, Active: true},
		Method:  access.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Audited agent",
		Scopes: []access.Scope{access.ScopeWorkWrite},
	}}
	v1 := http.NewServeMux()
	v1.HandleFunc("POST /api/v1/tasks/{number}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		WriteJSON(w, http.StatusCreated, map[string]string{"result": "created"})
	})
	protected := bearerAuthentication{tokens: tokens}.wrap(v1, http.NotFoundHandler())
	handler := RequestIDMiddleware(apiAccessAudit{
		store: audits, now: func() time.Time { return now }, routes: v1,
	}.wrap(protected))
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tasks/42?private=must-not-be-persisted",
		bytes.NewBufferString(`{"secret":"must-not-be-persisted"}`),
	)
	request.Header.Set("Authorization", "Bearer must-not-be-persisted")
	request.Header.Set("Cookie", "bb_session=must-not-be-persisted")
	request.Header.Set("X-CSRF-Token", "must-not-be-persisted")
	request.Header.Set("User-Agent", "audit-test")
	request.RemoteAddr = "192.0.2.10:4567"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, now, audits.event.OccurredAt)
	require.Equal(t, access.AuthenticationMethodAPIToken, audits.event.AuthMethod)
	require.Equal(t, access.AuthOutcomeAuthenticated, audits.event.AuthOutcome)
	require.Equal(t, userID, *audits.event.UserID)
	require.Equal(t, tokenID, *audits.event.TokenID)
	require.Equal(t, "Audited agent", audits.event.TokenName)
	require.Equal(t, http.MethodPost, audits.event.Method)
	require.Equal(t, "/api/v1/tasks/{number}", audits.event.RoutePattern)
	require.Equal(t, http.StatusCreated, audits.event.StatusCode)
	require.Equal(t, int64(response.Body.Len()), audits.event.ResponseBytes)
	require.Equal(t, "audit-test", audits.event.UserAgent)
	require.Equal(t, "192.0.2.10", audits.event.NetworkAddress)
	serialized, err := json.Marshal(audits.event)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "must-not-be-persisted")
}

func TestAccessAuditRecordsRejectedBearerWithoutCredential(t *testing.T) {
	audits := &recordingAccessAuditStore{}
	v1 := http.NewServeMux()
	v1.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequestIDMiddleware(apiAccessAudit{
		store: audits, now: time.Now, routes: v1,
	}.wrap(bearerAuthentication{
		tokens: &fakeBearerAuthenticator{err: access.ErrTokenInvalid},
	}.wrap(v1, http.NotFoundHandler())))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	request.Header.Set("Authorization", "Bearer must-not-be-persisted")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, access.AuthenticationMethodAPIToken, audits.event.AuthMethod)
	require.Equal(t, access.AuthOutcomeRejected, audits.event.AuthOutcome)
	require.Nil(t, audits.event.UserID)
	require.Nil(t, audits.event.TokenID)
	serialized, err := json.Marshal(audits.event)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "must-not-be-persisted")
}
