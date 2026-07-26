package api

import (
	"log/slog"
	"net/http"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type creditHandler struct {
	credits  *store.CreditStore
	bounties *store.BountyStore
}

type nominateRequest struct {
	UserID   uuid.UUID         `json:"user_id"`
	Role     domain.CreditRole `json:"role"`
	Evidence string            `json:"evidence"`
}

func (h *creditHandler) nominate(w http.ResponseWriter, r *http.Request) {
	bountyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	var req nominateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.UserID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "user_id is required"})
		return
	}

	b, err := h.bounties.GetByID(r.Context(), bountyID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	me := CurrentUser(r)
	if !domain.CanNominate(me, b) {
		slog.Warn("nomination refused",
			"bounty_id", bountyID, "actor_id", me.ID, "claimed_by", b.ClaimedBy)
		writeError(w, r, domain.ErrForbidden)
		return
	}

	nominator := me.ID
	c := domain.Credit{
		BountyID:    bountyID,
		UserID:      req.UserID,
		Role:        req.Role,
		Evidence:    req.Evidence,
		NominatedBy: &nominator,
		Status:      domain.CreditPending,
	}
	if err := domain.ValidateNomination(c); err != nil {
		writeError(w, r, err)
		return
	}

	out, err := h.credits.Nominate(r.Context(), c)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *creditHandler) listByBounty(w http.ResponseWriter, r *http.Request) {
	bountyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	out, err := h.credits.ListByBounty(r.Context(), bountyID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type respondRequest struct {
	Status domain.CreditStatus `json:"status"`
}

func (h *creditHandler) respond(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	var req respondRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Status != domain.CreditConfirmed && req.Status != domain.CreditDeclined {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "status must be CONFIRMED or DECLINED"})
		return
	}

	c, err := h.credits.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	me := CurrentUser(r)
	if err := domain.CanRespond(me, c); err != nil {
		slog.Warn("credit response refused",
			"credit_id", id, "actor_id", me.ID, "nominee_id", c.UserID,
			"credit_status", c.Status, "reason", err.Error())
		writeError(w, r, err)
		return
	}

	out, err := h.credits.Respond(r.Context(), id, req.Status)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *creditHandler) listPending(w http.ResponseWriter, r *http.Request) {
	out, err := h.credits.ListPendingForUser(r.Context(), CurrentUser(r).ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
