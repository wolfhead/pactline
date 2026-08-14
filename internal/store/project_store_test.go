package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func cleanupProject(t *testing.T, db *store.DB, projectID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		statements := []string{
			`DELETE FROM business_audit_events
			WHERE entity_type='project_repository' AND entity_id IN (
				SELECT id FROM project_repositories WHERE project_id=$1
			)`,
			`DELETE FROM business_audit_events
			WHERE entity_type='task_merge_request' AND entity_id IN (
				SELECT id FROM task_merge_requests WHERE project_id=$1
			)`,
			`DELETE FROM business_audit_events
			WHERE entity_type='project_membership' AND entity_id IN (
				SELECT id FROM project_memberships WHERE project_id=$1
			)`,
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
			`DELETE FROM task_merge_requests WHERE project_id=$1`,
			`DELETE FROM project_repositories WHERE project_id=$1`,
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

func TestProjectMembershipsProtectLastActiveAdministrator(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	projects := store.NewProjectStore(db)
	memberships := store.NewProjectMembershipStore(db)

	project, err := projects.Create(ctx, domain.Project{
		Name: "Membership invariants", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)

	creatorMembership, err := memberships.Get(ctx, project.Project.ID, userA)
	require.NoError(t, err)
	require.Equal(t, domain.ProjectRoleAdmin, creatorMembership.Role)

	_, err = memberships.ChangeRole(
		ctx, project.Project.ID, userA, domain.ProjectRoleMember, 1,
		domain.SessionOperation(userA, "membership-test"),
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	var wasActive bool
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT active FROM users WHERE id=$1`, userB).Scan(&wasActive))
	if !wasActive {
		require.NoError(t, store.NewUserStore(db).SetActive(ctx, userB, true))
		t.Cleanup(func() {
			require.NoError(t, store.NewUserStore(db).SetActive(context.Background(), userB, false))
		})
	}

	added, err := memberships.Add(
		ctx, project.Project.ID, userB, domain.ProjectRoleAdmin, 1,
		domain.SessionOperation(userA, "membership-test"),
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), added.ProjectVersion)

	demoted, err := memberships.ChangeRole(
		ctx, project.Project.ID, userA, domain.ProjectRoleMember, 2,
		domain.SessionOperation(userA, "membership-test"),
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), demoted.ProjectVersion)

	_, err = memberships.Remove(
		ctx, project.Project.ID, userB, 3,
		domain.SessionOperation(userA, "membership-test"),
	)
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestProjectArchiveRequiresConcludedMilestonesAndTasks(t *testing.T) {
	db := newTestDB(t)
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	ctx := context.Background()
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}

	project, err := projects.Create(ctx, domain.Project{
		Name: "Task Manager", CreatorID: userA,
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

	_, err = workflow.CancelTask(
		ctx, task.Task.Number, task.Task.Version, "Work is no longer required.", actor,
		domain.SessionOperation(userA, "cancel-project-task"), time.Now().UTC(),
	)
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
		Name: "First", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, first.Project.ID)
	second, err := projects.Create(ctx, domain.Project{
		Name: "Second", CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, second.Project.ID)
	milestone, err := milestones.Create(ctx, domain.Milestone{
		ProjectID: second.Project.ID, Name: "Second milestone",
		Outcome: "Second stage is done", OwnerID: userA, Position: 0,
	})
	require.NoError(t, err)

	_, err = tasks.Create(ctx, domain.Task{
		Title: "Invalid association", Context: "Context", ExpectedResult: "Result",
		CreatorID: userA,
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
		Name: "Agent checks", CreatorID: userA,
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
		Name: "Scope audit", CreatorID: userA,
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
