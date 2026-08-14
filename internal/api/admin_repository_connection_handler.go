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

type adminRepositoryConnectionService interface {
	List(context.Context) ([]domain.RepositoryConnection, error)
	Create(
		context.Context, application.CreateRepositoryConnection, domain.OperationActor,
	) (domain.RepositoryConnection, error)
	RotateCredential(
		context.Context, uuid.UUID, int64, string, *time.Time, domain.OperationActor,
	) (domain.RepositoryConnection, error)
	Validate(context.Context, uuid.UUID, int64, domain.OperationActor) (domain.RepositoryConnection, error)
	Disable(context.Context, uuid.UUID, int64, domain.OperationActor) (domain.RepositoryConnection, error)
}

type adminRepositoryConnectionHandler struct {
	connections adminRepositoryConnectionService
}

type repositoryConnectionResponse struct {
	ID                   uuid.UUID                         `json:"id"`
	Version              int64                             `json:"version"`
	Label                string                            `json:"label"`
	Provider             domain.RepositoryProvider         `json:"provider"`
	Origin               string                            `json:"origin"`
	ProviderRepositoryID string                            `json:"provider_repository_id"`
	PathWithNamespace    string                            `json:"path_with_namespace"`
	CanonicalWebURL      string                            `json:"canonical_web_url"`
	DefaultBranch        string                            `json:"default_branch"`
	CredentialExpiresAt  *time.Time                        `json:"credential_expires_at"`
	Status               domain.RepositoryConnectionStatus `json:"status"`
	LastValidatedAt      time.Time                         `json:"last_validated_at"`
	CreatedAt            time.Time                         `json:"created_at"`
	UpdatedAt            time.Time                         `json:"updated_at"`
}

func (h *adminRepositoryConnectionHandler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdministrator(w, r); !ok {
		return
	}
	connections, err := h.connections.List(r.Context())
	if err != nil {
		writeAdminRepositoryConnectionError(w, r, err)
		return
	}
	response := make([]repositoryConnectionResponse, len(connections))
	for index, connection := range connections {
		response[index] = repositoryConnectionFromDomain(connection)
	}
	WriteJSON(w, http.StatusOK, response)
}

func (h *adminRepositoryConnectionHandler) create(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	var request struct {
		Label               string                    `json:"label"`
		Provider            domain.RepositoryProvider `json:"provider"`
		RepositoryURL       string                    `json:"repository_url"`
		Credential          string                    `json:"credential"`
		CredentialExpiresAt *time.Time                `json:"credential_expires_at"`
	}
	if !decodeStrictBody(w, r, &request) {
		return
	}
	connection, err := h.connections.Create(r.Context(), application.CreateRepositoryConnection{
		Label: request.Label, Provider: request.Provider, RepositoryURL: request.RepositoryURL,
		Credential: request.Credential, CredentialExpiresAt: request.CredentialExpiresAt,
	}, domain.SessionOperation(current.Actor.ID, requestID(r)))
	request.Credential = ""
	if err != nil {
		writeAdminRepositoryConnectionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, repositoryConnectionFromDomain(connection))
}

func (h *adminRepositoryConnectionHandler) rotateCredential(w http.ResponseWriter, r *http.Request) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid Repository Connection id"})
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
		writeAdminRepositoryConnectionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, repositoryConnectionFromDomain(connection))
}

func (h *adminRepositoryConnectionHandler) validate(w http.ResponseWriter, r *http.Request) {
	h.adminVersionCommand(w, r, h.connections.Validate)
}

func (h *adminRepositoryConnectionHandler) disable(w http.ResponseWriter, r *http.Request) {
	h.adminVersionCommand(w, r, h.connections.Disable)
}

func (h *adminRepositoryConnectionHandler) adminVersionCommand(
	w http.ResponseWriter,
	r *http.Request,
	command func(context.Context, uuid.UUID, int64, domain.OperationActor) (domain.RepositoryConnection, error),
) {
	current, ok := requireAdministrator(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid Repository Connection id"})
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
		writeAdminRepositoryConnectionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, repositoryConnectionFromDomain(connection))
}

func repositoryConnectionFromDomain(connection domain.RepositoryConnection) repositoryConnectionResponse {
	return repositoryConnectionResponse{
		ID: connection.ID, Version: connection.Version, Label: connection.Label,
		Provider: connection.Provider, Origin: connection.Origin,
		ProviderRepositoryID: connection.ProviderRepositoryID,
		PathWithNamespace:    connection.PathWithNamespace,
		CanonicalWebURL:      connection.CanonicalWebURL, DefaultBranch: connection.DefaultBranch,
		CredentialExpiresAt: connection.CredentialExpiresAt, Status: connection.Status,
		LastValidatedAt: connection.LastValidatedAt,
		CreatedAt:       connection.CreatedAt, UpdatedAt: connection.UpdatedAt,
	}
}

func writeAdminRepositoryConnectionError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	safeMessage := "Repository Connection operation failed"
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
		status, safeMessage = http.StatusBadGateway, "Repository provider rejected the configured credential"
	case errors.Is(err, domain.ErrProviderUnavailable):
		status, safeMessage = http.StatusServiceUnavailable, "Repository provider is temporarily unavailable"
	case errors.Is(err, domain.ErrProviderRejected):
		status, safeMessage = http.StatusBadGateway, "Repository provider rejected the repository request"
	case errors.Is(err, domain.ErrIntegrationNotConfigured):
		status, safeMessage = http.StatusServiceUnavailable, "Repository integration is not configured"
	}
	logger := slog.With(
		"method", r.Method, "path", r.URL.Path, "status", status,
		"request_id", requestID(r), "error", err,
	)
	if status >= 500 {
		logger.Error("Repository Connection request failed")
	} else {
		logger.Warn("Repository Connection request rejected")
	}
	WriteJSON(w, status, ErrorBody{Error: safeMessage})
}
