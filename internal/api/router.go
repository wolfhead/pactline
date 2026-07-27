package api

import (
	"net/http"

	"bountyboard/internal/store"
)

// NewRouter builds the top-level API handler: the shared surface (currently
// just user listing) under the plain /api/ prefix, plus every mechanism
// endpoint mounted under /api/legacy/ (see legacyMux, and
// internal/legacy/README.md for why the mechanism lives there). Both are
// wrapped in the same identity middleware — legacy has no separate
// authentication story of its own.
func NewRouter(
	users *store.UserStore,
	legacyMux http.Handler,
) http.Handler {
	mux := http.NewServeMux()

	uh := &userHandler{users: users}
	mux.HandleFunc("GET /api/users", uh.list)

	mux.Handle("/api/legacy/", legacyMux)

	return withIdentity(users, mux)
}
