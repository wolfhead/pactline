package api

import (
	"net/http"

	"bountyboard/internal/store"
)

// NewRouter builds the API handler. credits may be nil before Task 9; the
// credit routes are then simply not registered.
func NewRouter(users *store.UserStore, bounties *store.BountyStore, credits *store.CreditStore) http.Handler {
	mux := http.NewServeMux()

	uh := &userHandler{users: users}
	bh := &bountyHandler{bounties: bounties}

	mux.HandleFunc("GET /api/users", uh.list)
	mux.HandleFunc("POST /api/bounties", bh.create)
	mux.HandleFunc("GET /api/bounties", bh.list)
	mux.HandleFunc("GET /api/bounties/{id}", bh.get)
	mux.HandleFunc("POST /api/bounties/{id}/transition", bh.transition)

	return withIdentity(users, mux)
}
