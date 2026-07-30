package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/channel"
	agentruntime "github.com/wolfhead/pactline/internal/agent/runtime"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/google/uuid"
)

const maxAgentCallbackBytes = 2 << 20

type AgentCallbackAdapter interface {
	channel.ChannelAdapter
	VerifyCallback(http.Header, []byte) error
	CallbackChallenge([]byte) (string, bool, error)
}

type agentIdentityResolver interface {
	FindExternalIdentity(
		context.Context,
		identity.PrincipalKey,
	) (identity.ExternalIdentity, domain.User, error)
}

type agentIngressRepository interface {
	CreateRun(context.Context, pactagent.Run) (pactagent.Run, bool, error)
	SaveRunInput(context.Context, pactagent.RunInput, time.Time) error
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

type AgentChannelHandler struct {
	adapter    AgentCallbackAdapter
	identities agentIdentityResolver
	runs       agentIngressRepository
	inputs     *pactagent.InputCipher
	model      string
	now        func() time.Time
}

func NewAgentChannelHandler(
	adapter AgentCallbackAdapter,
	identities agentIdentityResolver,
	runs agentIngressRepository,
	inputs *pactagent.InputCipher,
	model string,
	now func() time.Time,
) (*AgentChannelHandler, error) {
	if adapter == nil || identities == nil || runs == nil || inputs == nil ||
		strings.TrimSpace(model) == "" {
		return nil, errors.New("configure Agent channel handler: missing dependency")
	}
	if now == nil {
		now = time.Now
	}
	return &AgentChannelHandler{
		adapter: adapter, identities: identities, runs: runs,
		inputs: inputs, model: strings.TrimSpace(model), now: now,
	}, nil
}

func (h *AgentChannelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAgentCallbackBytes))
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid callback"})
		return
	}
	if err := h.adapter.VerifyCallback(r.Header, body); err != nil {
		slog.Warn("Agent callback verification rejected", "error_category", "verification")
		WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "callback verification failed"})
		return
	}
	if challenge, ok, challengeErr := h.adapter.CallbackChallenge(body); ok {
		if challengeErr != nil {
			WriteJSON(w, http.StatusUnauthorized, ErrorBody{Error: "callback verification failed"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
		return
	}
	incoming, err := h.adapter.NormalizeEvent(r.Context(), body)
	if errors.Is(err, channel.ErrUnsupportedMessage) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		slog.Warn("Agent callback normalization rejected", "error_category", "invalid_event")
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid callback event"})
		return
	}
	_, user, err := h.identities.FindExternalIdentity(r.Context(), identity.PrincipalKey{
		Provider: incoming.Provider, TenantID: incoming.TenantID,
		SubjectID: incoming.SenderSubjectID,
	})
	if err != nil || !user.Active {
		slog.Warn("Agent callback sender rejected",
			"provider", incoming.Provider, "tenant_id", incoming.TenantID,
			"event_id", incoming.EventID, "error_category", "identity")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	resumed, err := h.tryResume(r.Context(), incoming, user.ID)
	if err != nil {
		slog.Error("process Agent clarification failed",
			"event_id", incoming.EventID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "could not queue Agent clarification"})
		return
	}
	if resumed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !incoming.BotMentioned {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if strings.TrimSpace(incoming.Text) == "" {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "Agent command is empty"})
		return
	}
	now := h.now().UTC()
	run, err := pactagent.NewRun(pactagent.NewRunInput{
		Provider:             incoming.Provider,
		TenantID:             incoming.TenantID,
		ConversationID:       incoming.ConversationID,
		TriggerMessageID:     incoming.MessageID,
		ProviderEventID:      incoming.EventID,
		ThreadRootMessageID:  incoming.ThreadRootMessageID,
		ReplyParentMessageID: incoming.ReplyParentMessageID,
		TriggerOccurredAt:    incoming.CreatedAt,
		InitiatingUserID:     user.ID,
		InitiatingSubjectID:  incoming.SenderSubjectID,
		CommandKind:          classifyAgentCommand(incoming.Text),
		Model:                h.model,
		PromptVersion:        agentruntime.PromptVersion,
	}, now)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, ErrorBody{Error: "invalid Agent command"})
		return
	}
	stored, created, err := h.runs.CreateRun(r.Context(), run)
	if err != nil {
		slog.Error("persist Agent run failed", "event_id", incoming.EventID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "could not queue Agent command"})
		return
	}
	ciphertext, err := h.inputs.Encrypt(stored.ID, "command", []byte(incoming.Text))
	if err != nil {
		slog.Error("encrypt Agent command failed", "run_id", stored.ID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "could not queue Agent command"})
		return
	}
	if err := h.runs.SaveRunInput(r.Context(), pactagent.RunInput{
		RunID:             stored.ID,
		EncryptionKeyID:   h.inputs.KeyID(),
		CommandCiphertext: ciphertext,
	}, now); err != nil {
		slog.Error("persist Agent command failed", "run_id", stored.ID, "error", err)
		WriteJSON(w, http.StatusInternalServerError, ErrorBody{Error: "could not queue Agent command"})
		return
	}
	slog.Info("Agent command accepted",
		"run_id", stored.ID, "provider", stored.Provider,
		"event_id", stored.ProviderEventID, "deduplicated", !created)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AgentChannelHandler) tryResume(
	ctx context.Context,
	incoming channel.IncomingMessage,
	userID uuid.UUID,
) (bool, error) {
	if incoming.ReplyParentMessageID == "" {
		return false, nil
	}
	run, err := h.runs.FindWaitingRunByClarification(
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
			"run_id", run.ID, "event_id", incoming.EventID,
			"error_category", "initiating_user_mismatch")
		return true, nil
	}
	answer := strings.TrimSpace(incoming.Text)
	if answer == "" {
		return true, nil
	}
	ciphertext, err := h.inputs.Encrypt(run.ID, "clarification", []byte(answer))
	if err != nil {
		return true, fmt.Errorf("encrypt Agent clarification: %w", err)
	}
	if err := h.runs.ResumeWaitingRunWithInput(
		ctx,
		run.ID,
		userID,
		incoming.ReplyParentMessageID,
		ciphertext,
		h.now().UTC(),
	); err != nil {
		if errors.Is(err, pactagent.ErrRunNotWaiting) ||
			errors.Is(err, pactagent.ErrClarificationExpired) ||
			errors.Is(err, pactagent.ErrClarificationUserMismatch) {
			slog.Warn("resume Agent run rejected",
				"run_id", run.ID, "event_id", incoming.EventID,
				"error_category", "resume_state")
			return true, nil
		}
		return true, fmt.Errorf("resume Agent run: %w", err)
	}
	slog.Info("Agent clarification accepted", "run_id", run.ID, "event_id", incoming.EventID)
	return true, nil
}

func classifyAgentCommand(command string) pactagent.CommandKind {
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

var _ http.Handler = (*AgentChannelHandler)(nil)
