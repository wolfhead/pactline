package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type adminGitLabConnectionStub struct {
	connection       domain.GitLabConnection
	createInput      application.CreateGitLabConnection
	createOperation  domain.OperationActor
	rotateCredential string
	resultError      error
}

func (s *adminGitLabConnectionStub) List(context.Context) ([]domain.GitLabConnection, error) {
	return []domain.GitLabConnection{s.connection}, s.resultError
}

func (s *adminGitLabConnectionStub) Create(
	_ context.Context,
	input application.CreateGitLabConnection,
	operation domain.OperationActor,
) (domain.GitLabConnection, error) {
	s.createInput = input
	s.createOperation = operation
	return s.connection, s.resultError
}

func (s *adminGitLabConnectionStub) RotateCredential(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
	credential string,
	_ *time.Time,
	_ domain.OperationActor,
) (domain.GitLabConnection, error) {
	s.rotateCredential = credential
	return s.connection, s.resultError
}

func (s *adminGitLabConnectionStub) Validate(
	context.Context, uuid.UUID, int64, domain.OperationActor,
) (domain.GitLabConnection, error) {
	return s.connection, s.resultError
}

func (s *adminGitLabConnectionStub) Disable(
	context.Context, uuid.UUID, int64, domain.OperationActor,
) (domain.GitLabConnection, error) {
	return s.connection, s.resultError
}

func TestAdminGitLabConnectionCreateDoesNotReturnCredential(t *testing.T) {
	admin := adminGitLabTestUser(domain.PlatformRoleAdmin)
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	stub := &adminGitLabConnectionStub{connection: domain.GitLabConnection{
		ID: uuid.New(), Version: 1, Label: "Repository",
		Origin: "https://gitlab.example", GitLabProjectID: 17,
		PathWithNamespace: "group/repo", CanonicalWebURL: "https://gitlab.example/group/repo",
		Status: domain.GitLabConnectionStatusActive, LastValidatedAt: now,
		CreatedAt: now, UpdatedAt: now,
		CredentialCiphertext: []byte("ciphertext-must-not-leak"), EncryptionKeyID: "key-1",
	}}
	handler := &adminGitLabHandler{connections: stub}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/gitlab-connections", bytes.NewBufferString(`{
        "label":"Repository",
        "repository_url":"https://gitlab.example/group/repo",
        "credential":"plain-secret"
    }`))
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response := httptest.NewRecorder()

	handler.create(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, "plain-secret", stub.createInput.Credential)
	require.Equal(t, admin.ID, stub.createOperation.UserID)
	require.NotContains(t, response.Body.String(), "plain-secret")
	require.NotContains(t, response.Body.String(), "ciphertext-must-not-leak")
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	_, hasCredential := body["credential"]
	require.False(t, hasCredential)
}

func TestAdminGitLabConnectionRejectsMemberAndUnknownFields(t *testing.T) {
	member := adminGitLabTestUser(domain.PlatformRoleMember)
	handler := &adminGitLabHandler{connections: &adminGitLabConnectionStub{}}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/gitlab-connections", nil)
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: member, Subject: member}))
	response := httptest.NewRecorder()
	handler.list(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)

	admin := adminGitLabTestUser(domain.PlatformRoleAdmin)
	request = httptest.NewRequest(http.MethodPost, "/api/admin/gitlab-connections",
		bytes.NewBufferString(`{"label":"Repository","repository_url":"https://gitlab.example/group/repo","credential":"secret","unexpected":true}`))
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response = httptest.NewRecorder()
	handler.create(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAdminGitLabConnectionMapsProviderFailuresSafely(t *testing.T) {
	admin := adminGitLabTestUser(domain.PlatformRoleAdmin)
	handler := &adminGitLabHandler{connections: &adminGitLabConnectionStub{
		resultError: domain.ErrProviderUnauthorized,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/gitlab-connections", nil)
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response := httptest.NewRecorder()

	handler.list(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code)
	require.JSONEq(t, `{"error":"GitLab rejected the configured credential"}`, response.Body.String())
}

func adminGitLabTestUser(role domain.PlatformRole) domain.User {
	return domain.User{
		ID: uuid.New(), Name: "Test user", PlatformRole: role,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
}
