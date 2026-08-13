package store

import (
	"context"
	"fmt"

	"github.com/wolfhead/pactline/internal/events"

	"github.com/jackc/pgx/v5"
)

func insertEvent(ctx context.Context, tx pgx.Tx, event events.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events (
		id, aggregate_type, aggregate_id, event_type, recipient_id, payload, dedupe_key, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (dedupe_key) DO NOTHING`,
		event.ID, event.AggregateType, event.AggregateID, event.Type,
		event.RecipientID, event.Payload, event.DedupeKey, event.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert application event %s: %w", event.Type, err)
	}
	return nil
}
