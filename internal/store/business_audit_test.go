package store_test

import (
	"context"
	"testing"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/domain"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskOperationProvenanceAndBusinessAuditCommitTogether(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tokenID := createOperationToken(t, db, userA)
	tasks := store.NewTaskStore(db)
	task := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Operation provenance", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, task.Task.ID)
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM business_audit_events WHERE entity_id=$1`, task.Task.ID)
		require.NoError(t, err)
	})
	actor := domain.OperationActor{
		UserID: userA, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Codex nightly",
		RequestID: "req-business-audit",
	}
	title := "Operation provenance updated"

	updated, err := tasks.UpdateWithOperation(
		ctx, task.Task.Number, domain.TaskPatch{Title: &title}, actor,
	)

	require.NoError(t, err)
	require.Equal(t, title, updated.Task.Title)
	var (
		activityRequestID string
		activityMethod    domain.AuthenticationMethod
		activityTokenID   uuid.UUID
		activityTokenName string
	)
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT request_id, auth_method, api_token_id, token_name_snapshot
		FROM task_activity
		WHERE task_id=$1 AND field='title'
		ORDER BY created_at DESC, id DESC LIMIT 1`, task.Task.ID).
		Scan(&activityRequestID, &activityMethod, &activityTokenID, &activityTokenName))
	require.Equal(t, actor.RequestID, activityRequestID)
	require.Equal(t, actor.AuthMethod, activityMethod)
	require.Equal(t, tokenID, activityTokenID)
	require.Equal(t, actor.TokenName, activityTokenName)

	var (
		auditRequestID string
		auditMethod    domain.AuthenticationMethod
		auditTokenID   uuid.UUID
		auditTokenName string
		action         string
	)
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT request_id, auth_method, token_id, token_name, action
		FROM business_audit_events
		WHERE entity_type='task' AND entity_id=$1
		ORDER BY occurred_at DESC, id DESC LIMIT 1`, task.Task.ID).
		Scan(&auditRequestID, &auditMethod, &auditTokenID, &auditTokenName, &action))
	require.Equal(t, actor.RequestID, auditRequestID)
	require.Equal(t, actor.AuthMethod, auditMethod)
	require.Equal(t, tokenID, auditTokenID)
	require.Equal(t, actor.TokenName, auditTokenName)
	require.Equal(t, "updated", action)
}

func TestBusinessAuditInsertFailureRollsBackTaskTransaction(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tasks := store.NewTaskStore(db)
	task := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Rollback audit failure", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, task.Task.ID)
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM business_audit_events WHERE entity_id=$1`, task.Task.ID)
		require.NoError(t, err)
	})
	missingTokenID := uuid.New()
	actor := domain.OperationActor{
		UserID: userA, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &missingTokenID, TokenName: "Missing token",
		RequestID: "req-audit-failure",
	}
	before := task.Task.UpdatedAt
	sameTitle := task.Task.Title

	_, err := tasks.UpdateWithOperation(
		ctx, task.Task.Number, domain.TaskPatch{Title: &sameTitle}, actor,
	)

	require.ErrorContains(t, err, "insert business audit")
	unchanged, getErr := tasks.GetByNumber(ctx, task.Task.Number)
	require.NoError(t, getErr)
	require.Equal(t, task.Task.Title, unchanged.Task.Title)
	require.Equal(t, before, unchanged.Task.UpdatedAt,
		"the business write must roll back when its audit insert fails")
}

