package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

const (
	sessionCookieName = "bb_session"
	csrfCookieName    = "bb_csrf"
)

type identityMiddleware struct {
	sessions   *identity.Service
	appBaseURL *url.URL
	cookies    cookieSettings
	routes     routeResolver
}

func (m identityMiddleware) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		markAccessAuthentication(
			r, access.AuthenticationMethodSession, access.AuthOutcomeRejected,
			nil, nil, nil, "",
		)
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			slog.Warn("request without application session", "method", r.Method, "path", r.URL.Path)
			WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
			return
		}
		requestIdentity, session, err := m.sessions.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			m.cookies.clear(w)
			status := http.StatusUnauthorized
			message := "invalid or expired session"
			if errors.Is(err, identity.ErrUserInactive) {
				status = http.StatusForbidden
				message = "user is inactive"
			}
			slog.Warn("application session rejected",
				"method", r.Method, "path", r.URL.Path, "status", status, "error_category", sessionErrorCategory(err))
			WriteJSON(w, status, ErrorBody{Error: message})
			return
		}
		actorID := requestIdentity.Actor.ID
		markAccessAuthentication(
			r, access.AuthenticationMethodSession, access.AuthOutcomeAuthenticated,
			&actorID, nil, nil, "",
		)
		routePattern := "unmatched"
		if m.routes != nil {
			_, pattern := m.routes.Handler(r)
			if pattern != "" {
				routePattern = pattern
				if separator := strings.IndexByte(routePattern, ' '); separator >= 0 {
					routePattern = routePattern[separator+1:]
				}
			}
		}
		adminRouteDenied := requestIdentity.IsImpersonating() &&
			strings.HasPrefix(r.URL.Path, "/api/admin/") &&
			!(r.Method == http.MethodDelete && routePattern == "/api/admin/impersonation")
		writeDenied := requestIdentity.IsImpersonating() &&
			!identity.RequestAllowedDuringImpersonation(r.Method, routePattern)
		if adminRouteDenied || writeDenied {
			if writeDenied {
				if auditErr := m.sessions.RecordImpersonationWriteRejected(
					r.Context(), requestIdentity, r.Method, routePattern, requestID(r),
				); auditErr != nil {
					slog.Error("record impersonation write rejection failed",
						"method", r.Method, "route", routePattern, "session_id", requestIdentity.SessionID,
						"error", auditErr)
				}
			}
			slog.Warn("impersonated state change rejected",
				"method", r.Method, "route", routePattern,
				"actor_user_id", requestIdentity.Actor.ID, "subject_user_id", requestIdentity.Subject.ID,
				"session_id", requestIdentity.SessionID)
			message := "impersonation is read-only"
			if adminRouteDenied {
				message = "administrator routes are unavailable during impersonation"
			}
			WriteJSON(w, http.StatusForbidden, ErrorBody{Error: message})
			return
		}
		if methodRequiresCSRF(r.Method) {
			if !sameOriginRequest(r, m.appBaseURL) || !m.sessions.VerifyCSRF(session, r.Header.Get("X-CSRF-Token")) {
				slog.Warn("csrf validation rejected",
					"method", r.Method, "path", r.URL.Path, "session_id", requestIdentity.SessionID)
				WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "CSRF validation failed"})
				return
			}
		}
		ctx := identity.WithRequestIdentity(r.Context(), requestIdentity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func IdentityFromContext(ctx context.Context) (identity.RequestIdentity, bool) {
	return identity.FromContext(ctx)
}

func SubjectUserID(ctx context.Context) (uuid.UUID, bool) {
	return identity.SubjectUserID(ctx)
}

func ActorUserID(ctx context.Context) (uuid.UUID, bool) {
	return identity.ActorUserID(ctx)
}

// CurrentUser is the effective subject. Existing task and legacy handlers
// intentionally continue to consume only the internal subject UUID.
func CurrentUser(r *http.Request) domain.User {
	requestIdentity, _ := identity.FromContext(r.Context())
	return requestIdentity.Subject
}

func CurrentActor(r *http.Request) domain.User {
	requestIdentity, _ := identity.FromContext(r.Context())
	return requestIdentity.Actor
}

func methodRequiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func sameOriginRequest(r *http.Request, appBaseURL *url.URL) bool {
	if appBaseURL == nil {
		return false
	}
	fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if fetchSite != "" && fetchSite != "same-origin" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	value, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(value.Scheme, appBaseURL.Scheme) &&
		strings.EqualFold(value.Host, appBaseURL.Host)
}

func sessionErrorCategory(err error) string {
	switch {
	case errors.Is(err, identity.ErrUserInactive):
		return "user_inactive"
	case errors.Is(err, identity.ErrSessionExpired):
		return "session_expired"
	case errors.Is(err, identity.ErrSessionRevoked):
		return "session_revoked"
	default:
		return "session_invalid"
	}
}
