package domain

import (
	"regexp"
	"time"

	"github.com/google/uuid"
)

// quarterFormat is the expected shape of a calibration's Quarter label:
// four-digit year, literal "Q", one digit 1-4 — e.g. "2026Q3".
var quarterFormat = regexp.MustCompile(`^\d{4}Q[1-4]$`)

// IsValidQuarter reports whether q matches the expected YYYYQn shape.
func IsValidQuarter(q string) bool {
	return quarterFormat.MatchString(q)
}

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
//
// OriginalScore is copied from the bounty's SettledScore at calibration time
// (I3). Without it, the row only had one side of the comparison a reader
// actually needs — settled-then versus calibrated-now — and a caller would
// have to go re-fetch the bounty and hope its (mutable-over-time, non-
// snapshotted) SettledScore field still matched what was true when this
// calibration was recorded. Storing it here makes each calibration row a
// self-contained before/after, exactly like SettledScore/OriginalValue.
type Calibration struct {
	ID              uuid.UUID  `json:"id"`
	BountyID        uuid.UUID  `json:"bounty_id"`
	Quarter         string     `json:"quarter"`
	OriginalValue   ValueLevel `json:"original_value"`
	OriginalScore   float64    `json:"original_score"`
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
	if !IsValidQuarter(c.Quarter) {
		return ErrInvalidQuarterFormat
	}
	return nil
}
