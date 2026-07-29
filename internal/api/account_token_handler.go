package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

type accountTokenHandler struct {
	tokens *access.Service
	audit  accessAuditReader
}

type createAPITokenRequest struct {
	Name          string         `json:"name"`
	Scopes        []access.Scope `json:"scopes"`
	ExpiresInDays int            `json:"expires_in_days"`
}

type issuedAPITokenResponse struct {
	access.TokenMetadata
	Token string `json:"token"`
}

type tokenListResponse struct {
	Items []access.TokenMetadata `json:"items"`
}

func (h *accountTokenHandler) create(w http.ResponseWriter, r *http.Request) {
	current, ok := identity.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	var request createAPITokenRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	lifetime := time.Duration(request.ExpiresInDays) * 24 * time.Hour
	issued, err := h.tokens.Issue(r.Context(), access.IssueRequest{
		UserID: current.Actor.ID, Name: request.Name,
		Scopes: request.Scopes, Lifetime: lifetime,
	})
	if err != nil {
		if errors.Is(err, access.ErrInvalidName) ||
			errors.Is(err, access.ErrInvalidScope) ||
			errors.Is(err, access.ErrInvalidLifetime) {
			WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: err.Error()})
			return
		}
		slog.Error("create personal API token failed",
			"request_id", requestID(r), "user_id", current.Actor.ID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to create API token"})
		return
	}
	WriteJSON(w, http.StatusCreated, issuedAPITokenResponse{
		TokenMetadata: issued.Token, Token: issued.Value,
	})
}

func (h *accountTokenHandler) list(w http.ResponseWriter, r *http.Request) {
	current, ok := identity.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	tokens, err := h.tokens.ListUserTokens(r.Context(), current.Actor.ID)
	if err != nil {
		slog.Error("list personal API tokens failed",
			"request_id", requestID(r), "user_id", current.Actor.ID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to list API tokens"})
		return
	}
	if tokens == nil {
		tokens = []access.TokenMetadata{}
	}
	WriteJSON(w, http.StatusOK, tokenListResponse{Items: tokens})
}

func (h *accountTokenHandler) revoke(w http.ResponseWriter, r *http.Request) {
	current, ok := identity.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	tokenID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "API token not found"})
		return
	}
	if err := h.tokens.Revoke(r.Context(), tokenID, current.Actor.ID); err != nil {
		if errors.Is(err, access.ErrTokenNotFound) {
			WriteJSON(w, http.StatusNotFound, ErrorBody{Error: "API token not found"})
			return
		}
		slog.Error("revoke personal API token failed",
			"request_id", requestID(r), "user_id", current.Actor.ID,
			"token_id", tokenID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "failed to revoke API token"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *accountTokenHandler) activity(w http.ResponseWriter, r *http.Request) {
	current, ok := identity.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	filter, err := parseAccessAuditFilter(r)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	filter.UserID = &current.Actor.ID
	writeAccessAuditPage(w, r, h.audit, filter)
}
