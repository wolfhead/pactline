package api

import (
	"log/slog"
	"net/http"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

// anchorHandler implements spec §4.7's anchor list: plain CRUD over
// reference examples per level, steward-managed. No suggestion logic, no
// auto-promotion — the spec calls it "a list, no process needed", and that is
// exactly what this is.
type anchorHandler struct {
	bounties *store.BountyStore
	anchors  *store.AnchorStore
}

type anchorRequest struct {
	Dimension domain.AnchorDimension `json:"dimension"`
	Level     string                 `json:"level"`
	BountyID  uuid.UUID              `json:"bounty_id"`
	Note      string                 `json:"note"`
}

// create adds a reference example. Steward-only, per spec §4.7 ("steward-
// managed"). The referenced bounty must already exist — a dangling pointer
// into the archive would defeat the whole point of pointing at a precedent.
func (h *anchorHandler) create(w http.ResponseWriter, r *http.Request) {
	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("anchor create denied", "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req anchorRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if _, err := h.bounties.GetByID(r.Context(), req.BountyID); err != nil {
		writeError(w, r, err)
		return
	}

	a := domain.AnchorExample{Dimension: req.Dimension, Level: req.Level, BountyID: req.BountyID, Note: req.Note}
	if err := domain.ValidateAnchorExample(a); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.anchors.Create(r.Context(), a)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// list returns anchor examples, optionally filtered by dimension and/or
// level. Read access is unrestricted beyond authentication: the whole point
// of the list is that everyone doing leveling can point at it during a
// disagreement.
func (h *anchorHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AnchorFilter{Dimension: domain.AnchorDimension(q.Get("dimension")), Level: q.Get("level")}
	if f.Dimension != "" && !domain.IsValidAnchorDimension(f.Dimension) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "dimension is not a known value: " + string(f.Dimension)})
		return
	}
	out, err := h.anchors.List(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type updateAnchorRequest struct {
	Level string `json:"level"`
	Note  string `json:"note"`
}

// update changes an anchor's level and/or note. Steward-only. The dimension
// and the bounty it points at are the precedent's identity and are not
// editable here — delete and recreate if those are wrong.
func (h *anchorHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("anchor update denied", "anchor_id", id, "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req updateAnchorRequest
	if !decodeBody(w, r, &req) {
		return
	}

	existing, err := h.anchors.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	existing.Level = req.Level
	existing.Note = req.Note
	if err := domain.ValidateAnchorExample(existing); err != nil {
		writeError(w, r, err)
		return
	}

	out, err := h.anchors.Update(r.Context(), existing)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// delete removes an anchor example. Steward-only.
func (h *anchorHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("anchor delete denied", "anchor_id", id, "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	if err := h.anchors.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
