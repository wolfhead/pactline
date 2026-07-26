package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

// unknownUserNamePlaceholder marks a credit whose nominee could not be found
// in the user map. Since credits.user_id has a foreign key into users, this
// should be unreachable in practice — the map is now built from ListAll, not
// ListActive, so deactivation can no longer cause it. It is deliberately
// unlike any seeded display name so it reads as a data problem rather than
// an unnamed person.
const unknownUserNamePlaceholder = "[unknown user]"

// CreditView pairs a credit with the nominee's display name so the frontend
// need not join users itself.
type CreditView struct {
	Credit   domain.Credit `json:"credit"`
	UserName string        `json:"user_name"`
}

// WorkView is a terminal bounty plus its confirmed credits — the shape the work
// feed and personal portfolios render.
type WorkView struct {
	Bounty  domain.Bounty `json:"bounty"`
	Credits []CreditView  `json:"credits"`
}

type feedHandler struct {
	bounties *store.BountyStore
	credits  *store.CreditStore
	users    *store.UserStore
}

// works returns the feed: everything terminal, newest completion first.
// Abandoned work is included by design — archiving failure with its conclusion
// is what makes hard problems safe to attempt.
func (h *feedHandler) works(w http.ResponseWriter, r *http.Request) {
	list, err := h.bounties.List(r.Context(), store.BountyFilter{
		Statuses:           []domain.Status{domain.StatusCompleted, domain.StatusAbandoned},
		BusinessTag:        r.URL.Query().Get("tag"),
		OrderByCompletedAt: true,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	out, err := h.decorate(r.Context(), list)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// portfolio returns every terminal work the user holds a CONFIRMED credit on.
func (h *feedHandler) portfolio(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}

	// A real user with no credits is "200 []" (they have no works). An
	// unknown user id must be 404, not the same empty list — the two read
	// very differently: "no works yet" versus "the system has never heard of
	// this person". GetByID does not filter on active, so a deactivated
	// user's own portfolio still resolves.
	if _, err := h.users.GetByID(r.Context(), userID); err != nil {
		writeError(w, r, err)
		return
	}

	byBounty, err := h.credits.ListConfirmedBountyIDsForUser(r.Context(), userID)
	if err != nil {
		writeError(w, r, err)
		return
	}

	all, err := h.bounties.List(r.Context(), store.BountyFilter{
		Statuses:           []domain.Status{domain.StatusCompleted, domain.StatusAbandoned},
		OrderByCompletedAt: true,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	mine := make([]domain.Bounty, 0, len(byBounty))
	for _, b := range all {
		if _, ok := byBounty[b.ID]; ok {
			mine = append(mine, b)
		}
	}

	out, err := h.decorate(r.Context(), mine)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// decorate attaches confirmed credits and nominee names to each bounty.
//
// Names are resolved against ALL users, not just active ones (ListAll, not
// ListActive): the archive's whole point is that doing the work leaves a
// durable trace, and that trace must not go blank the moment someone leaves
// the team. GET /api/users (the identity switcher and nomination picker)
// deliberately keeps using ListActive instead — those two call sites want
// different populations and must not be conflated.
func (h *feedHandler) decorate(ctx context.Context, list []domain.Bounty) ([]WorkView, error) {
	users, err := h.users.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}

	out := make([]WorkView, 0, len(list))
	for _, b := range list {
		credits, err := h.credits.ListByBounty(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		views := []CreditView{}
		for _, c := range credits {
			// The mechanism's one hard constraint: unconfirmed credits are
			// invisible to every tally and every view.
			if c.Status != domain.CreditConfirmed {
				continue
			}
			name, ok := names[c.UserID]
			if !ok {
				slog.Warn("credit user missing from user map",
					"credit_id", c.ID, "bounty_id", c.BountyID, "user_id", c.UserID)
				name = unknownUserNamePlaceholder
			}
			views = append(views, CreditView{Credit: c, UserName: name})
		}
		sort.SliceStable(views, func(i, j int) bool {
			return creditRoleOrder(views[i].Credit.Role) < creditRoleOrder(views[j].Credit.Role)
		})
		out = append(out, WorkView{Bounty: b, Credits: views})
	}
	return out, nil
}

// creditRoleOrder fixes the display order of credits, mirroring how film
// credits are ordered by part rather than alphabetically.
func creditRoleOrder(r domain.CreditRole) int {
	switch r {
	case domain.CreditRoleDefine:
		return 0
	case domain.CreditRoleLead:
		return 1
	case domain.CreditRoleCoDeliver:
		return 2
	case domain.CreditRoleReview:
		return 3
	case domain.CreditRoleSupport:
		return 4
	case domain.CreditRoleBaseline:
		return 5
	default:
		return 9
	}
}
