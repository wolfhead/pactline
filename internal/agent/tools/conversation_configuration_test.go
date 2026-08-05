package tools

import (
	"context"
	"testing"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type conversationConfigurationClientStub struct {
	configuration generated.AgentConversation
	request       *generated.AgentConversationPatch
	params        generated.UpdateCurrentAgentConversationConfigurationParams
}

func (s *conversationConfigurationClientStub) GetCurrentAgentConversationConfiguration(
	context.Context,
) (generated.GetCurrentAgentConversationConfigurationRes, error) {
	return &generated.AgentConversationHeaders{Response: s.configuration}, nil
}

func (s *conversationConfigurationClientStub) UpdateCurrentAgentConversationConfiguration(
	_ context.Context,
	request *generated.AgentConversationPatch,
	params generated.UpdateCurrentAgentConversationConfigurationParams,
) (generated.UpdateCurrentAgentConversationConfigurationRes, error) {
	s.request = request
	s.params = params
	s.configuration.Version++
	if number, ok := request.DefaultProjectNumber.Get(); ok {
		s.configuration.DefaultProject = generated.NewOptProjectRef(generated.ProjectRef{
			ID: uuid.New(), Number: number, Name: "Strategy and Models",
		})
	}
	return &generated.AgentConversationHeaders{Response: s.configuration}, nil
}

func TestUpdateCurrentConversationConfigurationUsesRunScopedOpenAPI(t *testing.T) {
	runID := uuid.New()
	client := &conversationConfigurationClientStub{configuration: generated.AgentConversation{
		ID: uuid.New(), Provider: generated.AgentConversationProviderLark,
		Enabled: true, Version: 1, CanManage: true,
	}}
	projectNumber := int64(14)

	result, err := updateCurrentConversationConfiguration(
		context.Background(),
		Config{Run: pactagent.Run{ID: runID}},
		client,
		UpdateConversationConfigurationInput{
			ExpectedVersion: 1, DefaultProjectNumber: &projectNumber,
		},
	)

	require.NoError(t, err)
	require.Equal(t, `"1"`, client.params.IfMatch)
	require.Equal(t,
		pactagent.ConversationConfigurationIdempotencyKey(runID),
		client.params.IdempotencyKey.Value,
	)
	requestProject, ok := client.request.DefaultProjectNumber.Get()
	require.True(t, ok)
	require.Equal(t, projectNumber, requestProject)
	require.Equal(t, int64(2), result.Version)
	require.NotNil(t, result.DefaultProject)
	require.Equal(t, projectNumber, result.DefaultProject.Number)
}

func TestUpdateCurrentConversationConfigurationRequiresAChange(t *testing.T) {
	_, err := updateCurrentConversationConfiguration(
		context.Background(),
		Config{Run: pactagent.Run{ID: uuid.New()}},
		&conversationConfigurationClientStub{},
		UpdateConversationConfigurationInput{ExpectedVersion: 1},
	)
	require.ErrorIs(t, err, ErrToolInput)
}
