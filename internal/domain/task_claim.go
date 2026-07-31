package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TaskClaimActiveLifetime  = 7 * 24 * time.Hour
	TaskClaimWaitingLifetime = 24 * time.Hour
)

type TaskClaimStatus string

const (
	TaskClaimStatusActive       TaskClaimStatus = "active"
	TaskClaimStatusWaitingHuman TaskClaimStatus = "waiting_human"
	TaskClaimStatusSubmitted    TaskClaimStatus = "submitted"
	TaskClaimStatusReleased     TaskClaimStatus = "released"
	TaskClaimStatusExpired      TaskClaimStatus = "expired"
)

func (s TaskClaimStatus) Valid() bool {
	switch s {
	case TaskClaimStatusActive,
		TaskClaimStatusWaitingHuman,
		TaskClaimStatusSubmitted,
		TaskClaimStatusReleased,
		TaskClaimStatusExpired:
		return true
	}
	return false
}

func (s TaskClaimStatus) Terminal() bool {
	return s == TaskClaimStatusSubmitted ||
		s == TaskClaimStatusReleased ||
		s == TaskClaimStatusExpired
}

// TaskClaim binds one Task to one external client session. The client session
// owns the model context and cannot transfer an unfinished Claim.
type TaskClaim struct {
	ID                uuid.UUID
	TaskID            uuid.UUID
	TaskNumber        int64
	ClaimedByUserID   uuid.UUID
	ClaimedViaTokenID uuid.UUID
	TokenNameSnapshot string
	ClientKind        string
	ClientSessionID   string
	Status            TaskClaimStatus
	Version           int64
	ExpiresAt         time.Time
	TerminalReason    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
}

func NewTaskClaim(
	task Task,
	actor OperationActor,
	clientKind string,
	clientSessionID string,
	now time.Time,
) (TaskClaim, error) {
	if err := task.ValidateAgentClaim(actor.UserID); err != nil {
		return TaskClaim{}, err
	}
	if actor.AuthMethod != AuthenticationMethodAPIToken ||
		actor.TokenID == nil || *actor.TokenID == uuid.Nil {
		return TaskClaim{}, fmt.Errorf("%w: a personal API token is required", ErrForbidden)
	}
	clientKind = strings.TrimSpace(clientKind)
	clientSessionID = strings.TrimSpace(clientSessionID)
	if clientKind == "" || clientSessionID == "" {
		return TaskClaim{}, fmt.Errorf("%w: client kind and session are required", ErrInvalidInput)
	}
	now = now.UTC()
	claim := TaskClaim{
		ID:                uuid.New(),
		TaskID:            task.ID,
		TaskNumber:        task.Number,
		ClaimedByUserID:   actor.UserID,
		ClaimedViaTokenID: *actor.TokenID,
		TokenNameSnapshot: actor.TokenName,
		ClientKind:        clientKind,
		ClientSessionID:   clientSessionID,
		Status:            TaskClaimStatusActive,
		Version:           1,
		ExpiresAt:         now.Add(TaskClaimActiveLifetime),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return claim, claim.Validate()
}

func (t Task) ValidateAgentClaim(userID uuid.UUID) error {
	if t.ArchivedAt != nil ||
		t.Status != TaskStatusTodo ||
		t.ExecutionMode != TaskExecutionModeAgentAllowed {
		return fmt.Errorf("%w: Task is not available for Agent execution", ErrConflict)
	}
	if t.AssigneeID == nil || *t.AssigneeID != userID {
		return fmt.Errorf("%w: only the assigned user's Agent may claim the Task", ErrForbidden)
	}
	return nil
}

func (c TaskClaim) Validate() error {
	if c.ID == uuid.Nil ||
		c.TaskID == uuid.Nil ||
		c.TaskNumber < 1 ||
		c.ClaimedByUserID == uuid.Nil ||
		c.ClaimedViaTokenID == uuid.Nil ||
		strings.TrimSpace(c.ClientKind) == "" ||
		strings.TrimSpace(c.ClientSessionID) == "" ||
		!c.Status.Valid() ||
		c.Version < 1 ||
		c.ExpiresAt.IsZero() ||
		c.CreatedAt.IsZero() ||
		c.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Task Claim is invalid", ErrInvalidInput)
	}
	if c.Status.Terminal() != (c.CompletedAt != nil) {
		return fmt.Errorf("%w: Task Claim terminal timestamps are inconsistent", ErrInvalidInput)
	}
	return nil
}

func (c TaskClaim) OwnedBy(userID uuid.UUID, clientKind, clientSessionID string) bool {
	return c.ClaimedByUserID == userID &&
		c.ClientKind == strings.TrimSpace(clientKind) &&
		c.ClientSessionID == strings.TrimSpace(clientSessionID)
}

