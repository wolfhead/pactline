package domain

import (
	"time"

	"github.com/google/uuid"
)

// Calibration is spec §4.6's quarterly value-versus-reality correction. It is
// stored as its own row rather than mutated onto the bounty: the settlement
// snapshot (Bounty.SettledScore/SettledAt) stays the historical fact of what
// was scored at the time, and a calibration is a separate, attributable
// correction layered on top — this is what keeps §7.2's "changing constants
// must not rewrite history" promise literally true, and keeps the before and
// after both visible instead of one overwriting the other.
//
// OriginalValue is captured from the bounty's value level at the moment of
// calibration (not accepted from the client) so the record cannot be
// backdated to a value the bounty never actually carried.
//
// CalibratedScore is the score the work would have settled at had
// CalibratedValue been its value level all along, computed once at creation
// time using the same fixed constants a settlement run would use. It is
// stored, not recomputed on read, for the same reason a settlement snapshot
// is stored: a calibration is itself a historical record and must not drift
// under a later constant change either.
type Calibration struct {
	ID              uuid.UUID  `json:"id"`
	BountyID        uuid.UUID  `json:"bounty_id"`
	Quarter         string     `json:"quarter"`
	OriginalValue   ValueLevel `json:"original_value"`
	CalibratedValue ValueLevel `json:"calibrated_value"`
	CalibratedScore float64    `json:"calibrated_score"`
	Note            string     `json:"note,omitempty"`
	CreatedBy       uuid.UUID  `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ValidateCalibration checks the fields a caller controls. OriginalValue is
// validated too even though the caller does not set it directly, as a guard
// against a future call site that starts trusting client input for it.
func ValidateCalibration(c Calibration) error {
	if !IsValidValueLevel(c.OriginalValue) {
		return ErrInvalidValueLevel
	}
	if !IsValidValueLevel(c.CalibratedValue) {
		return ErrInvalidValueLevel
	}
	if c.Quarter == "" {
		return ErrQuarterRequired
	}
	return nil
}
