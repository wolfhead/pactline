package store_test

import (
	"context"
	"testing"
	"time"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	userA = uuid.MustParse("00000000-0000-0000-0000-000000000001") // 产品 A
	userB = uuid.MustParse("00000000-0000-0000-0000-000000000002") // 技术 Leader B
	userC = uuid.MustParse("00000000-0000-0000-0000-000000000003") // 研发 C
	userD = uuid.MustParse("00000000-0000-0000-0000-000000000004") // 研发 D
)

// cleanupTask deletes a task row (cascading to task_labels, task_comments,
// task_activity via ON DELETE CASCADE) once the test finishes, so this
// package's shared database is left with the tasks table empty as required.
func cleanupTask(t *testing.T, db *store.DB, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM tasks WHERE id = $1`, id)
		require.NoError(t, err)
	})
}

func cleanupLabel(t *testing.T, db *store.DB, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM labels WHERE id = $1`, id)
		require.NoError(t, err)
	})
}

func mustCreateTask(t *testing.T, ts *store.TaskStore, task domain.Task, labelIDs []uuid.UUID) store.TaskWithRelations {
	t.Helper()
	out, err := ts.Create(context.Background(), task, labelIDs)
	require.NoError(t, err)
	return out
}

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d
}

// TestTaskCreateDefaultsToBacklogAndNone pins the store-level defaults: a
// bare Task{} with no Status/Priority set must land as backlog/none, not as
// an empty string that would fail every later status comparison silently.
func TestTaskCreateDefaultsToBacklogAndNone(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)

	out := mustCreateTask(t, ts, domain.Task{Title: "Write onboarding doc", CreatorID: userA}, nil)
	cleanupTask(t, db, out.Task.ID)

	require.Equal(t, domain.TaskStatusBacklog, out.Task.Status)
	require.Equal(t, domain.TaskPriorityNone, out.Task.Priority)
	require.Nil(t, out.Task.AssigneeID)
	require.Nil(t, out.Assignee, "unassigned must be a first-class nil, not a zero-value user")
	require.Equal(t, userA, out.Creator.ID)
	require.Empty(t, out.Labels)
	require.Greater(t, out.Task.Number, int64(0))
}

// TestTaskNumberIsSequentialAndNeverReusedAfterArchive pins the two load-
// bearing properties of Number: it strictly increases across creates, and
// archiving a task (which never deletes its row) cannot free its number for
// a later task to reuse.
func TestTaskNumberIsSequentialAndNeverReusedAfterArchive(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	first := mustCreateTask(t, ts, domain.Task{Title: "First task", CreatorID: userA}, nil)
	cleanupTask(t, db, first.Task.ID)

	_, err := ts.SetArchived(ctx, first.Task.Number, true, userA)
	require.NoError(t, err)

	second := mustCreateTask(t, ts, domain.Task{Title: "Second task", CreatorID: userA}, nil)
	cleanupTask(t, db, second.Task.ID)

	require.Greater(t, second.Task.Number, first.Task.Number,
		"a fresh task's number must exceed every prior number, including archived ones")
	require.NotEqual(t, first.Task.Number, second.Task.Number)

	// The archived task's own number must still resolve to it, not 404 —
	// archiving hides nothing from direct lookup, only from default listing.
	stillThere, err := ts.GetByNumber(ctx, first.Task.Number)
	require.NoError(t, err)
	require.Equal(t, first.Task.ID, stillThere.Task.ID)
	require.NotNil(t, stillThere.Task.ArchivedAt)
}

