package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxAgentConversationContextRunes = 4000

// AgentConversation is the durable, provider-neutral configuration for one
// external conversation in which Pactline's first-party Agent participates.
// Provider message history is not retained here.
type AgentConversation struct {
	ID               uuid.UUID
	Provider         string
	TenantID         string
	ExternalID       string
	Name             string
	Enabled          bool
	BindingActive    bool
	DefaultProjectID *uuid.UUID
	BusinessContext  string
	Version          int64
	CreatedBy        uuid.UUID
	UpdatedBy        uuid.UUID
	LastSeenAt       time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AgentConversationRevision struct {
	ID               uuid.UUID
	ConversationID   uuid.UUID
	Version          int64
	Enabled          bool
	BindingActive    bool
	DefaultProjectID *uuid.UUID
	BusinessContext  string
	UpdatedBy        uuid.UUID
	CreatedAt        time.Time
}

func NormalizeAgentConversationContext(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > MaxAgentConversationContextRunes {
		return "", fmt.Errorf(
			"%w: Agent conversation business context exceeds %d characters",
			ErrInvalidInput,
			MaxAgentConversationContextRunes,
		)
	}
	return value, nil
}

func (conversation AgentConversation) Validate() error {
	if conversation.ID == uuid.Nil ||
		strings.TrimSpace(conversation.Provider) == "" ||
		strings.TrimSpace(conversation.TenantID) == "" ||
		strings.TrimSpace(conversation.ExternalID) == "" ||
		conversation.Version < 1 ||
		conversation.CreatedBy == uuid.Nil ||
		conversation.UpdatedBy == uuid.Nil ||
		conversation.LastSeenAt.IsZero() ||
		conversation.CreatedAt.IsZero() ||
		conversation.UpdatedAt.IsZero() {
		return ErrInvalidInput
	}
	if conversation.Provider != "lark" {
		return fmt.Errorf("%w: unsupported Agent conversation provider", ErrInvalidInput)
	}
	if conversation.BindingActive && conversation.DefaultProjectID == nil {
		return fmt.Errorf("%w: active Agent conversation binding requires a Project", ErrInvalidInput)
	}
	if conversation.DefaultProjectID != nil && *conversation.DefaultProjectID == uuid.Nil {
		return ErrInvalidInput
	}
	_, err := NormalizeAgentConversationContext(conversation.BusinessContext)
	return err
}
