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
	// ValueLevel lets the sponsor set the value level "when opening", per
	// spec §6.1. There is deliberately no Difficulty field here: difficulty is
	// never the sponsor's call, even for their own bounty — see
	// domain.CanSetDifficulty. It is set only through the dedicated
	// POST /api/bounties/{id}/difficulty endpoint.
	ValueLevel domain.ValueLevel `json:"value_level"`
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
	if req.ValueLevel != "" && !domain.IsValidValueLevel(req.ValueLevel) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "value_level is not a known value: " + string(req.ValueLevel)})
		return
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
		ValueLevel:         req.ValueLevel,
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
	// Completion belongs to the DELIVERED -> COMPLETED edge only: per spec
	// §6.1 the sponsor grades completion "at acceptance", i.e. exactly when
	// they accept the delivery into COMPLETED. It is optional — leaving it
	// unset is how a Phase 1-shaped acceptance keeps working unchanged, and
	// an unscorable bounty is reported (not blocked) at settlement time.
	Completion domain.Completion `json:"completion"`
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
	if req.Completion != "" && !domain.IsValidCompletion(req.Completion) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "completion is not a known value: " + string(req.Completion)})
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
	if req.Completion != "" {
		if req.To == domain.StatusCompleted {
			b.Completion = req.Completion
		} else {
			slog.Warn("ignored completion field on transition that does not target COMPLETED",
				"bounty_id", b.ID, "actor_id", me.ID, "from", b.Status, "to", req.To, "field", "completion")
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

// amendRequest carries the fields the correction channel may touch. There is
// deliberately no field for status, sponsor_id, claimed_by or business_lines:
// any such field in the request body is simply dropped by json.Decode
// because amendRequest has nowhere to put it, so the endpoint cannot become a
// general editor by accident.
//
// ValueLevel, Difficulty and Completion were added to close C1/C2: the
// dedicated channels for these (POST .../value-level, POST .../difficulty,
// and the completion field on transition-into-COMPLETED) are each a one-way
// door — value-level locks outside DRAFT/OPEN, completion is only ever
// applied on the DELIVERED->COMPLETED edge and COMPLETED is terminal, and
// difficulty now refuses once settled (I2). A sponsor who forgets to send
// completion at acceptance, or a value level that turns out to have been
// wrong, would otherwise be permanently unrecordable — every bounty already
// past those gates is stuck with no key. Recording a grade the pricing group
// actually decided here is not inventing one; it is the opposite of the
// failure the "never default a level" rule guards against, so long as a
// human (the steward, standing in for the pricing group) is the one
// supplying it. Difficulty is the one field here that still refuses once the
// bounty is settled (see domain.CanSetDifficulty) — value level and
// completion are not similarly restricted, since correcting them after
// settlement does not touch the immutable settled_score/settled_at snapshot
// (BountyStore.Update is structurally unable to write those columns) and is
// exactly the "record it after the fact" case C1/C2 exist to unblock.
type amendRequest struct {
	Retrospective *string            `json:"retrospective"`
	PersonDays    *float64           `json:"person_days"`
	ValueLevel    *domain.ValueLevel `json:"value_level"`
	Difficulty    *domain.Difficulty `json:"difficulty"`
	Completion    *domain.Completion `json:"completion"`
}

// amend is the steward-only correction channel required by spec §6.1 (强制修正).
// The status graph is a hard gate and there is no general edit endpoint, so a
// typo in a terminal bounty's retrospective, a wrong person_days figure, or a
// grading decision made after the ordinary channel already locked would
// otherwise be permanent. amend fixes exactly that, and nothing else: it
// works on bounties in any status, including terminal and settled ones
// (except difficulty once settled — see amendRequest's doc comment), but only
// ever touches the fields amendRequest names.
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
	if req.ValueLevel != nil && !domain.IsValidValueLevel(*req.ValueLevel) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "value_level is not a known value: " + string(*req.ValueLevel)})
		return
	}
	if req.Difficulty != nil && !domain.IsValidDifficulty(*req.Difficulty) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "difficulty is not a known value: " + string(*req.Difficulty)})
		return
	}
	if req.Completion != nil && !domain.IsValidCompletion(*req.Completion) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "completion is not a known value: " + string(*req.Completion)})
		return
	}

	b, err := h.bounties.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	if req.Difficulty != nil {
		// me is already known to hold STEWARD (checked above), so this call
		// can only fail on I2's settlement lock — the same rule that governs
		// the dedicated /difficulty endpoint. There is no steward escape
		// hatch for difficulty once settled: see domain.CanSetDifficulty's
		// doc comment for why.
		if err := domain.CanSetDifficulty(me, b); err != nil {
			slog.Warn("amend difficulty denied",
				"bounty_id", id, "actor_id", me.ID, "settled_at", b.SettledAt, "error", err)
			writeError(w, r, err)
			return
		}
	}

	beforeRetrospective := b.Retrospective
	beforePersonDays := personDaysLogValue(b.PersonDays)
	beforeValueLevel := b.ValueLevel
	beforeDifficulty := b.Difficulty
	beforeCompletion := b.Completion
	if req.Retrospective != nil {
		b.Retrospective = *req.Retrospective
	}
	if req.PersonDays != nil {
		b.PersonDays = req.PersonDays
	}
	if req.ValueLevel != nil {
		b.ValueLevel = *req.ValueLevel
	}
	if req.Difficulty != nil {
		b.Difficulty = *req.Difficulty
	}
	if req.Completion != nil {
		b.Completion = *req.Completion
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
	// C1/C2: each graded field a steward actually corrected is logged on its
	// own line with actor, bounty, field name and before/after, so a
	// challenged correction can be attributed and reconstructed from logs
	// alone — the same standard settlement's logging already holds itself to.
	if req.ValueLevel != nil {
		slog.Info("bounty amended by steward: field corrected",
			"bounty_id", updated.ID, "actor_id", me.ID, "field", "value_level",
			"before", beforeValueLevel, "after", updated.ValueLevel)
	}
	if req.Difficulty != nil {
		slog.Info("bounty amended by steward: field corrected",
			"bounty_id", updated.ID, "actor_id", me.ID, "field", "difficulty",
			"before", beforeDifficulty, "after", updated.Difficulty)
	}
	if req.Completion != nil {
		slog.Info("bounty amended by steward: field corrected",
			"bounty_id", updated.ID, "actor_id", me.ID, "field", "completion",
			"before", beforeCompletion, "after", updated.Completion)
	}
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
