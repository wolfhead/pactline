// Package ingress accepts provider-neutral messages and persists Agent runs.
package ingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

const acknowledgementTimeout = 5 * time.Second

type IdentityResolver interface {
	FindExternalIdentity(
		context.Context,
		identity.PrincipalKey,
	) (identity.ExternalIdentity, domain.User, error)
}

type RunRepository interface {
	CreateRunWithInput(
		context.Context,
		pactagent.Run,
		pactagent.RunInput,
		time.Time,
	) (pactagent.Run, bool, error)
	FindWaitingRunByClarification(
		context.Context,
		string,
		string,
		string,
		string,
	) (pactagent.Run, error)
	ResumeWaitingRunWithInput(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		[]byte,
		time.Time,
	) error
}

type ConversationObserver interface {
	ObserveConfiguration(
		context.Context,
		string,
		string,
		string,
		uuid.UUID,
		time.Time,
	) (pactagent.ConversationConfiguration, error)
}

type Config struct {
	Identities    IdentityResolver
	Runs          RunRepository
	Conversations ConversationObserver
	Inputs        *pactagent.InputCipher
	Acknowledgers map[string]channel.Acknowledger
	Model         string
	PromptVersion string
	Now           func() time.Time
}

type Service struct {
	identities    IdentityResolver
	runs          RunRepository
	conversations ConversationObserver
	inputs        *pactagent.InputCipher
	acknowledgers map[string]channel.Acknowledger
	model         string
	promptVersion string
	now           func() time.Time
}

func New(config Config) (*Service, error) {
	if config.Identities == nil || config.Runs == nil || config.Conversations == nil || config.Inputs == nil ||
		strings.TrimSpace(config.Model) == "" ||
		strings.TrimSpace(config.PromptVersion) == "" {
		return nil, errors.New("configure Agent ingress: missing dependency")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{
		identities:    config.Identities,
		runs:          config.Runs,
		conversations: config.Conversations,
		inputs:        config.Inputs,
		acknowledgers: config.Acknowledgers,
		model:         strings.TrimSpace(config.Model),
		promptVersion: strings.TrimSpace(config.PromptVersion),
		now:           config.Now,
	}, nil
}

func (s *Service) Accept(ctx context.Context, incoming channel.IncomingMessage) error {
	if incoming.Provider == "" || incoming.TenantID == "" ||
		incoming.EventID == "" || incoming.ConversationID == "" ||
		incoming.MessageID == "" || incoming.SenderSubjectID == "" ||
		incoming.CreatedAt.IsZero() {
		return channel.ErrInvalidEvent
	}
	_, user, err := s.identities.FindExternalIdentity(ctx, identity.PrincipalKey{
		Provider: incoming.Provider, TenantID: incoming.TenantID,
		SubjectID: incoming.SenderSubjectID,
	})
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !user.Active) {
		slog.Warn("Agent message sender rejected",
			"provider", incoming.Provider,
			"tenant_id", incoming.TenantID,
			"event_id", incoming.EventID,
			"error_category", "identity")
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve Agent message sender: %w", err)
	}
	configuration, err := s.conversations.ObserveConfiguration(
		ctx,
		incoming.Provider,
		incoming.TenantID,
		incoming.ConversationID,
		user.ID,
		s.now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("observe Agent conversation: %w", err)
	}
	resumed, err := s.tryResume(ctx, incoming, user.ID)
	if err != nil || resumed {
		return err
	}
	if !incoming.BotMentioned || strings.TrimSpace(incoming.Text) == "" {
		return nil
	}
	if !configuration.Enabled {
		slog.Info("Agent command ignored for disabled conversation",
			"provider", incoming.Provider,
			"conversation_id", incoming.ConversationID)
		return nil
	}
	commandKind := ClassifyCommand(incoming.Text)
	now := s.now().UTC()
	run, err := pactagent.NewRun(pactagent.NewRunInput{
		Provider:               incoming.Provider,
		TenantID:               incoming.TenantID,
		ConversationID:         incoming.ConversationID,
		TriggerMessageID:       incoming.MessageID,
		ProviderEventID:        incoming.EventID,
		ThreadRootMessageID:    incoming.ThreadRootMessageID,
		ReplyParentMessageID:   incoming.ReplyParentMessageID,
		ConversationRevisionID: &configuration.RevisionID,
		TriggerOccurredAt:      incoming.CreatedAt,
		InitiatingUserID:       user.ID,
		InitiatingSubjectID:    incoming.SenderSubjectID,
		CommandKind:            commandKind,
		Model:                  s.model,
		PromptVersion:          s.promptVersion,
	}, now)
	if err != nil {
		return fmt.Errorf("normalize Agent command: %w", err)
	}
	commandEnvelope, err := pactagent.EncodeCommandEnvelope(incoming.Text, incoming.Artifacts)
	if err != nil {
		return fmt.Errorf("encode Agent command: %w", err)
	}
	ciphertext, err := s.inputs.Encrypt(run.ID, "command", commandEnvelope)
	if err != nil {
		return fmt.Errorf("encrypt Agent command: %w", err)
	}
	stored, created, err := s.runs.CreateRunWithInput(ctx, run, pactagent.RunInput{
		RunID:             run.ID,
		EncryptionKeyID:   s.inputs.KeyID(),
		CommandCiphertext: ciphertext,
	}, now)
	if err != nil {
		return fmt.Errorf("persist Agent run and input: %w", err)
	}
	if !created {
		slog.Info("Agent command deduplicated",
			"run_id", stored.ID,
			"provider", stored.Provider,
			"event_id", stored.ProviderEventID)
		return nil
	}
	slog.Info("Agent command accepted",
		"run_id", stored.ID,
		"provider", stored.Provider,
		"event_id", stored.ProviderEventID)
	if acknowledger := s.acknowledgers[stored.Provider]; acknowledger != nil {
		go s.acknowledge(context.WithoutCancel(ctx), acknowledger, stored)
	}
	return nil
}

