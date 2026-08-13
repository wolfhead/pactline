package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wolfhead/pactline/internal/events"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	EventsExchange   = "pactline.events"
	RetryExchange    = "pactline.events.retry"
	DeadExchange     = "pactline.events.dead"
	NoopQueue        = "pactline.notifications.noop"
	LarkDMQueue      = "pactline.notifications.lark_dm"
	RetryQueue       = "pactline.notifications.retry"
	DeadQueue        = "pactline.notifications.dead"
	NoopConsumer     = "pactline.notifications.noop.v1"
	LarkDMConsumer   = "pactline.notifications.lark_dm.v1"
	maxDeliveryTries = 5
)

type EventHandler interface {
	Handle(context.Context, events.Event) error
}

type NoopConsumptionStore interface {
	ConsumeOnce(context.Context, string, uuid.UUID, json.RawMessage) (bool, error)
}

type EventConsumptionStore interface {
	WasConsumed(context.Context, string, uuid.UUID) (bool, error)
	MarkConsumed(context.Context, string, uuid.UUID) (bool, error)
}

type RabbitMQ struct {
	connection *amqp.Connection
	publisher  *amqp.Channel
	consumer   *amqp.Channel
	confirms   <-chan amqp.Confirmation
	returns    <-chan amqp.Return
}

type RecoveringPublisher struct {
	url    string
	mu     sync.Mutex
	client *RabbitMQ
}

func NewRecoveringPublisher(url string) (*RecoveringPublisher, error) {
	client, err := Dial(url)
	if err != nil {
		return nil, err
	}
	return &RecoveringPublisher{url: url, client: client}, nil
}

func (publisher *RecoveringPublisher) Publish(ctx context.Context, event events.Event) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if publisher.client == nil {
		client, err := Dial(publisher.url)
		if err != nil {
			return err
		}
		publisher.client = client
	}
	if err := publisher.client.Publish(ctx, event); err != nil {
		publisher.client.Close()
		publisher.client = nil
		return err
	}
	return nil
}

func (publisher *RecoveringPublisher) Close() {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if publisher.client != nil {
		publisher.client.Close()
		publisher.client = nil
	}
}