func TestProjectOperationProvenanceAndBusinessAuditCommitTogether(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tokenID := createOperationToken(t, db, userA)
	projects := store.NewProjectStore(db)
	project, err := projects.Create(ctx, domain.Project{
		Name: "Audited project", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)
	actor := domain.OperationActor{
		UserID: userA, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Codex nightly",
		RequestID: "req-project-business-audit",
	}
	name := "Audited project updated"

	updated, err := projects.UpdateWithOperation(
		ctx, project.Project.Number, store.ProjectPatch{Name: &name}, actor,
	)

	require.NoError(t, err)
	require.Equal(t, name, updated.Project.Name)
	var (
		activityRequestID string
		activityMethod    domain.AuthenticationMethod
		activityTokenID   uuid.UUID
		activityTokenName string
	)
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT request_id, auth_method, api_token_id, token_name_snapshot
		FROM project_activity
		WHERE project_id=$1 AND action='project_name_changed'
		ORDER BY created_at DESC, id DESC LIMIT 1`, project.Project.ID).
		Scan(&activityRequestID, &activityMethod, &activityTokenID, &activityTokenName))
	require.Equal(t, actor.RequestID, activityRequestID)
	require.Equal(t, actor.AuthMethod, activityMethod)
	require.Equal(t, tokenID, activityTokenID)
	require.Equal(t, actor.TokenName, activityTokenName)

	var auditCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM business_audit_events
		WHERE entity_type='project' AND entity_id=$1
		  AND request_id=$2 AND action='updated'`,
		project.Project.ID, actor.RequestID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestSecondaryWorkMutationsRecordBusinessAudit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tokenID := createOperationToken(t, db, userA)
	actor := domain.OperationActor{
		UserID: userA, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: "Codex nightly",
		RequestID: "req-secondary-business-audit",
	}

	tasks := store.NewTaskStore(db)
	task := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Secondary audit owners", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, task.Task.ID)

	projects := store.NewProjectStore(db)
	project, err := projects.Create(ctx, domain.Project{
		Name: "Secondary audit project", OwnerID: userA, CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, project.Project.ID)

	comment, err := store.NewCommentStore(db).CreateWithOperation(
		ctx, task.Task.ID, userA, "Audited comment", actor,
	)
	require.NoError(t, err)

	label, err := store.NewLabelStore(db).CreateWithOperation(ctx, "Audited label "+uuid.NewString(), actor)
	require.NoError(t, err)
	cleanupLabel(t, db, label.ID)

	milestone, err := store.NewMilestoneStore(db).CreateWithOperation(ctx, domain.Milestone{
		ProjectID: project.Project.ID, Name: "Audited milestone",
		Outcome: "Milestone writes are attributable", OwnerID: userA, Position: 0,
	}, actor)
	require.NoError(t, err)

	criterion, err := store.NewAcceptanceStore(db).CreateWithOperation(
		ctx,
		domain.AcceptanceCriterion{
			TaskID: &task.Task.ID, Criterion: "Audit exists",
			VerificationInstructions: "Query the business audit event",
			Revision:                 1,
		},
		actor,
	)
	require.NoError(t, err)
	check, err := store.NewAcceptanceStore(db).AddCheckWithOperation(
		ctx,
		domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome: domain.AcceptanceOutcomePassed, Evidence: "Verified by integration test",
			Checker: domain.Actor{Type: domain.ActorTypeUser, UserID: &actor.UserID},
		},
		actor,
	)
	require.NoError(t, err)

	expected := map[string]uuid.UUID{
		"comment": comment.ID, "label": label.ID, "milestone": milestone.ID,
		"acceptance_criterion": criterion.ID, "acceptance_check": check.ID,
	}
	for entityType, entityID := range expected {
		var (
			requestID string
			method    domain.AuthenticationMethod
			token     uuid.UUID
		)
		require.NoError(t, db.Pool.QueryRow(ctx, `
			SELECT request_id, auth_method, token_id
			FROM business_audit_events
			WHERE entity_type=$1 AND entity_id=$2
			ORDER BY occurred_at DESC, id DESC LIMIT 1`,
			entityType, entityID,
		).Scan(&requestID, &method, &token))
		require.Equal(t, actor.RequestID, requestID, entityType)
		require.Equal(t, actor.AuthMethod, method, entityType)
		require.Equal(t, tokenID, token, entityType)
	}
}

func createOperationToken(t *testing.T, db *store.DB, userID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	token := access.Token{
		ID: uuid.New(), UserID: userID, Name: "Business audit fixture",
		SecretHash:    access.HashSecret([]byte(uuid.NewString())),
		DisplayPrefix: "bb_pat_audit", Scopes: []access.Scope{access.ScopeWorkWrite},
		ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}
	require.NoError(t, store.NewAccessStore(db).CreateToken(context.Background(), token))
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM business_audit_events WHERE token_id=$1`, token.ID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, err)
	})
	return token.ID
}
