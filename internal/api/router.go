package api

import (
	"net/http"

	"bountyboard/internal/store"
)

// NewRouter builds the API handler.
func NewRouter(users *store.UserStore, bounties *store.BountyStore, credits *store.CreditStore) http.Handler {
	mux := http.NewServeMux()

	uh := &userHandler{users: users}
	bh := &bountyHandler{bounties: bounties, credits: credits}

	mux.HandleFunc("GET /api/users", uh.list)
	mux.HandleFunc("POST /api/bounties", bh.create)
	mux.HandleFunc("GET /api/bounties", bh.list)
	mux.HandleFunc("GET /api/bounties/{id}", bh.get)
	mux.HandleFunc("POST /api/bounties/{id}/transition", bh.transition)

	if credits != nil {
		ch := &creditHandler{credits: credits, bounties: bounties}
		mux.HandleFunc("POST /api/bounties/{id}/credits", ch.nominate)
		mux.HandleFunc("GET /api/bounties/{id}/credits", ch.listByBounty)
		mux.HandleFunc("POST /api/credits/{id}/respond", ch.respond)
		mux.HandleFunc("GET /api/credits/pending", ch.listPending)
	}

	return withIdentity(users, mux)
}
