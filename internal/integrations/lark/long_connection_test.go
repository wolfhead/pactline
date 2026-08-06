package lark

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/agent/channel"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"
)

type messageConsumerStub struct {
	message channel.IncomingMessage
	err     error
}

func (s *messageConsumerStub) Accept(
	_ context.Context,
	message channel.IncomingMessage,
) error {
	s.message = message
	return s.err
}

func TestLongConnectionTracksReconnectLifecycleWithoutExposingErrors(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	connection := &LongConnection{
		now: func() time.Time { return now },
		snapshot: ConnectionSnapshot{
			Enabled: true, State: ConnectionInitializing, LastTransitionAt: now,
		},
	}

	connection.onReady()
	ready := connection.Snapshot()
	require.Equal(t, ConnectionReady, ready.State)
	require.Equal(t, &now, ready.LastConnectedAt)
	require.Empty(t, ready.LastErrorCategory)

	now = now.Add(time.Minute)
	connection.onReconnecting()
	reconnecting := connection.Snapshot()
	require.Equal(t, ConnectionReconnecting, reconnecting.State)
	require.EqualValues(t, 1, reconnecting.ReconnectCount)

	now = now.Add(time.Minute)
	connection.onError(errors.New("credential details must stay internal"))
	degraded := connection.Snapshot()
	require.Equal(t, ConnectionDegraded, degraded.State)
	require.Equal(t, "provider_connection", degraded.LastErrorCategory)

	now = now.Add(time.Minute)
	connection.onReconnected()
	reconnected := connection.Snapshot()
	require.Equal(t, ConnectionReady, reconnected.State)
	require.Equal(t, &now, reconnected.LastConnectedAt)
	require.Empty(t, reconnected.LastErrorCategory)
}

func TestLongConnectionAcceptsMockLarkMessageWithoutRunningModelInline(t *testing.T) {
	server := newAgentChannelInitializationServer(t)
	defer server.Close()
	client := newAgentChannelTestClient(t, server.URL, server.Client())
	consumer := &messageConsumerStub{}
	connection := &LongConnection{lark: client, consumer: consumer}
	var event larkim.P2MessageReceiveV1
	require.NoError(t, json.Unmarshal([]byte(`{
		"schema":"2.0",
		"header":{
			"event_id":"event-1",
			"event_type":"im.message.receive_v1",
			"app_id":"app-id",
			"tenant_key":"tenant"
		},
		"event":{
			"sender":{"sender_id":{"open_id":"ou_user"},"tenant_key":"tenant"},
			"message":{
				"message_id":"message-1",
				"create_time":"1785398400000",
				"chat_id":"chat-1",
				"message_type":"text",
				"content":"{\"text\":\"@_user_1 create a Task\"}",
				"mentions":[{"key":"@_user_1","id":{"open_id":"ou_bot"},"name":"Pactline"}]
			}
		}
	}`), &event))

	require.NoError(t, connection.handleMessage(context.Background(), &event))
	require.Equal(t, "event-1", consumer.message.EventID)
	require.Equal(t, "create a Task", consumer.message.Text)
	require.Equal(t, "Project Alpha", consumer.message.ConversationName)
	require.True(t, consumer.message.BotMentioned)

	consumer.err = errors.New("database unavailable")
	require.ErrorContains(
		t,
		connection.handleMessage(context.Background(), &event),
		"accept Lark Agent message",
	)
}
