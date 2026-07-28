package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"bountyboard/internal/access"
	"bountyboard/internal/domain"
	"bountyboard/internal/identity"
)

type BearerAuthenticator interface {
	Authenticate(context.Context, string) (access.Principal, error)
}

type tokenOwnerVerifier interface {
	VerifyTokenOwner(context.Context, domain.User) error
}

type bearerAuthentication struct {
	tokens BearerAuthenticator
	owners tokenOwnerVerifier
}

func (m bearerAuthentication) wrap(bearerNext, sessionFallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if authorization == "" {
			sessionFallback.ServeHTTP(w, r)
			return
		}
		raw, valid := parseBearerAuthorization(authorization)
		if !valid || m.tokens == nil {
			writeBearerProblem(w, r, access.ErrTokenInvalid)
			return
		}
		principal, err := m.tokens.Authenticate(r.Context(), raw)
		if err != nil {
			writeBearerProblem(w, r, err)
			return
		}
		if m.owners != nil {
			if err := m.owners.VerifyTokenOwner(r.Context(), principal.User); err != nil {
				writeBearerProblem(w, r, err)
				return
			}
		}
		requestIdentity := identity.RequestIdentity{
			Actor: principal.User, Subject: principal.User,
			AuthenticationMethod: principal.Method,
			APITokenID:           principal.TokenID, APITokenName: principal.TokenName,
			Scopes: append([]access.Scope(nil), principal.Scopes...),
		}
		ctx := identity.WithRequestIdentity(r.Context(), requestIdentity)
		bearerNext.ServeHTTP(w, r.WithContext(ctx))
	})
}

func parseBearerAuthorization(value string) (string, bool) {
	scheme, credential, found := strings.Cut(value, " ")
	return credential, found &&
		strings.EqualFold(scheme, "Bearer") &&
		credential != "" &&
		!strings.ContainsAny(credential, " \t\r\n")
}

func writeBearerProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusUnauthorized
	title := "Authentication failed"
	detail := "The bearer token is invalid."
	code := "TOKEN_INVALID"
	switch {
	case errors.Is(err, access.ErrTokenInvalid), errors.Is(err, access.ErrTokenNotFound):
	case errors.Is(err, access.ErrTokenExpired):
		detail, code = "The bearer token has expired.", "TOKEN_EXPIRED"
	case errors.Is(err, access.ErrTokenRevoked):
		detail, code = "The bearer token has been revoked.", "TOKEN_REVOKED"
	case errors.Is(err, access.ErrUserInactive), errors.Is(err, identity.ErrUserInactive):
		status, title = http.StatusForbidden, "User inactive"
		detail, code = "The token owner is inactive.", "USER_INACTIVE"
	case errors.Is(err, identity.ErrProviderTransient):
		status, title = http.StatusServiceUnavailable, "Identity verification unavailable"
		detail, code = "The token owner's identity cannot currently be verified.", "IDENTITY_VERIFICATION_UNAVAILABLE"
	default:
		status, title = http.StatusInternalServerError, "Internal server error"
		detail, code = "The request could not be completed.", "INTERNAL_ERROR"
		slog.Error("bearer authentication failed",
			"request_id", requestID(r), "error", err)
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	WriteProblem(w, r, Problem{
		Title: title, Status: status, Detail: detail, Code: code,
	})
}

func RequireWorkRead(next http.Handler) http.Handler {
	return requireScope(access.ScopeWorkRead, next)
}

func RequireWorkWrite(next http.Handler) http.Handler {
	return requireScope(access.ScopeWorkWrite, next)
}

func requireScope(required access.Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestIdentity, ok := identity.FromContext(r.Context())
		if !ok {
			WriteProblem(w, r, Problem{
				Title: "Authentication required", Status: http.StatusUnauthorized,
				Detail: "Authentication is required.", Code: "AUTHENTICATION_REQUIRED",
			})
			return
		}
		if requestIdentity.AuthenticationMethod != access.AuthenticationMethodAPIToken {
			next.ServeHTTP(w, r)
			return
		}
		principal := access.Principal{Scopes: requestIdentity.Scopes}
		if !principal.HasScope(required) {
			WriteProblem(w, r, Problem{
				Title: "Insufficient scope", Status: http.StatusForbidden,
				Detail: "The bearer token does not grant the required scope.",
				Code:   "INSUFFICIENT_SCOPE",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isolateBearerFromInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		scheme, _, _ := strings.Cut(authorization, " ")
		v1 := r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/")
		if strings.EqualFold(scheme, "Bearer") && !v1 {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
