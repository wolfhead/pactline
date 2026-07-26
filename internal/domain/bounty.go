package domain

import (
	"time"

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
// - value_level, difficulty, completion, settled_score, settled_at: scoring system (future phase)
// - due_date: commitment tracking (future phase)
// - baseline_system_id: baseline contracts (future phase)
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
	Commitment         Commitment     `json:"commitment"`
	Status             Status         `json:"status"`
	SponsorID          uuid.UUID      `json:"sponsor_id"`
	ClaimedBy          *uuid.UUID     `json:"claimed_by,omitempty"`
	ClaimedAt          *time.Time     `json:"claimed_at,omitempty"`
	PersonDays         *float64       `json:"person_days,omitempty"`
	Retrospective      string         `json:"retrospective,omitempty"`
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
func CanEdit(u User, b Bounty) bool {
	return u.ID == b.SponsorID || u.HasRole(UserRoleSteward)
}

// CanClaim reports why the user may not claim, or nil when they may.
func CanClaim(u User, b Bounty) error {
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
	if !u.HasRole(UserRoleEngineer) && !u.HasRole(UserRoleSteward) {
		return ErrForbidden
	}
	return nil
}

// CanNominate reports whether the user may name credits on this bounty. Only
// the deliverer names credits; the steward may correct.
func CanNominate(u User, b Bounty) bool {
	if u.HasRole(UserRoleSteward) {
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
