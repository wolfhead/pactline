package store

import (
	"context"
	"fmt"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func InsertBusinessAudit(
	ctx context.Context,
	tx pgx.Tx,
	event domain.BusinessAuditEvent,
) error {
	if err := event.Actor.Validate(); err != nil {
		return fmt.Errorf("validate business audit actor: %w", err)
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		return fmt.Errorf("business audit occurrence time is required")
	}
	if event.EntityID == uuid.Nil || event.EntityType == "" || event.Action == "" {
		return fmt.Errorf("business audit entity and action are required")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO business_audit_events (
			id, occurred_at, request_id, actor_user_id, auth_method,
			token_id, token_name, agent_run_id, entity_type, entity_id,
			entity_number, action, old_value, new_value
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		event.ID, event.OccurredAt, event.Actor.RequestID, event.Actor.UserID,
		event.Actor.AuthMethod, event.Actor.TokenID, nullIfEmpty(event.Actor.TokenName),
		event.Actor.AgentRunID, event.EntityType, event.EntityID, event.EntityNumber, event.Action,
		nullJSON(event.OldValue), nullJSON(event.NewValue),
	)
	if err != nil {
		return fmt.Errorf("insert business audit: %w", err)
	}
	return nil
}

func nullJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
