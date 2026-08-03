package v1

import (
	"context"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"
)

func (h *Handler) ListAgentConversations(
	ctx context.Context,
) (generated.ListAgentConversationsRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.AgentConversations.List(ctx, subject)
	if err != nil {
		return nil, err
	}
	response := generated.AgentConversationList{
		Items: make([]generated.AgentConversation, 0, len(items)),
	}
	for _, item := range items {
		converted, err := h.agentConversationFromDomain(ctx, item, subject)
		if err != nil {
			return nil, err
		}
		response.Items = append(response.Items, converted)
	}
	return &generated.AgentConversationListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) GetAgentConversation(
	ctx context.Context,
	params generated.GetAgentConversationParams,
) (generated.GetAgentConversationRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := h.AgentConversations.Get(ctx, params.ID, subject)
	if err != nil {
		return nil, err
	}
	return h.agentConversationResponse(ctx, snapshot, subject)
}

func (h *Handler) UpdateAgentConversation(
	ctx context.Context,
	req *generated.AgentConversationPatch,
	params generated.UpdateAgentConversationParams,
) (generated.UpdateAgentConversationRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	input := application.AgentConversationUpdate{}
	if value, ok := req.Enabled.Get(); ok {
		input.Enabled = &value
	}
	if value, ok := req.BindingActive.Get(); ok {
		input.BindingActive = &value
	}
	if value, ok := req.DefaultProjectNumber.Get(); ok {
		input.DefaultProjectNumber = &value
		input.DefaultProjectSet = true
	}
	if value, ok := req.BusinessContext.Get(); ok {
		input.BusinessContext = &value
	}
	updated, err := h.AgentConversations.Update(
		ctx,
		params.ID,
		expectedVersion,
		input,
		subject,
	)
	if err != nil {
		return nil, err
	}
	return h.agentConversationResponse(ctx, updated, subject)
}

func (h *Handler) agentConversationResponse(
	ctx context.Context,
	snapshot store.AgentConversationSnapshot,
	subject domain.ProjectAccessSubject,
) (*generated.AgentConversationHeaders, error) {
	response, err := h.agentConversationFromDomain(ctx, snapshot, subject)
	if err != nil {
		return nil, err
	}
	return &generated.AgentConversationHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) agentConversationFromDomain(
	ctx context.Context,
	snapshot store.AgentConversationSnapshot,
	subject domain.ProjectAccessSubject,
) (generated.AgentConversation, error) {
	conversation := snapshot.Conversation
	canManage, err := h.AgentConversations.CanManage(ctx, snapshot, subject)
	if err != nil {
		return generated.AgentConversation{}, err
	}
	out := generated.AgentConversation{
		ID:              conversation.ID,
		Provider:        generated.AgentConversationProvider(conversation.Provider),
		ExternalID:      conversation.ExternalID,
		Name:            conversation.Name,
		Enabled:         conversation.Enabled,
		BindingActive:   conversation.BindingActive,
		BusinessContext: conversation.BusinessContext,
		Version:         conversation.Version,
		CanManage:       canManage,
		CreatedBy:       conversation.CreatedBy,
		UpdatedBy:       conversation.UpdatedBy,
		LastSeenAt:      conversation.LastSeenAt,
		CreatedAt:       conversation.CreatedAt,
		UpdatedAt:       conversation.UpdatedAt,
	}
	if snapshot.Project != nil {
		out.DefaultProject = generated.NewOptProjectRef(generated.ProjectRef{
			ID:     snapshot.Project.ID,
			Number: snapshot.Project.Number,
			Name:   snapshot.Project.Name,
		})
	}
	return out, nil
}
