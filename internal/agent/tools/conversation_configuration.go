package tools

import (
	"context"
	"fmt"
	"strings"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
)

type GetConversationConfigurationInput struct{}

type ConversationConfigurationResult struct {
	Enabled         bool              `json:"enabled"`
	BindingActive   bool              `json:"binding_active"`
	DefaultProject  *ProjectCandidate `json:"default_project,omitempty"`
	BusinessContext string            `json:"business_context"`
	Version         int64             `json:"version"`
	CanManage       bool              `json:"can_manage"`
}

type UpdateConversationConfigurationInput struct {
	ExpectedVersion      int64   `json:"expected_version" jsonschema:"required,description=Version returned by get_current_conversation_configuration"`
	Enabled              *bool   `json:"enabled,omitempty" jsonschema_description:"Optional Agent availability; false prevents future LLM Runs and can only be reversed in the web UI"`
	BindingActive        *bool   `json:"binding_active,omitempty" jsonschema_description:"Optional default-Project binding state"`
	DefaultProjectNumber *int64  `json:"default_project_number,omitempty" jsonschema_description:"Optional Project number resolved with search_projects"`
	BusinessContext      *string `json:"business_context,omitempty" jsonschema_description:"Optional Markdown business background, at most 4000 characters; use an empty string to clear"`
}

func getCurrentConversationConfiguration(
	ctx context.Context,
	client ConversationConfigurationClient,
) (ConversationConfigurationResult, error) {
	response, err := client.GetCurrentAgentConversationConfiguration(ctx)
	if err != nil {
		return ConversationConfigurationResult{}, fmt.Errorf("%w: get current conversation configuration: %w", ErrRetryable, err)
	}
	configuration, ok := response.(*generated.AgentConversationHeaders)
	if !ok {
		return ConversationConfigurationResult{}, openAPIResponseError(response)
	}
	return conversationConfigurationResult(configuration.Response), nil
}

func updateCurrentConversationConfiguration(
	ctx context.Context,
	config Config,
	client ConversationConfigurationClient,
	input UpdateConversationConfigurationInput,
) (ConversationConfigurationResult, error) {
	if input.ExpectedVersion <= 0 {
		return ConversationConfigurationResult{}, fmt.Errorf("%w: expected_version must be positive", ErrToolInput)
	}
	if input.Enabled == nil && input.BindingActive == nil &&
		input.DefaultProjectNumber == nil && input.BusinessContext == nil {
		return ConversationConfigurationResult{}, fmt.Errorf("%w: at least one configuration field is required", ErrToolInput)
	}
	patch := &generated.AgentConversationPatch{}
	if input.Enabled != nil {
		patch.Enabled = generated.NewOptBool(*input.Enabled)
	}
	if input.BindingActive != nil {
		patch.BindingActive = generated.NewOptBool(*input.BindingActive)
	}
	if input.DefaultProjectNumber != nil {
		if *input.DefaultProjectNumber <= 0 {
			return ConversationConfigurationResult{}, fmt.Errorf("%w: default_project_number must be positive", ErrToolInput)
		}
		patch.DefaultProjectNumber = generated.NewOptInt64(*input.DefaultProjectNumber)
	}
	if input.BusinessContext != nil {
		value := strings.TrimSpace(*input.BusinessContext)
		patch.BusinessContext = generated.NewOptString(value)
	}
	response, err := client.UpdateCurrentAgentConversationConfiguration(
		ctx,
		patch,
		generated.UpdateCurrentAgentConversationConfigurationParams{
			IfMatch: fmt.Sprintf("\"%d\"", input.ExpectedVersion),
			IdempotencyKey: generated.NewOptString(
				pactagent.ConversationConfigurationIdempotencyKey(config.Run.ID),
			),
		},
	)
	if err != nil {
		return ConversationConfigurationResult{}, fmt.Errorf("%w: update current conversation configuration: %w", ErrRetryable, err)
	}
	configuration, ok := response.(*generated.AgentConversationHeaders)
	if !ok {
		return ConversationConfigurationResult{}, openAPIResponseError(response)
	}
	return conversationConfigurationResult(configuration.Response), nil
}

func conversationConfigurationResult(
	configuration generated.AgentConversation,
) ConversationConfigurationResult {
	result := ConversationConfigurationResult{
		Enabled:         configuration.Enabled,
		BindingActive:   configuration.BindingActive,
		BusinessContext: configuration.BusinessContext,
		Version:         configuration.Version,
		CanManage:       configuration.CanManage,
	}
	if project, ok := configuration.DefaultProject.Get(); ok {
		result.DefaultProject = &ProjectCandidate{Number: project.Number, Name: project.Name}
	}
	return result
}
