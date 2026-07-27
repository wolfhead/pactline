package api

import (
	"log/slog"
	"net/http"

	sharedapi "bountyboard/internal/api"
	userdomain "bountyboard/internal/domain"
	"bountyboard/internal/legacy/domain"
	"bountyboard/internal/legacy/scoring"
	"bountyboard/internal/legacy/store"

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
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "id is not a UUID"})
		return
	}
	me := sharedapi.CurrentUser(r)
	if !me.HasRole(userdomain.UserRoleSteward) {
		slog.Warn("calibration denied", "bounty_id", bountyID, "actor_id", me.ID, "actor_roles", me.Roles)
		writeError(w, r, domain.ErrForbidden)
		return
	}
	var req createCalibrationRequest
	if !sharedapi.DecodeBody(w, r, &req) {
		return
	}

	// Validate the calibrated value before computing anything from it (minor
	// fix): a bad level is a client input error (400), not a state conflict
	// (409). Previously this fell through to scoring.ScoreWithValueLevel,
	// whose failure wraps domain.ErrUnscorable and maps to 409 — a caller who
	// mistyped a value level got the wrong signal about what went wrong.
	if !domain.IsValidValueLevel(req.CalibratedValue) {
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "calibrated_value is not a known value: " + string(req.CalibratedValue)})
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

	// original_score is read from the bounty's own settlement snapshot, same
	// as original_value — never accepted from the request body. b.SettledAt
	// != nil (checked above) guarantees b.SettledScore is non-nil: Settle
	// always writes both columns together.
	var originalScore float64
	if b.SettledScore != nil {
		originalScore = *b.SettledScore
	}

	c := domain.Calibration{
		BountyID:        bountyID,
		Quarter:         req.Quarter,
		OriginalValue:   b.ValueLevel,
		OriginalScore:   originalScore,
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
	// Minor fix: the calibration store's own log line carries the calibration
	// row's fields, but not the difficulty/completion that also fed
	// calibratedScore's computation (scoring.ScoreWithValueLevel uses the
	// bounty's difficulty, completion and commitment alongside the calibrated
	// value). Without those, a disputed calibrated score cannot be
	// re-derived from logs alone — settlement's own logging already carries
	// its full input state; this brings calibration up to the same standard.
	slog.Info("calibration input state",
		"calibration_id", out.ID, "bounty_id", b.ID,
		"difficulty", b.Difficulty, "completion", b.Completion, "commitment", b.Commitment,
		"original_value", out.OriginalValue, "original_score", out.OriginalScore,
		"calibrated_value", out.CalibratedValue, "calibrated_score", out.CalibratedScore)
	sharedapi.WriteJSON(w, http.StatusCreated, out)
}

// list returns every calibration recorded against a bounty. Read access is
// unrestricted beyond authentication, like credits and the work feed: a
// calibration is meant to be visible, not merely applied.
func (h *calibrationHandler) list(w http.ResponseWriter, r *http.Request) {
	bountyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		sharedapi.WriteJSON(w, http.StatusBadRequest, sharedapi.ErrorBody{Error: "id is not a UUID"})
		return
	}
	out, err := h.calibrations.ListByBounty(r.Context(), bountyID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	sharedapi.WriteJSON(w, http.StatusOK, out)
}
