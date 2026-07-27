package domain

import "errors"

// The mechanism-specific domain errors below moved here together with the
// bounty/credit/calibration/anchor entities that raise them (see
// internal/legacy/README.md). domain.ErrNotFound stays in the shared
// internal/domain package — see errors.go there — because it is also raised
// by the (non-legacy) user store.
var (
	// ErrInvalidTransition means the requested status is unreachable from the
	// current one.
	ErrInvalidTransition = errors.New("invalid status transition")
	// ErrRetrospectiveRequired means an abandoned bounty carries no conclusion.
	ErrRetrospectiveRequired = errors.New("retrospective is required when abandoning")
	// ErrNotClaimable means the bounty is not open.
	ErrNotClaimable = errors.New("bounty is not open for claiming")
	// ErrNotDirectedToYou means a directed bounty targets someone else.
	ErrNotDirectedToYou = errors.New("bounty is directed to another user")
	// ErrForbidden means the actor lacks the required role.
	ErrForbidden = errors.New("forbidden")
	// ErrNotYourCredit means the actor is not the nominee of this credit.
	ErrNotYourCredit = errors.New("credit belongs to another user")
	// ErrCreditNotPending means the credit was already confirmed or declined.
	ErrCreditNotPending = errors.New("credit is not pending")
	// ErrEvidenceRequired means a REVIEW credit carries no review record.
	ErrEvidenceRequired = errors.New("evidence is required for REVIEW credit")
	// ErrInvalidCreditRole means the nominated role is not one of the six
	// defined parts.
	ErrInvalidCreditRole = errors.New("invalid credit role")
	// ErrCreditNotDeclined means a steward tried to reset a credit that is
	// not currently DECLINED (the only source status the reset channel may
	// act on).
	ErrCreditNotDeclined = errors.New("credit is not declined")
	// ErrInvalidValueLevel means the given value is not one of S/A/B/C.
	ErrInvalidValueLevel = errors.New("invalid value level")
	// ErrInvalidDifficulty means the given value is not one of XS/S/M/L/XL.
	ErrInvalidDifficulty = errors.New("invalid difficulty level")
	// ErrInvalidCompletion means the given value is not one of
	// EXCEEDED/MET/PARTIAL/MISSED.
	ErrInvalidCompletion = errors.New("invalid completion level")
	// ErrValueLevelLocked means the bounty has left the DRAFT/OPEN window in
	// which its value level may be set or amended by the sponsor.
	ErrValueLevelLocked = errors.New("value level can only be set while the bounty is draft or open")
	// ErrAlreadySettled means a settlement run tried to overwrite a snapshot
	// that already exists. Settlement must skip these, never recompute them.
	ErrAlreadySettled = errors.New("bounty is already settled")
	// ErrNotSettled means an operation that requires a settlement snapshot
	// (such as recording a calibration) was attempted before one exists.
	ErrNotSettled = errors.New("bounty has not been settled yet")
	// ErrUnscorable means a terminal bounty is missing a level it needs to
	// compute a score. The caller must not invent a default; it must skip
	// the record and report why.
	ErrUnscorable = errors.New("bounty is missing a required level for scoring")
	// ErrInvalidAnchorDimension means the anchor's dimension is not VALUE or
	// DIFFICULTY.
	ErrInvalidAnchorDimension = errors.New("invalid anchor dimension")
	// ErrInvalidAnchorLevel means the anchor's level does not belong to its
	// dimension's valid set.
	ErrInvalidAnchorLevel = errors.New("invalid anchor level for dimension")
	// ErrQuarterRequired means a calibration was submitted without a quarter
	// label.
	ErrQuarterRequired = errors.New("quarter is required")
	// ErrInvalidQuarterFormat means a calibration's quarter label does not
	// match the expected YYYYQn shape (e.g. "2026Q3").
	ErrInvalidQuarterFormat = errors.New("quarter must be in YYYYQn format, e.g. 2026Q3")
	// ErrDifficultySettled means a difficulty change was attempted on a
	// bounty that has already been settled (I2). The settlement snapshot
	// survives regardless, but anchor_examples pins precedent to bounty ids,
	// and letting the displayed difficulty drift from what actually produced
	// the settled score would silently corrupt that precedent list.
	ErrDifficultySettled = errors.New("difficulty cannot be changed once the bounty has been settled")
)
