package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAgentConversationRequiresProjectForActiveBinding(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	conversation := AgentConversation{
		ID: uuid.New(), Provider: "lark", TenantID: "tenant", ExternalID: "chat",
		Enabled: true, BindingActive: true, Version: 1,
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
		LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}

	require.ErrorIs(t, conversation.Validate(), ErrInvalidInput)

	projectID := uuid.New()
	conversation.DefaultProjectID = &projectID
	require.NoError(t, conversation.Validate())
}

func TestNormalizeAgentConversationContextCountsRunes(t *testing.T) {
	value := "  " + strings.Repeat("群", MaxAgentConversationContextRunes) + "  "
	normalized, err := NormalizeAgentConversationContext(value)
	require.NoError(t, err)
	require.Len(t, []rune(normalized), MaxAgentConversationContextRunes)

	_, err = NormalizeAgentConversationContext(normalized + "群")
	require.ErrorIs(t, err, ErrInvalidInput)
}