func (s *Service) acknowledge(
	parent context.Context,
	acknowledger channel.Acknowledger,
	run pactagent.Run,
) {
	ctx, cancel := context.WithTimeout(parent, acknowledgementTimeout)
	defer cancel()
	err := acknowledger.Acknowledge(ctx, channel.AcknowledgeRequest{
		TenantID:        run.TenantID,
		TargetMessageID: run.TriggerMessageID,
	})
	if err != nil {
		slog.Warn("Agent command acknowledgement failed",
			"run_id", run.ID,
			"provider", run.Provider,
			"error_category", "provider",
			"error", err)
		return
	}
	slog.Info("Agent command acknowledged",
		"run_id", run.ID,
		"provider", run.Provider)
}

func (s *Service) tryResume(
	ctx context.Context,
	incoming channel.IncomingMessage,
	userID uuid.UUID,
) (bool, error) {
	if incoming.ReplyParentMessageID == "" {
		return false, nil
	}
	run, err := s.runs.FindWaitingRunByClarification(
		ctx,
		incoming.Provider,
		incoming.TenantID,
		incoming.ConversationID,
		incoming.ReplyParentMessageID,
	)
	if errors.Is(err, pactagent.ErrAgentRunNotFound) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("find waiting Agent run: %w", err)
	}
	if run.InitiatingUserID != userID {
		slog.Warn("Agent clarification sender rejected",
			"run_id", run.ID,
			"event_id", incoming.EventID,
			"error_category", "initiating_user_mismatch")
		return true, nil
	}
	answer := strings.TrimSpace(incoming.Text)
	if answer == "" {
		return true, nil
	}
	ciphertext, err := s.inputs.Encrypt(run.ID, "clarification", []byte(answer))
	if err != nil {
		return true, fmt.Errorf("encrypt Agent clarification: %w", err)
	}
	if err := s.runs.ResumeWaitingRunWithInput(
		ctx,
		run.ID,
		userID,
		incoming.ReplyParentMessageID,
		ciphertext,
		s.now().UTC(),
	); err != nil {
		if errors.Is(err, pactagent.ErrRunNotWaiting) ||
			errors.Is(err, pactagent.ErrClarificationExpired) ||
			errors.Is(err, pactagent.ErrClarificationUserMismatch) {
			slog.Warn("resume Agent run rejected",
				"run_id", run.ID,
				"event_id", incoming.EventID,
				"error_category", "resume_state")
			return true, nil
		}
		return true, fmt.Errorf("resume Agent run: %w", err)
	}
	slog.Info("Agent clarification accepted",
		"run_id", run.ID,
		"event_id", incoming.EventID)
	return true, nil
}

// ClassifyCommand applies the production trigger-language policy. The
// evaluation harness calls the same function for explicit-mention scenarios.
func ClassifyCommand(command string) pactagent.CommandKind {
	normalized := strings.ToLower(command)
	for _, marker := range []string{
		"以上讨论", "上述讨论", "前面的讨论", "above discussion", "previous discussion",
	} {
		if strings.Contains(normalized, marker) {
			return pactagent.CommandDiscussion
		}
	}
	return pactagent.CommandDirect
}
