package ingress

import (
	"context"
	"errors"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type identityStub struct {
	user domain.User
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
	cipher, err := pactagent.NewInputCipher(
		"input-key",
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	service, err := New(Config{
		Identities: identityStub{
			user: domain.User{ID: userID, Name: "User", Active: true},
		},
		Runs:          repository,
		Inputs:        cipher,
		Model:         "deepseek-v4-pro",
		PromptVersion: "first-party-task-v1",
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)
	incoming := channel.IncomingMessage{
		Provider: "lark", TenantID: "tenant", EventID: "event-1",
		ConversationID: "chat-1", MessageID: "message-1",
		SenderSubjectID: "ou_user", MessageType: "text",
		Text: "帮我创建一个 Task", CreatedAt: now, BotMentioned: true,
	}

	require.NoError(t, service.Accept(context.Background(), incoming))
	require.NoError(t, service.Accept(context.Background(), incoming))

	require.Equal(t, 2, repository.createN)
	require.Equal(t, 1, repository.saveN)
	require.Equal(t, repository.run.ID, repository.input.RunID)
	plaintext, err := cipher.Decrypt(
		repository.run.ID,
		"command",
		repository.input.CommandCiphertext,
	)
	require.NoError(t, err)
	require.Equal(t, "帮我创建一个 Task", string(plaintext))
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
			user: domain.User{ID: userID, Name: "User", Active: true},
		},
		Runs: repository, Inputs: cipher, Model: "deepseek-v4-pro",
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
			user: domain.User{ID: userID, Name: "User", Active: true},
		},
		Runs: repository, Inputs: cipher, Model: "deepseek-v4-pro",
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