func (c *TaskClaim) WaitForHuman(now time.Time) error {
	if c.Status != TaskClaimStatusActive {
		return fmt.Errorf("%w: only an active Claim may wait for a human", ErrConflict)
	}
	now = now.UTC()
	c.Status = TaskClaimStatusWaitingHuman
	c.ExpiresAt = now.Add(TaskClaimWaitingLifetime)
	c.UpdatedAt = now
	return nil
}

func (c *TaskClaim) Resume(now time.Time) error {
	if c.Status != TaskClaimStatusWaitingHuman {
		return fmt.Errorf("%w: Claim is not waiting for a human", ErrConflict)
	}
	now = now.UTC()
	if !now.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: Claim has expired", ErrConflict)
	}
	c.Status = TaskClaimStatusActive
	c.ExpiresAt = now.Add(TaskClaimActiveLifetime)
	c.UpdatedAt = now
	return nil
}

func (c *TaskClaim) Extend(now time.Time) error {
	if c.Status != TaskClaimStatusActive {
		return fmt.Errorf("%w: only an active Claim may be extended", ErrConflict)
	}
	now = now.UTC()
	if !now.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: Claim has expired", ErrConflict)
	}
	c.ExpiresAt = now.Add(TaskClaimActiveLifetime)
	c.UpdatedAt = now
	return nil
}

func (c *TaskClaim) Submit(now time.Time) error {
	return c.finish(TaskClaimStatusSubmitted, "", now)
}

func (c *TaskClaim) Release(reason string, now time.Time) error {
	return c.finish(TaskClaimStatusReleased, reason, now)
}

func (c *TaskClaim) Expire(now time.Time) error {
	now = now.UTC()
	if c.Status.Terminal() || now.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: Claim is not due to expire", ErrConflict)
	}
	return c.finish(TaskClaimStatusExpired, "deadline_elapsed", now)
}

func (c *TaskClaim) finish(status TaskClaimStatus, reason string, now time.Time) error {
	if c.Status.Terminal() {
		return fmt.Errorf("%w: Claim is already terminal", ErrConflict)
	}
	now = now.UTC()
	c.Status = status
	c.TerminalReason = strings.TrimSpace(reason)
	c.UpdatedAt = now
	c.CompletedAt = &now
	return nil
}

type TaskClaimMessageKind string

const (
	TaskClaimMessageProgress   TaskClaimMessageKind = "progress"
	TaskClaimMessageQuestion   TaskClaimMessageKind = "question"
	TaskClaimMessageAnswer     TaskClaimMessageKind = "answer"
	TaskClaimMessageHandoff    TaskClaimMessageKind = "handoff"
	TaskClaimMessageSubmission TaskClaimMessageKind = "submission"
)

func (k TaskClaimMessageKind) Valid() bool {
	switch k {
	case TaskClaimMessageProgress,
		TaskClaimMessageQuestion,
		TaskClaimMessageAnswer,
		TaskClaimMessageHandoff,
		TaskClaimMessageSubmission:
		return true
	}
	return false
}

type TaskClaimMessage struct {
	ID         uuid.UUID
	ClaimID    uuid.UUID
	TaskID     uuid.UUID
	Author     Actor
	Kind       TaskClaimMessageKind
	Body       string
	ReplyToID  *uuid.UUID
	RequestID  string
	APITokenID *uuid.UUID
	TokenName  string
	CreatedAt  time.Time
}

func (m TaskClaimMessage) Validate() error {
	if m.ID == uuid.Nil ||
		m.ClaimID == uuid.Nil ||
		m.TaskID == uuid.Nil ||
		!m.Kind.Valid() ||
		strings.TrimSpace(m.Body) == "" ||
		strings.TrimSpace(m.RequestID) == "" ||
		m.CreatedAt.IsZero() {
		return fmt.Errorf("%w: Task Claim message is invalid", ErrInvalidInput)
	}
	switch m.Author.Type {
	case ActorTypeAgent:
		if strings.TrimSpace(m.Author.Ref) == "" || m.APITokenID == nil {
			return fmt.Errorf("%w: Agent message provenance is required", ErrInvalidInput)
		}
		if m.Kind == TaskClaimMessageAnswer {
			return fmt.Errorf("%w: an Agent cannot author an answer", ErrForbidden)
		}
	case ActorTypeUser:
		if !m.Author.IsHuman() || m.Kind != TaskClaimMessageAnswer || m.APITokenID != nil {
			return fmt.Errorf("%w: a human may only author an answer", ErrForbidden)
		}
	case ActorTypeSystem:
		if strings.TrimSpace(m.Author.Ref) == "" || m.APITokenID != nil {
			return fmt.Errorf("%w: system message provenance is required", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: Task Claim message author is invalid", ErrInvalidInput)
	}
	return nil
}
