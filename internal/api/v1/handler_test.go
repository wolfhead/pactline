package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wolfhead/pactline/internal/access"
	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/stretchr/testify/require"
)

type testSecuritySource struct {
	method access.AuthenticationMethod
}

func (s testSecuritySource) BearerAuth(
	context.Context,
	generated.OperationName,
) (generated.BearerAuth, error) {
	if s.method != access.AuthenticationMethodAPIToken {
		return generated.BearerAuth{}, ogenerrors.ErrSkipClientSecurity
	}
	return generated.BearerAuth{Token: "test-token"}, nil
}

func (s testSecuritySource) SessionCookie(
	context.Context,
	generated.OperationName,
) (generated.SessionCookie, error) {
	if s.method != access.AuthenticationMethodSession {
		return generated.SessionCookie{}, ogenerrors.ErrSkipClientSecurity
	}
	return generated.SessionCookie{APIKey: "test-session"}, nil
}

func TestGeneratedServerAuthenticatesSessionAndReadToken(t *testing.T) {
	handler := newGeneratedTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	for _, method := range []access.AuthenticationMethod{
		access.AuthenticationMethodSession,
		access.AuthenticationMethodAPIToken,
	} {
		t.Run(string(method), func(t *testing.T) {
			client, err := generated.NewClient(server.URL, testSecuritySource{method: method})
			require.NoError(t, err)

			response, err := client.GetCurrentPrincipal(context.Background())
			require.NoError(t, err)
			principal, ok := response.(*generated.CurrentPrincipalHeaders)
			require.True(t, ok, "unexpected response type %T", response)
			require.Equal(t, generated.CurrentPrincipalAuthenticationMethod(method),
				principal.Response.AuthenticationMethod)
			require.Equal(t, "Test User", principal.Response.Subject.Name)
		})
	}
}

func TestGeneratedServerRejectsReadTokenMutation(t *testing.T) {
	handler := newGeneratedTestHandler(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := generated.NewClient(
		server.URL,
		testSecuritySource{method: access.AuthenticationMethodAPIToken},
	)
	require.NoError(t, err)

	response, err := client.CreateLabel(
		context.Background(),
		&generated.LabelWrite{Name: "blocked"},
		generated.CreateLabelParams{},
	)
	require.NoError(t, err)
	problem, ok := response.(*generated.ProblemStatusCodeWithHeaders)
	require.True(t, ok, "unexpected response type %T", response)
	require.Equal(t, http.StatusForbidden, problem.StatusCode)
	require.Equal(t, generated.ProblemCode("INSUFFICIENT_SCOPE"), problem.Response.Code)
}

func TestGeneratedServerRejectsUnknownRequestFields(t *testing.T) {
	handler := newGeneratedTestHandler(t)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/labels",
		bytes.NewBufferString(`{"name":"label","unexpected":true}`),
	)
	request.Header.Set("Authorization", "Bearer write-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	var problem baseapi.Problem
	require.NoError(t, json.NewDecoder(response.Body).Decode(&problem))
	require.Equal(t, "VALIDATION_FAILED", problem.Code)
	require.NotEmpty(t, problem.RequestID)
}

func TestGeneratedServerDoesNotExposeInternalRoutes(t *testing.T) {
	handler := newGeneratedTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/account/tokens", nil)
	request.AddCookie(&http.Cookie{Name: "bb_session", Value: "test-session"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestGeneratedServerRequiresIfMatchBeforeCallingHandler(t *testing.T) {
	handler := newGeneratedTestHandler(t)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/labels/00000000-0000-0000-0000-000000000001",
		bytes.NewBufferString(`{"name":"renamed"}`),
	)
	request.Header.Set("Authorization", "Bearer write-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusPreconditionRequired, response.Code, response.Body.String())
	var problem baseapi.Problem
	require.NoError(t, json.NewDecoder(response.Body).Decode(&problem))
	require.Equal(t, "PRECONDITION_REQUIRED", problem.Code)
}

func newGeneratedTestHandler(t *testing.T) http.Handler {
	t.Helper()
	server, err := NewServer(&Handler{})
	require.NoError(t, err)
	injectIdentity := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := domain.User{
			ID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			Name: "Test User", PlatformRole: domain.PlatformRoleAdmin, Active: true,
		}
		requestIdentity := identity.RequestIdentity{Actor: user, Subject: user}
		switch {
		case strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "):
			requestIdentity.AuthenticationMethod = access.AuthenticationMethodAPIToken
			requestIdentity.Scopes = []access.Scope{access.ScopeWorkRead}
			if r.Header.Get("Authorization") == "Bearer write-token" {
				requestIdentity.Scopes = append(requestIdentity.Scopes, access.ScopeWorkWrite)
			}
		case hasCookie(r, "bb_session"):
			requestIdentity.AuthenticationMethod = access.AuthenticationMethodSession
		default:
			server.ServeHTTP(w, r)
			return
		}
		ctx := identity.WithRequestIdentity(r.Context(), requestIdentity)
		server.ServeHTTP(w, r.WithContext(ctx))
	})
	return baseapi.RequestIDMiddleware(injectIdentity)
}

func hasCookie(r *http.Request, name string) bool {
	_, err := r.Cookie(name)
	return err == nil
}
