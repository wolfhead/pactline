package api

import (
	"log/slog"
	"math"
	"net/http"

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
