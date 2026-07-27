// Package api exposes the HTTP interface.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"bountyboard/internal/domain"
)

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

// writeError maps a domain error onto an HTTP status. Unknown errors become 500
// and are logged with their full text; they are never silently swallowed.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrInvalidCreditRole),
		errors.Is(err, domain.ErrInvalidValueLevel),
		errors.Is(err, domain.ErrInvalidDifficulty),
		errors.Is(err, domain.ErrInvalidCompletion),
		errors.Is(err, domain.ErrInvalidAnchorDimension),
		errors.Is(err, domain.ErrInvalidAnchorLevel),
		errors.Is(err, domain.ErrQuarterRequired):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrForbidden),
		errors.Is(err, domain.ErrNotDirectedToYou),
		errors.Is(err, domain.ErrNotYourCredit):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrInvalidTransition),
		errors.Is(err, domain.ErrRetrospectiveRequired),
		errors.Is(err, domain.ErrNotClaimable),
		errors.Is(err, domain.ErrEvidenceRequired),
		errors.Is(err, domain.ErrCreditNotPending),
		errors.Is(err, domain.ErrCreditNotDeclined),
		errors.Is(err, domain.ErrValueLevelLocked),
		errors.Is(err, domain.ErrAlreadySettled),
		errors.Is(err, domain.ErrNotSettled),
		errors.Is(err, domain.ErrUnscorable):
		status = http.StatusConflict
	}

	logger := slog.With("method", r.Method, "path", r.URL.Path, "status", status, "error", err.Error())
	if status >= 500 {
		logger.Error("request failed")
	} else {
		logger.Warn("request rejected")
	}
	writeJSON(w, status, errorBody{Error: err.Error()})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		slog.Warn("decode request body", "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid JSON body"})
		return false
	}
	return true
}
