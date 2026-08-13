package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func insertAccessRequestedEvent(
	ctx context.Context,
	tx pgx.Tx,
	requester domain.User,
	occurredAt time.Time,
) error {
	var administratorID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM users
		WHERE platform_role='ADMIN' AND access_status='APPROVED' AND active
		LIMIT 1`).Scan(&administratorID)
	if errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("access request notification omitted", "reason", "administrator_unavailable",
			"requester_id", requester.ID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve access request administrator: %w", err)
	}
	event, err := events.New(events.NewEvent{
		AggregateType: "access_request", AggregateID: requester.ID,
		Type: events.AccessRequested, RecipientID: administratorID,
		Payload: events.AccessRequestedPayload{
			RequesterID: requester.ID, RequesterName: requester.Name,
			RequesterEmail: requester.Email, RequestedAt: occurredAt,
		},
		DedupeKey: fmt.Sprintf("%s:%s", events.AccessRequested, requester.ID),
		CreatedAt: occurredAt,
	})
	if err != nil {
		return fmt.Errorf("build access requested event: %w", err)
	}
	return insertEvent(ctx, tx, event)
}

func insertAccessApprovedEvent(
	ctx context.Context,
	tx pgx.Tx,
	userID, actorID uuid.UUID,
	occurredAt time.Time,
) error {
	var userName, actorName string
	if err := tx.QueryRow(ctx, `SELECT subject.name, actor.name
		FROM users subject JOIN users actor ON actor.id=$2
		WHERE subject.id=$1`, userID, actorID).Scan(&userName, &actorName); err != nil {
		return fmt.Errorf("resolve access approval participants: %w", err)
	}
	event, err := events.New(events.NewEvent{
		AggregateType: "access_request", AggregateID: userID,
		Type: events.AccessApproved, RecipientID: userID,
		Payload: events.AccessApprovedPayload{
			UserID: userID, UserName: userName,
			ApprovedByID: actorID, ApprovedByName: actorName, ApprovedAt: occurredAt,
		},
		DedupeKey: fmt.Sprintf("%s:%s", events.AccessApproved, userID),
		CreatedAt: occurredAt,
	})
	if err != nil {
		return fmt.Errorf("build access approved event: %w", err)
	}
	return insertEvent(ctx, tx, event)
}
