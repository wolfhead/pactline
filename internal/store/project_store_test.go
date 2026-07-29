package store_test

import (
	"context"
	"errors"
	"testing"

	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func cleanupProject(t *testing.T, db *store.DB, projectID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		statements := []string{
			`DELETE FROM business_audit_events
			WHERE (entity_type='project' AND entity_id=$1)
			   OR (entity_type='milestone' AND entity_id IN (
					SELECT id FROM milestones WHERE project_id=$1
			   ))
			   OR (entity_type='task' AND entity_id IN (
					SELECT id FROM tasks WHERE project_id=$1
			   ))`,
			`DELETE FROM acceptance_checks
			WHERE criterion_id IN (
				SELECT id FROM acceptance_criteria
				WHERE milestone_id IN (
					SELECT id FROM milestones WHERE project_id=$1
				) OR task_id IN (
					SELECT id FROM tasks WHERE project_id=$1
				)
			)`,
			`DELETE FROM acceptance_criteria
			WHERE milestone_id IN (
				SELECT id FROM milestones WHERE project_id=$1
			) OR task_id IN (
				SELECT id FROM tasks WHERE project_id=$1
			)`,
			`DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM task_comments WHERE task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM task_activity WHERE task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
			`DELETE FROM project_activity WHERE project_id=$1`,
			`DELETE FROM tasks WHERE project_id=$1`,
			`DELETE FROM milestones WHERE project_id=$1`,
			`DELETE FROM projects WHERE id=$1`,
		}
		for _, statement := range statements {
			_, err := db.Pool.Exec(ctx, statement, projectID)
			require.NoError(t, err)
		}
	})
}

func TestProjectArchiveRequiresConcludedMilestonesAndTasks(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	tasks := store.NewTaskStore(db)
	ctx := context.Background()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}

	project, err := projects.Create(ctx, domain.Project{
		Name: "Task Manager", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)

	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: project.Project.ID, Name: "Project center",
		Outcome: "Project-first workflow is usable", OwnerID: userA, Position: 0,
	})
	require.NoError(t, err)
	task := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Build Project center", CreatorID: userA,
		ProjectID: project.Project.ID, MilestoneID: &milestone.ID,
	}, nil)

	_, err = projects.ApplyLifecycle(
		ctx, project.Project.Number, store.ProjectActionArchive, actor, "",
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	cancelled := domain.TaskStatusCancelled
	_, err = tasks.Update(ctx, task.Task.Number, domain.TaskPatch{Status: &cancelled}, userA)
	require.NoError(t, err)
	_, err = milestones.ApplyLifecycle(
		ctx, project.Project.ID, milestone.ID, store.MilestoneActionCancel, actor, "",
	)
	require.NoError(t, err)

	archived, err := projects.ApplyLifecycle(
		ctx, project.Project.Number, store.ProjectActionArchive, actor, "",
	)
	require.NoError(t, err)
	require.NotNil(t, archived.Project.ArchivedAt)
}

func TestTaskRejectsMilestoneFromAnotherProject(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	tasks := store.NewTaskStore(db)
	ctx := context.Background()

	first, err := projects.Create(ctx, domain.Project{
		Name: "First", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, first.Project.ID)
	second, err := projects.Create(ctx, domain.Project{
		Name: "Second", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, second.Project.ID)
	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: second.Project.ID, Name: "Second milestone",
		Outcome: "Second stage is done", OwnerID: userA, Position: 0,
	})
	require.NoError(t, err)

	_, err = tasks.Create(ctx, domain.Task{
		Title: "Invalid association", CreatorID: userA,
		ProjectID: first.Project.ID, MilestoneID: &milestone.ID,
	}, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvalidInput), "error = %v", err)
}

func TestAcceptanceExternalCheckerRequiresDatabaseReference(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	acceptance := store.NewAcceptanceStore(db)
	ctx := context.Background()

	project, err := projects.Create(ctx, domain.Project{
		Name: "Agent checks", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: project.Project.ID, Name: "Agent-ready checks",
		Outcome: "Agent provenance is retained", OwnerID: userA,
	})
	require.NoError(t, err)
	criterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		MilestoneID: &milestone.ID, Criterion: "Check is attributable",
		VerificationInstructions: "Inspect checker provenance", Position: 0,
	})
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO acceptance_checks
			(id, criterion_id, criterion_revision, outcome, evidence, checker_type)
		VALUES ($1,$2,$3,'passed','Automated output','agent')`,
		uuid.New(), criterion.ID, criterion.Revision)
	require.Error(t, err)
}

func TestRemovingActiveMilestoneCriterionRequiresReasonAndRetainsOne(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	acceptance := store.NewAcceptanceStore(db)
	ctx := context.Background()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}

	project, err := projects.Create(ctx, domain.Project{
		Name: "Scope audit", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: project.Project.ID, Name: "Auditable scope",
		Outcome: "Scope changes remain explainable", OwnerID: userA,
	})
	require.NoError(t, err)
	criterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		MilestoneID: &milestone.ID, Criterion: "Original scope is checked",
		VerificationInstructions: "Review the scope", Position: 0,
	})
	require.NoError(t, err)
	_, err = acceptance.Create(ctx, domain.AcceptanceCriterion{
		MilestoneID: &milestone.ID, Criterion: "Replacement scope is checked",
		VerificationInstructions: "Review the replacement", Position: 1,
	})
	require.NoError(t, err)
	_, err = milestones.ApplyLifecycle(
		ctx, project.Project.ID, milestone.ID, store.MilestoneActionActivate, actor, "",
	)
	require.NoError(t, err)

	err = acceptance.RemoveCriterion(ctx, criterion.ID, actor, "")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	err = acceptance.RemoveCriterion(ctx, criterion.ID, actor, "The requirement was superseded")
	require.NoError(t, err)

	var remaining int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM acceptance_criteria
		WHERE milestone_id=$1 AND archived_at IS NULL`, milestone.ID).Scan(&remaining))
	require.Equal(t, 1, remaining)
}
