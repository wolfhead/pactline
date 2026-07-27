package store_test

import (
	"context"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestTaskListFiltersByStatusPriorityAssigneeAndLabel plants one decoy per
// axis: for each filter under test, at least one created task differs from
// the target on exactly that axis and must be excluded, while an unassigned
// task that otherwise matches must still appear (assignee is a filter axis,
// not a precondition for appearing in the others).
func TestTaskListFiltersByStatusPriorityAssigneeAndLabel(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ls := store.NewLabelStore(db)
	ctx := context.Background()

	marker := "listfilter-" + uuid.NewString()

	feature, err := ls.Create(ctx, marker+"-feature")
	require.NoError(t, err)
	cleanupLabel(t, db, feature.ID)
	chore, err := ls.Create(ctx, marker+"-chore")
	require.NoError(t, err)
	cleanupLabel(t, db, chore.ID)

	// Target: status=in_progress, priority=high, assignee=userC, label=feature.
	target := mustCreateTask(t, ts, domain.Task{
		Title: marker + " target", CreatorID: userA, Status: domain.TaskStatusInProgress,
		Priority: domain.TaskPriorityHigh, AssigneeID: &userC,
	}, []uuid.UUID{feature.ID})
	cleanupTask(t, db, target.Task.ID)

	// Decoy: matches everything except status.
	decoyStatus := mustCreateTask(t, ts, domain.Task{
		Title: marker + " decoy-status", CreatorID: userA, Status: domain.TaskStatusTodo,
		Priority: domain.TaskPriorityHigh, AssigneeID: &userC,
	}, []uuid.UUID{feature.ID})
	cleanupTask(t, db, decoyStatus.Task.ID)

	// Decoy: matches everything except priority.
	decoyPriority := mustCreateTask(t, ts, domain.Task{
		Title: marker + " decoy-priority", CreatorID: userA, Status: domain.TaskStatusInProgress,
		Priority: domain.TaskPriorityLow, AssigneeID: &userC,
	}, []uuid.UUID{feature.ID})
	cleanupTask(t, db, decoyPriority.Task.ID)

	// Decoy: matches everything except assignee.
	decoyAssignee := mustCreateTask(t, ts, domain.Task{
		Title: marker + " decoy-assignee", CreatorID: userA, Status: domain.TaskStatusInProgress,
		Priority: domain.TaskPriorityHigh, AssigneeID: &userD,
	}, []uuid.UUID{feature.ID})
	cleanupTask(t, db, decoyAssignee.Task.ID)

	// Decoy: matches everything except label.
	decoyLabel := mustCreateTask(t, ts, domain.Task{
		Title: marker + " decoy-label", CreatorID: userA, Status: domain.TaskStatusInProgress,
		Priority: domain.TaskPriorityHigh, AssigneeID: &userC,
	}, []uuid.UUID{chore.ID})
	cleanupTask(t, db, decoyLabel.Task.ID)

	// Unassigned task matching status+priority+label must still survive
	// every filter that doesn't constrain assignee.
	unassigned := mustCreateTask(t, ts, domain.Task{
		Title: marker + " unassigned", CreatorID: userA, Status: domain.TaskStatusInProgress,
		Priority: domain.TaskPriorityHigh,
	}, []uuid.UUID{feature.ID})
	cleanupTask(t, db, unassigned.Task.ID)

	// Filter by status+priority+label (no assignee constraint): target,
	// unassigned and decoy-assignee all match (assignee is unconstrained
	// here); decoy-status, decoy-priority and decoy-label are excluded on
	// their one bad axis each.
	res, err := ts.List(ctx, store.TaskListFilter{
		Statuses:   []domain.TaskStatus{domain.TaskStatusInProgress},
		Priorities: []domain.TaskPriority{domain.TaskPriorityHigh},
		LabelIDs:   []uuid.UUID{feature.ID},
		Search:     marker,
		Limit:      50,
	})
	require.NoError(t, err)
	gotIDs := taskIDs(res.Items)
	require.ElementsMatch(t, []uuid.UUID{target.Task.ID, unassigned.Task.ID, decoyAssignee.Task.ID}, gotIDs,
		"unassigned must survive filters that don't constrain assignee, and every status/priority/label-axis decoy must be excluded")

	// Now also filter by assignee=userC: only target remains.
	res, err = ts.List(ctx, store.TaskListFilter{
		Statuses:   []domain.TaskStatus{domain.TaskStatusInProgress},
		Priorities: []domain.TaskPriority{domain.TaskPriorityHigh},
		LabelIDs:   []uuid.UUID{feature.ID},
		AssigneeID: &userC,
		Search:     marker,
		Limit:      50,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{target.Task.ID}, taskIDs(res.Items))

	// Filter by Unassigned=true: only the unassigned task remains.
	res, err = ts.List(ctx, store.TaskListFilter{
		Statuses:   []domain.TaskStatus{domain.TaskStatusInProgress},
		Priorities: []domain.TaskPriority{domain.TaskPriorityHigh},
		LabelIDs:   []uuid.UUID{feature.ID},
		Unassigned: true,
		Search:     marker,
		Limit:      50,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{unassigned.Task.ID}, taskIDs(res.Items))
}

// TestTaskListSearchMatchesTitleOrDescriptionOnly plants a decoy that shares
// no words with the search term at all, so a broken search that (say)
// ignores the WHERE clause and returns everything would be caught.
func TestTaskListSearchMatchesTitleOrDescriptionOnly(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	marker := "searchtok-" + uuid.NewString()[:8]

	titleMatch := mustCreateTask(t, ts, domain.Task{Title: "Fix the " + marker + " bug", CreatorID: userA}, nil)
	cleanupTask(t, db, titleMatch.Task.ID)

	descMatch := mustCreateTask(t, ts, domain.Task{Title: "Unrelated title", Description: "root cause is " + marker, CreatorID: userA}, nil)
	cleanupTask(t, db, descMatch.Task.ID)

	decoy := mustCreateTask(t, ts, domain.Task{Title: "Totally unrelated", Description: "nothing to see here", CreatorID: userA}, nil)
	cleanupTask(t, db, decoy.Task.ID)

	res, err := ts.List(ctx, store.TaskListFilter{Search: marker, Archived: "all", Limit: 50})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{titleMatch.Task.ID, descMatch.Task.ID}, taskIDs(res.Items))
}

// TestTaskListArchivedDefaultExcludesAndFilterIncludes pins the three-way
// archived filter: default view hides archived tasks, "only" shows
// exclusively archived ones, "all" shows both.
func TestTaskListArchivedDefaultExcludesAndFilterIncludes(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	marker := "archfilter-" + uuid.NewString()

	active := mustCreateTask(t, ts, domain.Task{Title: marker + " active", CreatorID: userA}, nil)
	cleanupTask(t, db, active.Task.ID)
	archived := mustCreateTask(t, ts, domain.Task{Title: marker + " archived", CreatorID: userA}, nil)
	cleanupTask(t, db, archived.Task.ID)
	_, err := ts.SetArchived(ctx, archived.Task.Number, true, userA)
	require.NoError(t, err)

	def, err := ts.List(ctx, store.TaskListFilter{Search: marker, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{active.Task.ID}, taskIDs(def.Items), "default list must exclude archived tasks")

	only, err := ts.List(ctx, store.TaskListFilter{Search: marker, Archived: "only", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{archived.Task.ID}, taskIDs(only.Items))

	all, err := ts.List(ctx, store.TaskListFilter{Search: marker, Archived: "all", Limit: 50})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{active.Task.ID, archived.Task.ID}, taskIDs(all.Items))
}

// TestTaskListPaginationBoundary creates exactly 5 matching tasks, pages
// through with a page size of 2, and asserts: every page but the last is
// full and reports HasMore, the last page is short and reports !HasMore, no
// task is skipped, and none repeats — the classic off-by-one boundaries
// (page size divides evenly vs. not, and the exact last element).
func TestTaskListPaginationBoundary(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	marker := "pagebound-" + uuid.NewString()
	var created []uuid.UUID
	for i := 0; i < 5; i++ {
		out := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA}, nil)
		cleanupTask(t, db, out.Task.ID)
		created = append(created, out.Task.ID)
	}

	var seen []uuid.UUID
	cursor := ""
	pages := 0
	for {
		pages++
		require.Less(t, pages, 10, "must not loop forever")
		res, err := ts.List(ctx, store.TaskListFilter{Search: marker, Limit: 2, Cursor: cursor, Sort: "number", Order: "asc"})
		require.NoError(t, err)
		for _, item := range res.Items {
			seen = append(seen, item.Task.ID)
		}
		if !res.HasMore {
			require.Len(t, res.Items, 1, "5 items at page size 2: last page must hold exactly the 1 remainder")
			require.Empty(t, res.NextCursor)
			break
		}
		require.Len(t, res.Items, 2, "every non-final page at page size 2 must be full")
		require.NotEmpty(t, res.NextCursor)
		cursor = res.NextCursor
	}
	require.Equal(t, 3, pages, "5 items at page size 2 must take exactly 3 pages (2, 2, 1)")
	require.Equal(t, created, seen, "ascending-by-number pagination must return every task exactly once, in creation order")
}

// TestTaskListSortByDueDatePutsUnsetDatesLast pins NULLS-LAST behaviour for
// both directions, and uses a decoy (a task with an early due date) to make
// sure ascending order isn't accidentally satisfied by chance.
func TestTaskListSortByDueDatePutsUnsetDatesLast(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	marker := "duesort-" + uuid.NewString()
	early := mustParseDate(t, "2026-08-01")
	late := mustParseDate(t, "2026-09-01")

	taskEarly := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA, DueDate: &early}, nil)
	cleanupTask(t, db, taskEarly.Task.ID)
	taskLate := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA, DueDate: &late}, nil)
	cleanupTask(t, db, taskLate.Task.ID)
	taskNone := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA}, nil)
	cleanupTask(t, db, taskNone.Task.ID)

	asc, err := ts.List(ctx, store.TaskListFilter{Search: marker, Sort: "due_date", Order: "asc", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{taskEarly.Task.ID, taskLate.Task.ID, taskNone.Task.ID}, taskIDs(asc.Items))

	desc, err := ts.List(ctx, store.TaskListFilter{Search: marker, Sort: "due_date", Order: "desc", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{taskLate.Task.ID, taskEarly.Task.ID, taskNone.Task.ID}, taskIDs(desc.Items),
		"unset due dates must sort last even in descending order")
}

// TestTaskListSortByPriorityUsesSeverityRankNotAlphabeticalOrder plants the
// exact decoy this rule needs: "low" < "urgent" alphabetically, but urgent
// must sort first in descending priority order. A broken implementation
// that ORDERs BY the raw text column would pass an alphabetical-only check
// but fail this one.
func TestTaskListSortByPriorityUsesSeverityRankNotAlphabeticalOrder(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	ctx := context.Background()

	marker := "priosort-" + uuid.NewString()
	low := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA, Priority: domain.TaskPriorityLow}, nil)
	cleanupTask(t, db, low.Task.ID)
	urgent := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA, Priority: domain.TaskPriorityUrgent}, nil)
	cleanupTask(t, db, urgent.Task.ID)
	medium := mustCreateTask(t, ts, domain.Task{Title: marker, CreatorID: userA, Priority: domain.TaskPriorityMedium}, nil)
	cleanupTask(t, db, medium.Task.ID)

	res, err := ts.List(ctx, store.TaskListFilter{Search: marker, Sort: "priority", Order: "desc", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{urgent.Task.ID, medium.Task.ID, low.Task.ID}, taskIDs(res.Items))
}

func TestTaskListRejectsUnknownSortField(t *testing.T) {
	db := newTestDB(t)
	ts := store.NewTaskStore(db)
	_, err := ts.List(context.Background(), store.TaskListFilter{Sort: "not-a-field"})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func taskIDs(items []store.TaskWithRelations) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i, it := range items {
		out[i] = it.Task.ID
	}
	return out
}
