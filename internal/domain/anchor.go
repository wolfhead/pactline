package domain

import (
	"time"

	"github.com/google/uuid"
)

// AnchorDimension names which of the two graded axes an anchor example
// belongs to.
type AnchorDimension string

const (
	AnchorDimensionValue      AnchorDimension = "VALUE"
	AnchorDimensionDifficulty AnchorDimension = "DIFFICULTY"
)

// ValidAnchorDimensions lists every known AnchorDimension constant.
var ValidAnchorDimensions = []AnchorDimension{AnchorDimensionValue, AnchorDimensionDifficulty}

// IsValidAnchorDimension reports whether d is one of the two defined dimensions.
func IsValidAnchorDimension(d AnchorDimension) bool {
	for _, v := range ValidAnchorDimensions {
		if v == d {
			return true
		}
	}
	return false
}

// AnchorExample is spec §4.7's precedent list: a reference bounty pinned to a
// dimension and level so that grading arguments converge on "this is like
// last quarter's X" instead of restarting from nothing each quarter. Plain
// CRUD, steward-managed, no suggestion logic and no auto-promotion — the spec
// calls it "a list, no process needed", and that is exactly what this is.
type AnchorExample struct {
	ID        uuid.UUID       `json:"id"`
	Dimension AnchorDimension `json:"dimension"`
	Level     string          `json:"level"`
	BountyID  uuid.UUID       `json:"bounty_id"`
	Note      string          `json:"note,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ValidateAnchorExample checks that Level actually belongs to Dimension's
// value set — a VALUE anchor pinned to "XL" or a DIFFICULTY anchor pinned to
// "S-tier" would silently corrupt the precedent list otherwise.
func ValidateAnchorExample(a AnchorExample) error {
	switch a.Dimension {
	case AnchorDimensionValue:
		if !IsValidValueLevel(ValueLevel(a.Level)) {
			return ErrInvalidAnchorLevel
		}
	case AnchorDimensionDifficulty:
		if !IsValidDifficulty(Difficulty(a.Level)) {
			return ErrInvalidAnchorLevel
		}
	default:
		return ErrInvalidAnchorDimension
	}
	return nil
}
