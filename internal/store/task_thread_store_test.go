package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskThreadMessagesHaveCommonHumanOwnershipRules(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Thread message ownership", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	threads := store.NewTaskThreadStore(db)
	main, err := threads.GetMainByTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	author := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}

	message, err := threads.AddItem(
		ctx, main.ID, domain.ThreadItemKindMessage, "Initial context", nil, []uuid.UUID{userB}, author,
		domain.SessionOperation(userA, "add-message"), now,
	)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{userB}, message.MentionedUserIDs)

	other := domain.Actor{Type: domain.ActorTypeUser, UserID: &userB}
	_, err = threads.EditMessage(
		ctx, message.ID, message.Version, "Unauthorized edit", nil, other,
		domain.SessionOperation(userB, "wrong-edit"), now.Add(time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrForbidden)

	edited, err := threads.EditMessage(
		ctx, message.ID, message.Version, "Clarified context", nil, author,
		domain.SessionOperation(userA, "edit-message"), now.Add(2*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), edited.Version)
	require.Empty(t, edited.MentionedUserIDs)

	deleted, err := threads.DeleteMessage(
		ctx, message.ID, edited.Version, author,
		domain.SessionOperation(userA, "delete-message"), now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	require.NotNil(t, deleted.DeletedAt)
	require.Empty(t, deleted.Body)
}

func TestTaskThreadProgressIsImmutableAndMainOnly(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Immutable progress", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	threads := store.NewTaskThreadStore(db)
	main, err := threads.GetMainByTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	author := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	operation := domain.SessionOperation(userA, "add-progress")

	progress, err := threads.AddItem(
		ctx, main.ID, domain.ThreadItemKindProgress, "Execution tests are green.",
		nil, nil, author, operation, now,
	)
	require.NoError(t, err)
	require.Equal(t, domain.ThreadItemKindProgress, progress.Kind)

	_, err = threads.EditMessage(
		ctx, progress.ID, progress.Version, "Rewrite history", nil, author,
		domain.SessionOperation(userA, "edit-progress"), now.Add(time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	workflow := store.NewTaskWorkflowStore(db)
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "ready-progress"), now,
	)
	require.NoError(t, err)
	working, claim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, author,
		domain.SessionOperation(userA, "claim-progress"), "browser", "", now,
	)
	require.NoError(t, err)
	_, _, issue, err := workflow.RequestResolution(
		ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
		domain.IssueThreadTypeDependencyRequired, "The environment is unavailable.",
		author, domain.SessionOperation(userA, "block-progress"), now,
	)
	require.NoError(t, err)

	_, err = threads.AddItem(
		ctx, issue.ID, domain.ThreadItemKindProgress, "Not a Main update", nil, nil,
		author, domain.SessionOperation(userA, "issue-progress"), now,
	)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestTaskThreadRecentItemsAreBoundedAndChronological(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Bounded Thread context", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	threads := store.NewTaskThreadStore(db)
	main, err := threads.GetMainByTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	author := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	for index, body := range []string{"one", "two", "three", "four"} {
		_, err := threads.AddItem(
			ctx, main.ID, domain.ThreadItemKindMessage, body, nil, nil, author,
			domain.SessionOperation(userA, "bounded-thread"), now.Add(time.Duration(index)*time.Minute),
		)
		require.NoError(t, err)
	}

	recent, err := threads.ListRecentItems(ctx, main.ID, 2)
	require.NoError(t, err)
	require.Equal(t, 4, recent.TotalCount)
	require.Equal(t, []string{"three", "four"}, []string{recent.Items[0].Body, recent.Items[1].Body})

	_, err = threads.ListRecentItems(ctx, main.ID, 0)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}
