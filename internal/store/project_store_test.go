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
			`DELETE FROM acceptance_checks
			WHERE criterion_id IN (
				SELECT id FROM acceptance_criteria
				WHERE project_id=$1 OR milestone_id IN (
					SELECT id FROM milestones WHERE project_id=$1
				)
			)`,
			`DELETE FROM acceptance_criteria
			WHERE project_id=$1 OR milestone_id IN (
				SELECT id FROM milestones WHERE project_id=$1
			)`,
			`DELETE FROM project_activity WHERE project_id=$1`,
			`UPDATE tasks SET project_id=NULL, milestone_id=NULL WHERE project_id=$1`,
			`DELETE FROM milestones WHERE project_id=$1`,
			`DELETE FROM projects WHERE id=$1`,
		}
		for _, statement := range statements {
			_, err := db.Pool.Exec(ctx, statement, projectID)
			require.NoError(t, err)
		}
	})
}

func TestProjectLifecycleUsesAcceptanceMilestonesAndTasks(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	acceptance := store.NewAcceptanceStore(db)
	tasks := store.NewTaskStore(db)
	ctx := context.Background()

	project, err := projects.Create(ctx, domain.Project{
		Name: "Reduce latency", Outcome: "P95 is below 250ms",
		OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)

	projectCriterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		ProjectID: &project.Project.ID, Criterion: "P95 is below 250ms",
		VerificationInstructions: "Run the production benchmark", Position: 0,
	})
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	_, err = projects.ApplyLifecycle(ctx, project.Project.Number, store.ProjectActionActivate, actor, "")
	require.NoError(t, err)

	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: project.Project.ID, Name: "Benchmark ready",
		Outcome: "The benchmark is repeatable", Position: 0,
	})
	require.NoError(t, err)
	milestoneCriterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		MilestoneID: &milestone.ID, Criterion: "Benchmark is repeatable",
		VerificationInstructions: "Run it twice", Position: 0,
	})
	require.NoError(t, err)

	task := mustCreateTask(t, tasks, domain.Task{
		Title: "Build benchmark", CreatorID: userA, Status: domain.TaskStatusDone,
		ProjectID: &project.Project.ID, MilestoneID: &milestone.ID,
	}, nil)
	cleanupTask(t, db, task.Task.ID)

	for _, criterion := range []domain.AcceptanceCriterion{projectCriterion, milestoneCriterion} {
		_, err = acceptance.AddCheck(ctx, domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome: domain.AcceptanceOutcomePassed, Evidence: "Verified output",
			Checker: actor,
		})
		require.NoError(t, err)
	}

	_, err = milestones.ApplyLifecycle(ctx, project.Project.ID, milestone.ID, store.MilestoneActionComplete, actor, "")
	require.NoError(t, err)
	completed, err := projects.ApplyLifecycle(ctx, project.Project.Number, store.ProjectActionComplete, actor, "")
	require.NoError(t, err)
	require.Equal(t, domain.ProjectStatusCompleted, completed.Project.Status)
	require.Equal(t, 1, completed.CompletedTasks)
	require.Equal(t, 1, completed.EligibleTasks)
	require.Equal(t, 1, completed.SatisfiedCriteria)
}

func TestTaskRejectsMilestoneFromAnotherProject(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	tasks := store.NewTaskStore(db)
	ctx := context.Background()

	first, err := projects.Create(ctx, domain.Project{
		Name: "First", Outcome: "First outcome", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, first.Project.ID)
	second, err := projects.Create(ctx, domain.Project{
		Name: "Second", Outcome: "Second outcome", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, second.Project.ID)
	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: second.Project.ID, Name: "Second milestone",
		Outcome: "Second stage is done", Position: 0,
	})
	require.NoError(t, err)

	_, err = tasks.Create(ctx, domain.Task{
		Title: "Invalid association", CreatorID: userA,
		ProjectID: &first.Project.ID, MilestoneID: &milestone.ID,
	}, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvalidInput), "error = %v", err)
}

func TestAcceptanceExternalCheckerRequiresDatabaseReference(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	acceptance := store.NewAcceptanceStore(db)
	ctx := context.Background()

	project, err := projects.Create(ctx, domain.Project{
		Name: "Agent checks", Outcome: "Agent provenance is retained",
		OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	criterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		ProjectID: &project.Project.ID, Criterion: "Check is attributable",
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

func TestRemovingActiveAcceptanceCriterionRequiresReasonAndKeepsHistory(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	acceptance := store.NewAcceptanceStore(db)
	ctx := context.Background()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}

	project, err := projects.Create(ctx, domain.Project{
		Name: "Scope audit", Outcome: "Scope changes remain explainable",
		OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	criterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		ProjectID: &project.Project.ID, Criterion: "Original scope is checked",
		VerificationInstructions: "Review the scope", Position: 0,
	})
	require.NoError(t, err)
	_, err = acceptance.Create(ctx, domain.AcceptanceCriterion{
		ProjectID: &project.Project.ID, Criterion: "Replacement scope is checked",
		VerificationInstructions: "Review the replacement", Position: 1,
	})
	require.NoError(t, err)
	_, err = projects.ApplyLifecycle(ctx, project.Project.Number, store.ProjectActionActivate, actor, "")
	require.NoError(t, err)

	err = acceptance.RemoveCriterion(ctx, criterion.ID, actor, "")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	err = acceptance.RemoveCriterion(ctx, criterion.ID, actor, "The requirement was superseded")
	require.NoError(t, err)

	var archived bool
	err = db.Pool.QueryRow(ctx,
		`SELECT archived_at IS NOT NULL FROM acceptance_criteria WHERE id=$1`, criterion.ID).
		Scan(&archived)
	require.NoError(t, err)
	require.True(t, archived)
}
