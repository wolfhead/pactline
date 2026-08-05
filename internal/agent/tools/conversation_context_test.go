package tools

import (
	"context"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/channel"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetConversationContextPreservesProviderCursorAfterUnsupportedMessagesAreFiltered(t *testing.T) {
	now := time.Now().UTC()
	run := pactagent.Run{
		ID: uuid.New(), TenantID: "tenant", ConversationID: "chat",
		TriggerMessageID: "trigger", TriggerOccurredAt: now,
	}
	repository := &contextRepositoryStub{}
	result, err := getConversationContext(
		context.Background(),
		Config{
			Run: run, WorkerID: "worker", Now: func() time.Time { return now },
			Channel: &contextChannelStub{messages: []channel.ChannelMessage{{
				MessageID: "message", Cursor: "provider-next-page",
				Text: "Relevant message", CreatedAt: now.Add(-time.Minute),
			}}},
			Repository: repository,
		},
		GetConversationContextInput{PageSize: 20},
	)

	require.NoError(t, err)
	require.Equal(t, "provider-next-page", result.NextCursor)
	require.Equal(t, 1, result.Used)
	require.Equal(t, 1, repository.added)
}

type contextChannelStub struct {
	messages []channel.ChannelMessage
}

func (s *contextChannelStub) FetchContext(
	context.Context,
	channel.ContextRequest,
) ([]channel.ChannelMessage, error) {
	return s.messages, nil
}

func (*contextChannelStub) Reply(context.Context, channel.ReplyRequest) (channel.ProviderMessageID, error) {
	panic("unexpected Reply call")
}

type contextRepositoryStub struct {
	added int
}

func (*contextRepositoryStub) GetRun(context.Context, uuid.UUID) (pactagent.Run, error) {
	panic("unexpected GetRun call")
}

func (*contextRepositoryStub) GetCompletedToolCall(context.Context, uuid.UUID, string) (pactagent.ToolCall, error) {
	panic("unexpected GetCompletedToolCall call")
}

func (s *contextRepositoryStub) AddContextMessages(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	count int,
	_ time.Time,
) (int, error) {
	s.added += count
	return s.added, nil
}

func (*contextRepositoryStub) AttachTask(context.Context, uuid.UUID, string, uuid.UUID, int64, time.Time) (uuid.UUID, int64, bool, error) {
	panic("unexpected AttachTask call")
}
