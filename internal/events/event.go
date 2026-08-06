// Package events defines Pactline's durable application-event contract.
// Producers persist Events in the same transaction as the state change that
// caused them. RabbitMQ routing and delivery consumers remain downstream
// concerns.
package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	CommentMentioned = "comment.mentioned"
	CommentReplied   = "comment.replied"
	AccessRequested  = "access.requested"
	AccessApproved   = "access.approved"
)

type Event struct {
	ID            uuid.UUID       `json:"id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   uuid.UUID       `json:"aggregate_id"`
	Type          string          `json:"type"`
	RecipientID   uuid.UUID       `json:"recipient_id"`
	Payload       json.RawMessage `json:"payload"`
	DedupeKey     string          `json:"dedupe_key"`
	AttemptCount  int             `json:"-"`
	CreatedAt     time.Time       `json:"created_at"`
}

type NewEvent struct {
	ID            uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	Type          string
	RecipientID   uuid.UUID
	Payload       any
	DedupeKey     string
	CreatedAt     time.Time
}

func New(input NewEvent) (Event, error) {
	if input.Payload == nil {
		return Event{}, fmt.Errorf("event payload is required")
	}
	if input.ID == uuid.Nil {
		input.ID = uuid.New()
	}
	if payload, ok := input.Payload.(interface{ Validate() error }); ok {
		if err := payload.Validate(); err != nil {
			return Event{}, err
		}
	}
	event := Event{
		ID: input.ID, AggregateType: strings.TrimSpace(input.AggregateType),
		AggregateID: input.AggregateID, Type: strings.TrimSpace(input.Type),
		RecipientID: input.RecipientID, DedupeKey: strings.TrimSpace(input.DedupeKey),
		CreatedAt: input.CreatedAt.UTC(),
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode event payload: %w", err)
	}
	event.Payload = payload
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (event Event) Validate() error {
	if event.ID == uuid.Nil || event.AggregateID == uuid.Nil || event.RecipientID == uuid.Nil ||
		strings.TrimSpace(event.AggregateType) == "" || strings.TrimSpace(event.Type) == "" ||
		strings.TrimSpace(event.DedupeKey) == "" || event.CreatedAt.IsZero() ||
		!json.Valid(event.Payload) {
		return fmt.Errorf("invalid application event")
	}
	return nil
}

func DecodePayload[T any](event Event) (T, error) {
	var payload T
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return payload, fmt.Errorf("decode %s event payload: %w", event.Type, err)
	}
	if validator, ok := any(payload).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return payload, err
		}
	}
	return payload, nil
}
