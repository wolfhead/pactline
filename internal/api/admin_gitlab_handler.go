package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
)

type adminGitLabConnectionService interface {
	List(context.Context) ([]domain.GitLabConnection, error)
	Create(
		context.Context, application.CreateGitLabConnection, domain.OperationActor,
	) (domain.GitLabConnection, error)
	RotateCredential(
		context.Context, uuid.UUID, int64, string, *time.Time, domain.OperationActor,
	) (domain.GitLabConnection, error)
	Validate(
		context.Context, uuid.UUID, int64, domain.OperationActor,
	) (domain.GitLabConnection, error)
	Disable(
		context.Context, uuid.UUID, int64, domain.OperationActor,
	) (domain.GitLabConnection, error)
}

type adminGitLabHandler struct {
	connections adminGitLabConnectionService
}

type gitLabConnectionResponse struct {
	ID                  uuid.UUID                     `json:"id"`
	Version             int64                         `json:"version"`
	Label               string                        `json:"label"`
	Origin              string                        `json:"origin"`
	GitLabProjectID     int64                         `json:"gitlab_project_id"`
	PathWithNamespace   string                        `json:"path_with_namespace"`
	CanonicalWebURL     string                        `json:"canonical_web_url"`
	DefaultBranch       string                        `json:"default_branch"`
	CredentialExpiresAt *time.Time                    `json:"credential_expires_at"`
	Status              domain.GitLabConnectionStatus `json:"status"`
	LastValidatedAt     time.Time                     `json:"last_validated_at"`
	CreatedAt           time.Time                     `json:"created_at"`
	UpdatedAt           time.Time                     `json:"updated_at"`
}

func (h *adminGitLabHandler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdministrator(w, r); !ok {
		return
	}
	connections, err := h.connections.List(r.Context())
	if err != nil {
		writeAdminGitLabError(w, r, err)
		return
	}
	response := make([]gitLabConnectionResponse, len(connections))
	for i, connection := range connections {
		response[i] = gitLabConnectionFromDomain(connection)
	}
	WriteJSON(w, http.StatusOK, response)
}

func (h *adminGitLabHandler) create(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	var request struct {
		Label               string     `json:"label"`
		RepositoryURL       string     `json:"repository_url"`
		Credential          string     `json:"credential"`
		CredentialExpiresAt *time.Time `json:"credential_expires_at"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	connection, err := h.connections.Create(r.Context(), application.CreateGitLabConnection{
		Label: request.Label, RepositoryURL: request.RepositoryURL,
		Credential: request.Credential, CredentialExpiresAt: request.CredentialExpiresAt,
	}, domain.SessionOperation(current.Actor.ID, requestID(r)))
	request.Credential = ""
	if err != nil {
		writeAdminGitLabError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, gitLabConnectionFromDomain(connection))
}

func (h *adminGitLabHandler) rotateCredential(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid GitLab Connection id"})
		return
	}
	var request struct {
		Version             int64      `json:"version"`
		Credential          string     `json:"credential"`
		CredentialExpiresAt *time.Time `json:"credential_expires_at"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	connection, err := h.connections.RotateCredential(
		r.Context(), id, request.Version, request.Credential, request.CredentialExpiresAt,
		domain.SessionOperation(current.Actor.ID, requestID(r)),
	)
	request.Credential = ""
	if err != nil {
		writeAdminGitLabError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, gitLabConnectionFromDomain(connection))
}

func (h *adminGitLabHandler) validate(w http.ResponseWriter, r *http.Request) {
	h.adminVersionCommand(w, r, func(
		ctx context.Context, id uuid.UUID, version int64, operation domain.OperationActor,
	) (domain.GitLabConnection, error) {
		return h.connections.Validate(ctx, id, version, operation)
	})
}

func (h *adminGitLabHandler) disable(w http.ResponseWriter, r *http.Request) {
	h.adminVersionCommand(w, r, func(
		ctx context.Context, id uuid.UUID, version int64, operation domain.OperationActor,
	) (domain.GitLabConnection, error) {
		return h.connections.Disable(ctx, id, version, operation)
	})
}

func (h *adminGitLabHandler) adminVersionCommand(
	w http.ResponseWriter,
	r *http.Request,
	command func(context.Context, uuid.UUID, int64, domain.OperationActor) (domain.GitLabConnection, error),
) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid GitLab Connection id"})
		return
	}
	var request struct {
		Version int64 `json:"version"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	connection, err := command(
		r.Context(), id, request.Version, domain.SessionOperation(current.Actor.ID, requestID(r)),
	)
	if err != nil {
		writeAdminGitLabError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, gitLabConnectionFromDomain(connection))
}

func gitLabConnectionFromDomain(connection domain.GitLabConnection) gitLabConnectionResponse {
	return gitLabConnectionResponse{
		ID: connection.ID, Version: connection.Version, Label: connection.Label,
		Origin: connection.Origin, GitLabProjectID: connection.GitLabProjectID,
		PathWithNamespace: connection.PathWithNamespace,
		CanonicalWebURL:   connection.CanonicalWebURL, DefaultBranch: connection.DefaultBranch,
		CredentialExpiresAt: connection.CredentialExpiresAt, Status: connection.Status,
		LastValidatedAt: connection.LastValidatedAt,
		CreatedAt:       connection.CreatedAt, UpdatedAt: connection.UpdatedAt,
	}
}

func writeAdminGitLabError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	safeMessage := "GitLab Connection operation failed"
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		status, safeMessage = http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, safeMessage = http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		status, safeMessage = http.StatusForbidden, err.Error()
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrVersionConflict):
		status, safeMessage = http.StatusConflict, err.Error()
	case errors.Is(err, domain.ErrProviderUnauthorized):
		status, safeMessage = http.StatusBadGateway, "GitLab rejected the configured credential"
	case errors.Is(err, domain.ErrProviderUnavailable):
		status, safeMessage = http.StatusServiceUnavailable, "GitLab is temporarily unavailable"
	case errors.Is(err, domain.ErrProviderRejected):
		status, safeMessage = http.StatusBadGateway, "GitLab rejected the repository request"
	case errors.Is(err, domain.ErrIntegrationNotConfigured):
		status, safeMessage = http.StatusServiceUnavailable, "GitLab credential encryption is not configured"
	}
	logger := slog.With(
		"method", r.Method, "path", r.URL.Path, "status", status,
		"request_id", requestID(r), "error", err,
	)
	if status >= 500 {
		logger.Error("GitLab Connection request failed")
	} else {
		logger.Warn("GitLab Connection request rejected")
	}
	WriteJSON(w, status, ErrorBody{Error: safeMessage})
}