func Dial(url string) (*RabbitMQ, error) {
	connection, err := amqp.DialConfig(url, amqp.Config{
		Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(10 * time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("dial RabbitMQ: %w", err)
	}
	publisher, err := connection.Channel()
	if err != nil {
		connection.Close() //nolint:errcheck
		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	consumer, err := connection.Channel()
	if err != nil {
		publisher.Close()  //nolint:errcheck
		connection.Close() //nolint:errcheck
		return nil, fmt.Errorf("open RabbitMQ consumer channel: %w", err)
	}
	rabbit := &RabbitMQ{connection: connection, publisher: publisher, consumer: consumer}
	if err := rabbit.declareTopology(); err != nil {
		rabbit.Close()
		return nil, err
	}
	if err := publisher.Confirm(false); err != nil {
		rabbit.Close()
		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	rabbit.confirms = publisher.NotifyPublish(make(chan amqp.Confirmation, 1))
	rabbit.returns = publisher.NotifyReturn(make(chan amqp.Return, 1))
	return rabbit, nil
}

func (r *RabbitMQ) declareTopology() error {
	for _, exchange := range []struct {
		name string
	}{
		{name: EventsExchange}, {name: RetryExchange}, {name: DeadExchange},
	} {
		if err := r.publisher.ExchangeDeclare(exchange.name, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare RabbitMQ exchange %s: %w", exchange.name, err)
		}
	}
	if _, err := r.publisher.QueueDeclare(NoopQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": DeadExchange,
	}); err != nil {
		return fmt.Errorf("declare RabbitMQ noop queue: %w", err)
	}
	if err := r.publisher.QueueBind(NoopQueue, "comment.*", EventsExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ noop queue: %w", err)
	}
	if _, err := r.publisher.QueueDeclare(LarkDMQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": RetryExchange,
	}); err != nil {
		return fmt.Errorf("declare RabbitMQ Lark DM queue: %w", err)
	}
	if err := r.publisher.QueueBind(LarkDMQueue, "access.*", EventsExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ Lark DM queue: %w", err)
	}
	if err := r.publisher.QueueBind(LarkDMQueue, events.NotificationTest, EventsExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ notification test route: %w", err)
	}
	if _, err := r.publisher.QueueDeclare(RetryQueue, true, false, false, false, amqp.Table{
		"x-message-ttl": int32(30000), "x-dead-letter-exchange": EventsExchange,
	}); err != nil {
		return fmt.Errorf("declare RabbitMQ retry queue: %w", err)
	}
	if err := r.publisher.QueueBind(RetryQueue, "access.*", RetryExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ retry queue: %w", err)
	}
	if err := r.publisher.QueueBind(RetryQueue, events.NotificationTest, RetryExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ notification test retry route: %w", err)
	}
	if _, err := r.publisher.QueueDeclare(DeadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare RabbitMQ dead queue: %w", err)
	}
	if err := r.publisher.QueueBind(DeadQueue, "#", DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ dead queue: %w", err)
	}
	return nil
}

func (r *RabbitMQ) Publish(ctx context.Context, event events.Event) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate RabbitMQ event: %w", err)
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode RabbitMQ event: %w", err)
	}
	publishing := amqp.Publishing{
		Headers: amqp.Table{
			"recipient_id": event.RecipientID.String(), "dedupe_key": event.DedupeKey,
		},
		ContentType: "application/json", DeliveryMode: amqp.Persistent,
		MessageId: event.ID.String(), Type: event.Type,
		Timestamp: event.CreatedAt, Body: body,
	}
	if err := r.publisher.PublishWithContext(ctx, EventsExchange, event.Type, true, false, publishing); err != nil {
		return fmt.Errorf("publish RabbitMQ event: %w", err)
	}
	select {
	case returned := <-r.returns:
		return fmt.Errorf("RabbitMQ returned unroutable event %s: %s", event.ID, returned.ReplyText)
	case confirmation, ok := <-r.confirms:
		if !ok {
			return errors.New("RabbitMQ publisher confirm channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("RabbitMQ negatively acknowledged event %s", event.ID)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *RabbitMQ) ConsumeNoop(ctx context.Context, outbox NoopConsumptionStore) error {
	deliveries, err := r.consumer.ConsumeWithContext(
		ctx, NoopQueue, NoopConsumer, false, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("start RabbitMQ noop consumer: %w", err)
	}
	for delivery := range deliveries {
		event, parseErr := decodeDelivery(delivery)
		if parseErr != nil {
			slog.Error("reject invalid RabbitMQ event", "message_id", delivery.MessageId, "error", parseErr)
			if err := delivery.Nack(false, false); err != nil {
				return err
			}
			continue
		}
		first, consumeErr := outbox.ConsumeOnce(ctx, NoopConsumer, event.ID, delivery.Body)
		if consumeErr != nil {
			slog.Error("noop RabbitMQ consumer failed", "event_id", event.ID, "error", consumeErr)
			if err := delivery.Nack(false, true); err != nil {
				return err
			}
			continue
		}
		if first {
			slog.Info("noop notification event consumed", "event_id", event.ID, "event_type", delivery.Type)
		}
		if err := delivery.Ack(false); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *RabbitMQ) ConsumeLarkDM(
	ctx context.Context,
	outbox EventConsumptionStore,
	handler EventHandler,
) error {
	deliveries, err := r.consumer.ConsumeWithContext(
		ctx, LarkDMQueue, LarkDMConsumer, false, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("start RabbitMQ Lark DM consumer: %w", err)
	}
	for delivery := range deliveries {
		event, decodeErr := decodeDelivery(delivery)
		if decodeErr != nil {
			slog.Error("dead-letter invalid Lark DM event", "message_id", delivery.MessageId, "error", decodeErr)
			if err := r.deadLetter(ctx, delivery); err != nil {
				return err
			}
			continue
		}
		consumed, consumeErr := outbox.WasConsumed(ctx, LarkDMConsumer, event.ID)
		if consumeErr != nil {
			slog.Error("check Lark DM event consumption", "event_id", event.ID, "error", consumeErr)
			if err := delivery.Nack(false, false); err != nil {
				return err
			}
			continue
		}
		if consumed {
			if err := delivery.Ack(false); err != nil {
				return err
			}
			continue
		}
		deliveryContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		handleErr := handler.Handle(deliveryContext, event)
		cancel()
		if handleErr != nil {
			attempt := deliveryAttempt(delivery) + 1
			if retryableDelivery(handleErr) && attempt < maxDeliveryTries {
				slog.Warn("Lark DM event delivery deferred", "event_id", event.ID,
					"event_type", event.Type, "attempt", attempt, "error", handleErr)
				if err := delivery.Nack(false, false); err != nil {
					return err
				}
				continue
			}
			slog.Error("Lark DM event delivery exhausted", "event_id", event.ID,
				"event_type", event.Type, "attempt", attempt, "error", handleErr)
			if err := r.deadLetter(ctx, delivery); err != nil {
				return err
			}
			continue
		}
		if _, err := outbox.MarkConsumed(ctx, LarkDMConsumer, event.ID); err != nil {
			slog.Error("record Lark DM event consumption", "event_id", event.ID, "error", err)
			if nackErr := delivery.Nack(false, false); nackErr != nil {
				return nackErr
			}
			continue
		}
		if err := delivery.Ack(false); err != nil {
			return err
		}
		slog.Info("Lark DM notification delivered", "event_id", event.ID, "event_type", event.Type)
	}
	return ctx.Err()
}

func decodeDelivery(delivery amqp.Delivery) (events.Event, error) {
	var event events.Event
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		return events.Event{}, fmt.Errorf("decode RabbitMQ event: %w", err)
	}
	if err := event.Validate(); err != nil {
		return events.Event{}, err
	}
	if delivery.MessageId != event.ID.String() || delivery.Type != event.Type {
		return events.Event{}, fmt.Errorf("RabbitMQ event envelope mismatch")
	}
	return event, nil
}

func retryableDelivery(err error) bool {
	category, categorized := identity.ProviderCategoryFromError(err)
	if !categorized {
		return true
	}
	return category == identity.ProviderRateLimited || category == identity.ProviderUnavailable
}

func deliveryAttempt(delivery amqp.Delivery) int64 {
	deaths, ok := delivery.Headers["x-death"].([]any)
	if !ok {
		return 0
	}
	var attempts int64
	for _, raw := range deaths {
		death, ok := raw.(amqp.Table)
		if !ok || death["queue"] != LarkDMQueue {
			continue
		}
		if count, ok := death["count"].(int64); ok && count > attempts {
			attempts = count
		}
	}
	return attempts
}

func (r *RabbitMQ) deadLetter(ctx context.Context, delivery amqp.Delivery) error {
	publishing := amqp.Publishing{
		Headers: delivery.Headers, ContentType: delivery.ContentType,
		DeliveryMode: amqp.Persistent, MessageId: delivery.MessageId,
		Type: delivery.Type, Timestamp: delivery.Timestamp, Body: delivery.Body,
	}
	if err := r.publisher.PublishWithContext(
		ctx, DeadExchange, delivery.RoutingKey, false, false, publishing,
	); err != nil {
		return fmt.Errorf("publish dead-letter event: %w", err)
	}
	select {
	case confirmation, ok := <-r.confirms:
		if !ok || !confirmation.Ack {
			return fmt.Errorf("dead-letter event was not confirmed")
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge dead-lettered event: %w", err)
	}
	return nil
}

func ConsumeNoopForever(ctx context.Context, url string, outbox NoopConsumptionStore) {
	for ctx.Err() == nil {
		client, err := Dial(url)
		if err == nil {
			err = client.ConsumeNoop(ctx, outbox)
			client.Close()
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("RabbitMQ noop consumer reconnecting", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func ConsumeLarkDMForever(
	ctx context.Context,
	url string,
	outbox EventConsumptionStore,
	handler EventHandler,
) {
	for ctx.Err() == nil {
		client, err := Dial(url)
		if err == nil {
			err = client.ConsumeLarkDM(ctx, outbox, handler)
			client.Close()
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("RabbitMQ Lark DM consumer reconnecting", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *RabbitMQ) Close() {
	if r.consumer != nil {
		r.consumer.Close() //nolint:errcheck
	}
	if r.publisher != nil {
		r.publisher.Close() //nolint:errcheck
	}
	if r.connection != nil {
		r.connection.Close() //nolint:errcheck
	}
}
