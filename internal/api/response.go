// Package api exposes the HTTP interface.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
)

// ErrorBody is the JSON envelope every error response uses. Exported (along
// with WriteJSON, WriteError and DecodeBody below) so internal/legacy/api's
// handlers — a separate package after the legacy split, see
// internal/legacy/README.md — can keep sharing this response-writing
// infrastructure instead of duplicating it.
type ErrorBody struct {
	Error string `json:"error"`
}

type userResponse struct {
	ID           uuid.UUID           `json:"id"`
	Name         string              `json:"name"`
	Email        *string             `json:"email"`
	AvatarURL    *string             `json:"avatar_url"`
	PlatformRole domain.PlatformRole `json:"platform_role"`
	Roles        []domain.UserRole   `json:"roles"`
	Active       bool                `json:"active"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

func userResponses(users []domain.User) []userResponse {
	out := make([]userResponse, len(users))
	for i, user := range users {
		out[i] = userResponse{
			ID:           user.ID,
			Name:         user.Name,
			Email:        user.Email,
			AvatarURL:    user.AvatarURL,
			PlatformRole: user.PlatformRole,
			Roles:        user.Roles,
			Active:       user.Active,
			CreatedAt:    user.CreatedAt,
			UpdatedAt:    user.UpdatedAt,
		}
	}
	return out
}

// WriteJSON writes v as the JSON body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

// WriteError maps a domain error onto an HTTP status. Unknown errors become
// 500 and are logged with their full text; they are never silently
// swallowed.
//
// Beyond domain.ErrNotFound (shared with the legacy mechanism's stores, e.g.
// an unknown user), this package also raises domain.ErrInvalidInput,
// domain.ErrForbidden and domain.ErrConflict for the task/comment/label
// surface — see internal/domain/errors.go for what each means.
// internal/legacy/api has its own writeError for the mechanism-specific
// domain errors that moved with it.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidInput):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
	}

	logger := slog.With("method", r.Method, "path", r.URL.Path, "status", status, "error", err.Error())
	if status >= 500 {
		logger.Error("request failed")
	} else {
		logger.Warn("request rejected")
	}
	WriteJSON(w, status, ErrorBody{Error: err.Error()})
}

// DecodeBody decodes the request body into dst, writing a 400 and returning
// false on failure.
func DecodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		slog.Warn("decode request body", "path", r.URL.Path, "error", err)
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid JSON body"})
		return false
	}
	return true
}
