package events

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CommentPayload struct {
	ProjectID        uuid.UUID  `json:"project_id"`
	TaskID           uuid.UUID  `json:"task_id"`
	CommentID        uuid.UUID  `json:"comment_id"`
	CommentAuthorID  uuid.UUID  `json:"comment_author_id"`
	ReplyToCommentID *uuid.UUID `json:"reply_to_comment_id,omitempty"`
	OccurredAt       time.Time  `json:"occurred_at"`
}

type AccessRequestedPayload struct {
	RequesterID    uuid.UUID `json:"requester_id"`
	RequesterName  string    `json:"requester_name"`
	RequesterEmail *string   `json:"requester_email,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
}

type AccessApprovedPayload struct {
	UserID         uuid.UUID `json:"user_id"`
	UserName       string    `json:"user_name"`
	ApprovedByID   uuid.UUID `json:"approved_by_id"`
	ApprovedByName string    `json:"approved_by_name"`
	ApprovedAt     time.Time `json:"approved_at"`
}

func (payload CommentPayload) Validate() error {
	if payload.ProjectID == uuid.Nil || payload.TaskID == uuid.Nil || payload.CommentID == uuid.Nil ||
		payload.CommentAuthorID == uuid.Nil || payload.OccurredAt.IsZero() {
		return fmt.Errorf("invalid comment event payload")
	}
	return nil
}

func (payload AccessRequestedPayload) Validate() error {
	if payload.RequesterID == uuid.Nil || strings.TrimSpace(payload.RequesterName) == "" ||
		payload.RequestedAt.IsZero() {
		return fmt.Errorf("invalid access requested event payload")
	}
	return nil
}

func (payload AccessApprovedPayload) Validate() error {
	if payload.UserID == uuid.Nil || strings.TrimSpace(payload.UserName) == "" ||
		payload.ApprovedByID == uuid.Nil || strings.TrimSpace(payload.ApprovedByName) == "" ||
		payload.ApprovedAt.IsZero() {
		return fmt.Errorf("invalid access approved event payload")
	}
	return nil
}
