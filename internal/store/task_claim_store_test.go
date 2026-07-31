package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskClaimStoreRunsQuestionAnswerAndSubmitWorkflow(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	token := createTaskClaimToken(t, db, userA, now)
	task := createAgentReadyTask(t, db, userA)
	claims := store.NewTaskClaimStore(db)
	acceptance := store.NewAcceptanceStore(db)
	actor := taskClaimActor(userA, token, "claim-workflow")
	criterion, err := acceptance.Create(ctx, domain.AcceptanceCriterion{
		TaskID: &task.Task.ID, Criterion: "The implementation is verified",
		VerificationInstructions: "Run the focused regression test", Position: 0,
	})
	require.NoError(t, err)

	claim, err := claims.Claim(ctx, task.Task.Number, "codex", "thread-A", actor, now)
	require.NoError(t, err)
	cleanupTaskClaimAudit(t, db, claim.ID)
	require.Equal(t, domain.TaskClaimStatusActive, claim.Status)
	require.Equal(t, int64(1), claim.Version)
	currentTask, err := store.NewTaskStore(db).GetByNumber(ctx, task.Task.Number)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusInProgress, currentTask.Task.Status)
	require.NotNil(t, currentTask.AgentWork)
	require.Equal(t, claim.ID, currentTask.AgentWork.ClaimID)
	require.Equal(t, domain.TaskClaimStatusActive, currentTask.AgentWork.Status)
	require.Equal(t, token.Name, currentTask.AgentWork.TokenName)
	require.Equal(t, "codex", currentTask.AgentWork.ClientKind)

	waiting, question, err := claims.Ask(
		ctx,
		claim.ID,
		claim.Version,
		"codex",
		"thread-A",
		"Should the API preserve the old response shape?",
		actor,
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStatusWaitingHuman, waiting.Status)
	require.Equal(t, domain.TaskClaimMessageQuestion, question.Kind)
	currentTask, err = store.NewTaskStore(db).GetByNumber(ctx, task.Task.Number)
	require.NoError(t, err)
	require.NotNil(t, currentTask.AgentWork)
	require.Equal(t, domain.TaskClaimStatusWaitingHuman, currentTask.AgentWork.Status)

	answerActor := domain.SessionOperation(userA, "answer-workflow")
	active, answer, err := claims.Answer(
		ctx,
		claim.ID,
		waiting.Version,
		"Preserve it for existing clients.",
		answerActor,
		now.Add(2*time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStatusActive, active.Status)
	require.Equal(t, domain.TaskClaimMessageAnswer, answer.Kind)
	require.Equal(t, &question.ID, answer.ReplyToID)

	_, err = acceptance.AddClaimCheckVersioned(
		ctx,
		claim.ID,
		"codex",
		"thread-A",
		criterion.ID,
		criterion.Version,
		domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome: domain.AcceptanceOutcomePassed, Evidence: "Focused regression passed",
			Checker: domain.Actor{Type: domain.ActorTypeAgent, Ref: token.Name},
		},
		actor,
	)
	require.NoError(t, err)

	submitted, submission, err := claims.Submit(
		ctx,
		claim.ID,
		active.Version,
		"codex",
		"thread-A",
		"Verified the expected result with focused tests.",
		actor,
		now.Add(3*time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStatusSubmitted, submitted.Status)
	require.Equal(t, domain.TaskClaimMessageSubmission, submission.Kind)
	currentTask, err = store.NewTaskStore(db).GetByNumber(ctx, task.Task.Number)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusInReview, currentTask.Task.Status)
	require.NotNil(t, currentTask.AgentWork)
	require.Equal(t, domain.TaskClaimStatusSubmitted, currentTask.AgentWork.Status)
	require.NotNil(t, currentTask.AgentWork.CompletedAt)

	done := domain.TaskStatusDone
	_, err = store.NewTaskStore(db).Update(
		ctx, task.Task.Number, domain.TaskPatch{Status: &done}, userA,
	)
	require.ErrorIs(t, err, domain.ErrConflict,
		"an Agent self-check is evidence for review, not human acceptance")

	_, err = acceptance.AddCheck(ctx, domain.AcceptanceCheck{
		CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
		Outcome:   domain.AcceptanceOutcomePassed,
		Evidence:  "Reviewed the Agent submission and reproduced the result",
		Checker:   domain.Actor{Type: domain.ActorTypeUser, UserID: &userA},
		CheckedAt: now.Add(4 * time.Hour),
	})
	require.NoError(t, err)
	completed, err := store.NewTaskStore(db).Update(
		ctx, task.Task.Number, domain.TaskPatch{Status: &done}, userA,
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusDone, completed.Task.Status)
	require.Nil(t, completed.AgentWork,
		"a submitted Claim is only task-list state while the Task remains in review")

	messages, err := claims.ListMessages(ctx, claim.ID)
	require.NoError(t, err)
	require.Equal(
		t,
		[]domain.TaskClaimMessageKind{
			domain.TaskClaimMessageQuestion,
			domain.TaskClaimMessageAnswer,
			domain.TaskClaimMessageSubmission,
		},
		taskClaimMessageKinds(messages),
	)
	_, err = claims.GetCurrent(ctx, userA, "codex", "thread-A")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTaskClaimStoreEnforcesTaskAndSessionExclusivity(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	token := createTaskClaimToken(t, db, userA, now)
	task := createAgentReadyTask(t, db, userA)
	claims := store.NewTaskClaimStore(db)

	type result struct {
		claim domain.TaskClaim
		err   error
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, sessionID := range []string{"thread-A", "thread-B"} {
		go func(session string) {
			start.Wait()
			actor := taskClaimActor(userA, token, "race-"+session)
			claim, err := claims.Claim(ctx, task.Task.Number, "codex", session, actor, now)
			results <- result{claim: claim, err: err}
		}(sessionID)
	}
	start.Done()

	var won domain.TaskClaim
	var failures int
	for range 2 {
		result := <-results
		if result.err == nil {
			won = result.claim
		} else {
			require.ErrorIs(t, result.err, domain.ErrConflict)
			failures++
		}
	}
	require.NotEqual(t, uuid.Nil, won.ID)
	require.Equal(t, 1, failures)
	cleanupTaskClaimAudit(t, db, won.ID)

	secondTask := createAgentReadyTask(t, db, userA)
	_, err := claims.Claim(
		ctx,
		secondTask.Task.Number,
		"codex",
		won.ClientSessionID,
		taskClaimActor(userA, token, "second-task"),
		now,
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	_, err = claims.Extend(
		ctx,
		won.ID,
		won.Version,
		"codex",
		"different-thread",
		taskClaimActor(userA, token, "wrong-thread"),
		now.Add(time.Hour),
	)
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestTaskClaimStoreReleaseAndExpiryReturnOnlyInProgressTaskToTodo(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	token := createTaskClaimToken(t, db, userA, now)
	claims := store.NewTaskClaimStore(db)
	tasks := store.NewTaskStore(db)

	releasedTask := createAgentReadyTask(t, db, userA)
	actor := taskClaimActor(userA, token, "release")
	claim, err := claims.Claim(
		ctx, releasedTask.Task.Number, "codex", "release-thread", actor, now,
	)
	require.NoError(t, err)
	cleanupTaskClaimAudit(t, db, claim.ID)
	released, err := claims.Release(
		ctx,
		claim.ID,
		claim.Version,
		"codex",
		"release-thread",
		"Local dependency is unavailable.",
		actor,
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStatusReleased, released.Status)
	current, err := tasks.GetByNumber(ctx, releasedTask.Task.Number)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusTodo, current.Task.Status)
	require.Nil(t, current.AgentWork, "released Claims must not appear as current Agent work")

	humanChangedTask := createAgentReadyTask(t, db, userA)
	claim, err = claims.Claim(
		ctx, humanChangedTask.Task.Number, "codex", "human-change-thread", actor, now,
	)
	require.NoError(t, err)
	cleanupTaskClaimAudit(t, db, claim.ID)
	cancelled := domain.TaskStatusCancelled
	current, err = tasks.GetByNumber(ctx, humanChangedTask.Task.Number)
	require.NoError(t, err)
	_, err = tasks.UpdateVersionedWithOperation(
		ctx,
		humanChangedTask.Task.Number,
		current.Task.Version,
		domain.TaskPatch{Status: &cancelled},
		domain.SessionOperation(userA, "human-cancel"),
	)
	require.NoError(t, err)
	_, err = claims.Release(
		ctx,
		claim.ID,
		claim.Version,
		"codex",
		"human-change-thread",
		"",
		actor,
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	current, err = tasks.GetByNumber(ctx, humanChangedTask.Task.Number)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusCancelled, current.Task.Status)

	expiredTask := createAgentReadyTask(t, db, userA)
	claim, err = claims.Claim(
		ctx, expiredTask.Task.Number, "codex", "expiry-thread", actor, now,
	)
	require.NoError(t, err)
	cleanupTaskClaimAudit(t, db, claim.ID)
	_, err = db.Pool.Exec(ctx, `
		UPDATE task_claims SET expires_at=$2 WHERE id=$1`,
		claim.ID,
		now.Add(2*time.Hour),
	)
	require.NoError(t, err)
	_, err = claims.AddProgress(
		ctx,
		claim.ID,
		"codex",
		"expiry-thread",
		"This must not be accepted after the deadline.",
		actor,
		now.Add(2*time.Hour),
	)
	require.ErrorIs(t, err, domain.ErrConflict)
	count, err := claims.ExpireDue(ctx, now.Add(2*time.Hour), 10)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	current, err = tasks.GetByNumber(ctx, expiredTask.Task.Number)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusTodo, current.Task.Status)
	stored, err := claims.Get(ctx, claim.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStatusExpired, stored.Status)
}

func TestTaskClaimStoreRejectsFirstPartyAgentRelease(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	token := createTaskClaimToken(t, db, userA, now)
	task := createAgentReadyTask(t, db, userA)
	claims := store.NewTaskClaimStore(db)
	claim, err := claims.Claim(
		ctx,
		task.Task.Number,
		"codex",
		"thread-A",
		taskClaimActor(userA, token, "claim-before-delegate-release"),
		now,
	)
	require.NoError(t, err)
	cleanupTaskClaimAudit(t, db, claim.ID)

	runID := uuid.New()
	_, err = claims.Release(
		ctx,
		claim.ID,
		claim.Version,
		"",
		"",
		"",
		domain.OperationActor{
			UserID: userA, AuthMethod: domain.AuthenticationMethodAgentDelegate,
			AgentRunID: &runID, RequestID: "delegate-release",
		},
		now.Add(time.Hour),
	)
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func createTaskClaimToken(
	t *testing.T,
	db *store.DB,
	userID uuid.UUID,
	now time.Time,
) access.Token {
	t.Helper()
	token := access.Token{
		ID: uuid.New(), UserID: userID, Name: "Codex worker",
		SecretHash:    access.HashSecret([]byte(uuid.NewString())),
		DisplayPrefix: "bb_pat_claim", Scopes: []access.Scope{access.ScopeWorkWrite},
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
	}
	require.NoError(t, store.NewAccessStore(db).CreateToken(context.Background(), token))
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `
			DELETE FROM business_audit_events WHERE token_id=$1`,
			token.ID,
		)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `
			DELETE FROM task_activity WHERE api_token_id=$1`,
			token.ID,
		)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `
			DELETE FROM task_claims WHERE claimed_via_token_id=$1`,
			token.ID,
		)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, err)
	})
	return token
}

