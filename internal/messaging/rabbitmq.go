package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	EventsExchange = "pactline.events"
	RetryExchange  = "pactline.events.retry"
	DeadExchange   = "pactline.events.dead"
	NoopQueue      = "pactline.notifications.noop"
	RetryQueue     = "pactline.notifications.retry"
	DeadQueue      = "pactline.notifications.dead"
	NoopConsumer   = "pactline.notifications.noop.v1"
)

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

func (publisher *RecoveringPublisher) Publish(ctx context.Context, event domain.OutboxEvent) error {
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
	if _, err := r.publisher.QueueDeclare(RetryQueue, true, false, false, false, amqp.Table{
		"x-message-ttl": int32(30000), "x-dead-letter-exchange": EventsExchange,
	}); err != nil {
		return fmt.Errorf("declare RabbitMQ retry queue: %w", err)
	}
	if err := r.publisher.QueueBind(RetryQueue, "comment.*", RetryExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ retry queue: %w", err)
	}
	if _, err := r.publisher.QueueDeclare(DeadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare RabbitMQ dead queue: %w", err)
	}
	if err := r.publisher.QueueBind(DeadQueue, "#", DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ dead queue: %w", err)
	}
	return nil
}

func (r *RabbitMQ) Publish(ctx context.Context, event domain.OutboxEvent) error {
	publishing := amqp.Publishing{
		Headers: amqp.Table{
			"recipient_id": event.RecipientID.String(), "dedupe_key": event.DedupeKey,
		},
		ContentType: "application/json", DeliveryMode: amqp.Persistent,
		MessageId: event.ID.String(), Type: event.EventType,
		Timestamp: event.CreatedAt, Body: event.Payload,
	}
	if err := r.publisher.PublishWithContext(ctx, EventsExchange, event.EventType, true, false, publishing); err != nil {
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

func (r *RabbitMQ) ConsumeNoop(ctx context.Context, outbox *store.OutboxStore) error {
	deliveries, err := r.consumer.ConsumeWithContext(
		ctx, NoopQueue, NoopConsumer, false, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("start RabbitMQ noop consumer: %w", err)
	}
	for delivery := range deliveries {
		eventID, parseErr := uuid.Parse(delivery.MessageId)
		if parseErr != nil {
			slog.Error("reject RabbitMQ event with invalid message ID", "message_id", delivery.MessageId)
			if err := delivery.Nack(false, false); err != nil {
				return err
			}
			continue
		}
		first, consumeErr := outbox.ConsumeOnce(ctx, NoopConsumer, eventID, delivery.Body)
		if consumeErr != nil {
			slog.Error("noop RabbitMQ consumer failed", "event_id", eventID, "error", consumeErr)
			if err := delivery.Nack(false, true); err != nil {
				return err
			}
			continue
		}
		if first {
			slog.Info("noop notification event consumed", "event_id", eventID, "event_type", delivery.Type)
		}
		if err := delivery.Ack(false); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func ConsumeNoopForever(ctx context.Context, url string, outbox *store.OutboxStore) {
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
