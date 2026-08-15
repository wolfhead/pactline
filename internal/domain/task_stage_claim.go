package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type StageClaimStatus string

const (
	StageClaimStatusActive    StageClaimStatus = "active"
	StageClaimStatusCompleted StageClaimStatus = "completed"
	StageClaimStatusReleased  StageClaimStatus = "released"
	StageClaimStatusExpired   StageClaimStatus = "expired"
	StageClaimStatusCancelled StageClaimStatus = "cancelled"
)

func (s StageClaimStatus) Valid() bool {
	switch s {
	case StageClaimStatusActive,
		StageClaimStatusCompleted,
		StageClaimStatusReleased,
		StageClaimStatusExpired,
		StageClaimStatusCancelled:
		return true
	default:
		return false
	}
}

func (s StageClaimStatus) Terminal() bool {
	return s != StageClaimStatusActive && s.Valid()
}

type TaskClaimOutcome string

const (
	TaskClaimOutcomeExecutionCompleted  TaskClaimOutcome = "execution_completed"
	TaskClaimOutcomeTaskAccepted        TaskClaimOutcome = "task_accepted"
	TaskClaimOutcomeChangesRequested    TaskClaimOutcome = "changes_requested"
	TaskClaimOutcomeNeedsResolution     TaskClaimOutcome = "needs_resolution"
	TaskClaimOutcomeVoluntarilyReleased TaskClaimOutcome = "voluntarily_released"
	TaskClaimOutcomeDeadlineElapsed     TaskClaimOutcome = "deadline_elapsed"
	TaskClaimOutcomeTaskCancelled       TaskClaimOutcome = "task_cancelled"
)

func (o TaskClaimOutcome) Valid() bool {
	switch o {
	case TaskClaimOutcomeExecutionCompleted,
		TaskClaimOutcomeTaskAccepted,
		TaskClaimOutcomeChangesRequested,
		TaskClaimOutcomeNeedsResolution,
		TaskClaimOutcomeVoluntarilyReleased,
		TaskClaimOutcomeDeadlineElapsed,
		TaskClaimOutcomeTaskCancelled:
		return true
	default:
		return false
	}
}

// TaskStageClaim is the actor-neutral target Claim model. It is intentionally
// separate from the legacy Agent-only TaskClaim while the additive schema and
// API replacement are under construction.
type TaskStageClaim struct {
	ID              uuid.UUID
	TaskID          uuid.UUID
	TaskNumber      int64
	Stage           TaskClaimStage
	ClaimedBy       Actor
	SubjectUserID   uuid.UUID
	AuthMethod      AuthenticationMethod
	APITokenID      *uuid.UUID
	TokenName       string
	AgentRunID      *uuid.UUID
	ClientKind      string
	ClientSessionID string
	Status          StageClaimStatus
	Outcome         TaskClaimOutcome
	Version         int64
	ExpiresAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
}

func NewTaskStageClaim(
	taskID uuid.UUID,
	taskNumber int64,
	stage TaskClaimStage,
	claimedBy Actor,
	operation OperationActor,
	clientKind string,
	clientSessionID string,
	now time.Time,
) (TaskStageClaim, error) {
	if err := operation.Validate(); err != nil {
		return TaskStageClaim{}, err
	}
	claim := TaskStageClaim{
		ID: uuid.New(), TaskID: taskID, TaskNumber: taskNumber, Stage: stage,
		ClaimedBy: claimedBy, SubjectUserID: operation.UserID,
		AuthMethod: operation.AuthMethod,
		APITokenID: operation.TokenID, TokenName: operation.TokenName,
		AgentRunID:      operation.AgentRunID,
		ClientKind:      strings.TrimSpace(clientKind),
		ClientSessionID: strings.TrimSpace(clientSessionID),
		Status:          StageClaimStatusActive, Version: 1,
		ExpiresAt: now.UTC().Add(TaskClaimActiveLifetime),
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	return claim, claim.Validate()
}

func (c TaskStageClaim) Validate() error {
	if c.ID == uuid.Nil || c.TaskID == uuid.Nil || c.TaskNumber < 1 ||
		c.SubjectUserID == uuid.Nil ||
		!c.Stage.Valid() || !validThreadActor(c.ClaimedBy) ||
		!c.Status.Valid() || c.Version < 1 || c.ExpiresAt.IsZero() ||
		c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Task Claim identity, stage, actor, and lifecycle are required", ErrInvalidInput)
	}
	if err := validateTaskClaimAuthentication(c); err != nil {
		return err
	}
	if c.Status == StageClaimStatusActive {
		if c.Outcome != "" || c.CompletedAt != nil {
			return fmt.Errorf("%w: active Task Claim cannot have a terminal outcome", ErrInvalidInput)
		}
		return nil
	}
	if !c.Outcome.Valid() || c.CompletedAt == nil {
		return fmt.Errorf("%w: terminal Task Claim requires outcome and completion time", ErrInvalidInput)
	}
	if !claimStatusAcceptsOutcome(c.Status, c.Outcome) {
		return fmt.Errorf(
			"%w: Task Claim status %q does not accept outcome %q",
			ErrInvalidInput,
			c.Status,
			c.Outcome,
		)
	}
	return nil
}

