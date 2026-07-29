package domain

import (
	"time"

	userdomain "github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
)

// Status is the lifecycle position of a bounty. A bounty that reaches
// StatusCompleted or StatusAbandoned is a work and appears in the work feed.
type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusOpen      Status = "OPEN"
	StatusClaimed   Status = "CLAIMED"
	StatusDelivered Status = "DELIVERED"
	StatusCompleted Status = "COMPLETED"
	StatusAbandoned Status = "ABANDONED"
)

// BountyType separates defining work from implementing work. The two are
// independent records linked by ParentID.
type BountyType string

const (
	BountyTypePlan     BountyType = "PLAN"
	BountyTypeDelivery BountyType = "DELIVERY"
)

// Visibility controls who may claim a bounty.
type Visibility string

const (
	VisibilityPublic     Visibility = "PUBLIC"
	VisibilityRestricted Visibility = "RESTRICTED"
	VisibilityDirected   Visibility = "DIRECTED"
)

// Commitment distinguishes time-bound promises from exploratory work that is
// allowed to fail.
type Commitment string

const (
	CommitmentCommitted   Commitment = "COMMITTED"
	CommitmentExploratory Commitment = "EXPLORATORY"
)

// BusinessTagPlatform marks shared platform work that belongs to no business
// line. Without it, platform contributors vanish from every line's view.
const BusinessTagPlatform = "PLATFORM"

// ValueLevel is the sponsor-set claim of how much a work is worth. Spec §7.1
// deliberately uses four coarse levels rather than a continuous score: a
// precise number invites arguing over decimals, and multiplying several
// subjective estimates together only amplifies their error.
type ValueLevel string

const (
	ValueS ValueLevel = "S"
	ValueA ValueLevel = "A"
	ValueB ValueLevel = "B"
	ValueC ValueLevel = "C"
)

// ValidValueLevels lists every known ValueLevel constant.
var ValidValueLevels = []ValueLevel{ValueS, ValueA, ValueB, ValueC}

// IsValidValueLevel reports whether v is one of the four defined levels.
func IsValidValueLevel(v ValueLevel) bool {
	for _, l := range ValidValueLevels {
		if l == v {
			return true
		}
	}
	return false
}

// Difficulty is the tech-lead-set estimate of how hard a work is. Never the
// sponsor's call, even for their own bounty — separating "how valuable" from
// "how hard" is the mechanism's point (spec §6.1), not an accident.
type Difficulty string

const (
	DifficultyXS Difficulty = "XS"
	DifficultyS  Difficulty = "S"
	DifficultyM  Difficulty = "M"
	DifficultyL  Difficulty = "L"
	DifficultyXL Difficulty = "XL"
)

// ValidDifficulties lists every known Difficulty constant.
var ValidDifficulties = []Difficulty{DifficultyXS, DifficultyS, DifficultyM, DifficultyL, DifficultyXL}

// IsValidDifficulty reports whether d is one of the five defined levels.
func IsValidDifficulty(d Difficulty) bool {
	for _, l := range ValidDifficulties {
		if l == d {
			return true
		}
	}
	return false
}

// Completion is the sponsor-set grade of how well a delivered work met its
// acceptance criteria. Set at acceptance (the DELIVERED -> COMPLETED edge);
// ABANDONED bounties never carry one — spec §7.1.1 scores those by status and
// commitment alone.
type Completion string

const (
	CompletionExceeded Completion = "EXCEEDED"
	CompletionMet      Completion = "MET"
	CompletionPartial  Completion = "PARTIAL"
	CompletionMissed   Completion = "MISSED"
)

// ValidCompletions lists every known Completion constant.
var ValidCompletions = []Completion{CompletionExceeded, CompletionMet, CompletionPartial, CompletionMissed}

// IsValidCompletion reports whether c is one of the four defined levels.
func IsValidCompletion(c Completion) bool {
	for _, l := range ValidCompletions {
		if l == c {
			return true
		}
	}
	return false
}

// BusinessLine is a weighted attribution tag. Weights are expected to sum to 1,
// but a mismatch is surfaced as a warning rather than rejected.
type BusinessLine struct {
	Tag    string  `json:"tag"`
	Weight float64 `json:"weight"`
}

