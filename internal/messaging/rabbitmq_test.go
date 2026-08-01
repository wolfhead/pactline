package messaging

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQTopologyAndPublisherConfirm(t *testing.T) {
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5673/"
	}
	rabbit, err := Dial(url)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("RabbitMQ is required in CI: %v", err)
		}
		t.Skipf("RabbitMQ unavailable; run make up: %v", err)
	}
	defer rabbit.Close()
	_, err = rabbit.consumer.QueuePurge(NoopQueue, false)
	require.NoError(t, err)

	eventID := uuid.New()
	payload, err := json.Marshal(map[string]any{
		"event_id": eventID, "event_type": domain.EventCommentMentioned,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, rabbit.Publish(ctx, domain.OutboxEvent{
		ID: eventID, AggregateType: "comment", AggregateID: uuid.New(),
		EventType: domain.EventCommentMentioned, RecipientID: uuid.New(),
		Payload: payload, DedupeKey: "test:" + eventID.String(), CreatedAt: time.Now().UTC(),
	}))

	delivery, ok, err := rabbit.consumer.Get(NoopQueue, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, eventID.String(), delivery.MessageId)
	require.Equal(t, domain.EventCommentMentioned, delivery.Type)
	require.NoError(t, delivery.Ack(false))
}