func createAgentReadyTask(
	t *testing.T,
	db *store.DB,
	userID uuid.UUID,
) store.TaskWithRelations {
	t.Helper()
	task := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title:         "Agent-ready Task " + uuid.NewString(),
		CreatorID:     userID,
		AssigneeID:    &userID,
		ExecutionMode: domain.TaskExecutionModeAgentAllowed,
	}, nil)
	cleanupTask(t, db, task.Task.ID)
	return task
}

func taskClaimActor(
	userID uuid.UUID,
	token access.Token,
	requestID string,
) domain.OperationActor {
	tokenID := token.ID
	return domain.OperationActor{
		UserID: userID, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: token.Name, RequestID: requestID,
	}
}

func cleanupTaskClaimAudit(t *testing.T, db *store.DB, claimID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `
			DELETE FROM business_audit_events
			WHERE (entity_type='task_claim' AND entity_id=$1)
			   OR (
					entity_type='task_claim_message'
					AND new_value->>'claim_id'=$1::text
			   )`,
			claimID,
		)
		require.NoError(t, err)
	})
}

func taskClaimMessageKinds(
	messages []domain.TaskClaimMessage,
) []domain.TaskClaimMessageKind {
	kinds := make([]domain.TaskClaimMessageKind, len(messages))
	for i, message := range messages {
		kinds[i] = message.Kind
	}
	return kinds
}
