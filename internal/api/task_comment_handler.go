package api

import (
	"net/http"

	"bountyboard/internal/store"

	"github.com/google/uuid"
)

type commentHandler struct {
	tasks    *store.TaskStore
	comments *store.CommentStore
}

type commentBodyRequest struct {
	Body string `json:"body"`
}

func (h *commentHandler) list(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	task, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	cs, err := h.comments.List(r.Context(), task.Task.ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	views := make([]commentView, len(cs))
	for i, c := range cs {
		views[i] = newCommentView(c)
	}
	WriteJSON(w, http.StatusOK, views)
}

func (h *commentHandler) create(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	var req commentBodyRequest
	if !DecodeBody(w, r, &req) {
		return
	}
	task, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	me := CurrentUser(r)
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	c, err := h.comments.CreateWithOperation(r.Context(), task.Task.ID, me.ID, req.Body, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, newCommentView(c))
}

func (h *commentHandler) update(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "id is not a UUID"})
		return
	}
	var req commentBodyRequest
	if !DecodeBody(w, r, &req) {
		return
	}
	task, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	c, err := h.comments.UpdateWithOperation(r.Context(), task.Task.ID, id, req.Body, actor)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newCommentView(c))
}

func (h *commentHandler) delete(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "id is not a UUID"})
		return
	}
	task, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	actor, ok := operationActor(r)
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "authentication required"})
		return
	}
	if err := h.comments.DeleteWithOperation(r.Context(), task.Task.ID, id, actor); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusNoContent, nil)
}
