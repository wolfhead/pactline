package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

type AgentConversationService struct {
	Conversations *store.AgentConversationStore
	Projects      *store.ProjectStore
	Access        *ProjectAccessService
	Now           func() time.Time
}

type AgentConversationUpdate struct {
	Enabled              *bool
	BindingActive        *bool
	DefaultProjectNumber *int64
	DefaultProjectSet    bool
	BusinessContext      *string
}

func (s *AgentConversationService) List(
	ctx context.Context,
	subject domain.ProjectAccessSubject,
) ([]store.AgentConversationSnapshot, error) {
	return s.Conversations.ListVisible(ctx, subject)
}

func (s *AgentConversationService) Get(
	ctx context.Context,
	id uuid.UUID,
	subject domain.ProjectAccessSubject,
) (store.AgentConversationSnapshot, error) {
	snapshot, err := s.Conversations.Get(ctx, id)
	if err != nil {
		return store.AgentConversationSnapshot{}, err
	}
	if err := s.requireRead(ctx, snapshot, subject); err != nil {
		return store.AgentConversationSnapshot{}, err
	}
	return snapshot, nil
}

func (s *AgentConversationService) CanManage(
	ctx context.Context,
	snapshot store.AgentConversationSnapshot,
	subject domain.ProjectAccessSubject,
) (bool, error) {
	err := s.requireAdmin(ctx, snapshot, subject)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (s *AgentConversationService) Update(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	input AgentConversationUpdate,
	subject domain.ProjectAccessSubject,
) (store.AgentConversationSnapshot, error) {
	current, err := s.Conversations.Get(ctx, id)
	if err != nil {
		return store.AgentConversationSnapshot{}, err
	}
	if err := s.requireAdmin(ctx, current, subject); err != nil {
		return store.AgentConversationSnapshot{}, err
	}
	patch := store.AgentConversationPatch{
		Enabled: input.Enabled, BindingActive: input.BindingActive,
		BusinessContext: input.BusinessContext,
	}
	if input.DefaultProjectSet {
		if input.DefaultProjectNumber == nil {
			return store.AgentConversationSnapshot{}, fmt.Errorf(
				"%w: clearing the default Project uses binding_active=false",
				domain.ErrInvalidInput,
			)
		}
		project, err := s.Access.RequireProjectByNumber(
			ctx,
			*input.DefaultProjectNumber,
			subject,
			ProjectPermissionAdmin,
		)
		if err != nil {
			return store.AgentConversationSnapshot{}, err
		}
		if project.Project.ArchivedAt != nil {
			return store.AgentConversationSnapshot{}, fmt.Errorf(
				"%w: archived Projects cannot become a conversation default",
				domain.ErrConflict,
			)
		}
		patch.DefaultProjectID = &project.Project.ID
		patch.DefaultProjectSet = true
		patch.BindingActive = bindingActiveForProjectUpdate(patch.BindingActive)
	}
	if input.BusinessContext != nil && current.Conversation.DefaultProjectID == nil && !input.DefaultProjectSet {
		return store.AgentConversationSnapshot{}, fmt.Errorf(
			"%w: bind the conversation to a Project before editing business context",
			domain.ErrConflict,
		)
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	updated, err := s.Conversations.UpdateVersioned(
		ctx,
		id,
		expectedVersion,
		patch,
		subject.UserID,
		now,
	)
	if err != nil {
		return store.AgentConversationSnapshot{}, err
	}
	slog.Info("Agent conversation configuration updated",
		"conversation_id", id,
		"version", updated.Conversation.Version,
		"binding_active", updated.Conversation.BindingActive,
		"enabled", updated.Conversation.Enabled,
		"actor_id", subject.UserID,
	)
	return updated, nil
}

func bindingActiveForProjectUpdate(explicit *bool) *bool {
	if explicit != nil {
		return explicit
	}
	active := true
	return &active
}

func (s *AgentConversationService) requireRead(
	ctx context.Context,
	snapshot store.AgentConversationSnapshot,
	subject domain.ProjectAccessSubject,
) error {
	if subject.IsPlatformAdministrator() || snapshot.Conversation.CreatedBy == subject.UserID {
		return nil
	}
	if snapshot.Conversation.DefaultProjectID == nil {
		return domain.ErrNotFound
	}
	return s.Access.RequireProjectByID(
		ctx,
		*snapshot.Conversation.DefaultProjectID,
		subject,
		ProjectPermissionRead,
	)
}

func (s *AgentConversationService) requireAdmin(
	ctx context.Context,
	snapshot store.AgentConversationSnapshot,
	subject domain.ProjectAccessSubject,
) error {
	if subject.IsPlatformAdministrator() {
		return nil
	}
	if snapshot.Conversation.DefaultProjectID == nil {
		if snapshot.Conversation.CreatedBy == subject.UserID {
			return nil
		}
		return domain.ErrNotFound
	}
	return s.Access.RequireProjectByID(
		ctx,
		*snapshot.Conversation.DefaultProjectID,
		subject,
		ProjectPermissionAdmin,
	)
}
