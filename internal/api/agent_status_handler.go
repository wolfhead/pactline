package api

import (
	"net/http"

	"github.com/wolfhead/pactline/internal/agent/channel"
)

type agentStatusHandler struct {
	status channel.StatusProvider
}

func (h *agentStatusHandler) get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdministrator(w, r); !ok {
		return
	}
	if h.status == nil {
		WriteJSON(w, http.StatusServiceUnavailable, ErrorBody{Error: "Agent status unavailable"})
		return
	}
	WriteJSON(w, http.StatusOK, h.status.Snapshot())
}
