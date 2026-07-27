package api

import (
	"net/http"

	"bountyboard/internal/store"
)

type activityHandler struct {
	tasks *store.TaskStore
}

func (h *activityHandler) list(w http.ResponseWriter, r *http.Request) {
	number, ok := parseTaskNumber(w, r)
	if !ok {
		return
	}
	task, err := h.tasks.GetByNumber(r.Context(), number)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	entries, err := h.tasks.ListActivity(r.Context(), task.Task.ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	views := make([]activityView, len(entries))
	for i, e := range entries {
		views[i] = newActivityView(e)
	}
	WriteJSON(w, http.StatusOK, views)
}
