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
	if b.Visibility == VisibilityDirected {
		if b.DirectedTo == nil || *b.DirectedTo != u.ID {
			return ErrNotDirectedToYou
		}
		return nil
	}
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

// BusinessLineWeightSum totals attribution weights so callers can warn on drift.
func BusinessLineWeightSum(lines []BusinessLine) float64 {
	var sum float64
	for _, l := range lines {
		sum += l.Weight
	}
	return sum
}
