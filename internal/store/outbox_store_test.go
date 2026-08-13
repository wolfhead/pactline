package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOutboxStoreEnqueuesApplicationEvent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	eventID := uuid.New()
	event, err := events.New(events.NewEvent{
		ID: eventID, AggregateType: "notification_test", AggregateID: eventID,
		Type: events.NotificationTest, RecipientID: primarySeedID,
		Payload: events.NotificationTestPayload{
			TriggeredByID: primarySeedID, TriggeredByName: "Administrator",
			TriggeredAt: time.Now().UTC(),
		},
		DedupeKey: events.NotificationTest + ":" + eventID.String(), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE id=$1`, eventID)
		require.NoError(t, cleanupErr)
	})

	require.NoError(t, store.NewOutboxStore(db).Enqueue(ctx, event))
	var eventType string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT event_type FROM outbox_events WHERE id=$1`, eventID).Scan(&eventType))
	require.Equal(t, events.NotificationTest, eventType)
}
