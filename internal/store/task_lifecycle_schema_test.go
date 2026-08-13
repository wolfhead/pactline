package store_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wolfhead/pactline"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func migrationsBeforeTaskLifecycle(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	for _, name := range embeddedMigrationNames(t) {
		if name >= "0026_" {
			continue
		}
		body, err := fs.ReadFile(pactline.MigrationFS, "migrations/"+name)
		require.NoError(t, err)
		out["migrations/"+name] = &fstest.MapFile{Data: body}
	}
	return out
}

func TestTaskLifecycleMigrationBackfillsHistoryAndInitializesNewTasks(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))
	db, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.Migrate(ctx, migrationsBeforeTaskLifecycle(t)))
	projectID := uuid.New()
	legacyTaskID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO projects (id,name,description,creator_id)
		VALUES ($1,'Lifecycle migration','','00000000-0000-0000-0000-000000000001')`,
		projectID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO tasks (
			id,title,context,expected_result,status,priority,execution_mode,
			creator_id,project_id
		) VALUES (
			$1,'Historical Task','Existing context','Existing outcome',
			'todo','none','human_only',
			'00000000-0000-0000-0000-000000000001',$2
		)`, legacyTaskID, projectID)
	require.NoError(t, err)

	require.NoError(t, db.Migrate(ctx, pactline.MigrationFS))

	var phase string
	var activity *string
	var reviewCycle int64
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT phase,activity_state,review_cycle
		FROM tasks WHERE id=$1`, legacyTaskID).Scan(&phase, &activity, &reviewCycle))
	require.Equal(t, "backlog", phase)
	require.Nil(t, activity)
	require.Zero(t, reviewCycle)

	var legacyMainThreads int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM task_threads WHERE task_id=$1 AND role='main'`,
		legacyTaskID,
	).Scan(&legacyMainThreads))
	require.Equal(t, 1, legacyMainThreads)

	newTaskID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO tasks (
			id,title,context,expected_result,status,priority,execution_mode,
			creator_id,project_id
		) VALUES (
			$1,'New Task','New context','New outcome',
			'todo','none','human_only',
			'00000000-0000-0000-0000-000000000001',$2
		)`, newTaskID, projectID)
	require.NoError(t, err)

	var newPhase string
	var newActivity *string
	var newReviewCycle int64
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT phase,activity_state,review_cycle
		FROM tasks WHERE id=$1`, newTaskID).
		Scan(&newPhase, &newActivity, &newReviewCycle))
	require.Equal(t, "backlog", newPhase)
	require.Nil(t, newActivity)
	require.Zero(t, newReviewCycle)

	var newMainThreads int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM task_threads WHERE task_id=$1 AND role='main'`,
		newTaskID,
	).Scan(&newMainThreads))
	require.Equal(t, 1, newMainThreads)
}

func TestTaskLifecycleBackfillReconcilesObservedLegacyShape(t *testing.T) {
	ctx := context.Background()
	dsn := createThrowawayDatabase(t, testDSN(t))
	db, err := store.Connect(ctx, dsn)
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.Migrate(ctx, migrationsBeforeTaskLifecycle(t)))
	const (
		userID      = "00000000-0000-0000-0000-000000000001"
		projectID   = "10000000-0000-4000-8000-000000000001"
		doneTaskID  = "20000000-0000-4000-8000-000000000002"
		tokenID     = "30000000-0000-4000-8000-000000000001"
		claimID     = "40000000-0000-4000-8000-000000000001"
		criterionID = "50000000-0000-4000-8000-000000000001"
	)
	fixtureSQL := `
		INSERT INTO projects (id,name,description,creator_id)
		VALUES ($3,'Migration fixture','',$2);

		INSERT INTO tasks (
			id,number,title,context,expected_result,status,priority,
			assignee_id,creator_id,project_id,execution_mode,
			created_at,updated_at,completed_at
		) VALUES (
			$1,2,'Completed fixture','Context','Expected result','done','medium',
			$2,$2,$3,'agent_allowed',
			'2026-07-31 09:00:00+00','2026-07-31 09:33:34+00','2026-07-31 09:33:34+00'
		);

		INSERT INTO tasks (
			id,number,title,context,expected_result,status,priority,
			creator_id,project_id,execution_mode,created_at,updated_at
		)
		SELECT
			('21000000-0000-4000-8000-' || lpad(item::text,12,'0'))::uuid,
			CASE item WHEN 1 THEN 1 ELSE item + 1 END,
			'Backlog fixture ' || item,'Context','Expected result','todo','none',
			$2,$3,'human_only','2026-07-31 08:00:00+00','2026-07-31 08:00:00+00'
		FROM generate_series(1,4) item;

		INSERT INTO api_tokens (
			id,user_id,name,secret_hash,display_prefix,scopes,expires_at,created_at
		) VALUES (
			$4,$2,'migration-fixture',decode(repeat('01',32),'hex'),'pact_fixture',
			ARRAY['work:read','work:execute'],
			'2027-07-31 09:00:00+00','2026-07-31 08:59:00+00'
		);

		INSERT INTO task_claims (
			id,task_id,claimed_by_user_id,claimed_via_token_id,
			token_name_snapshot,client_kind,client_session_id,status,version,
			expires_at,created_at,updated_at,completed_at
		) VALUES (
			$5,$1,$2,$4,'migration-fixture','codex','fixture-session','submitted',2,
			'2026-08-07 09:00:27+00','2026-07-31 09:00:27+00',
			'2026-07-31 09:01:30+00','2026-07-31 09:01:30+00'
		);

		INSERT INTO task_claim_messages (
			id,claim_id,author_type,author_user_id,kind,body,request_id,
			api_token_id,token_name_snapshot,created_at
		) VALUES
			('60000000-0000-4000-8000-000000000001',$5,'agent',$2,'progress',
			 'Progress one','fixture-progress-1',$4,'migration-fixture','2026-07-31 09:00:42+00'),
			('60000000-0000-4000-8000-000000000002',$5,'agent',$2,'progress',
			 'Progress two','fixture-progress-2',$4,'migration-fixture','2026-07-31 09:01:10+00'),
			('60000000-0000-4000-8000-000000000003',$5,'agent',$2,'submission',
			 'Submitted work','fixture-submission',$4,'migration-fixture','2026-07-31 09:01:30+00');

		INSERT INTO task_comments (
			id,task_id,author_id,body,created_at,updated_at,version,thread_root_id
		) VALUES (
			'70000000-0000-4000-8000-000000000001',$1,$2,'Review discussion',
			'2026-07-31 09:20:00+00','2026-07-31 09:20:00+00',1,
			'70000000-0000-4000-8000-000000000001'
		);

		INSERT INTO task_activity (id,task_id,actor_id,field,old_value,new_value,created_at)
		VALUES
			('80000000-0000-4000-8000-000000000001',$1,$2,'status','todo','in_progress','2026-07-31 09:00:27+00'),
			('80000000-0000-4000-8000-000000000002',$1,$2,'status','in_progress','in_review','2026-07-31 09:01:30+00'),
			('80000000-0000-4000-8000-000000000003',$1,$2,'status','in_review','done','2026-07-31 09:33:34+00');

		INSERT INTO acceptance_criteria (
			id,task_id,criterion,verification_instructions,revision,position,
			created_at,updated_at
		) VALUES (
			$6,$1,'Fixture criterion','Verify fixture',1,0,
			'2026-07-31 08:59:00+00','2026-07-31 08:59:00+00'
		);

		INSERT INTO acceptance_checks (
			id,criterion_id,criterion_revision,outcome,evidence,checker_type,
			checked_by_user_id,checker_ref,checked_at
		) VALUES
			('90000000-0000-4000-8000-000000000001',$6,1,'passed','Agent evidence',
			 'agent',NULL,'api-token/migration-fixture','2026-07-31 09:01:16+00'),
			('90000000-0000-4000-8000-000000000002',$6,1,'passed','Human evidence',
			 'user',$2,NULL,'2026-07-31 09:33:27+00');
	`
	fixtureSQL = strings.NewReplacer(
		"$1", "'"+doneTaskID+"'::uuid",
		"$2", "'"+userID+"'::uuid",
		"$3", "'"+projectID+"'::uuid",
		"$4", "'"+tokenID+"'::uuid",
		"$5", "'"+claimID+"'::uuid",
		"$6", "'"+criterionID+"'::uuid",
	).Replace(fixtureSQL)
	fixtureConn, err := db.Pool.Acquire(ctx)
	require.NoError(t, err)
	defer fixtureConn.Release()
	_, err = fixtureConn.Conn().PgConn().Exec(ctx, fixtureSQL).ReadAll()
	require.NoError(t, err)

	require.NoError(t, db.Migrate(ctx, pactline.MigrationFS))

	var classifiedTasks, mainThreads, issueThreads, threadItems int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE phase IS NOT NULL`).Scan(&classifiedTasks))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM task_threads WHERE role='main'`).Scan(&mainThreads))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM task_threads WHERE role='issue'`).Scan(&issueThreads))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM task_thread_items`).Scan(&threadItems))
	require.Equal(t, 5, classifiedTasks)
	require.Equal(t, 5, mainThreads)
	require.Zero(t, issueThreads)
	require.Equal(t, 4, threadItems)

	var backlogTasks, doneTasks int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE phase='backlog' AND review_cycle=0`).Scan(&backlogTasks))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE phase='done' AND review_cycle=1`).Scan(&doneTasks))
	require.Equal(t, 4, backlogTasks)
	require.Equal(t, 1, doneTasks)

	rows, err := db.Pool.Query(ctx, `SELECT stage,status,outcome,count(*) FROM task_stage_claims GROUP BY 1,2,3 ORDER BY 1`)
	require.NoError(t, err)
	defer rows.Close()
	claimShapes := map[string]int{}
	for rows.Next() {
		var stage, status, outcome string
		var count int
		require.NoError(t, rows.Scan(&stage, &status, &outcome, &count))
		claimShapes[stage+"/"+status+"/"+outcome] = count
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]int{
		"execution/completed/work_submitted": 1,
		"review/completed/task_accepted":     1,
	}, claimShapes)

	var executionChecks, acceptanceChecks int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE purpose='execution_verification' AND task_review_cycle=0),
			count(*) FILTER (WHERE purpose='acceptance' AND task_review_cycle=1)
		FROM acceptance_checks`).Scan(&executionChecks, &acceptanceChecks))
	require.Equal(t, 1, executionChecks)
	require.Equal(t, 1, acceptanceChecks)

	var progressItems, submissionItems, messageItems int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE kind='progress'),
			count(*) FILTER (WHERE kind='work_submission'),
			count(*) FILTER (WHERE kind='message')
		FROM task_thread_items`).Scan(&progressItems, &submissionItems, &messageItems))
	require.Equal(t, 2, progressItems)
	require.Equal(t, 1, submissionItems)
	require.Equal(t, 1, messageItems)
}
