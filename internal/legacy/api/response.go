package api

import (
	"errors"
	"log/slog"
	"net/http"

	sharedapi "bountyboard/internal/api"
	userdomain "bountyboard/internal/domain"
	"bountyboard/internal/legacy/domain"
)

// writeError maps a domain error onto an HTTP status. Unknown errors become
// 500 and are logged with their full text; they are never silently
// swallowed.
//
// This is the legacy mechanism's own copy of internal/api's WriteError,
// extended with the mechanism-specific domain errors that moved to
// internal/legacy/domain (see internal/legacy/README.md) — those errors
// live in a different package now, so the switch below can no longer be
// shared verbatim with internal/api/response.go. userdomain.ErrNotFound is
// the one domain error still shared with the rest of the application (also
// raised by the user store), and is mapped to the same 404 here as it is
// there.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrInvalidCreditRole),
		errors.Is(err, domain.ErrInvalidValueLevel),
		errors.Is(err, domain.ErrInvalidDifficulty),
		errors.Is(err, domain.ErrInvalidCompletion),
		errors.Is(err, domain.ErrInvalidAnchorDimension),
		errors.Is(err, domain.ErrInvalidAnchorLevel),
		errors.Is(err, domain.ErrQuarterRequired),
		errors.Is(err, domain.ErrInvalidQuarterFormat):
		status = http.StatusBadRequest
	case errors.Is(err, userdomain.ErrNotFound):
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
		errors.Is(err, domain.ErrUnscorable),
		errors.Is(err, domain.ErrDifficultySettled):
		status = http.StatusConflict
	}

	logger := slog.With("method", r.Method, "path", r.URL.Path, "status", status, "error", err.Error())
	if status >= 500 {
		logger.Error("request failed")
	} else {
		logger.Warn("request rejected")
	}
	sharedapi.WriteJSON(w, status, sharedapi.ErrorBody{Error: err.Error()})
}
