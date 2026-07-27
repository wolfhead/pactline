package api

import (
	"net/http"

	"bountyboard/internal/store"
)

// NewRouter builds the API handler.
func NewRouter(
	users *store.UserStore,
	bounties *store.BountyStore,
	credits *store.CreditStore,
	calibrations *store.CalibrationStore,
	anchors *store.AnchorStore,
) http.Handler {
	mux := http.NewServeMux()

	uh := &userHandler{users: users}
	bh := &bountyHandler{bounties: bounties, credits: credits}

	mux.HandleFunc("GET /api/users", uh.list)
	mux.HandleFunc("POST /api/bounties", bh.create)
	mux.HandleFunc("GET /api/bounties", bh.list)
	mux.HandleFunc("GET /api/bounties/{id}", bh.get)
	mux.HandleFunc("POST /api/bounties/{id}/transition", bh.transition)
	mux.HandleFunc("POST /api/bounties/{id}/amend", bh.amend)
	mux.HandleFunc("POST /api/bounties/{id}/value-level", bh.setValueLevel)
	mux.HandleFunc("POST /api/bounties/{id}/difficulty", bh.setDifficulty)

	ch := &creditHandler{credits: credits, bounties: bounties}
	mux.HandleFunc("POST /api/bounties/{id}/credits", ch.nominate)
	mux.HandleFunc("GET /api/bounties/{id}/credits", ch.listByBounty)
	mux.HandleFunc("POST /api/credits/{id}/respond", ch.respond)
	mux.HandleFunc("POST /api/credits/{id}/reset", ch.reset)
	mux.HandleFunc("GET /api/credits/pending", ch.listPending)

	fh := &feedHandler{bounties: bounties, credits: credits, users: users}
	mux.HandleFunc("GET /api/works", fh.works)
	mux.HandleFunc("GET /api/users/{id}/portfolio", fh.portfolio)

	sh := &settlementHandler{bounties: bounties}
	mux.HandleFunc("POST /api/settlements", sh.settle)

	calh := &calibrationHandler{bounties: bounties, calibrations: calibrations}
	mux.HandleFunc("POST /api/bounties/{id}/calibrations", calh.create)
	mux.HandleFunc("GET /api/bounties/{id}/calibrations", calh.list)

	ah := &anchorHandler{bounties: bounties, anchors: anchors}
	mux.HandleFunc("POST /api/anchors", ah.create)
	mux.HandleFunc("GET /api/anchors", ah.list)
	mux.HandleFunc("PUT /api/anchors/{id}", ah.update)
	mux.HandleFunc("DELETE /api/anchors/{id}", ah.delete)

	return withIdentity(users, mux)
}
