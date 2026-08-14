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

type adminRepositoryConnectionStub struct {
	connection       domain.RepositoryConnection
	createInput      application.CreateRepositoryConnection
	createOperation  domain.OperationActor
	rotateCredential string
	resultError      error
}

func (s *adminRepositoryConnectionStub) List(context.Context) ([]domain.RepositoryConnection, error) {
	return []domain.RepositoryConnection{s.connection}, s.resultError
}

func (s *adminRepositoryConnectionStub) Create(
	_ context.Context,
	input application.CreateRepositoryConnection,
	operation domain.OperationActor,
) (domain.RepositoryConnection, error) {
	s.createInput = input
	s.createOperation = operation
	return s.connection, s.resultError
}

func (s *adminRepositoryConnectionStub) RotateCredential(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
	credential string,
	_ *time.Time,
	_ domain.OperationActor,
) (domain.RepositoryConnection, error) {
	s.rotateCredential = credential
	return s.connection, s.resultError
}

func (s *adminRepositoryConnectionStub) Validate(
	context.Context, uuid.UUID, int64, domain.OperationActor,
) (domain.RepositoryConnection, error) {
	return s.connection, s.resultError
}

func (s *adminRepositoryConnectionStub) Disable(
	context.Context, uuid.UUID, int64, domain.OperationActor,
) (domain.RepositoryConnection, error) {
	return s.connection, s.resultError
}

func TestAdminRepositoryConnectionCreateDoesNotReturnCredential(t *testing.T) {
	admin := adminRepositoryTestUser(domain.PlatformRoleAdmin)
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	stub := &adminRepositoryConnectionStub{connection: domain.RepositoryConnection{
		ID: uuid.New(), Version: 1, Label: "Repository",
		Provider: domain.RepositoryProviderGitLab,
		Origin:   "https://gitlab.example", ProviderRepositoryID: "17",
		PathWithNamespace: "group/repo", CanonicalWebURL: "https://gitlab.example/group/repo",
		Status: domain.RepositoryConnectionStatusActive, LastValidatedAt: now,
		CreatedAt: now, UpdatedAt: now,
		CredentialCiphertext: []byte("ciphertext-must-not-leak"), EncryptionKeyID: "key-1",
	}}
	handler := &adminRepositoryConnectionHandler{connections: stub}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/repository-connections", bytes.NewBufferString(`{
        "label":"Repository",
		"provider":"gitlab",
        "repository_url":"https://gitlab.example/group/repo",
        "credential":"plain-secret"
    }`))
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response := httptest.NewRecorder()

	handler.create(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, "plain-secret", stub.createInput.Credential)
	require.Equal(t, domain.RepositoryProviderGitLab, stub.createInput.Provider)
	require.Equal(t, admin.ID, stub.createOperation.UserID)
	require.NotContains(t, response.Body.String(), "plain-secret")
	require.NotContains(t, response.Body.String(), "ciphertext-must-not-leak")
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	_, hasCredential := body["credential"]
	require.False(t, hasCredential)
}

func TestAdminRepositoryConnectionRejectsMemberAndUnknownFields(t *testing.T) {
	member := adminRepositoryTestUser(domain.PlatformRoleMember)
	handler := &adminRepositoryConnectionHandler{connections: &adminRepositoryConnectionStub{}}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/repository-connections", nil)
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: member, Subject: member}))
	response := httptest.NewRecorder()
	handler.list(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)

	admin := adminRepositoryTestUser(domain.PlatformRoleAdmin)
	request = httptest.NewRequest(http.MethodPost, "/api/admin/repository-connections",
		bytes.NewBufferString(`{"label":"Repository","provider":"gitlab","repository_url":"https://gitlab.example/group/repo","credential":"secret","unexpected":true}`))
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response = httptest.NewRecorder()
	handler.create(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAdminRepositoryConnectionMapsProviderFailuresSafely(t *testing.T) {
	admin := adminRepositoryTestUser(domain.PlatformRoleAdmin)
	handler := &adminRepositoryConnectionHandler{connections: &adminRepositoryConnectionStub{
		resultError: domain.ErrProviderUnauthorized,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/repository-connections", nil)
	request = request.WithContext(identity.WithRequestIdentity(request.Context(),
		identity.RequestIdentity{Actor: admin, Subject: admin}))
	response := httptest.NewRecorder()

	handler.list(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code)
	require.JSONEq(t, `{"error":"Repository provider rejected the configured credential"}`, response.Body.String())
}

func adminRepositoryTestUser(role domain.PlatformRole) domain.User {
	return domain.User{
		ID: uuid.New(), Name: "Test user", PlatformRole: role,
		AccessStatus: domain.AccessStatusApproved, Active: true,
	}
}
