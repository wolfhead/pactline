// Package agent owns Pactline's durable first-party Agent state and policy.
// Provider SDKs, model SDKs, and task stores remain outside this boundary.
package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MaxClarificationRounds = 3
	MaxContextMessages     = 100
	ClarificationLifetime  = 24 * time.Hour
	DefaultLeaseDuration   = 2 * time.Minute
)

type RunStatus string

const (
	RunQueued      RunStatus = "queued"
	RunRunning     RunStatus = "running"
	RunWaitingUser RunStatus = "waiting_user"
	RunSucceeded   RunStatus = "succeeded"
	RunFailed      RunStatus = "failed"
	RunCancelled   RunStatus = "cancelled"
)

type CommandKind string

const (
	CommandDirect     CommandKind = "direct"
	CommandDiscussion CommandKind = "discussion"
)

type OutboxKind string

const (
	OutboxClarification     OutboxKind = "clarification"
	OutboxSuccess           OutboxKind = "success"
	OutboxPermissionFailure OutboxKind = "permission_failure"
	OutboxTerminalFailure   OutboxKind = "terminal_failure"
	OutboxRetrying          OutboxKind = "retrying"
	OutboxExpired           OutboxKind = "expired"
)

type OutboxState string

const (
	OutboxPending    OutboxState = "pending"
	OutboxDelivering OutboxState = "delivering"
	OutboxDelivered  OutboxState = "delivered"
	OutboxFailed     OutboxState = "failed"
)

type ToolCallState string

const (
	ToolCallRunning   ToolCallState = "running"
	ToolCallCompleted ToolCallState = "completed"
	ToolCallFailed    ToolCallState = "failed"
)

type ToolCallClaimKind string

const (
	ToolCallClaimAcquired ToolCallClaimKind = "acquired"
	ToolCallClaimReplay   ToolCallClaimKind = "replay"
	ToolCallClaimConflict ToolCallClaimKind = "conflict"
	ToolCallClaimRunning  ToolCallClaimKind = "running"
)

type ToolCallClaim struct {
	Kind   ToolCallClaimKind
	Result []byte
}

var (
	ErrInvalidRun                 = errors.New("agent run is invalid")
	ErrInvalidTransition          = errors.New("agent run transition is invalid")
	ErrRunTerminal                = errors.New("agent run is terminal")
	ErrRunNotWaiting              = errors.New("agent run is not waiting for clarification")
	ErrClarificationUserMismatch  = errors.New("only the initiating user may clarify this run")
	ErrClarificationExpired       = errors.New("agent clarification has expired")
	ErrClarificationLimit         = errors.New("agent clarification limit reached")
	ErrTaskAlreadyCreated         = errors.New("agent run already created a task")
	ErrContextLimit               = errors.New("agent context message limit reached")
	ErrToolCallProtocol           = errors.New("agent tool call protocol violation")
	ErrAgentRunNotFound           = errors.New("agent run not found")
	ErrAgentRunLeaseLost          = errors.New("agent run lease lost")
	ErrAgentCheckpointNotFound    = errors.New("agent checkpoint not found")
	ErrAgentOutboxDeliveryClaimed = errors.New("agent outbox delivery lease lost")
)

type Run struct {
	ID                       uuid.UUID
	Provider                 string
	TenantID                 string
	ConversationID           string
	TriggerMessageID         string
	ProviderEventID          string
	ThreadRootMessageID      string
	ReplyParentMessageID     string
	TriggerOccurredAt        time.Time
	InitiatingUserID         uuid.UUID
	InitiatingSubjectID      string
	Status                   RunStatus
	CommandKind              CommandKind
	Model                    string
	PromptVersion            string
	AttemptCount             int
	ClarificationRounds      int
	ClarificationMessageID   string
	ClarificationInterruptID string
	ClarificationExpiresAt   *time.Time
	ContextMessagesUsed      int
	LeaseOwner               string
	LeaseExpiresAt           *time.Time
	AvailableAt              time.Time
	CreatedTaskID            *uuid.UUID
	CreatedTaskNumber        *int64
	TerminalErrorCategory    string
	TerminalErrorDetail      string
	CreatedAt                time.Time
	UpdatedAt                time.Time
	CompletedAt              *time.Time
}

func NewRun(input NewRunInput, now time.Time) (Run, error) {
	run := Run{
		ID:                   uuid.New(),
		Provider:             strings.TrimSpace(input.Provider),
		TenantID:             strings.TrimSpace(input.TenantID),
		ConversationID:       strings.TrimSpace(input.ConversationID),
		TriggerMessageID:     strings.TrimSpace(input.TriggerMessageID),
		ProviderEventID:      strings.TrimSpace(input.ProviderEventID),
		ThreadRootMessageID:  strings.TrimSpace(input.ThreadRootMessageID),
		ReplyParentMessageID: strings.TrimSpace(input.ReplyParentMessageID),
		TriggerOccurredAt:    input.TriggerOccurredAt.UTC(),
		InitiatingUserID:     input.InitiatingUserID,
		InitiatingSubjectID:  strings.TrimSpace(input.InitiatingSubjectID),
		Status:               RunQueued,
		CommandKind:          input.CommandKind,
		Model:                strings.TrimSpace(input.Model),
		PromptVersion:        strings.TrimSpace(input.PromptVersion),
		AvailableAt:          now.UTC(),
		CreatedAt:            now.UTC(),
		UpdatedAt:            now.UTC(),
	}
	if err := run.Validate(); err != nil {
		return Run{}, err
	}
	return run, nil
}

