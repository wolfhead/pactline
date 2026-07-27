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
	l, err := h.labels.Create(r.Context(), req.Name)
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
	l, err := h.labels.Rename(r.Context(), id, req.Name)
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
	if err := h.labels.Delete(r.Context(), id); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusNoContent, nil)
}
