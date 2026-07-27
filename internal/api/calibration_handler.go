package api

import (
	"log/slog"
	"net/http"

	"bountyboard/internal/domain"
	"bountyboard/internal/scoring"
	"bountyboard/internal/store"

	"github.com/google/uuid"
)

// calibrationHandler implements spec §4.6's quarterly value calibration: an
// independent correction of a bounty's claimed value against what it turned
// out to be worth, recorded as its own row rather than mutating the
// settlement snapshot.
type calibrationHandler struct {
	bounties     *store.BountyStore
	calibrations *store.CalibrationStore
}

type createCalibrationRequest struct {
	Quarter         string            `json:"quarter"`
	CalibratedValue domain.ValueLevel `json:"calibrated_value"`
	Note            string            `json:"note"`
}

// create records a calibration. Steward-only: spec §7.1's mechanism doc
// explains why this cannot be the sponsor's own call — they set the original
// value, so grading themselves would reintroduce the exact incentive to
// inflate that the calibration step exists to check. OriginalValue is always
// captured from the bounty's own record, never accepted from the request
// body, so it cannot be backdated to a value the bounty never actually
// carried.
func (h *calibrationHandler) create(w http.ResponseWriter, r *http.Request) {
	bountyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	me := CurrentUser(r)
	if !me.HasRole(domain.UserRoleSteward) {
		slog.Warn("calibration denied", "bounty_id", bountyID, "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req createCalibrationRequest
	if !decodeBody(w, r, &req) {
		return
	}

	b, err := h.bounties.GetByID(r.Context(), bountyID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if b.SettledAt == nil {
		slog.Warn("calibration refused: bounty not settled", "bounty_id", bountyID, "actor_id", me.ID)
		writeError(w, r, domain.ErrNotSettled)
		return
	}

	calibratedScore, err := scoring.ScoreWithValueLevel(b, req.CalibratedValue)
	if err != nil {
		slog.Warn("calibration refused: cannot compute calibrated score",
			"bounty_id", bountyID, "actor_id", me.ID, "calibrated_value", req.CalibratedValue, "error", err)
		writeError(w, r, err)
		return
	}

	c := domain.Calibration{
		BountyID:        bountyID,
		Quarter:         req.Quarter,
		OriginalValue:   b.ValueLevel,
		CalibratedValue: req.CalibratedValue,
		CalibratedScore: calibratedScore,
		Note:            req.Note,
		CreatedBy:       me.ID,
	}
	if err := domain.ValidateCalibration(c); err != nil {
		writeError(w, r, err)
		return
	}

	out, err := h.calibrations.Create(r.Context(), c)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// list returns every calibration recorded against a bounty. Read access is
// unrestricted beyond authentication, like credits and the work feed: a
// calibration is meant to be visible, not merely applied.
func (h *calibrationHandler) list(w http.ResponseWriter, r *http.Request) {
	bountyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "id is not a UUID"})
		return
	}
	out, err := h.calibrations.ListByBounty(r.Context(), bountyID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