func TestTaskCreateRejectsBlankTitle(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	_, err := ts.Create(context.Background(), domain.Task{Title: "   ", CreatorID: userA}, nil)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestTaskCreateRejectsUnknownStatusAndPriority(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	_, err := ts.Create(ctx, domain.Task{Title: "x", CreatorID: userA, Status: "not-a-status"}, nil)
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = ts.Create(ctx, domain.Task{Title: "x", CreatorID: userA, Priority: "not-a-priority"}, nil)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// TestTaskCreateWritesCreatedActivity pins that Create itself appends to the
// activity log, in the same call, not as an afterthought a caller could
// forget.
func TestTaskCreateWritesCreatedActivity(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	out := mustCreateTask(t, ts, domain.Task{Title: "Task with activity", CreatorID: userB}, nil)
	cleanupTask(t, db, out.Task.ID)

	entries, err := ts.ListActivity(ctx, out.Task.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, domain.ActivityFieldCreated, entries[0].Field)
	require.Equal(t, userB, entries[0].ActorID)
	require.NotNil(t, entries[0].NewValue)
	require.Equal(t, "backlog", *entries[0].NewValue)
}

// TestTaskUpdateAppliesOnlyProvidedFields plants a decoy: Description is left
// out of the patch entirely, so a broken Update that (say) always resets
// Description to "" alongside Title would be caught here.
func TestTaskUpdateAppliesOnlyProvidedFields(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	out := mustCreateTask(t, ts, domain.Task{
		Title: "Original title", Description: "Original description", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, out.Task.ID)

	newTitle := "Updated title"
	updated, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Title: &newTitle}, userA)
	require.NoError(t, err)
	require.Equal(t, "Updated title", updated.Task.Title)
	require.Equal(t, "Original description", updated.Task.Description, "Description was not in the patch and must survive unchanged")

	entries, err := ts.ListActivity(ctx, out.Task.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2, "created + exactly one title change; description must NOT generate an entry")
	require.Equal(t, domain.ActivityFieldTitle, entries[1].Field)
	require.Equal(t, "Original title", *entries[1].OldValue)
	require.Equal(t, "Updated title", *entries[1].NewValue)
}

// TestTaskUpdateAssigneeExplicitNullVsAbsent pins the presence-vs-null
// distinction TaskPatch exists to carry: an absent AssigneeSet must leave
// the assignee untouched, while AssigneeSet=true with a nil AssigneeID must
// clear it. A broken patch that conflates "not set" with "set to null"
// would fail the first assertion below (clearing when it shouldn't).
func TestTaskUpdateAssigneeExplicitNullVsAbsent(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	out := mustCreateTask(t, ts, domain.Task{Title: "Assign me", CreatorID: userA, AssigneeID: &userC}, nil)
	cleanupTask(t, db, out.Task.ID)
	require.NotNil(t, out.Assignee)

	// Patch with AssigneeSet=false must leave the assignee alone.
	newTitle := "Still assigned"
	untouched, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Title: &newTitle}, userA)
	require.NoError(t, err)
	require.NotNil(t, untouched.Task.AssigneeID)
	require.Equal(t, userC, *untouched.Task.AssigneeID)

	// Patch with AssigneeSet=true and AssigneeID=nil must clear it.
	cleared, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{AssigneeSet: true, AssigneeID: nil}, userA)
	require.NoError(t, err)
	require.Nil(t, cleared.Task.AssigneeID)
	require.Nil(t, cleared.Assignee)

	entries, err := ts.ListActivity(ctx, out.Task.ID)
	require.NoError(t, err)
	var assigneeEntries []domain.Activity
	for _, e := range entries {
		if e.Field == domain.ActivityFieldAssignee {
			assigneeEntries = append(assigneeEntries, e)
		}
	}
	require.Len(t, assigneeEntries, 1, "only the clearing update actually changed the assignee")
	require.Equal(t, userC.String(), *assigneeEntries[0].OldValue)
	require.Nil(t, assigneeEntries[0].NewValue)
}

// TestTaskUpdateStatusToDoneSetsCompletedAtAndClearingReverts pins the
// automatic (not caller-set) completed_at bookkeeping: entering "done" stamps
// it, leaving "done" clears it. A decoy: the third update moves between two
// non-done statuses, which must NOT touch completed_at at all.
func TestTaskUpdateStatusToDoneSetsCompletedAtAndClearingReverts(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	out := mustCreateTask(t, ts, domain.Task{Title: "Ship it", CreatorID: userA, Status: domain.TaskStatusInProgress}, nil)
	cleanupTask(t, db, out.Task.ID)
	require.Nil(t, out.Task.CompletedAt)

	done := domain.TaskStatusDone
	completed, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Status: &done}, userA)
	require.NoError(t, err)
	require.NotNil(t, completed.Task.CompletedAt)
	require.WithinDuration(t, time.Now(), *completed.Task.CompletedAt, 5*time.Second)

	reopened := domain.TaskStatusInReview
	back, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Status: &reopened}, userA)
	require.NoError(t, err)
	require.Nil(t, back.Task.CompletedAt, "leaving done must clear completed_at")

	todo := domain.TaskStatusTodo
	moved, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Status: &todo}, userA)
	require.NoError(t, err)
	require.Nil(t, moved.Task.CompletedAt, "moving between two non-done statuses must not resurrect completed_at")
}

// TestTaskUpdateResendingSameValueWritesNoActivity plants a decoy for
// recordFieldChange: patching Status to the value it already holds must not
// add a new activity entry, only a broken implementation that compares
// nothing (or always writes) would fail this.
func TestTaskUpdateResendingSameValueWritesNoActivity(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	out := mustCreateTask(t, ts, domain.Task{Title: "No-op patch", CreatorID: userA, Status: domain.TaskStatusTodo}, nil)
	cleanupTask(t, db, out.Task.ID)

	same := domain.TaskStatusTodo
	_, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Status: &same}, userA)
	require.NoError(t, err)

	entries, err := ts.ListActivity(ctx, out.Task.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the creation entry; resending the current status must add nothing")
}

func TestTaskUpdateMissingNumberReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	title := "x"
	_, err := ts.Update(context.Background(), 987654321, domain.TaskPatch{Title: &title}, userA)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTaskUpdateRejectsUnknownStatus(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()
	out := mustCreateTask(t, ts, domain.Task{Title: "x", CreatorID: userA}, nil)
	cleanupTask(t, db, out.Task.ID)

	bad := domain.TaskStatus("nope")
	_, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{Status: &bad}, userA)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// TestTaskArchiveAndRestoreRoundTripWithActivity pins archive/restore as
// orthogonal to status (status must NOT change) and idempotent (re-archiving
// an already-archived task adds no second activity entry).
func TestTaskArchiveAndRestoreRoundTripWithActivity(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	out := mustCreateTask(t, ts, domain.Task{Title: "Archive me", CreatorID: userA, Status: domain.TaskStatusInProgress}, nil)
	cleanupTask(t, db, out.Task.ID)

	archived, err := ts.SetArchived(ctx, out.Task.Number, true, userB)
	require.NoError(t, err)
	require.NotNil(t, archived.Task.ArchivedAt)
	require.Equal(t, domain.TaskStatusInProgress, archived.Task.Status, "archiving must not touch status")

	// Idempotent re-archive: no error, no duplicate activity.
	again, err := ts.SetArchived(ctx, out.Task.Number, true, userB)
	require.NoError(t, err)
	require.NotNil(t, again.Task.ArchivedAt)

	restored, err := ts.SetArchived(ctx, out.Task.Number, false, userA)
	require.NoError(t, err)
	require.Nil(t, restored.Task.ArchivedAt)

	entries, err := ts.ListActivity(ctx, out.Task.ID)
	require.NoError(t, err)
	var archiveEntries []domain.Activity
	for _, e := range entries {
		if e.Field == domain.ActivityFieldArchived {
			archiveEntries = append(archiveEntries, e)
		}
	}
	require.Len(t, archiveEntries, 2, "one for archiving, one for restoring; the idempotent re-archive must add nothing")
	require.Equal(t, "false", *archiveEntries[0].OldValue)
	require.Equal(t, "true", *archiveEntries[0].NewValue)
	require.Equal(t, "true", *archiveEntries[1].OldValue)
	require.Equal(t, "false", *archiveEntries[1].NewValue)
}

func TestTaskArchiveMissingNumberReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	_, err := ts.SetArchived(context.Background(), 987654322, true, userA)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// TestTaskUpdateLabelsWritesLabelsActivity exercises the label-set path of
// Update end-to-end: attaching then replacing labels updates task_labels and
// records exactly the label-name diff.
func TestTaskUpdateLabelsWritesLabelsActivity(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ls := store.NewLabelStore(db)
	ctx := context.Background()

	bug, err := ls.Create(ctx, "bug-"+uuid.NewString())
	require.NoError(t, err)
	cleanupLabel(t, db, bug.ID)
	urgent, err := ls.Create(ctx, "urgent-"+uuid.NewString())
	require.NoError(t, err)
	cleanupLabel(t, db, urgent.ID)

	out := mustCreateTask(t, ts, domain.Task{Title: "Labeled task", CreatorID: userA}, []uuid.UUID{bug.ID})
	cleanupTask(t, db, out.Task.ID)
	require.Len(t, out.Labels, 1)

	updated, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{LabelsSet: true, LabelIDs: []uuid.UUID{urgent.ID}}, userA)
	require.NoError(t, err)
	require.Len(t, updated.Labels, 1)
	require.Equal(t, urgent.ID, updated.Labels[0].ID)

	entries, err := ts.ListActivity(ctx, out.Task.ID)
	require.NoError(t, err)
	var labelEntries []domain.Activity
	for _, e := range entries {
		if e.Field == domain.ActivityFieldLabels {
			labelEntries = append(labelEntries, e)
		}
	}
	require.Len(t, labelEntries, 1)
	require.Equal(t, bug.Name, *labelEntries[0].OldValue)
	require.Equal(t, urgent.Name, *labelEntries[0].NewValue)

	// Clearing labels (LabelsSet=true, empty slice) must empty the list.
	cleared, err := ts.Update(ctx, out.Task.Number, domain.TaskPatch{LabelsSet: true, LabelIDs: nil}, userA)
	require.NoError(t, err)
	require.Empty(t, cleared.Labels)
}
