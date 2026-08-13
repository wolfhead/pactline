package messaging

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

type consumptionStoreStub struct {
	mu       sync.Mutex
	consumed map[uuid.UUID]bool
	marked   chan uuid.UUID
}

func (stub *consumptionStoreStub) WasConsumed(
	_ context.Context,
	_ string,
	eventID uuid.UUID,
) (bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.consumed[eventID], nil
}

func (stub *consumptionStoreStub) MarkConsumed(
	_ context.Context,
	_ string,
	eventID uuid.UUID,
) (bool, error) {
	stub.mu.Lock()
	first := !stub.consumed[eventID]
	stub.consumed[eventID] = true
	stub.mu.Unlock()
	stub.marked <- eventID
	return first, nil
}

type eventHandlerStub struct {
	handled chan events.Event
}

func (stub eventHandlerStub) Handle(_ context.Context, event events.Event) error {
	stub.handled <- event
	return nil
}

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
	_, err = rabbit.consumer.QueuePurge(LarkDMQueue, false)
	require.NoError(t, err)

	eventID := uuid.New()
	event, err := events.New(events.NewEvent{
		ID: eventID, AggregateType: "comment", AggregateID: uuid.New(),
		Type: events.CommentMentioned, RecipientID: uuid.New(),
		Payload:   map[string]string{"body": "test"},
		DedupeKey: "test:" + eventID.String(), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, rabbit.Publish(ctx, event))

	delivery, ok, err := rabbit.consumer.Get(NoopQueue, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, eventID.String(), delivery.MessageId)
	require.Equal(t, events.CommentMentioned, delivery.Type)
	require.NoError(t, delivery.Ack(false))

	accessEvent, err := events.New(events.NewEvent{
		AggregateType: "access_request", AggregateID: uuid.New(),
		Type: events.AccessRequested, RecipientID: uuid.New(),
		Payload:   map[string]string{"body": "test"},
		DedupeKey: "test:access:" + eventID.String(), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.NoError(t, rabbit.Publish(ctx, accessEvent))
	delivery, ok, err = rabbit.consumer.Get(LarkDMQueue, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, events.AccessRequested, delivery.Type)
	require.NoError(t, delivery.Ack(false))

	notificationEventID := uuid.New()
	notificationEvent, err := events.New(events.NewEvent{
		ID: notificationEventID, AggregateType: "notification_test", AggregateID: notificationEventID,
		Type: events.NotificationTest, RecipientID: uuid.New(),
		Payload:   map[string]string{"body": "test"},
		DedupeKey: "test:notification:" + eventID.String(), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.NoError(t, rabbit.Publish(ctx, notificationEvent))
	delivery, ok, err = rabbit.consumer.Get(LarkDMQueue, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, events.NotificationTest, delivery.Type)
	require.NoError(t, delivery.Ack(false))
}

func TestRabbitMQLarkDMConsumerMarksSuccessfulDelivery(t *testing.T) {
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
	_, err = rabbit.consumer.QueuePurge(LarkDMQueue, false)
	require.NoError(t, err)

	event, err := events.New(events.NewEvent{
		AggregateType: "access_request", AggregateID: uuid.New(),
		Type: events.AccessApproved, RecipientID: uuid.New(),
		Payload:   map[string]string{"status": "approved"},
		DedupeKey: "test:consumer:" + uuid.NewString(), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	store := &consumptionStoreStub{consumed: map[uuid.UUID]bool{}, marked: make(chan uuid.UUID, 1)}
	handler := eventHandlerStub{handled: make(chan events.Event, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rabbit.ConsumeLarkDM(ctx, store, handler) }()
	require.NoError(t, rabbit.Publish(context.Background(), event))

	select {
	case handled := <-handler.handled:
		require.Equal(t, event.ID, handled.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Lark DM handler")
	}
	select {
	case marked := <-store.marked:
		require.Equal(t, event.ID, marked)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Lark DM consumption marker")
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

type categorizedError struct {
	category identity.ProviderErrorCategory
}

func (err categorizedError) Error() string { return string(err.category) }
func (err categorizedError) ProviderCategory() identity.ProviderErrorCategory {
	return err.category
}

func TestRabbitMQDeliveryRetryClassification(t *testing.T) {
	require.True(t, retryableDelivery(errors.New("database unavailable")))
	require.True(t, retryableDelivery(categorizedError{category: identity.ProviderUnavailable}))
	require.False(t, retryableDelivery(categorizedError{category: identity.ProviderUnauthorized}))
	require.EqualValues(t, 3, deliveryAttempt(amqp.Delivery{Headers: amqp.Table{
		"x-death": []any{amqp.Table{"queue": LarkDMQueue, "count": int64(3)}},
	}}))
}
