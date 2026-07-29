package store_test

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLabelCreateListAndRename(t *testing.T) {
	db := newTestDB(t)
	ls := store.NewLabelStore(db)
	ctx := context.Background()

	name := "backend-" + uuid.NewString()
	l, err := ls.Create(ctx, name)
	require.NoError(t, err)
	cleanupLabel(t, db, l.ID)
	require.Equal(t, name, l.Name)

	all, err := ls.List(ctx)
	require.NoError(t, err)
	require.Contains(t, labelNames(all), name)

	renamed, err := ls.Rename(ctx, l.ID, name+"-renamed")
	require.NoError(t, err)
	require.Equal(t, name+"-renamed", renamed.Name)

	all, err = ls.List(ctx)
	require.NoError(t, err)
	require.NotContains(t, labelNames(all), name, "the old name must no longer be listed")
	require.Contains(t, labelNames(all), name+"-renamed")
}

func TestLabelCreateDuplicateNameConflicts(t *testing.T) {
	db := newTestDB(t)
	ls := store.NewLabelStore(db)
	ctx := context.Background()

	name := "dup-" + uuid.NewString()
	first, err := ls.Create(ctx, name)
	require.NoError(t, err)
	cleanupLabel(t, db, first.ID)

	_, err = ls.Create(ctx, name)
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestLabelCreateRejectsBlankName(t *testing.T) {
	db := newTestDB(t)
	ls := store.NewLabelStore(db)
	_, err := ls.Create(context.Background(), "   ")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestLabelRenameMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	ls := store.NewLabelStore(db)
	_, err := ls.Rename(context.Background(), uuid.New(), "whatever")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// TestLabelDeleteRemovesTaskAssociationButKeepsTask pins that deleting a
// label (ON DELETE CASCADE on task_labels) drops the task's reference to it
// without deleting the task itself — a decoy would be a broken cascade that
// deletes the task too, or none at all that leaves the label listed.
func TestLabelDeleteRemovesTaskAssociationButKeepsTask(t *testing.T) {
	db := newTestDB(t)
	ls := store.NewLabelStore(db)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	l, err := ls.Create(ctx, "todelete-"+uuid.NewString())
	require.NoError(t, err)

	task := mustCreateTask(t, db, ts, domain.Task{Title: "Labeled", CreatorID: userA}, []uuid.UUID{l.ID})
	cleanupTask(t, db, task.Task.ID)
	require.Len(t, task.Labels, 1)

	require.NoError(t, ls.Delete(ctx, l.ID))

	err = ls.Delete(ctx, l.ID)
	require.ErrorIs(t, err, domain.ErrNotFound, "deleting an already-deleted label must report not found")

	again, err := ts.GetByNumber(ctx, task.Task.Number)
	require.NoError(t, err)
	require.Empty(t, again.Labels, "the task must survive, with the deleted label dropped from its label list")
}

func labelNames(labels []domain.Label) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l.Name
	}
	return out
}