func (c *TaskStageClaim) Complete(outcome TaskClaimOutcome, now time.Time) error {
	if c.Status != StageClaimStatusActive {
		return fmt.Errorf("%w: Task Claim is already terminal", ErrConflict)
	}
	status := StageClaimStatusCompleted
	switch outcome {
	case TaskClaimOutcomeExecutionCompleted,
		TaskClaimOutcomeTaskAccepted,
		TaskClaimOutcomeChangesRequested:
	case TaskClaimOutcomeNeedsResolution, TaskClaimOutcomeVoluntarilyReleased:
		status = StageClaimStatusReleased
	case TaskClaimOutcomeDeadlineElapsed:
		status = StageClaimStatusExpired
	case TaskClaimOutcomeTaskCancelled:
		status = StageClaimStatusCancelled
	default:
		return fmt.Errorf("%w: invalid Task Claim outcome %q", ErrInvalidInput, outcome)
	}
	if !stageAcceptsOutcome(c.Stage, outcome) {
		return fmt.Errorf(
			"%w: %s Claim cannot finish with outcome %q",
			ErrConflict,
			c.Stage,
			outcome,
		)
	}
	completedAt := now.UTC()
	c.Status = status
	c.Outcome = outcome
	c.CompletedAt = &completedAt
	c.UpdatedAt = completedAt
	c.Version++
	return nil
}

func validateTaskClaimAuthentication(c TaskStageClaim) error {
	switch c.AuthMethod {
	case AuthenticationMethodSession:
		if c.ClaimedBy.Type != ActorTypeUser || c.APITokenID != nil || c.AgentRunID != nil {
			return fmt.Errorf("%w: session Claim provenance is invalid", ErrInvalidInput)
		}
		if c.ClaimedBy.UserID == nil || *c.ClaimedBy.UserID != c.SubjectUserID {
			return fmt.Errorf("%w: session Claim subject does not match actor", ErrInvalidInput)
		}
	case AuthenticationMethodAPIToken:
		if c.APITokenID == nil || *c.APITokenID == uuid.Nil ||
			strings.TrimSpace(c.TokenName) == "" || c.AgentRunID != nil ||
			c.ClaimedBy.Type != ActorTypeAgent {
			return fmt.Errorf("%w: API Token Claim provenance is invalid", ErrInvalidInput)
		}
	case AuthenticationMethodAgentDelegate:
		if c.AgentRunID == nil || *c.AgentRunID == uuid.Nil || c.APITokenID != nil ||
			c.ClaimedBy.Type != ActorTypeAgent {
			return fmt.Errorf("%w: delegated Agent Claim provenance is invalid", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: Task Claim authentication method is invalid", ErrInvalidInput)
	}
	return nil
}

func stageAcceptsOutcome(stage TaskClaimStage, outcome TaskClaimOutcome) bool {
	if outcome == TaskClaimOutcomeNeedsResolution ||
		outcome == TaskClaimOutcomeVoluntarilyReleased ||
		outcome == TaskClaimOutcomeDeadlineElapsed ||
		outcome == TaskClaimOutcomeTaskCancelled {
		return true
	}
	if stage == TaskClaimStageExecution {
		return outcome == TaskClaimOutcomeExecutionCompleted ||
			outcome == TaskClaimOutcomeTaskAccepted
	}
	return stage == TaskClaimStageReview &&
		(outcome == TaskClaimOutcomeTaskAccepted || outcome == TaskClaimOutcomeChangesRequested)
}

func claimStatusAcceptsOutcome(status StageClaimStatus, outcome TaskClaimOutcome) bool {
	switch status {
	case StageClaimStatusCompleted:
		return outcome == TaskClaimOutcomeExecutionCompleted ||
			outcome == TaskClaimOutcomeTaskAccepted ||
			outcome == TaskClaimOutcomeChangesRequested
	case StageClaimStatusReleased:
		return outcome == TaskClaimOutcomeNeedsResolution ||
			outcome == TaskClaimOutcomeVoluntarilyReleased
	case StageClaimStatusExpired:
		return outcome == TaskClaimOutcomeDeadlineElapsed
	case StageClaimStatusCancelled:
		return outcome == TaskClaimOutcomeTaskCancelled
	default:
		return false
	}
}
