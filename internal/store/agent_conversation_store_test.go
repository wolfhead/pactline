package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/stretchr/testify/require"
)

func TestAgentConversationStoresImmutableConfigurationRevisions(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	projects := store.NewProjectStore(db)
	conversations := store.NewAgentConversationStore(db)

	project, err := projects.Create(ctx, domain.Project{
		Name: "Conversation defaults", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)

	initial, err := conversations.Observe(
		ctx,
		"lark",
		"tenant-conversation-test",
		"chat-conversation-test",
		"Release room",
		userA,
		now,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupContext := context.Background()
		_, cleanupErr := db.Pool.Exec(cleanupContext,
			`DELETE FROM agent_conversation_revisions WHERE conversation_id=$1`,
			initial.Conversation.ID,
		)
		require.NoError(t, cleanupErr)
		_, cleanupErr = db.Pool.Exec(cleanupContext,
			`DELETE FROM agent_conversations WHERE id=$1`,
			initial.Conversation.ID,
		)
		require.NoError(t, cleanupErr)
	})
	require.Equal(t, int64(1), initial.Conversation.Version)
	require.Equal(t, int64(1), initial.Revision.Version)
	require.Nil(t, initial.Revision.DefaultProjectID)

	_, err = conversations.ObserveConfiguration(
		ctx,
		"lark",
		"tenant-conversation-test",
		"chat-conversation-test",
		"Release planning room",
		userA,
		now.Add(30*time.Second),
	)
	require.NoError(t, err)
	observed, err := conversations.Get(ctx, initial.Conversation.ID)
	require.NoError(t, err)
	require.Equal(t, "Release planning room", observed.Conversation.Name)

	initialConfiguration, err := conversations.GetConfigurationRevision(ctx, initial.Revision.ID)
	require.NoError(t, err)
	require.Nil(t, initialConfiguration.DefaultProjectID)
	require.Nil(t, initialConfiguration.DefaultProjectNumber)
	require.Empty(t, initialConfiguration.DefaultProjectName)

	enabled := true
	background := "This room discusses the release validation workflow."
	updated, err := conversations.UpdateVersioned(
		ctx,
		initial.Conversation.ID,
		initial.Conversation.Version,
		store.AgentConversationPatch{
			BindingActive:     &enabled,
			DefaultProjectID:  &project.Project.ID,
			DefaultProjectSet: true,
			BusinessContext:   &background,
		},
		userA,
		now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Conversation.Version)
	require.Equal(t, int64(2), updated.Revision.Version)
	require.Equal(t, project.Project.ID, *updated.Revision.DefaultProjectID)
	require.Equal(t, background, updated.Revision.BusinessContext)

	oldRevision, err := conversations.GetRevision(ctx, initial.Revision.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), oldRevision.Version)
	require.Nil(t, oldRevision.DefaultProjectID)
	require.Empty(t, oldRevision.BusinessContext)

	configuration, err := conversations.GetConfigurationRevision(ctx, updated.Revision.ID)
	require.NoError(t, err)
	require.True(t, configuration.Enabled)
	require.True(t, configuration.BindingActive)
	require.Equal(t, project.Project.Number, *configuration.DefaultProjectNumber)
	require.Equal(t, project.Project.Name, configuration.DefaultProjectName)
	require.Equal(t, background, configuration.BusinessContext)
}
