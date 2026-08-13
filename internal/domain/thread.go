package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ThreadRole string

const (
	ThreadRoleMain  ThreadRole = "main"
	ThreadRoleIssue ThreadRole = "issue"
)

type IssueThreadType string

const (
	IssueThreadTypeDecisionRequired   IssueThreadType = "decision_required"
	IssueThreadTypeDependencyRequired IssueThreadType = "dependency_required"
)

func (t IssueThreadType) Valid() bool {
	return t == IssueThreadTypeDecisionRequired ||
		t == IssueThreadTypeDependencyRequired
}

type IssueThreadStatus string

const (
	IssueThreadStatusOpen     IssueThreadStatus = "open"
	IssueThreadStatusResolved IssueThreadStatus = "resolved"
)

type Thread struct {
	ID              uuid.UUID
	TaskID          uuid.UUID
	Role            ThreadRole
	IssueType       IssueThreadType
	IssueStatus     IssueThreadStatus
	OpenedFromPhase TaskPhase
	OpenedBy        Actor
	ResolvedBy      *Actor
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResolvedAt      *time.Time
}

func NewMainThread(taskID uuid.UUID, now time.Time) (Thread, error) {
	thread := Thread{
		ID: uuid.New(), TaskID: taskID, Role: ThreadRoleMain,
		Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	return thread, thread.Validate()
}

func NewIssueThread(
	taskID uuid.UUID,
	issueType IssueThreadType,
	phase TaskPhase,
	openedBy Actor,
	now time.Time,
) (Thread, error) {
	if !issueType.Valid() {
		return Thread{}, fmt.Errorf("%w: %q", ErrWrongIssueType, issueType)
	}
	thread := Thread{
		ID: uuid.New(), TaskID: taskID, Role: ThreadRoleIssue,
		IssueType: issueType, IssueStatus: IssueThreadStatusOpen,
		OpenedFromPhase: phase, OpenedBy: openedBy,
		Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	return thread, thread.Validate()
}

func (t Thread) Validate() error {
	if t.ID == uuid.Nil || t.TaskID == uuid.Nil || t.Version < 1 ||
		t.CreatedAt.IsZero() || t.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Thread identity, version, and timestamps are required", ErrInvalidInput)
	}
	switch t.Role {
	case ThreadRoleMain:
		if t.IssueType != "" || t.IssueStatus != "" || t.OpenedFromPhase != "" ||
			t.ResolvedBy != nil || t.ResolvedAt != nil {
			return fmt.Errorf("%w: Main Thread cannot carry Issue fields", ErrInvalidInput)
		}
	case ThreadRoleIssue:
		if !t.IssueType.Valid() {
			return fmt.Errorf("%w: %q", ErrWrongIssueType, t.IssueType)
		}
		if (t.OpenedFromPhase != TaskPhaseInProgress && t.OpenedFromPhase != TaskPhaseInReview) ||
			!validThreadActor(t.OpenedBy) {
			return fmt.Errorf("%w: Issue Thread type, phase, and opener are required", ErrInvalidInput)
		}
		switch t.IssueStatus {
		case IssueThreadStatusOpen:
			if t.ResolvedBy != nil || t.ResolvedAt != nil {
				return fmt.Errorf("%w: open Issue Thread cannot carry resolution metadata", ErrInvalidInput)
			}
		case IssueThreadStatusResolved:
			if t.ResolvedBy == nil || !validThreadActor(*t.ResolvedBy) || t.ResolvedAt == nil {
				return fmt.Errorf("%w: resolved Issue Thread requires resolver and time", ErrInvalidInput)
			}
		default:
			return fmt.Errorf("%w: invalid Issue Thread status %q", ErrInvalidInput, t.IssueStatus)
		}
	default:
		return fmt.Errorf("%w: invalid Thread role %q", ErrInvalidInput, t.Role)
	}
	return nil
}

func (t *Thread) Resolve(resolvedBy Actor, now time.Time) error {
	if t.Role != ThreadRoleIssue || t.IssueStatus != IssueThreadStatusOpen {
		return fmt.Errorf("%w: only an open Issue Thread may be resolved", ErrConflict)
	}
	if !validThreadActor(resolvedBy) {
		return fmt.Errorf("%w: Issue resolver is required", ErrInvalidInput)
	}
	resolvedAt := now.UTC()
	t.IssueStatus = IssueThreadStatusResolved
	t.ResolvedBy = &resolvedBy
	t.ResolvedAt = &resolvedAt
	t.UpdatedAt = resolvedAt
	t.Version++
	return nil
}

type ThreadItemKind string

const (
	ThreadItemKindMessage            ThreadItemKind = "message"
	ThreadItemKindProgress           ThreadItemKind = "progress"
	ThreadItemKindHandoff            ThreadItemKind = "handoff"
	ThreadItemKindWorkSubmission     ThreadItemKind = "work_submission"
	ThreadItemKindExecutionCompleted ThreadItemKind = "execution_completed"
	ThreadItemKindReviewOutcome      ThreadItemKind = "review_outcome"
	ThreadItemKindResolutionRequest  ThreadItemKind = "resolution_request"
	ThreadItemKindResolution         ThreadItemKind = "resolution"
	ThreadItemKindIssueResolution    ThreadItemKind = "issue_resolution"
	ThreadItemKindSystemEvent        ThreadItemKind = "system_event"
)

func (k ThreadItemKind) Valid() bool {
	switch k {
	case ThreadItemKindMessage,
		ThreadItemKindProgress,
		ThreadItemKindHandoff,
		ThreadItemKindWorkSubmission,
		ThreadItemKindExecutionCompleted,
		ThreadItemKindReviewOutcome,
		ThreadItemKindResolutionRequest,
		ThreadItemKindResolution,
		ThreadItemKindIssueResolution,
		ThreadItemKindSystemEvent:
		return true
	default:
		return false
	}
}

func (k ThreadItemKind) Immutable() bool {
	return k != ThreadItemKindMessage
}

type IssueResolutionPayload struct {
	IssueThreadID uuid.UUID
	IssueType     IssueThreadType
	Request       string
	Resolution    string
	OpenedBy      Actor
	ResolvedBy    Actor
	OpenedAt      time.Time
	ResolvedAt    time.Time
}

type CriterionRevisionSnapshot struct {
	CriterionID uuid.UUID `json:"criterion_id"`
	Revision    int64     `json:"revision"`
}

type ExecutionCompletedPayload struct {
	ReviewCycle        int64                       `json:"review_cycle"`
	SubmissionItemIDs  []uuid.UUID                 `json:"submission_item_ids"`
	ExecutionCheckIDs  []uuid.UUID                 `json:"execution_check_ids"`
	CriterionRevisions []CriterionRevisionSnapshot `json:"criterion_revisions"`
	MergeRequests      []MergeRequestSnapshot      `json:"merge_requests,omitempty"`
}

func (p ExecutionCompletedPayload) Validate() error {
	if p.ReviewCycle < 1 {
		return fmt.Errorf("%w: execution completion review cycle is required", ErrInvalidInput)
	}
	for _, id := range append(append([]uuid.UUID(nil), p.SubmissionItemIDs...), p.ExecutionCheckIDs...) {
		if id == uuid.Nil {
			return fmt.Errorf("%w: execution completion references are invalid", ErrInvalidInput)
		}
	}
	for _, revision := range p.CriterionRevisions {
		if revision.CriterionID == uuid.Nil || revision.Revision < 1 {
			return fmt.Errorf("%w: execution completion criterion snapshot is invalid", ErrInvalidInput)
		}
	}
	for _, mergeRequest := range p.MergeRequests {
		if err := mergeRequest.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (p IssueResolutionPayload) Validate() error {
	if p.IssueThreadID == uuid.Nil || !p.IssueType.Valid() ||
		strings.TrimSpace(p.Request) == "" || strings.TrimSpace(p.Resolution) == "" ||
		!validThreadActor(p.OpenedBy) || !validThreadActor(p.ResolvedBy) ||
		p.OpenedAt.IsZero() || p.ResolvedAt.IsZero() || p.ResolvedAt.Before(p.OpenedAt) {
		return fmt.Errorf("%w: Issue resolution summary is incomplete", ErrInvalidInput)
	}
	return nil
}

type ThreadItem struct {
	ID                 uuid.UUID
	ThreadID           uuid.UUID
	Kind               ThreadItemKind
	Author             Actor
	Body               string
	IssueResolution    *IssueResolutionPayload
	ExecutionCompleted *ExecutionCompletedPayload
	TaskStageClaimID   *uuid.UUID
	TaskReviewCycle    *int64
	ReplyToItemID      *uuid.UUID
	MentionedUserIDs   []uuid.UUID
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

func (i ThreadItem) Validate() error {
	if i.ID == uuid.Nil || i.ThreadID == uuid.Nil || !i.Kind.Valid() ||
		!validThreadActor(i.Author) || i.Version < 1 ||
		i.CreatedAt.IsZero() || i.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Thread Item identity, author, kind, and timestamps are required", ErrInvalidInput)
	}
	if i.Kind == ThreadItemKindIssueResolution {
		if i.IssueResolution == nil {
			return fmt.Errorf("%w: issue_resolution Item requires a typed payload", ErrInvalidInput)
		}
		if err := i.IssueResolution.Validate(); err != nil {
			return err
		}
	} else if i.IssueResolution != nil {
		return fmt.Errorf("%w: only issue_resolution Item accepts that payload", ErrInvalidInput)
	}
	if i.Kind == ThreadItemKindExecutionCompleted {
		if i.ExecutionCompleted == nil {
			return fmt.Errorf("%w: execution_completed Item requires a typed payload", ErrInvalidInput)
		}
		if err := i.ExecutionCompleted.Validate(); err != nil {
			return err
		}
	} else if i.ExecutionCompleted != nil {
		return fmt.Errorf("%w: only execution_completed Item accepts that payload", ErrInvalidInput)
	}
	if i.Kind == ThreadItemKindWorkSubmission || i.Kind == ThreadItemKindExecutionCompleted {
		if i.TaskStageClaimID == nil || *i.TaskStageClaimID == uuid.Nil ||
			i.TaskReviewCycle == nil || *i.TaskReviewCycle < 1 {
			return fmt.Errorf("%w: delivery Item requires Claim and review-cycle context", ErrInvalidInput)
		}
		if i.ExecutionCompleted != nil && i.ExecutionCompleted.ReviewCycle != *i.TaskReviewCycle {
			return fmt.Errorf("%w: execution completion review cycle does not match Item context", ErrInvalidInput)
		}
	} else if i.TaskStageClaimID != nil || i.TaskReviewCycle != nil {
		return fmt.Errorf("%w: only delivery Items accept Claim and review-cycle context", ErrInvalidInput)
	}
	if i.DeletedAt != nil {
		if i.Kind != ThreadItemKindMessage {
			return fmt.Errorf("%w: immutable Thread Item cannot be deleted", ErrInvalidInput)
		}
		return nil
	}
	if strings.TrimSpace(i.Body) == "" && i.Kind != ThreadItemKindIssueResolution {
		return fmt.Errorf("%w: Thread Item body is required", ErrInvalidInput)
	}
	return nil
}

func (i *ThreadItem) Edit(body string, mentionedUserIDs []uuid.UUID, now time.Time) error {
	if i.Kind.Immutable() || i.DeletedAt != nil {
		return fmt.Errorf("%w: this Thread Item is immutable", ErrConflict)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: Thread Item body is required", ErrInvalidInput)
	}
	i.Body = body
	i.MentionedUserIDs = append([]uuid.UUID(nil), mentionedUserIDs...)
	i.UpdatedAt = now.UTC()
	i.Version++
	return nil
}

func (i *ThreadItem) Delete(now time.Time) error {
	if i.Kind.Immutable() || i.DeletedAt != nil {
		return fmt.Errorf("%w: this Thread Item cannot be deleted", ErrConflict)
	}
	deletedAt := now.UTC()
	i.DeletedAt = &deletedAt
	i.Body = ""
	i.MentionedUserIDs = nil
	i.UpdatedAt = deletedAt
	i.Version++
	return nil
}

func validThreadActor(actor Actor) bool {
	switch actor.Type {
	case ActorTypeUser:
		return actor.IsHuman()
	case ActorTypeAgent, ActorTypeSystem:
		return strings.TrimSpace(actor.Ref) != ""
	default:
		return false
	}
}