type NewRunInput struct {
	Provider             string
	TenantID             string
	ConversationID       string
	TriggerMessageID     string
	ProviderEventID      string
	ThreadRootMessageID  string
	ReplyParentMessageID string
	TriggerOccurredAt    time.Time
	InitiatingUserID     uuid.UUID
	InitiatingSubjectID  string
	CommandKind          CommandKind
	Model                string
	PromptVersion        string
}

func (r Run) Validate() error {
	if r.ID == uuid.Nil ||
		strings.TrimSpace(r.Provider) == "" ||
		strings.TrimSpace(r.TenantID) == "" ||
		strings.TrimSpace(r.ConversationID) == "" ||
		strings.TrimSpace(r.TriggerMessageID) == "" ||
		strings.TrimSpace(r.ProviderEventID) == "" ||
		r.TriggerOccurredAt.IsZero() ||
		r.InitiatingUserID == uuid.Nil ||
		strings.TrimSpace(r.InitiatingSubjectID) == "" ||
		strings.TrimSpace(r.Model) == "" ||
		strings.TrimSpace(r.PromptVersion) == "" ||
		r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.AvailableAt.IsZero() {
		return ErrInvalidRun
	}
	if r.Provider != "lark" {
		return ErrInvalidRun
	}
	if r.CommandKind != CommandDirect && r.CommandKind != CommandDiscussion {
		return ErrInvalidRun
	}
	if !validStatus(r.Status) ||
		r.AttemptCount < 0 ||
		r.ClarificationRounds < 0 ||
		r.ClarificationRounds > MaxClarificationRounds ||
		r.ContextMessagesUsed < 0 ||
		r.ContextMessagesUsed > MaxContextMessages {
		return ErrInvalidRun
	}
	if (r.CreatedTaskID == nil) != (r.CreatedTaskNumber == nil) {
		return ErrInvalidRun
	}
	if r.CreatedTaskID != nil && (*r.CreatedTaskID == uuid.Nil || *r.CreatedTaskNumber <= 0) {
		return ErrInvalidRun
	}
	isTerminal := r.IsTerminal()
	if isTerminal != (r.CompletedAt != nil) {
		return ErrInvalidRun
	}
	if r.Status == RunRunning {
		if strings.TrimSpace(r.LeaseOwner) == "" || r.LeaseExpiresAt == nil {
			return ErrInvalidRun
		}
	} else if r.LeaseOwner != "" || r.LeaseExpiresAt != nil {
		return ErrInvalidRun
	}
	if r.Status == RunWaitingUser {
		if strings.TrimSpace(r.ClarificationMessageID) == "" ||
			strings.TrimSpace(r.ClarificationInterruptID) == "" ||
			r.ClarificationExpiresAt == nil {
			return ErrInvalidRun
		}
	}
	return nil
}

func (r Run) IsTerminal() bool {
	return r.Status == RunSucceeded || r.Status == RunFailed || r.Status == RunCancelled
}

