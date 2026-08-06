package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"

	"github.com/google/uuid"
)

type OutboxStore struct{ db *DB }

func NewOutboxStore(db *DB) *OutboxStore { return &OutboxStore{db: db} }

func (s *OutboxStore) ClaimBatch(ctx context.Context, limit int) ([]events.Event, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim outbox events: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `WITH candidates AS (
		SELECT id FROM outbox_events
		WHERE status IN ('pending','publishing') AND next_attempt_at <= now()
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED LIMIT $1
	)
	UPDATE outbox_events event
	SET status='publishing', attempt_count=attempt_count+1,
		next_attempt_at=now()+interval '30 seconds', last_error=NULL
	FROM candidates WHERE event.id=candidates.id
	RETURNING event.id, event.aggregate_type, event.aggregate_id, event.event_type,
		event.recipient_id, event.payload, event.dedupe_key,
		event.attempt_count, event.created_at`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()
	claimed := []events.Event{}
	for rows.Next() {
		var event events.Event
		if err := rows.Scan(
			&event.ID, &event.AggregateType, &event.AggregateID,
			&event.Type, &event.RecipientID, &event.Payload,
			&event.DedupeKey, &event.AttemptCount, &event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		claimed = append(claimed, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim outbox events: %w", err)
	}
	return claimed, nil
}

func (s *OutboxStore) MarkPublished(ctx context.Context, id uuid.UUID) error {
	command, err := s.db.Pool.Exec(ctx, `UPDATE outbox_events
		SET status='published', published_at=now(), last_error=NULL WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *OutboxStore) MarkFailed(ctx context.Context, id uuid.UUID, attempt int, publishError error) error {
	delay := time.Second << min(attempt, 8)
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	message := publishError.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err := s.db.Pool.Exec(ctx, `UPDATE outbox_events
		SET status='pending', next_attempt_at=now()+$2::interval, last_error=$3
		WHERE id=$1`, id, delay.String(), message)
	if err != nil {
		return fmt.Errorf("mark outbox event failed: %w", err)
	}
	return nil
}

func (s *OutboxStore) ConsumeOnce(
	ctx context.Context, consumerName string, eventID uuid.UUID, payload json.RawMessage,
) (bool, error) {
	if !json.Valid(payload) {
		return false, fmt.Errorf("invalid event JSON")
	}
	return s.MarkConsumed(ctx, consumerName, eventID)
}

func (s *OutboxStore) WasConsumed(
	ctx context.Context,
	consumerName string,
	eventID uuid.UUID,
) (bool, error) {
	var consumed bool
	if err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM message_consumptions WHERE consumer_name=$1 AND event_id=$2
	)`, consumerName, eventID).Scan(&consumed); err != nil {
		return false, fmt.Errorf("check message consumption: %w", err)
	}
	return consumed, nil
}

func (s *OutboxStore) MarkConsumed(
	ctx context.Context,
	consumerName string,
	eventID uuid.UUID,
) (bool, error) {
	command, err := s.db.Pool.Exec(ctx, `INSERT INTO message_consumptions
		(consumer_name, event_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, consumerName, eventID)
	if err != nil {
		return false, fmt.Errorf("record message consumption: %w", err)
	}
	return command.RowsAffected() == 1, nil
}
