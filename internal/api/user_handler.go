package api

import (
	"net/http"

	"bountyboard/internal/store"
)

type userHandler struct{ users *store.UserStore }

func (h *userHandler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.users.ListActive(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponses(out))
}
