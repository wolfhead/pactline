package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type ctxKey int

const userKey ctxKey = iota

// withIdentity resolves the X-User-Id header into a domain.User.
//
// Phase 1 has no authentication on purpose: the frontend ships a user switcher
// so the whole loop can be exercised locally. This middleware and the switcher
// are both removed when Feishu OAuth lands in Phase 6.
func withIdentity(users *store.UserStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("X-User-Id")
		if raw == "" {
			slog.Warn("request without identity", "path", r.URL.Path)
			WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "X-User-Id header is required"})
			return
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			slog.Warn("malformed identity header", "path", r.URL.Path, "value", raw)
			WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "X-User-Id is not a UUID"})
			return
		}
		u, err := users.GetByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				slog.Warn("unknown identity", "path", r.URL.Path, "user_id", id, "error", err)
				WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "unknown user"})
				return
			}
			slog.Error("identity resolution failed", "path", r.URL.Path, "user_id", id, "error", err)
			WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "identity resolution failed"})
			return
		}
		// Decision: "active" governs who may ACT — every endpoint reachable
		// through this middleware, read or write — and who appears in
		// pickers (UserStore.ListActive, backing GET /api/users). It never
		// governs who is REMEMBERED: credit names are resolved from
		// UserStore.ListAll regardless of active (see feed_handler.decorate),
		// so a person who has left still keeps their name on the work they
		// are credited on. Gating here, in the one place identity is
		// resolved, closes every mutating endpoint to a deactivated user
		// without each handler re-checking the flag — and closes reads too,
		// since a deactivated user has left and has no standing to ask the
		// system anything, not just to change it.
		if !u.Active {
			slog.Warn("deactivated identity denied", "path", r.URL.Path, "user_id", id)
			WriteJSON(w, http.StatusForbidden, ErrorBody{Error: "user is deactivated"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// CurrentUser returns the caller resolved by withIdentity.
func CurrentUser(r *http.Request) domain.User {
	u, _ := r.Context().Value(userKey).(domain.User)
	return u
}
