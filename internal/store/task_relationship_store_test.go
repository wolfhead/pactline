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

func TestTaskParentRelationshipIsOneLevelAndSharesWorkContext(t *testing.T) {
	db := newTestDB(t)
	tasks := store.NewTaskStore(db)

	parent := mustCreateTask(t, db, tasks, domain.Task{
		Title:     "Parent task",
		CreatorID: userA,
	}, nil)
	cleanupTask(t, db, parent.Task.ID)
	child := mustCreateTask(t, db, tasks, domain.Task{
		Title:        "Child task",
		CreatorID:    userA,
		ProjectID:    parent.Task.ProjectID,
		MilestoneID:  parent.Task.MilestoneID,
		ParentTaskID: &parent.Task.ID,
	}, nil)
	cleanupTask(t, db, child.Task.ID)

	require.NotNil(t, child.Parent)
	require.Equal(t, parent.Task.Number, child.Parent.Number)

	reloadedParent, err := tasks.GetByNumber(context.Background(), parent.Task.Number)
	require.NoError(t, err)
	require.Len(t, reloadedParent.Children, 1)
	require.Equal(t, child.Task.Number, reloadedParent.Children[0].Number)

	grandchild := domain.Task{
		Title:          "Unsupported grandchild",
		Context:        "Test context",
		ExpectedResult: "Test result",
		CreatorID:      userA,
		ProjectID:      parent.Task.ProjectID,
		MilestoneID:    parent.Task.MilestoneID,
		ParentTaskID:   &child.Task.ID,
	}
	_, err = tasks.Create(context.Background(), grandchild, nil)
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestTaskDependenciesPreventCyclesAndPrematureCompletion(t *testing.T) {
	db := newTestDB(t)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)

	predecessor := mustCreateTask(t, db, tasks, domain.Task{
		Title:     "Predecessor",
		CreatorID: userA,
	}, nil)
	cleanupTask(t, db, predecessor.Task.ID)
	dependent := mustCreateTask(t, db, tasks, domain.Task{
		Title:     "Dependent",
		CreatorID: userA,
		ProjectID: predecessor.Task.ProjectID,
	}, nil)
	cleanupTask(t, db, dependent.Task.ID)

	linked, err := tasks.Update(
		context.Background(),
		dependent.Task.Number,
		domain.TaskPatch{
			DependenciesSet: true,
			DependencyIDs:   []uuid.UUID{predecessor.Task.ID},
		},
		userA,
	)
	require.NoError(t, err)
	require.True(t, linked.Blocked)
	require.Len(t, linked.Dependencies, 1)

	_, err = workflow.MarkReady(
		context.Background(), dependent.Task.Number, linked.Task.Version,
		domain.SessionOperation(userA, "dependent-premature-ready"), now,
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	_, err = tasks.Update(
		context.Background(),
		predecessor.Task.Number,
		domain.TaskPatch{
			DependenciesSet: true,
			DependencyIDs:   []uuid.UUID{dependent.Task.ID},
		},
		userA,
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	completeTaskWorkflow(t, workflow, predecessor.Task.Number, predecessor.Task.Version, now)

	reloaded, err := tasks.GetByNumber(context.Background(), dependent.Task.Number)
	require.NoError(t, err)
	require.False(t, reloaded.Blocked)
	completeTaskWorkflow(t, workflow, dependent.Task.Number, reloaded.Task.Version, now.Add(time.Hour))
}

func completeTaskWorkflow(
	t *testing.T,
	workflow *store.TaskWorkflowStore,
	taskNumber, version int64,
	now time.Time,
) {
	t.Helper()
	ctx := context.Background()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	ready, err := workflow.MarkReady(
		ctx, taskNumber, version, domain.SessionOperation(userA, "complete-ready"), now,
	)
	require.NoError(t, err)
	working, execution, err := workflow.Claim(
		ctx, taskNumber, ready.Version, actor,
		domain.SessionOperation(userA, "complete-execution-claim"), "browser", "", now.Add(time.Minute),
	)
	require.NoError(t, err)
	review, _, err := workflow.SubmitWork(
		ctx, taskNumber, execution.ID, working.Version, execution.Version,
		"Execution complete.", actor,
		domain.SessionOperation(userA, "complete-submit"), now.Add(2*time.Minute),
	)
	require.NoError(t, err)
	reviewWorking, reviewClaim, err := workflow.Claim(
		ctx, taskNumber, review.Version, actor,
		domain.SessionOperation(userA, "complete-review-claim"), "browser", "", now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	_, _, err = workflow.AcceptTask(
		ctx, taskNumber, reviewClaim.ID, reviewWorking.Version, reviewClaim.Version,
		"Accepted.", actor,
		domain.SessionOperation(userA, "complete-accept"), now.Add(4*time.Minute),
	)
	require.NoError(t, err)
}

func TestMovingParentMovesChildrenAtomically(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	tasks := store.NewTaskStore(db)

	project, err := projects.Create(ctx, domain.Project{
		Name:      "Relationship movement test",
		CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	first, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: project.Project.ID,
		Name:      "First milestone",
		Outcome:   "First outcome",
		OwnerID:   userA,
	})
	require.NoError(t, err)
	second, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: project.Project.ID,
		Name:      "Second milestone",
		Outcome:   "Second outcome",
		OwnerID:   userA,
	})
	require.NoError(t, err)

	parent := mustCreateTask(t, db, tasks, domain.Task{
		Title:       "Movable parent",
		CreatorID:   userA,
		ProjectID:   project.Project.ID,
		MilestoneID: &first.ID,
	}, nil)
	child := mustCreateTask(t, db, tasks, domain.Task{
		Title:        "Movable child",
		CreatorID:    userA,
		ProjectID:    project.Project.ID,
		MilestoneID:  &first.ID,
		ParentTaskID: &parent.Task.ID,
	}, nil)

	_, err = tasks.Update(ctx, child.Task.Number, domain.TaskPatch{
		MilestoneSet: true,
		MilestoneID:  &second.ID,
	}, userA)
	require.ErrorIs(t, err, domain.ErrConflict)

	movedParent, err := tasks.Update(ctx, parent.Task.Number, domain.TaskPatch{
		MilestoneSet: true,
		MilestoneID:  &second.ID,
	}, userA)
	require.NoError(t, err)
	require.Equal(t, &second.ID, movedParent.Task.MilestoneID)

	movedChild, err := tasks.GetByNumber(ctx, child.Task.Number)
	require.NoError(t, err)
	require.Equal(t, &second.ID, movedChild.Task.MilestoneID)
	require.Greater(t, movedChild.Task.Version, child.Task.Version)
}

func TestShiftingParentScheduleShiftsScheduledChildrenAtomically(t *testing.T) {
	db := newTestDB(t)
	tasks := store.NewTaskStore(db)
	start := mustParseDate(t, "2026-08-03")
	due := mustParseDate(t, "2026-08-05")
	childStart := mustParseDate(t, "2026-08-04")
	childDue := mustParseDate(t, "2026-08-08")

	parent := mustCreateTask(t, db, tasks, domain.Task{
		Title:     "Scheduled parent",
		CreatorID: userA,
		StartDate: &start,
		DueDate:   &due,
	}, nil)
	cleanupTask(t, db, parent.Task.ID)
	child := mustCreateTask(t, db, tasks, domain.Task{
		Title:        "Scheduled child",
		CreatorID:    userA,
		ProjectID:    parent.Task.ProjectID,
		MilestoneID:  parent.Task.MilestoneID,
		ParentTaskID: &parent.Task.ID,
		StartDate:    &childStart,
		DueDate:      &childDue,
	}, nil)
	cleanupTask(t, db, child.Task.ID)

	shift := 3
	moved, err := tasks.Update(
		context.Background(),
		parent.Task.Number,
		domain.TaskPatch{ScheduleShiftDays: &shift},
		userA,
	)
	require.NoError(t, err)
	require.Equal(t, "2026-08-06", moved.Task.StartDate.Format("2006-01-02"))
	require.Equal(t, "2026-08-08", moved.Task.DueDate.Format("2006-01-02"))

	movedChild, err := tasks.GetByNumber(context.Background(), child.Task.Number)
	require.NoError(t, err)
	require.Equal(t, "2026-08-07", movedChild.Task.StartDate.Format("2006-01-02"))
	require.Equal(t, "2026-08-11", movedChild.Task.DueDate.Format("2006-01-02"))
	require.Greater(t, movedChild.Task.Version, child.Task.Version)
}
