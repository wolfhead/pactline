package ingress

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type identityStub struct {
	user domain.User
}

type conversationObserverStub struct {
	configuration pactagent.ConversationConfiguration
	observedName  *string
}

func (s conversationObserverStub) ObserveConfiguration(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	name string,
	_ uuid.UUID,
	_ time.Time,
) (pactagent.ConversationConfiguration, error) {
	if s.observedName != nil {
		*s.observedName = name
	}
	if s.configuration.RevisionID == uuid.Nil {
		s.configuration.RevisionID = uuid.New()
		s.configuration.Enabled = true
	}
	return s.configuration, nil
}

func (s identityStub) FindExternalIdentity(
	context.Context,
	identity.PrincipalKey,
) (identity.ExternalIdentity, domain.User, error) {
	return identity.ExternalIdentity{UserID: s.user.ID}, s.user, nil
}

type repositoryStub struct {
	run       pactagent.Run
	input     pactagent.RunInput
	createN   int
	saveN     int
	createErr error
	resumeErr error
}

type acknowledgerStub struct {
	mu       sync.Mutex
	requests []channel.AcknowledgeRequest
	called   chan struct{}
	err      error
}

func (s *acknowledgerStub) Acknowledge(
	_ context.Context,
	request channel.AcknowledgeRequest,
) error {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()
	if s.called != nil {
		select {
		case s.called <- struct{}{}:
		default:
		}
	}
	return s.err
}

func (s *repositoryStub) CreateRunWithInput(
	_ context.Context,
	run pactagent.Run,
	input pactagent.RunInput,
	_ time.Time,
) (pactagent.Run, bool, error) {
	s.createN++
	if s.createErr != nil {
		err := s.createErr
		s.createErr = nil
		return pactagent.Run{}, false, err
	}
	if s.run.ID != uuid.Nil {
		return s.run, false, nil
	}
	s.run = run
	s.input = input
	s.saveN++
	return run, true, nil
}

func (s *repositoryStub) FindWaitingRunByClarification(
	context.Context,
	string,
	string,
	string,
	string,
) (pactagent.Run, error) {
	if s.run.Status != pactagent.RunWaitingUser {
		return pactagent.Run{}, pactagent.ErrAgentRunNotFound
	}
	return s.run, nil
}

func (s *repositoryStub) ResumeWaitingRunWithInput(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
	[]byte,
	time.Time,
) error {
	return s.resumeErr
}

