package domain

import (
	"time"

	"github.com/google/uuid"
)

// Comment is a remark on a task. It is editable and deletable only by its
// own author (enforced in the store, see task_comment_store.go).
type Comment struct {
	ID               uuid.UUID
	TaskID           uuid.UUID
	AuthorID         uuid.UUID
	Body             string
	ReplyToCommentID *uuid.UUID
	ThreadRootID     uuid.UUID
	MentionedUserIDs []uuid.UUID
	DeletedAt        *time.Time
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