func (r *Run) Claim(workerID string, now time.Time, leaseDuration time.Duration) error {
	if r.IsTerminal() {
		return ErrRunTerminal
	}
	if r.Status != RunQueued || strings.TrimSpace(workerID) == "" || now.Before(r.AvailableAt) {
		return ErrInvalidTransition
	}
	if leaseDuration <= 0 {
		leaseDuration = DefaultLeaseDuration
	}
	expiresAt := now.UTC().Add(leaseDuration)
	r.Status = RunRunning
	r.LeaseOwner = strings.TrimSpace(workerID)
	r.LeaseExpiresAt = &expiresAt
	r.AttemptCount++
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) RenewLease(workerID string, now time.Time, leaseDuration time.Duration) error {
	if r.Status != RunRunning || r.LeaseOwner != workerID ||
		r.LeaseExpiresAt == nil || now.After(*r.LeaseExpiresAt) {
		return ErrAgentRunLeaseLost
	}
	if leaseDuration <= 0 {
		leaseDuration = DefaultLeaseDuration
	}
	expiresAt := now.UTC().Add(leaseDuration)
	r.LeaseExpiresAt = &expiresAt
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) WaitForUser(messageID, interruptID string, now time.Time) error {
	if r.Status != RunRunning {
		return ErrInvalidTransition
	}
	if r.ClarificationRounds >= MaxClarificationRounds {
		return ErrClarificationLimit
	}
	messageID = strings.TrimSpace(messageID)
	interruptID = strings.TrimSpace(interruptID)
	if messageID == "" || interruptID == "" {
		return ErrInvalidTransition
	}
	expiresAt := now.UTC().Add(ClarificationLifetime)
	r.Status = RunWaitingUser
	r.ClarificationRounds++
	r.ClarificationMessageID = messageID
	r.ClarificationInterruptID = interruptID
	r.ClarificationExpiresAt = &expiresAt
	r.clearLease()
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) Resume(userID uuid.UUID, now time.Time) error {
	if r.Status != RunWaitingUser {
		return ErrRunNotWaiting
	}
	if userID != r.InitiatingUserID {
		return ErrClarificationUserMismatch
	}
	if r.ClarificationExpiresAt == nil || !now.UTC().Before(*r.ClarificationExpiresAt) {
		return ErrClarificationExpired
	}
	r.Status = RunQueued
	r.AvailableAt = now.UTC()
	r.ClarificationMessageID = ""
	r.ClarificationExpiresAt = nil
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) AddContextMessages(count int, now time.Time) error {
	if count < 0 || r.ContextMessagesUsed+count > MaxContextMessages {
		return ErrContextLimit
	}
	r.ContextMessagesUsed += count
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) AttachTask(taskID uuid.UUID, taskNumber int64, now time.Time) error {
	if r.CreatedTaskID != nil {
		if *r.CreatedTaskID == taskID && r.CreatedTaskNumber != nil && *r.CreatedTaskNumber == taskNumber {
			return nil
		}
		return ErrTaskAlreadyCreated
	}
	if r.Status != RunRunning || taskID == uuid.Nil || taskNumber <= 0 {
		return ErrInvalidTransition
	}
	r.CreatedTaskID = &taskID
	r.CreatedTaskNumber = &taskNumber
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) Succeed(now time.Time) error {
	if r.Status != RunRunning || r.CreatedTaskID == nil {
		return ErrInvalidTransition
	}
	return r.terminal(RunSucceeded, "", "", now)
}

func (r *Run) Fail(category, detail string, now time.Time) error {
	if r.Status != RunRunning && r.Status != RunQueued {
		return ErrInvalidTransition
	}
	return r.terminal(RunFailed, category, detail, now)
}

func (r *Run) Cancel(category, detail string, now time.Time) error {
	if r.IsTerminal() {
		return ErrRunTerminal
	}
	return r.terminal(RunCancelled, category, detail, now)
}

func (r *Run) Retry(availableAt time.Time, now time.Time) error {
	if r.Status != RunRunning {
		return ErrInvalidTransition
	}
	r.Status = RunQueued
	r.AvailableAt = availableAt.UTC()
	r.clearLease()
	r.UpdatedAt = now.UTC()
	return nil
}

func (r *Run) terminal(status RunStatus, category, detail string, now time.Time) error {
	if status != RunSucceeded && status != RunFailed && status != RunCancelled {
		return ErrInvalidTransition
	}
	completedAt := now.UTC()
	r.Status = status
	r.TerminalErrorCategory = strings.TrimSpace(category)
	r.TerminalErrorDetail = strings.TrimSpace(detail)
	r.CompletedAt = &completedAt
	r.ClarificationMessageID = ""
	r.ClarificationInterruptID = ""
	r.ClarificationExpiresAt = nil
	r.clearLease()
	r.UpdatedAt = completedAt
	return nil
}

func (r *Run) clearLease() {
	r.LeaseOwner = ""
	r.LeaseExpiresAt = nil
}

func validStatus(status RunStatus) bool {
	switch status {
	case RunQueued, RunRunning, RunWaitingUser, RunSucceeded, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

type Checkpoint struct {
	RunID           uuid.UUID
	FormatVersion   int
	EinoVersion     string
	Model           string
	EncryptionKeyID string
	Ciphertext      []byte
	UpdatedAt       time.Time
}

type ToolCall struct {
	RunID           uuid.UUID
	ToolCallID      string
	ToolName        string
	ArgumentHash    []byte
	ArgumentSummary []byte
	State           ToolCallState
	Result          []byte
	ErrorCategory   string
	StartedAt       time.Time
	CompletedAt     *time.Time
}

type OutboxMessage struct {
	ID                uuid.UUID
	RunID             uuid.UUID
	DeduplicationKey  string
	Kind              OutboxKind
	TargetMessageID   string
	Body              string
	State             OutboxState
	AttemptCount      int
	AvailableAt       time.Time
	LeaseOwner        string
	LeaseExpiresAt    *time.Time
	ProviderMessageID string
	ProviderErrorCode string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeliveredAt       *time.Time
}

func ShortRunReference(runID uuid.UUID) string {
	value := strings.ReplaceAll(runID.String(), "-", "")
	if len(value) < 8 {
		return value
	}
	return value[:8]
}

func CreateTaskIdempotencyKey(runID uuid.UUID) string {
	return fmt.Sprintf("agent-run:%s:create-task:v1", runID)
}
