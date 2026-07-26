package api

import (
	"log/slog"
	"math"
	"net/http"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type bountyHandler struct{ bounties *store.BountyStore }

type createBountyRequest struct {
	Type               domain.BountyType     `json:"type"`
	ParentID           *uuid.UUID            `json:"parent_id"`
	Title              string                `json:"title"`
	Goal               string                `json:"goal"`
	AcceptanceCriteria string                `json:"acceptance_criteria"`
	Visibility         domain.Visibility     `json:"visibility"`
	Restriction        string                `json:"restriction"`
	DirectedTo         *uuid.UUID            `json:"directed_to"`
	BusinessLines      []domain.BusinessLine `json:"business_lines"`
	Commitment         domain.Commitment     `json:"commitment"`
}

func (h *bountyHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createBountyRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "title is required"})
		return
	}
	if req.Visibility == "" {
		req.Visibility = domain.VisibilityPublic
	}
	if req.Commitment == "" {
		req.Commitment = domain.CommitmentCommitted
	}
	if req.Type == "" {
		req.Type = domain.BountyTypeDelivery
	}

	// Attribution weights are expected to sum to 1, but a mismatch is a warning
	// rather than a rejection — the mechanism deliberately leaves this to team
	// convention and surfaces drift at settlement instead.
	if sum := domain.BusinessLineWeightSum(req.BusinessLines); len(req.BusinessLines) > 0 && math.Abs(sum-1) > 1e-6 {
		slog.Warn("business line weights do not sum to 1", "sum", sum, "title", req.Title)
	}

	me := CurrentUser(r)
	created, err := h.bounties.Create(r.Context(), domain.Bounty{
		Type:               req.Type,
		ParentID:           req.ParentID,
		Title:              req.Title,
		Goal:               req.Goal,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Visibility:         req.Visibility,
		Restriction:        req.Restriction,
		DirectedTo:         req.DirectedTo,
		BusinessLines:      req.BusinessLines,
		Commitment:         req.Commitment,
		Status:             domain.StatusDraft,
		SponsorID:          me.ID,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *bountyHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (h *bountyHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.BountyFilter{BusinessTag: q.Get("tag")}
	for _, s := range q["status"] {
		st := domain.Status(s)
		if !domain.IsValidStatus(st) {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "status is not a known value: " + s})
			return
		}
		f.Statuses = append(f.Statuses, st)
	}
	if t := q.Get("type"); t != "" {
		bt := domain.BountyType(t)
		if !domain.IsValidBountyType(bt) {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "type is not a known value: " + t})
			return
		}
		f.Type = &bt
	}
	if v := q.Get("claimed_by"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "claimed_by is not a UUID"})
			return
		}
		f.ClaimedBy = &id
	}
	if v := q.Get("sponsor_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "sponsor_id is not a UUID"})
			return
		}
		f.SponsorID = &id
	}
	f.OrderByCompletedAt = q.Get("order") == "completed_at"

	out, err := h.bounties.List(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type transitionRequest struct {
	To            domain.Status `json:"to"`
	Retrospective string        `json:"retrospective"`
	PersonDays    *float64      `json:"person_days"`
}

// transition moves a bounty along the status graph and applies the side effects
// tied to each edge. Authorisation depends on the target state: claiming is
// governed by CanClaim; accepting into COMPLETED requires CanEdit (sponsor or
// steward) only; every other edge accepts CanEdit or being the claimer.
func (h *bountyHandler) transition(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	var req transitionRequest
	if !decodeBody(w, r, &req) {
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	me := CurrentUser(r)

	if req.Retrospective != "" {
		b.Retrospective = req.Retrospective
	}
	if req.PersonDays != nil {
		b.PersonDays = req.PersonDays
	}

	if err := authorizeTransition(me, b, req.To); err != nil {
		slog.Warn("transition denied",
			"bounty_id", b.ID, "actor_id", me.ID, "from", b.Status, "to", req.To, "error", err)
		writeError(w, r, err)
		return
	}
	if err := domain.ValidateTransition(b, req.To); err != nil {
		slog.Warn("transition rejected",
			"bounty_id", b.ID, "actor_id", me.ID, "from", b.Status, "to", req.To, "error", err)
		writeError(w, r, err)
		return
	}

	from := b.Status
	applyTransitionEffects(&b, me, req.To)
	b.Status = req.To

	updated, err := h.bounties.Update(r.Context(), b)
	if err != nil {
		writeError(w, r, err)
		return
	}
	slog.Info("bounty transitioned",
		"bounty_id", updated.ID, "from", from, "to", updated.Status,
		"actor_id", me.ID, "person_days", updated.PersonDays)
	writeJSON(w, http.StatusOK, updated)
}

func authorizeTransition(me domain.User, b domain.Bounty, to domain.Status) error {
	if me.HasRole(domain.UserRoleSteward) {
		return nil
	}
	if to == domain.StatusClaimed && b.Status == domain.StatusOpen {
		// This is the actual claim edge (OPEN -> CLAIMED); CanClaim enforces
		// role and DIRECTED-visibility rules. The other edge that targets
		// CLAIMED is DELIVERED -> CLAIMED (the claimer handing the bounty
		// back), which falls through to the CanEdit/isClaimer check below —
		// CanClaim does not apply there since the bounty is no longer OPEN.
		return domain.CanClaim(me, b)
	}
	if to == domain.StatusCompleted {
		// Acceptance is the sponsor's (or a steward's, handled above) call.
		// The claimer must not be able to accept their own delivery.
		if domain.CanEdit(me, b) {
			return nil
		}
		return domain.ErrForbidden
	}
	isClaimer := b.ClaimedBy != nil && *b.ClaimedBy == me.ID
	if domain.CanEdit(me, b) || isClaimer {
		return nil
	}
	return domain.ErrForbidden
}

func applyTransitionEffects(b *domain.Bounty, me domain.User, to domain.Status) {
	now := time.Now().UTC()
	switch to {
	case domain.StatusClaimed:
		// transitionRequest carries no field for naming another claimer, so
		// ClaimedBy is always set to the caller's own ID here. The nil check
		// guards the one edge where ClaimedBy is already set: walking a
		// DELIVERED bounty back to CLAIMED, where it preserves the original
		// claimer and claim timestamp instead of overwriting them.
		if b.ClaimedBy == nil {
			claimer := me.ID
			b.ClaimedBy = &claimer
			b.ClaimedAt = &now
		}
	case domain.StatusOpen:
		b.ClaimedBy = nil
		b.ClaimedAt = nil
	case domain.StatusCompleted, domain.StatusAbandoned:
		b.CompletedAt = &now
	}
}
