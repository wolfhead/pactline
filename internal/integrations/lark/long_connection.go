package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wolfhead/pactline/internal/agent/channel"

	larksdk "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

const (
	ConnectionInitializing = channel.ConnectionInitializing
	ConnectionConnecting   = channel.ConnectionConnecting
	ConnectionReady        = channel.ConnectionReady
	ConnectionReconnecting = channel.ConnectionReconnecting
	ConnectionDegraded     = channel.ConnectionDegraded
	ConnectionStopped      = channel.ConnectionStopped
)

type ConnectionSnapshot = channel.ConnectionStatus

type MessageConsumer interface {
	Accept(context.Context, channel.IncomingMessage) error
}

type websocketClient interface {
	Start(context.Context) error
	Close()
}

type LongConnection struct {
	mu        sync.RWMutex
	snapshot  ConnectionSnapshot
	now       func() time.Time
	lark      *Client
	consumer  MessageConsumer
	transport websocketClient
}

func NewLongConnection(
	appID string,
	appSecret string,
	larkClient *Client,
	consumer MessageConsumer,
) (*LongConnection, error) {
	if appID == "" || appSecret == "" || larkClient == nil || consumer == nil ||
		!larkClient.AgentChannelReady() {
		return nil, errors.New("configure Lark long connection: missing initialized dependency")
	}
	connection := &LongConnection{
		now:      func() time.Time { return time.Now().UTC() },
		lark:     larkClient,
		consumer: consumer,
	}
	connection.snapshot = ConnectionSnapshot{
		Enabled:          true,
		State:            ConnectionInitializing,
		LastTransitionAt: connection.now(),
	}
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(connection.handleMessage)
	connection.transport = larkws.NewClient(
		appID,
		appSecret,
		larkws.WithDomain(larksdk.LarkBaseUrl),
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithLogger(quietSDKLogger{}),
		larkws.WithOnReady(connection.onReady),
		larkws.WithOnError(connection.onError),
		larkws.WithOnReconnecting(connection.onReconnecting),
		larkws.WithOnReconnected(connection.onReconnected),
		larkws.WithOnDisconnected(connection.onDisconnected),
	)
	return connection, nil
}

func (c *LongConnection) Run(ctx context.Context) {
	c.transition(ConnectionConnecting, "", false, false)
	started := make(chan error, 1)
	go func() {
		started <- c.transport.Start(ctx)
	}()
	select {
	case <-ctx.Done():
		c.transport.Close()
		c.transition(ConnectionStopped, "", false, false)
	case err := <-started:
		if err != nil {
			c.transition(ConnectionDegraded, "provider_connection", false, false)
			slog.Error("Lark Agent long connection stopped",
				"error_category", "provider_connection",
				"error", err)
			return
		}
		c.transition(ConnectionDegraded, "unexpected_stop", false, false)
		slog.Error("Lark Agent long connection stopped",
			"error_category", "unexpected_stop")
	}
}

func (c *LongConnection) Snapshot() ConnectionSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *LongConnection) handleMessage(
	ctx context.Context,
	event *larkim.P2MessageReceiveV1,
) error {
	incoming, err := c.lark.NormalizeMessageEvent(event)
	if errors.Is(err, channel.ErrUnsupportedMessage) {
		messageType := ""
		if event != nil && event.Event != nil && event.Event.Message != nil {
			messageType = stringValue(event.Event.Message.MessageType)
		}
		slog.Info("Lark Agent message ignored",
			"reason", "unsupported_message_type",
			"message_type", messageType)
		return nil
	}
	if err != nil {
		slog.Warn("Lark Agent message rejected",
			"error_category", "invalid_event")
		return nil
	}
	conversationName, err := c.lark.conversationName(
		ctx,
		incoming.TenantID,
		incoming.ConversationID,
	)
	if err != nil {
		slog.Warn("Lark Agent conversation metadata unavailable",
			"conversation_id", incoming.ConversationID,
			"error_category", "provider_metadata",
			"error", err)
	} else {
		incoming.ConversationName = conversationName
	}
	if err := c.consumer.Accept(ctx, incoming); err != nil {
		slog.Error("Lark Agent message persistence failed",
			"event_id", incoming.EventID,
			"error_category", "ingress",
			"error", err)
		return fmt.Errorf("accept Lark Agent message: %w", err)
	}
	return nil
}

func (c *LongConnection) onReady() {
	c.transition(ConnectionReady, "", true, false)
	slog.Info("Lark Agent long connection ready")
}

func (c *LongConnection) onError(err error) {
	c.transition(ConnectionDegraded, "provider_connection", false, false)
	slog.Error("Lark Agent long connection error",
		"error_category", "provider_connection",
		"error", err)
}

func (c *LongConnection) onReconnecting() {
	c.transition(ConnectionReconnecting, "", false, true)
	slog.Warn("Lark Agent long connection reconnecting")
}

func (c *LongConnection) onReconnected() {
	c.transition(ConnectionReady, "", true, false)
	slog.Info("Lark Agent long connection reconnected")
}

func (c *LongConnection) onDisconnected() {
	c.transition(ConnectionDegraded, "disconnected", false, false)
	slog.Warn("Lark Agent long connection disconnected")
}

func (c *LongConnection) transition(
	state channel.ConnectionState,
	errorCategory string,
	connected bool,
	reconnecting bool,
) {
	now := c.now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.State = state
	c.snapshot.LastTransitionAt = now
	c.snapshot.LastErrorCategory = errorCategory
	if connected {
		connectedAt := now
		c.snapshot.LastConnectedAt = &connectedAt
	}
	if reconnecting {
		c.snapshot.ReconnectCount++
	}
}

// quietSDKLogger prevents the SDK from logging connection URLs or event
// payloads. Lifecycle callbacks above provide the operational diagnostics.
type quietSDKLogger struct{}

func (quietSDKLogger) Debug(context.Context, ...interface{}) {}
func (quietSDKLogger) Info(context.Context, ...interface{})  {}
func (quietSDKLogger) Warn(context.Context, ...interface{})  {}
func (quietSDKLogger) Error(context.Context, ...interface{}) {}

var _ larkcore.Logger = quietSDKLogger{}
var _ channel.StatusProvider = (*LongConnection)(nil)
