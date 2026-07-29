package api

import (
	"net/http"

	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type labelHandler struct {
	labels *store.LabelStore
}

type labelNameRequest struct {
	Name string `json:"name"`
}

func (h *labelHandler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.labels.List(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *labelHandler) create(w http.ResponseWriter, r *http.Request) {
	var req labelNameRequest
	if !DecodeBody(w, r, &req) {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	l, err := h.labels.CreateWithOperation(r.Context(), req.Name, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, l)
}

func (h *labelHandler) rename(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "id is not a UUID"})
		return
	}
	var req labelNameRequest
	if !DecodeBody(w, r, &req) {
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	l, err := h.labels.RenameWithOperation(r.Context(), id, req.Name, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, l)
}

func (h *labelHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "id is not a UUID"})
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	if err := h.labels.DeleteWithOperation(r.Context(), id, actor); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusNoContent, nil)
}