func TestServiceQueuesMentionOnceAndEncryptsCommand(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &repositoryStub{}
	observedConversationName := ""
	acknowledger := &acknowledgerStub{called: make(chan struct{}, 1)}
	cipher, err := pactagent.NewInputCipher(
		"input-key",
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	service, err := New(Config{
		Identities: identityStub{
			user: domain.User{ID: userID, Name: "User", Active: true, AccessStatus: domain.AccessStatusApproved},
		},
		Runs:          repository,
		Conversations: conversationObserverStub{observedName: &observedConversationName},
		Inputs:        cipher,
		Acknowledgers: map[string]channel.Acknowledger{"lark": acknowledger},
		Model:         "deepseek-v4-pro",
		PromptVersion: "first-party-task-v1",
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)
	incoming := channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-1",
		ConversationID: "chat-1", MessageID: "message-1",
		ConversationName: "Project Alpha",
		SenderSubjectID:  "ou_user", MessageType: "text",
		Text: "帮我创建一个 Task", CreatedAt: now, BotMentioned: true,
		Artifacts: []artifact.Reference{{
			ID: "trigger-image", Kind: artifact.KindImage, Name: "report.png",
			Availability: artifact.AvailabilityAvailable,
		}},
	}

	require.NoError(t, service.Accept(context.Background(), incoming))
	require.NoError(t, service.Accept(context.Background(), incoming))
	select {
	case <-acknowledger.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Agent acknowledgement")
	}

	require.Equal(t, 2, repository.createN)
	require.Equal(t, 1, repository.saveN)
	require.Equal(t, "Project Alpha", observedConversationName)
	require.Equal(t, repository.run.ID, repository.input.RunID)
	plaintext, err := cipher.Decrypt(
		repository.run.ID,
		"command",
		repository.input.CommandCiphertext,
	)
	require.NoError(t, err)
	envelope, err := pactagent.DecodeCommandEnvelope(plaintext)
	require.NoError(t, err)
	require.Equal(t, "帮我创建一个 Task", envelope.Text)
	require.Equal(t, incoming.Artifacts, envelope.Artifacts)
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	require.Equal(t, []channel.AcknowledgeRequest{{
		TenantID:        "tenant",
		TargetMessageID: "message-1",
	}}, acknowledger.requests)
}

func TestServiceRejectsPendingUserBeforeStartingRun(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	repository := &repositoryStub{}
	cipher, err := pactagent.NewInputCipher(
		"input-key",
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	service, err := New(Config{
		Identities: identityStub{user: domain.User{
			ID:           uuid.New(),
			Name:         "Pending User",
			Active:       true,
			AccessStatus: domain.AccessStatusPending,
		}},
		Runs:          repository,
		Conversations: conversationObserverStub{},
		Inputs:        cipher,
		Model:         "deepseek-v4-pro",
		PromptVersion: "first-party-work-v12",
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)

	require.NoError(t, service.Accept(context.Background(), channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-pending",
		ConversationID: "chat-pending", MessageID: "message-pending",
		SenderSubjectID: "ou_pending", MessageType: "text",
		Text: "帮我创建任务", CreatedAt: now, BotMentioned: true,
	}))
	require.Zero(t, repository.createN)
}

func TestServiceDoesNotStartRunForDisabledConversation(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &repositoryStub{}
	cipher, err := pactagent.NewInputCipher(
		"input-key",
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	service, err := New(Config{
		Identities: identityStub{user: domain.User{ID: userID, Name: "User", Active: true, AccessStatus: domain.AccessStatusApproved}},
		Runs:       repository,
		Conversations: conversationObserverStub{configuration: pactagent.ConversationConfiguration{
			RevisionID: uuid.New(), Enabled: false,
		}},
		Inputs: cipher, Model: "deepseek-v4-pro", PromptVersion: "first-party-work-v12",
		Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	require.NoError(t, service.Accept(context.Background(), channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-disabled",
		ConversationID: "chat-disabled", MessageID: "message-disabled",
		SenderSubjectID: "ou_user", MessageType: "text",
		Text: "重新启用本群 Agent", CreatedAt: now, BotMentioned: true,
	}))
	require.Zero(t, repository.createN)
}

func TestServiceReturnsPersistenceFailureForLarkRetry(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	expiresAt := now.Add(time.Hour)
	repository := &repositoryStub{
		run: pactagent.Run{
			ID: userID, Provider: "lark", TenantID: "tenant",
			ConversationID: "chat-1", TriggerMessageID: "trigger-1",
			ProviderEventID: "event-original", TriggerOccurredAt: now,
			InitiatingUserID: userID, InitiatingSubjectID: "ou_user",
			Status: pactagent.RunWaitingUser, CommandKind: pactagent.CommandDirect,
			Model: "deepseek-v4-pro", PromptVersion: "first-party-task-v1",
			ClarificationRounds: 1, ClarificationMessageID: "reply-1",
			ClarificationInterruptID: "interrupt-1",
			ClarificationExpiresAt:   &expiresAt,
			AvailableAt:              now, CreatedAt: now, UpdatedAt: now,
		},
		resumeErr: errors.New("database unavailable"),
	}
	cipher, err := pactagent.NewInputCipher(
		"input-key",
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	service, err := New(Config{
		Identities: identityStub{
			user: domain.User{ID: userID, Name: "User", Active: true, AccessStatus: domain.AccessStatusApproved},
		},
		Runs: repository, Conversations: conversationObserverStub{}, Inputs: cipher, Model: "deepseek-v4-pro",
		PromptVersion: "first-party-task-v1",
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)

	err = service.Accept(context.Background(), channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-reply",
		ConversationID: "chat-1", MessageID: "message-reply",
		ReplyParentMessageID: "reply-1", SenderSubjectID: "ou_user",
		MessageType: "text", Text: "只做登录问题", CreatedAt: now,
	})

	require.ErrorContains(t, err, "resume Agent run")
}

func TestServiceRetriesAtomicRunAndInputPersistence(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &repositoryStub{createErr: errors.New("database unavailable")}
	cipher, err := pactagent.NewInputCipher(
		"input-key",
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	service, err := New(Config{
		Identities: identityStub{
			user: domain.User{ID: userID, Name: "User", Active: true, AccessStatus: domain.AccessStatusApproved},
		},
		Runs: repository, Conversations: conversationObserverStub{}, Inputs: cipher, Model: "deepseek-v4-pro",
		PromptVersion: "first-party-task-v1",
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)
	incoming := channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-1",
		ConversationID: "chat-1", MessageID: "message-1",
		SenderSubjectID: "ou_user", MessageType: "text",
		Text: "create a Task", CreatedAt: now, BotMentioned: true,
	}

	require.ErrorContains(
		t,
		service.Accept(context.Background(), incoming),
		"persist Agent run and input",
	)
	require.NoError(t, service.Accept(context.Background(), incoming))

	require.Equal(t, 2, repository.createN)
	require.Equal(t, 1, repository.saveN)
	require.NotEqual(t, uuid.Nil, repository.run.ID)
	require.Equal(t, repository.run.ID, repository.input.RunID)
}

func TestClassifyCommandLeavesConversationConfigurationIntentToModel(t *testing.T) {
	require.Equal(t, pactagent.CommandDirect, ClassifyCommand("查看本群配置"))
	require.Equal(t, pactagent.CommandDirect, ClassifyCommand("设置本群默认项目到 策略与模型"))
	require.Equal(t, pactagent.CommandDiscussion, ClassifyCommand("请根据以上讨论创建任务"))
	require.Equal(t, pactagent.CommandDirect, ClassifyCommand("帮我创建任务"))
}
