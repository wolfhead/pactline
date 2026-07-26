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

type bountyHandler struct {
	bounties *store.BountyStore
	credits  *store.CreditStore
}

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

	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSponsor) && !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("create bounty denied",
			"actor_id", me.ID, "actor_roles", me.Roles, "title", req.Title)
		writeError(w, r, domain.ErrForbidden)
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
	if created.ParentID != nil && h.credits != nil {
		if n, err := h.credits.InheritDefineCredits(r.Context(), created); err != nil {
			// The bounty exists; failing to inherit must not lose it. Surface
			// the problem loudly and let the deliverer nominate manually.
			slog.Error("inherit define credits",
				"bounty_id", created.ID, "parent_id", *created.ParentID, "error", err)
		} else {
			slog.Info("define credits inherited on create",
				"bounty_id", created.ID, "count", n)
		}
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
	me := CurrentUser(r)
	if b.Status == domain.StatusDraft && !canViewDraft(me, b) {
		// 404, not 403: spec §5 says DRAFT is visible only to its sponsor —
		// "invisible", not merely "access denied". A 403 would still confirm
		// to every other engineer that a bounty with this id exists and is
		// someone's draft; 404 makes a foreign draft indistinguishable from
		// no bounty at all, which is the stronger reading of "invisible".
		slog.Warn("draft bounty hidden from non-sponsor",
			"bounty_id", b.ID, "actor_id", me.ID, "sponsor_id", b.SponsorID)
		writeError(w, r, domain.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// canViewDraft reports whether the user may see a DRAFT bounty: its sponsor,
// or a steward. This is the same predicate as domain.CanEdit — spec §5 ties
// DRAFT visibility to the same "sponsor or steward" line as editing — kept as
// a separate, locally-named check here because the call site is about
// visibility, not mutation, and a future change to one must not silently
// change the other's behaviour.
func canViewDraft(u domain.User, b domain.Bounty) bool {
	return u.ID == b.SponsorID || u.HasRole(domain.UserRoleSteward)
}

func (h *bountyHandler) list(w http.ResponseWriter, r *http.Request) {
	me := CurrentUser(r)
	q := r.URL.Query()
	// Viewer MUST be set here: this is the untrusted HTTP entry point
	// store.BountyFilter's own doc comment warns about — leaving it nil would
	// silently let any authenticated caller list every sponsor's drafts.
	f := store.BountyFilter{
		BusinessTag: q.Get("tag"),
		Viewer:      &store.DraftViewer{ID: me.ID, IsSteward: me.HasRole(domain.UserRoleSteward)},
	}
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
	// A6: reject an unknown target status before authorising. authorizeTransition's
	// fall-through (CanEdit-or-claimer) is the most permissive branch, so an
	// unknown status would otherwise inherit the loosest gate instead of being
	// rejected outright.
	if !domain.IsValidStatus(req.To) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "to is not a known status: " + string(req.To)})
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	me := CurrentUser(r)

	applyTransitionFormFields(&b, me, req)

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

// applyTransitionFormFields applies request fields that belong to the
// requested edge only: retrospective belongs to the ABANDONED edge,
// person_days belongs to the DELIVERED edge (A3). A field sent for the wrong
// edge is silently ignored as far as the transition's outcome goes — the
// transition itself must still succeed, this is not a validation error — but
// it is logged at warn so a stale field reused by a second client or a curl
// script is diagnosable purely from logs.
func applyTransitionFormFields(b *domain.Bounty, me domain.User, req transitionRequest) {
	if req.Retrospective != "" {
		if req.To == domain.StatusAbandoned {
			b.Retrospective = req.Retrospective
		} else {
			slog.Warn("ignored retrospective field on transition that does not target ABANDONED",
				"bounty_id", b.ID, "actor_id", me.ID, "from", b.Status, "to", req.To, "field", "retrospective")
		}
	}
	if req.PersonDays != nil {
		if req.To == domain.StatusDelivered {
			b.PersonDays = req.PersonDays
		} else {
			slog.Warn("ignored person_days field on transition that does not target DELIVERED",
				"bounty_id", b.ID, "actor_id", me.ID, "from", b.Status, "to", req.To, "field", "person_days")
		}
	}
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

// amendRequest carries only the two fields the correction channel may touch.
// There is deliberately no field for status, sponsor_id, claimed_by or
// business_lines: any such field in the request body is simply dropped by
// json.Decode because amendRequest has nowhere to put it, so the endpoint
// cannot become a general editor by accident.
type amendRequest struct {
	Retrospective *string  `json:"retrospective"`
	PersonDays    *float64 `json:"person_days"`
}

// amend is the steward-only correction channel required by spec §6.1 (强制修正).
// The status graph is a hard gate and there is no edit endpoint, so a typo in
// a terminal bounty's retrospective or a wrong person_days figure would
// otherwise be permanent. amend fixes exactly that, and nothing else: it
// works on bounties in any status, including terminal ones, but only ever
// touches retrospective and person_days.
func (h *bountyHandler) amend(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("amend denied", "bounty_id", id, "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req amendRequest
	if !decodeBody(w, r, &req) {
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	beforeRetrospective := b.Retrospective
	beforePersonDays := personDaysLogValue(b.PersonDays)
	if req.Retrospective != nil {
		b.Retrospective = *req.Retrospective
	}
	if req.PersonDays != nil {
		b.PersonDays = req.PersonDays
	}

	updated, err := h.bounties.Update(r.Context(), b)
	if err != nil {
		writeError(w, r, err)
		return
	}
	slog.Info("bounty amended by steward",
		"bounty_id", updated.ID, "actor_id", me.ID, "status", updated.Status,
		"retrospective_before", beforeRetrospective, "retrospective_after", updated.Retrospective,
		"person_days_before", beforePersonDays, "person_days_after", personDaysLogValue(updated.PersonDays))
	writeJSON(w, http.StatusOK, updated)
}

// personDaysLogValue turns a possibly-nil *float64 into a slog-friendly value
// (the dereferenced number, or nil) instead of a pointer address.
func personDaysLogValue(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
