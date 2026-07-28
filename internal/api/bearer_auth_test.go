package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bountyboard/internal/access"
	"bountyboard/internal/domain"
	"bountyboard/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeBearerAuthenticator struct {
	principal access.Principal
	err       error
	raw       string
}

func (a *fakeBearerAuthenticator) Authenticate(_ context.Context, raw string) (access.Principal, error) {
	a.raw = raw
	return a.principal, a.err
}

func TestBearerAuthenticationCreatesNonImpersonatingIdentity(t *testing.T) {
	tokenID := uuid.New()
	user := domain.User{ID: uuid.New(), Name: "Agent owner", Active: true}
	authenticator := &fakeBearerAuthenticator{principal: access.Principal{
		User: user, Method: access.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Nightly agent",
		Scopes: []access.Scope{access.ScopeWorkRead, access.ScopeWorkWrite},
	}}
	var got identity.RequestIdentity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = identity.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequestIDMiddleware(bearerAuthentication{tokens: authenticator}.wrap(next, http.NotFoundHandler()))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	request.Header.Set("Authorization", "Bearer bb_pat_example")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, "bb_pat_example", authenticator.raw)
	require.Equal(t, user, got.Actor)
	require.Equal(t, user, got.Subject)
	require.False(t, got.IsImpersonating())
	require.Equal(t, access.AuthenticationMethodAPIToken, got.AuthenticationMethod)
	require.Equal(t, tokenID, *got.APITokenID)
	require.Equal(t, "Nightly agent", got.APITokenName)
	require.Equal(t, []access.Scope{access.ScopeWorkRead, access.ScopeWorkWrite}, got.Scopes)
}

func TestBearerAuthenticationUsesSessionFallbackWithoutAuthorization(t *testing.T) {
	authenticator := &fakeBearerAuthenticator{}
	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Fallback", "session")
		w.WriteHeader(http.StatusNoContent)
	})
	handler := bearerAuthentication{tokens: authenticator}.wrap(http.NotFoundHandler(), fallback)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, "session", response.Header().Get("X-Fallback"))
	require.Empty(t, authenticator.raw)
}

func TestBearerAuthenticationMapsTokenFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid", err: access.ErrTokenInvalid, status: http.StatusUnauthorized, code: "TOKEN_INVALID"},
		{name: "expired", err: access.ErrTokenExpired, status: http.StatusUnauthorized, code: "TOKEN_EXPIRED"},
		{name: "revoked", err: access.ErrTokenRevoked, status: http.StatusUnauthorized, code: "TOKEN_REVOKED"},
		{name: "inactive", err: access.ErrUserInactive, status: http.StatusForbidden, code: "USER_INACTIVE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := RequestIDMiddleware(bearerAuthentication{
				tokens: &fakeBearerAuthenticator{err: test.err},
			}.wrap(http.NotFoundHandler(), http.NotFoundHandler()))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
			request.Header.Set("Authorization", "Bearer token")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, test.status, response.Code)
			require.Equal(t, "Bearer", response.Header().Get("WWW-Authenticate"))
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			require.Equal(t, test.code, problem.Code)
		})
	}
}

func TestBearerAuthenticationRejectsMalformedAuthorization(t *testing.T) {
	handler := RequestIDMiddleware(bearerAuthentication{
		tokens: &fakeBearerAuthenticator{},
	}.wrap(http.NotFoundHandler(), http.NotFoundHandler()))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	request.Header.Set("Authorization", "Basic credential")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, "TOKEN_INVALID", problem.Code)
}

func TestScopeMiddlewareAllowsSessionsAndChecksBearerScopes(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	for _, test := range []struct {
		name       string
		identity   identity.RequestIdentity
		handler    http.Handler
		wantStatus int
	}{
		{
			name:     "session write",
			identity: identity.RequestIdentity{AuthenticationMethod: access.AuthenticationMethodSession},
			handler:  RequireWorkWrite(next), wantStatus: http.StatusNoContent,
		},
		{
			name: "write implies read",
			identity: identity.RequestIdentity{
				AuthenticationMethod: access.AuthenticationMethodAPIToken,
				Scopes:               []access.Scope{access.ScopeWorkWrite},
			},
			handler: RequireWorkRead(next), wantStatus: http.StatusNoContent,
		},
		{
			name: "read cannot write",
			identity: identity.RequestIdentity{
				AuthenticationMethod: access.AuthenticationMethodAPIToken,
				Scopes:               []access.Scope{access.ScopeWorkRead},
			},
			handler: RequireWorkWrite(next), wantStatus: http.StatusForbidden,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
			request = request.WithContext(identity.WithRequestIdentity(request.Context(), test.identity))
			response := httptest.NewRecorder()
			RequestIDMiddleware(test.handler).ServeHTTP(response, request)
			require.Equal(t, test.wantStatus, response.Code)
			if test.wantStatus == http.StatusForbidden {
				var problem Problem
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
				require.Equal(t, "INSUFFICIENT_SCOPE", problem.Code)
			}
		})
	}
}

func TestInternalRouteIsolationHidesRoutesFromBearer(t *testing.T) {
	internal := isolateBearerFromInternal(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{
		"/api/admin/users",
		"/api/account/tokens",
		"/api/auth/logout",
		"/api/legacy/feed",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer hidden")
			response := httptest.NewRecorder()
			internal.ServeHTTP(response, request)
			require.Equal(t, http.StatusNotFound, response.Code)

			sessionRequest := httptest.NewRequest(http.MethodGet, path, nil)
			sessionResponse := httptest.NewRecorder()
			internal.ServeHTTP(sessionResponse, sessionRequest)
			require.Equal(t, http.StatusNoContent, sessionResponse.Code)
		})
	}
}
