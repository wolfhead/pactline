package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bountyboard/internal/identity"
	"bountyboard/internal/integrations/devauth"

	"github.com/google/uuid"
)

type developmentAuthenticator interface {
	Authenticate(ctx context.Context, userID uuid.UUID, requestID string) (identity.SessionTokens, error)
}

type authHandler struct {
	sessions    *identity.Service
	development developmentAuthenticator
	cookies     cookieSettings
}

type cookieSettings struct {
	secure bool
}

func (h *authHandler) developmentSession(w http.ResponseWriter, r *http.Request) {
	var request devauth.LoginRequest
	if !DecodeBody(w, r, &request) {
		return
	}
	if request.UserID == uuid.Nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "user_id is required"})
		return
	}
	tokens, err := h.development.Authenticate(r.Context(), request.UserID, requestID(r))
	if err != nil {
		status := http.StatusInternalServerError
		message := "development session failed"
		if errors.Is(err, identity.ErrSessionInvalid) {
			status, message = http.StatusUnauthorized, "unknown user"
		} else if errors.Is(err, identity.ErrUserInactive) {
			status, message = http.StatusForbidden, "user is inactive"
		}
		logger := slog.With(
			"method", r.Method, "path", r.URL.Path, "status", status,
			"user_id", request.UserID, "error_category", sessionErrorCategory(err))
		if status >= 500 {
			logger.Error("development session creation failed")
		} else {
			logger.Warn("development session rejected")
		}
		WriteJSON(w, status, ErrorBody{Error: message})
		return
	}
	h.cookies.set(w, h.sessions, tokens)
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	requestIdentity, ok := identity.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	var impersonation *impersonationResponse
	if requestIdentity.Impersonation != nil {
		impersonation = &impersonationResponse{
			ID: requestIdentity.Impersonation.ID, SessionID: requestIdentity.Impersonation.SessionID,
			ActorUserID:   requestIdentity.Impersonation.ActorUserID,
			SubjectUserID: requestIdentity.Impersonation.SubjectUserID,
			StartedAt:     requestIdentity.Impersonation.StartedAt,
		}
	}
	WriteJSON(w, http.StatusOK, meResponse{
		Actor:         userResponseFromDomain(requestIdentity.Actor),
		Subject:       userResponseFromDomain(requestIdentity.Subject),
		Impersonation: impersonation,
	})
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	h.cookies.clear(w)
	requestIdentity, ok := identity.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	if err := h.sessions.Logout(r.Context(), requestIdentity, requestID(r)); err != nil {
		slog.Error("logout failed",
			"session_id", requestIdentity.SessionID, "actor_user_id", requestIdentity.Actor.ID,
			"error_category", "session_revoke_failed", "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "logout failed"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	Actor         userResponse           `json:"actor"`
	Subject       userResponse           `json:"subject"`
	Impersonation *impersonationResponse `json:"impersonation"`
}

type impersonationResponse struct {
	ID            uuid.UUID `json:"id"`
	SessionID     uuid.UUID `json:"session_id"`
	ActorUserID   uuid.UUID `json:"actor_user_id"`
	SubjectUserID uuid.UUID `json:"subject_user_id"`
	StartedAt     time.Time `json:"started_at"`
}

func (c cookieSettings) set(w http.ResponseWriter, sessions *identity.Service, tokens identity.SessionTokens) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: sessions.SessionCookieValue(tokens),
		Path: "/", HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: tokens.CSRFSecret,
		Path: "/", HttpOnly: false, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (c cookieSettings) clear(w http.ResponseWriter) {
	expired := time.Unix(1, 0)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, Expires: expired,
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: "", Path: "/", MaxAge: -1, Expires: expired,
		HttpOnly: false, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
}

func requestID(r *http.Request) string {
	return r.Header.Get("X-Request-Id")
}
