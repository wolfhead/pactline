package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ActorType string

const (
	ActorTypeUser   ActorType = "user"
	ActorTypeAgent  ActorType = "agent"
	ActorTypeSystem ActorType = "system"
)

type Actor struct {
	Type   ActorType
	UserID *uuid.UUID
	Ref    string
}

func (a Actor) IsHuman() bool {
	return a.Type == ActorTypeUser && a.UserID != nil && *a.UserID != uuid.Nil
}

type AcceptanceCriterion struct {
	ID                       uuid.UUID
	Version                  int64
	MilestoneID              *uuid.UUID
	TaskID                   *uuid.UUID
	Criterion                string
	VerificationInstructions string
	Revision                 int
	Position                 int
	ArchivedAt               *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

func (c AcceptanceCriterion) Validate() error {
	ownerCount := 0
	for _, ownerID := range []*uuid.UUID{c.MilestoneID, c.TaskID} {
		if ownerID != nil {
			ownerCount++
		}
	}
	if ownerCount != 1 {
		return fmt.Errorf("%w: acceptance criterion must have exactly one owner", ErrInvalidInput)
	}
	if strings.TrimSpace(c.Criterion) == "" || strings.TrimSpace(c.VerificationInstructions) == "" {
		return fmt.Errorf("%w: criterion and verification instructions are required", ErrInvalidInput)
	}
	if c.Revision < 1 || c.Position < 0 {
		return fmt.Errorf("%w: criterion revision and position are invalid", ErrInvalidInput)
	}
	return nil
}

func (c *AcceptanceCriterion) Edit(criterion, verificationInstructions string) {
	if c.Criterion != criterion || c.VerificationInstructions != verificationInstructions {
		c.Criterion = criterion
		c.VerificationInstructions = verificationInstructions
		c.Revision++
		c.UpdatedAt = time.Now().UTC()
	}
}

func (c *AcceptanceCriterion) Move(position int) {
	if c.Position != position {
		c.Position = position
		c.UpdatedAt = time.Now().UTC()
	}
}

type AcceptanceOutcome string

const (
	AcceptanceOutcomePassed AcceptanceOutcome = "passed"
	AcceptanceOutcomeFailed AcceptanceOutcome = "failed"
	AcceptanceOutcomeUnable AcceptanceOutcome = "unable"
	AcceptanceOutcomeWaived AcceptanceOutcome = "waived"
)

func (o AcceptanceOutcome) Valid() bool {
	switch o {
	case AcceptanceOutcomePassed, AcceptanceOutcomeFailed, AcceptanceOutcomeUnable, AcceptanceOutcomeWaived:
		return true
	}
	return false
}

func (o AcceptanceOutcome) Satisfies() bool {
	return o == AcceptanceOutcomePassed || o == AcceptanceOutcomeWaived
}

type AcceptanceCheckPurpose string

const (
	AcceptanceCheckPurposeExecutionVerification AcceptanceCheckPurpose = "execution_verification"
	AcceptanceCheckPurposeAcceptance            AcceptanceCheckPurpose = "acceptance"
)

func (p AcceptanceCheckPurpose) Valid() bool {
	return p == AcceptanceCheckPurposeExecutionVerification ||
		p == AcceptanceCheckPurposeAcceptance
}

type AcceptanceCheck struct {
	ID                uuid.UUID
	CriterionID       uuid.UUID
	CriterionRevision int
	Outcome           AcceptanceOutcome
	Evidence          string
	Checker           Actor
	Purpose           AcceptanceCheckPurpose
	TaskClaimID       *uuid.UUID
	TaskReviewCycle   *int64
	CheckedAt         time.Time
}

// ValidateForTaskClaim validates a new Task check against the Claim stage and
// current review cycle. The purpose is explicit, while actor type remains
// provenance and cannot turn execution self-verification into acceptance.
func (c AcceptanceCheck) ValidateForTaskClaim(
	criterion AcceptanceCriterion,
	claimID uuid.UUID,
	stage TaskClaimStage,
	reviewCycle int64,
) error {
	if err := c.validateBase(criterion); err != nil {
		return err
	}
	if claimID == uuid.Nil || c.TaskClaimID == nil || *c.TaskClaimID != claimID ||
		c.TaskReviewCycle == nil || *c.TaskReviewCycle != reviewCycle || reviewCycle < 0 {
		return fmt.Errorf("%w: Task check Claim and review cycle do not match", ErrConflict)
	}
	if !c.Purpose.Valid() ||
		(stage == TaskClaimStageExecution && c.Purpose != AcceptanceCheckPurposeExecutionVerification) ||
		(stage == TaskClaimStageReview && c.Purpose != AcceptanceCheckPurposeAcceptance) ||
		!stage.Valid() {
		return fmt.Errorf("%w: check purpose does not match Claim stage", ErrConflict)
	}
	return nil
}

func (c AcceptanceCheck) SatisfiesTaskReview(reviewCycle int64) bool {
	return c.Purpose == AcceptanceCheckPurposeAcceptance &&
		c.TaskReviewCycle != nil && *c.TaskReviewCycle == reviewCycle &&
		c.Outcome.Satisfies()
}

func (c AcceptanceCheck) ValidateAgainst(criterion AcceptanceCriterion) error {
	return c.validateBase(criterion)
}

func (c AcceptanceCheck) validateBase(criterion AcceptanceCriterion) error {
	if c.CriterionID != criterion.ID || c.CriterionRevision != criterion.Revision {
		return fmt.Errorf("%w: acceptance criterion revision changed", ErrConflict)
	}
	if !c.Outcome.Valid() || strings.TrimSpace(c.Evidence) == "" {
		return fmt.Errorf("%w: valid outcome and evidence are required", ErrInvalidInput)
	}
	switch c.Checker.Type {
	case ActorTypeUser:
		if !c.Checker.IsHuman() {
			return fmt.Errorf("%w: checker user is required", ErrInvalidInput)
		}
	case ActorTypeAgent, ActorTypeSystem:
		if strings.TrimSpace(c.Checker.Ref) == "" {
			return fmt.Errorf("%w: checker reference is required", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: invalid checker type", ErrInvalidInput)
	}
	return nil
}
