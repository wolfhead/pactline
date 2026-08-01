package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"
)

type EventPublisher interface {
	Publish(context.Context, domain.OutboxEvent) error
}

type OutboxRelay struct {
	Store     *store.OutboxStore
	Publisher EventPublisher
	Interval  time.Duration
}

func (r OutboxRelay) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := r.PublishPending(ctx); err != nil && ctx.Err() == nil {
			slog.Error("relay pending outbox events", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r OutboxRelay) PublishPending(ctx context.Context) error {
	events, err := r.Store.ClaimBatch(ctx, 50)
	if err != nil {
		return err
	}
	for _, event := range events {
		publishContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := r.Publisher.Publish(publishContext, event)
		cancel()
		if err != nil {
			if markErr := r.Store.MarkFailed(ctx, event.ID, event.AttemptCount, err); markErr != nil {
				return markErr
			}
			slog.Warn("outbox event publish deferred", "event_id", event.ID,
				"event_type", event.EventType, "attempt", event.AttemptCount, "error", err)
			continue
		}
		if err := r.Store.MarkPublished(ctx, event.ID); err != nil {
			return err
		}
		slog.Info("outbox event published", "event_id", event.ID,
			"event_type", event.EventType, "attempt", event.AttemptCount)
	}
	return nil
}
