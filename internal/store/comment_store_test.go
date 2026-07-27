package store_test

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/stretchr/testify/require"
)

func TestCommentCreateListAndOnlyAuthorMayEditOrDelete(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	cs := store.NewCommentStore(db)
	ctx := context.Background()

	task := mustCreateTask(t, ts, domain.Task{Title: "Commented task", CreatorID: userA}, nil)
	cleanupTask(t, db, task.Task.ID)

	c, err := cs.Create(ctx, task.Task.ID, userC, "first remark")
	require.NoError(t, err)
	require.Equal(t, "first remark", c.Body)
	require.Equal(t, userC, c.AuthorID)

	list, err := cs.List(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	// A different user may not edit it.
	_, err = cs.Update(ctx, task.Task.ID, c.ID, userD, "hijacked")
	require.ErrorIs(t, err, domain.ErrForbidden)

	// The author may.
	edited, err := cs.Update(ctx, task.Task.ID, c.ID, userC, "edited remark")
	require.NoError(t, err)
	require.Equal(t, "edited remark", edited.Body)

	// A different user may not delete it either.
	err = cs.Delete(ctx, task.Task.ID, c.ID, userD)
	require.ErrorIs(t, err, domain.ErrForbidden)

	list, err = cs.List(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Len(t, list, 1, "the forbidden delete attempt must not have removed the comment")

	require.NoError(t, cs.Delete(ctx, task.Task.ID, c.ID, userC))

	list, err = cs.List(ctx, task.Task.ID)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestCommentCreateRejectsBlankBody(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	cs := store.NewCommentStore(db)
	ctx := context.Background()

	task := mustCreateTask(t, ts, domain.Task{Title: "x", CreatorID: userA}, nil)
	cleanupTask(t, db, task.Task.ID)

	_, err := cs.Create(ctx, task.Task.ID, userA, "   ")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// TestCommentUpdateWrongTaskIDReturnsNotFound pins the taskID-scoping
// defense in CommentStore.Update/Delete: a comment ID that is real, but
// requested under a different task's number, must 404 rather than silently
// operate across task boundaries.
func TestCommentUpdateWrongTaskIDReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	cs := store.NewCommentStore(db)
	ctx := context.Background()

	taskOne := mustCreateTask(t, ts, domain.Task{Title: "one", CreatorID: userA}, nil)
	cleanupTask(t, db, taskOne.Task.ID)
	taskTwo := mustCreateTask(t, ts, domain.Task{Title: "two", CreatorID: userA}, nil)
	cleanupTask(t, db, taskTwo.Task.ID)

	c, err := cs.Create(ctx, taskOne.Task.ID, userA, "belongs to task one")
	require.NoError(t, err)

	_, err = cs.Update(ctx, taskTwo.Task.ID, c.ID, userA, "cross-task edit")
	require.ErrorIs(t, err, domain.ErrNotFound)
}
