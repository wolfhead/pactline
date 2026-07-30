package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type agentCallbackStub struct {
	incoming channel.IncomingMessage
}

func (s *agentCallbackStub) VerifyCallback(http.Header, []byte) error { return nil }
func (s *agentCallbackStub) CallbackChallenge([]byte) (string, bool, error) {
	return "", false, nil
}
func (s *agentCallbackStub) NormalizeEvent(context.Context, []byte) (channel.IncomingMessage, error) {
	return s.incoming, nil
}
func (*agentCallbackStub) FetchContext(context.Context, channel.ContextRequest) ([]channel.ChannelMessage, error) {
	return nil, nil
}
func (*agentCallbackStub) Reply(context.Context, channel.ReplyRequest) (channel.ProviderMessageID, error) {
	return "", nil
}

type agentIdentityStub struct {
	user domain.User
}

func (s agentIdentityStub) FindExternalIdentity(
	context.Context,
	identity.PrincipalKey,
) (identity.ExternalIdentity, domain.User, error) {
	return identity.ExternalIdentity{UserID: s.user.ID}, s.user, nil
}

type agentIngressStoreStub struct {
	run       pactagent.Run
	input     pactagent.RunInput
	createN   int
	resumeErr error
}

func (s *agentIngressStoreStub) CreateRun(
	_ context.Context, run pactagent.Run,
) (pactagent.Run, bool, error) {
	s.createN++
	if s.run.ID != uuid.Nil {
		return s.run, false, nil
	}
	s.run = run
	return run, true, nil
}
func (s *agentIngressStoreStub) SaveRunInput(
	_ context.Context, input pactagent.RunInput, _ time.Time,
) error {
	s.input = input
	return nil
}
func (s *agentIngressStoreStub) FindWaitingRunByClarification(
	context.Context, string, string, string, string,
) (pactagent.Run, error) {
	if s.run.Status != pactagent.RunWaitingUser {
		return pactagent.Run{}, pactagent.ErrAgentRunNotFound
	}
	return s.run, nil
}
func (s *agentIngressStoreStub) ResumeWaitingRunWithInput(
	context.Context, uuid.UUID, uuid.UUID, string, []byte, time.Time,
) error {
	return s.resumeErr
}

func TestAgentChannelHandlerQueuesMentionOnceAndEncryptsCommand(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	adapter := &agentCallbackStub{incoming: channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-1",
		ConversationID: "chat-1", MessageID: "message-1",
		SenderSubjectID: "ou_user", MessageType: "text",
		Text: "帮我创建一个 Task", CreatedAt: now, BotMentioned: true,
	}}
	repository := &agentIngressStoreStub{}
	cipher, err := pactagent.NewInputCipher(
		"input-key", []byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	handler, err := NewAgentChannelHandler(
		adapter,
		agentIdentityStub{user: domain.User{ID: userID, Name: "User", Active: true}},
		repository,
		cipher,
		"deepseek-v4-pro",
		func() time.Time { return now },
	)
	require.NoError(t, err)
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/api/integrations/lark/events", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusNoContent, response.Code)
	}
	require.Equal(t, 2, repository.createN)
	require.Equal(t, repository.run.ID, repository.input.RunID)
	plaintext, err := cipher.Decrypt(
		repository.run.ID, "command", repository.input.CommandCiphertext,
	)
	require.NoError(t, err)
	require.Equal(t, "帮我创建一个 Task", string(plaintext))
}

func TestAgentChannelHandlerReturnsRetryableStatusOnResumePersistenceFailure(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	expiresAt := now.Add(time.Hour)
	repository := &agentIngressStoreStub{
		run: pactagent.Run{
			ID: userID, Provider: "lark", TenantID: "tenant",
			ConversationID: "chat-1", TriggerMessageID: "trigger-1",
			ProviderEventID: "event-original", TriggerOccurredAt: now,
			InitiatingUserID: userID, InitiatingSubjectID: "ou_user",
			Status: pactagent.RunWaitingUser, CommandKind: pactagent.CommandDirect,
			Model: "deepseek-v4-pro", PromptVersion: "first-party-task-v1",
			ClarificationRounds: 1, ClarificationMessageID: "reply-1",
			ClarificationInterruptID: "interrupt-1", ClarificationExpiresAt: &expiresAt,
			AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		},
		resumeErr: errors.New("database unavailable"),
	}
	adapter := &agentCallbackStub{incoming: channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-reply",
		ConversationID: "chat-1", MessageID: "message-reply",
		ReplyParentMessageID: "reply-1", SenderSubjectID: "ou_user",
		MessageType: "text", Text: "只做登录问题", CreatedAt: now,
	}}
	cipher, err := pactagent.NewInputCipher(
		"input-key", []byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	handler, err := NewAgentChannelHandler(
		adapter,
		agentIdentityStub{user: domain.User{ID: userID, Name: "User", Active: true}},
		repository,
		cipher,
		"deepseek-v4-pro",
		func() time.Time { return now },
	)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/api/integrations/lark/events", nil),
	)
	require.Equal(t, http.StatusInternalServerError, response.Code)
}
