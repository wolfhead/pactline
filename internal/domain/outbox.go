package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	EventCommentMentioned = "comment.mentioned"
	EventCommentReplied   = "comment.replied"
)

type OutboxEvent struct {
	ID            uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	EventType     string
	RecipientID   uuid.UUID
	Payload       json.RawMessage
	DedupeKey     string
	AttemptCount  int
	CreatedAt     time.Time
}