// Bounty is a unit of work. Before completion it is a claimable bounty; after
// completion it is a work displayed in the feed. There is no second entity.
//
// Note: some columns in migrations/0001_init.sql are deliberately not yet included here
// because the features that depend on them ship in later phases:
// - due_date: commitment tracking (Phase 3)
// - baseline_system_id: baseline contracts (Phase 5)
type Bounty struct {
	ID                 uuid.UUID      `json:"id"`
	Type               BountyType     `json:"type"`
	ParentID           *uuid.UUID     `json:"parent_id,omitempty"`
	Title              string         `json:"title"`
	Goal               string         `json:"goal"`
	AcceptanceCriteria string         `json:"acceptance_criteria"`
	Visibility         Visibility     `json:"visibility"`
	Restriction        string         `json:"restriction,omitempty"`
	DirectedTo         *uuid.UUID     `json:"directed_to,omitempty"`
	BusinessLines      []BusinessLine `json:"business_lines"`
	ValueLevel         ValueLevel     `json:"value_level,omitempty"`
	Difficulty         Difficulty     `json:"difficulty,omitempty"`
	Commitment         Commitment     `json:"commitment"`
	Completion         Completion     `json:"completion,omitempty"`
	Status             Status         `json:"status"`
	SponsorID          uuid.UUID      `json:"sponsor_id"`
	ClaimedBy          *uuid.UUID     `json:"claimed_by,omitempty"`
	ClaimedAt          *time.Time     `json:"claimed_at,omitempty"`
	PersonDays         *float64       `json:"person_days,omitempty"`
	Retrospective      string         `json:"retrospective,omitempty"`
	SettledScore       *float64       `json:"settled_score,omitempty"`
	SettledAt          *time.Time     `json:"settled_at,omitempty"`
	CompletedAt        *time.Time     `json:"completed_at,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// IsWork reports whether the bounty has reached a terminal state and therefore
// belongs in the work feed. Abandoned work is included on purpose: archiving
// failure with its conclusion is what makes hard problems safe to attempt.
func (b Bounty) IsWork() bool {
	return b.Status == StatusCompleted || b.Status == StatusAbandoned
}

// allowedTransitions is the enforced status graph. Field prerequisites (for
// example an unset difficulty level) are deliberately NOT enforced here; those
// are surfaced in the UI and in later settlement checks.
var allowedTransitions = map[Status][]Status{
	StatusDraft:     {StatusOpen, StatusAbandoned},
	StatusOpen:      {StatusDraft, StatusClaimed, StatusAbandoned},
	StatusClaimed:   {StatusOpen, StatusDelivered, StatusAbandoned},
	StatusDelivered: {StatusClaimed, StatusCompleted, StatusAbandoned},
	StatusCompleted: {},
	StatusAbandoned: {},
}

// ValidateTransition checks the status graph and the retrospective requirement.
func ValidateTransition(b Bounty, to Status) error {
	permitted := false
	for _, s := range allowedTransitions[b.Status] {
		if s == to {
			permitted = true
			break
		}
	}
	if !permitted {
		return ErrInvalidTransition
	}
	if to == StatusAbandoned && b.Retrospective == "" {
		return ErrRetrospectiveRequired
	}
	return nil
}

// CanEdit reports whether the user may change the bounty's own fields.
func CanEdit(u userdomain.User, b Bounty) bool {
	return u.ID == b.SponsorID || u.HasRole(userdomain.UserRoleSteward)
}

// CanClaim reports why the user may not claim, or nil when they may.
func CanClaim(u userdomain.User, b Bounty) error {
	if b.Status != StatusOpen {
		return ErrNotClaimable
	}
	// Directed bounties are claimed only by their named target. Notably, this check
	// returns before the engineer/steward role check below. This is intentional:
	// a directed bounty names one specific person, and the act of naming them is
	// itself the authorization to claim—no role is required.
	if b.Visibility == VisibilityDirected {
		if b.DirectedTo == nil || *b.DirectedTo != u.ID {
			return ErrNotDirectedToYou
		}
		return nil
	}
	// Public and restricted bounties both require engineer or steward role. Restricted
	// visibility has no machine enforcement; the Restriction field holds a human-readable
	// context requirement (e.g., "needs Bidding Engine context") that software cannot
	// evaluate. It is advisory guidance for the claimer, not a programmatic gate.
	if !u.HasRole(userdomain.UserRoleEngineer) && !u.HasRole(userdomain.UserRoleSteward) {
		return ErrForbidden
	}
	return nil
}

// CanSetValueLevel reports whether the user may set or amend the bounty's
// value level through the dedicated POST /api/legacy/bounties/{id}/value-level
// channel. Per spec §6.1, opening/editing a bounty (which includes setting
// its value level) is the sponsor's call, or a steward's — but §6.1 names no
// window for it. The DRAFT/OPEN restriction below is NOT from the spec: an
// earlier version of this comment claimed it was ("when opening, and
// amendable while the bounty is still open" — spec §6.1), and that sentence
// does not appear anywhere in §6.1. It was a fabricated citation and has been
// removed.
//
// The window is kept here anyway, as a deliberate implementation choice
// independent of the spec: "the deliverer committed against this value" is a
// reasonable argument for locking the sponsor's own channel once a bounty is
// claimed. But per §2/§6.2, this system does not otherwise gate on strong
// state-machine checks, and a lock with no escape hatch at all made every
// terminal bounty in the archive permanently unscorable — the STEWARD side of
// this gate does NOT apply: a steward corrects a value level through the
// amend channel (bountyHandler.amend) regardless of status, including on
// settled work. That is not inventing a grade; it is recording one a human
// (the pricing group) actually decided, which is the opposite of the
// "never default a level" failure this lock exists to prevent.
func CanSetValueLevel(u userdomain.User, b Bounty) error {
	if !CanEdit(u, b) {
		return ErrForbidden
	}
	if b.Status != StatusDraft && b.Status != StatusOpen {
		return ErrValueLevelLocked
	}
	return nil
}

// CanSetDifficulty reports whether the user may set the bounty's difficulty
// level. Deliberately NOT the sponsor's call, even for their own bounty — see
// Difficulty's doc comment. Only a TECH_LEAD or STEWARD may set it, and only
// before the bounty has been settled (I2): the settlement snapshot itself is
// protected structurally (BountyStore.Update never writes settled_score or
// settled_at), but the displayed difficulty is one of the inputs that
// produced that snapshot, and anchor_examples pins level precedent to
// specific bounty ids. Rewriting the difficulty after settlement would let
// the displayed grade silently diverge from what actually produced the score
// and would silently corrupt the precedent an anchor entry exists to
// preserve. Grading late — any time before settlement, often in a batch — is
// legitimate and stays unrestricted; this only closes the door after
// settlement, not before.
func CanSetDifficulty(u userdomain.User, b Bounty) error {
	if !u.HasRole(userdomain.UserRoleTechLead) && !u.HasRole(userdomain.UserRoleSteward) {
		return ErrForbidden
	}
	if b.SettledAt != nil {
		return ErrDifficultySettled
	}
	return nil
}

// CanNominate reports whether the user may name credits on this bounty. Only
// the deliverer names credits; the steward may correct.
func CanNominate(u userdomain.User, b Bounty) bool {
	if u.HasRole(userdomain.UserRoleSteward) {
		return true
	}
	return b.ClaimedBy != nil && *b.ClaimedBy == u.ID
}

// ValidStatuses lists every known Status constant, in enum declaration order.
var ValidStatuses = []Status{
	StatusDraft, StatusOpen, StatusClaimed, StatusDelivered, StatusCompleted, StatusAbandoned,
}

// IsValidStatus reports whether s is one of the known Status constants.
func IsValidStatus(s Status) bool {
	for _, v := range ValidStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// ValidBountyTypes lists every known BountyType constant.
var ValidBountyTypes = []BountyType{BountyTypePlan, BountyTypeDelivery}

// IsValidBountyType reports whether t is one of the known BountyType constants.
func IsValidBountyType(t BountyType) bool {
	for _, v := range ValidBountyTypes {
		if v == t {
			return true
		}
	}
	return false
}

// BusinessLineWeightSum totals attribution weights so callers can warn on drift.
func BusinessLineWeightSum(lines []BusinessLine) float64 {
	var sum float64
	for _, l := range lines {
		sum += l.Weight
	}
	return sum
}
