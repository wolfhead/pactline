package api

import (
	"net/http"

	"bountyboard/internal/legacy/store"
	sharedstore "bountyboard/internal/store"
)

// NewRouter builds the legacy mechanism's HTTP handler: bounties, credits,
// the work feed, portfolios, settlement, calibration and the anchor list —
// every route this package owns, registered under the /api/legacy/ prefix.
// See internal/legacy/README.md for why this mechanism moved here.
//
// The caller (internal/api.NewRouter) mounts the returned handler at
// /api/legacy/ and wraps the combined mux in the identity middleware exactly
// once — this router does not wrap itself a second time.
func NewRouter(
	users *sharedstore.UserStore,
	bounties *store.BountyStore,
	credits *store.CreditStore,
	calibrations *store.CalibrationStore,
	anchors *store.AnchorStore,
) http.Handler {
	mux := http.NewServeMux()

	bh := &bountyHandler{bounties: bounties, credits: credits}
	mux.HandleFunc("POST /api/legacy/bounties", bh.create)
	mux.HandleFunc("GET /api/legacy/bounties", bh.list)
	mux.HandleFunc("GET /api/legacy/bounties/{id}", bh.get)
	mux.HandleFunc("POST /api/legacy/bounties/{id}/transition", bh.transition)
	mux.HandleFunc("POST /api/legacy/bounties/{id}/amend", bh.amend)
	mux.HandleFunc("POST /api/legacy/bounties/{id}/value-level", bh.setValueLevel)
	mux.HandleFunc("POST /api/legacy/bounties/{id}/difficulty", bh.setDifficulty)

	ch := &creditHandler{credits: credits, bounties: bounties}
	mux.HandleFunc("POST /api/legacy/bounties/{id}/credits", ch.nominate)
	mux.HandleFunc("GET /api/legacy/bounties/{id}/credits", ch.listByBounty)
	mux.HandleFunc("POST /api/legacy/credits/{id}/respond", ch.respond)
	mux.HandleFunc("POST /api/legacy/credits/{id}/reset", ch.reset)
	mux.HandleFunc("GET /api/legacy/credits/pending", ch.listPending)

	fh := &feedHandler{bounties: bounties, credits: credits, users: users}
	mux.HandleFunc("GET /api/legacy/works", fh.works)
	mux.HandleFunc("GET /api/legacy/users/{id}/portfolio", fh.portfolio)

	sh := &settlementHandler{bounties: bounties}
	mux.HandleFunc("POST /api/legacy/settlements", sh.settle)

	calh := &calibrationHandler{bounties: bounties, calibrations: calibrations}
	mux.HandleFunc("POST /api/legacy/bounties/{id}/calibrations", calh.create)
	mux.HandleFunc("GET /api/legacy/bounties/{id}/calibrations", calh.list)

	ah := &anchorHandler{bounties: bounties, anchors: anchors}
	mux.HandleFunc("POST /api/legacy/anchors", ah.create)
	mux.HandleFunc("GET /api/legacy/anchors", ah.list)
	mux.HandleFunc("PUT /api/legacy/anchors/{id}", ah.update)
	mux.HandleFunc("DELETE /api/legacy/anchors/{id}", ah.delete)

	return mux
}
