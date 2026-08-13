package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskWorkflowSupportsHumanExecutionAndAgentAcceptance(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	threads := store.NewTaskThreadStore(db)
	claims := store.NewTaskStageClaimStore(db)
	token := createTaskClaimToken(t, db, userA, now)

	created := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Actor-neutral workflow", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	require.Equal(t, domain.TaskPhaseBacklog, created.Task.Phase)
	require.Empty(t, created.Task.Activity)
	require.Zero(t, created.Task.ReviewCycle)

	criterion, err := store.NewAcceptanceStore(db).Create(ctx, domain.AcceptanceCriterion{
		TaskID: &created.Task.ID, Criterion: "Outcome is demonstrably correct",
		VerificationInstructions: "Run the focused workflow test", Position: 0,
	})
	require.NoError(t, err)

	humanOperation := domain.SessionOperation(userA, "human-mark-ready")
	ready, err := workflow.MarkReady(ctx, created.Task.Number, created.Task.Version, humanOperation, now)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseReady, ready.Lifecycle.Phase)

	humanActor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	humanOperation = domain.SessionOperation(userA, "human-claim")
	working, executionClaim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, humanActor, humanOperation,
		"browser", "", now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseInProgress, working.Lifecycle.Phase)
	require.Equal(t, domain.TaskActivityWorking, working.Lifecycle.Activity)
	require.Equal(t, domain.TaskClaimStageExecution, executionClaim.Stage)
	require.Equal(t, humanActor, executionClaim.ClaimedBy)

	_, err = workflow.RecordAcceptanceCheck(
		ctx, created.Task.Number, executionClaim.ID,
		working.Version, executionClaim.Version,
		domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome: domain.AcceptanceOutcomePassed, Evidence: "Focused test passed",
		},
		humanActor, domain.SessionOperation(userA, "human-check"), now.Add(2*time.Minute),
	)
	require.NoError(t, err)

	reviewAvailable, submittedClaim, err := workflow.SubmitWork(
		ctx, created.Task.Number, executionClaim.ID,
		working.Version, executionClaim.Version,
		"Implementation and focused test are ready for acceptance.",
		humanActor, domain.SessionOperation(userA, "human-submit"), now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseInReview, reviewAvailable.Lifecycle.Phase)
	require.Equal(t, domain.TaskActivityAvailable, reviewAvailable.Lifecycle.Activity)
	require.Equal(t, int64(1), reviewAvailable.Lifecycle.ReviewCycle)
	require.Equal(t, domain.TaskClaimOutcomeWorkSubmitted, submittedClaim.Outcome)

	agentOperation := taskClaimActor(userA, token, "agent-review-claim")
	agentActor := domain.Actor{Type: domain.ActorTypeAgent, Ref: "codex/review-thread"}
	reviewWorking, reviewClaim, err := workflow.Claim(
		ctx, created.Task.Number, reviewAvailable.Version,
		agentActor, agentOperation, "codex", "review-thread", now.Add(4*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskClaimStageReview, reviewClaim.Stage)
	require.Equal(t, domain.TaskActivityWorking, reviewWorking.Lifecycle.Activity)
	currentClaim, err := claims.GetCurrentForClient(
		ctx, agentOperation, "codex", "review-thread",
	)
	require.NoError(t, err)
	require.Equal(t, reviewClaim.ID, currentClaim.ID)

	agentOperation.RequestID = "agent-premature-accept"
	_, _, err = workflow.AcceptTask(
		ctx, created.Task.Number, reviewClaim.ID,
		reviewWorking.Version, reviewClaim.Version,
		"Acceptance contract satisfied.", agentActor, agentOperation, now.Add(5*time.Minute),
	)
	require.ErrorIs(t, err, domain.ErrConflict)
	stillActive, err := claims.GetActiveForTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	require.Equal(t, reviewClaim.ID, stillActive.ID,
		"a failed acceptance command must roll back every mutation")

	agentOperation.RequestID = "agent-acceptance-check"
	check, err := workflow.RecordAcceptanceCheck(
		ctx, created.Task.Number, reviewClaim.ID,
		reviewWorking.Version, reviewClaim.Version,
		domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome:  domain.AcceptanceOutcomePassed,
			Evidence: "Independently reproduced the expected outcome",
		},
		agentActor, agentOperation, now.Add(5*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.AcceptanceCheckPurposeAcceptance, check.Purpose)
	require.Equal(t, int64(1), *check.TaskReviewCycle)

	agentOperation.RequestID = "agent-accept-task"
	done, acceptedClaim, err := workflow.AcceptTask(
		ctx, created.Task.Number, reviewClaim.ID,
		reviewWorking.Version, reviewClaim.Version,
		"Acceptance contract satisfied.", agentActor, agentOperation, now.Add(6*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseDone, done.Lifecycle.Phase)
	require.Empty(t, done.Lifecycle.Activity)
	require.Equal(t, domain.TaskClaimOutcomeTaskAccepted, acceptedClaim.Outcome)
	_, err = claims.GetCurrentForClient(ctx, agentOperation, "codex", "review-thread")
	require.ErrorIs(t, err, domain.ErrNotFound)

	claimHistory, err := claims.ListForTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	require.Len(t, claimHistory, 2)
	require.Equal(t, domain.ActorTypeUser, claimHistory[0].ClaimedBy.Type)
	require.Equal(t, domain.ActorTypeAgent, claimHistory[1].ClaimedBy.Type)

	main, err := threads.GetMainByTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	items, err := threads.ListItems(ctx, main.ID)
	require.NoError(t, err)
	require.Equal(t, []domain.ThreadItemKind{
		domain.ThreadItemKindSystemEvent,
		domain.ThreadItemKindSystemEvent,
		domain.ThreadItemKindWorkSubmission,
		domain.ThreadItemKindSystemEvent,
		domain.ThreadItemKindReviewOutcome,
	}, threadItemKinds(items))
}

func TestTaskWorkflowAcceptanceRequiresCurrentReviewCycleEvidence(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 5, 30, 0, 0, time.UTC)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	created := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Current review-cycle evidence", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	criterion, err := store.NewAcceptanceStore(db).Create(ctx, domain.AcceptanceCriterion{
		TaskID: &created.Task.ID, Criterion: "The current result is correct",
		VerificationInstructions: "Inspect the current submission", Position: 0,
	})
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "cycle-ready"), now,
	)
	require.NoError(t, err)
	execution1, claim1, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "cycle-execution-1"), "browser", "", now,
	)
	require.NoError(t, err)
	review1, _, err := workflow.SubmitWork(
		ctx, created.Task.Number, claim1.ID, execution1.Version, claim1.Version,
		"First submission.", actor,
		domain.SessionOperation(userA, "cycle-submit-1"), now,
	)
	require.NoError(t, err)
	reviewWorking1, reviewClaim1, err := workflow.Claim(
		ctx, created.Task.Number, review1.Version, actor,
		domain.SessionOperation(userA, "cycle-review-1"), "browser", "", now,
	)
	require.NoError(t, err)
	check1, err := workflow.RecordAcceptanceCheck(
		ctx, created.Task.Number, reviewClaim1.ID,
		reviewWorking1.Version, reviewClaim1.Version,
		domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome: domain.AcceptanceOutcomePassed, Evidence: "Cycle one passed.",
		},
		actor, domain.SessionOperation(userA, "cycle-check-1"), now,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), *check1.TaskReviewCycle)
	executionAvailable, _, err := workflow.RequestChanges(
		ctx, created.Task.Number, reviewClaim1.ID,
		reviewWorking1.Version, reviewClaim1.Version,
		"Change the result and review it again.", actor,
		domain.SessionOperation(userA, "cycle-changes"), now,
	)
	require.NoError(t, err)
	execution2, claim2, err := workflow.Claim(
		ctx, created.Task.Number, executionAvailable.Version, actor,
		domain.SessionOperation(userA, "cycle-execution-2"), "browser", "", now,
	)
	require.NoError(t, err)
	review2, _, err := workflow.SubmitWork(
		ctx, created.Task.Number, claim2.ID, execution2.Version, claim2.Version,
		"Second submission.", actor,
		domain.SessionOperation(userA, "cycle-submit-2"), now,
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), review2.Lifecycle.ReviewCycle)
	reviewWorking2, reviewClaim2, err := workflow.Claim(
		ctx, created.Task.Number, review2.Version, actor,
		domain.SessionOperation(userA, "cycle-review-2"), "browser", "", now,
	)
	require.NoError(t, err)

	_, _, err = workflow.AcceptTask(
		ctx, created.Task.Number, reviewClaim2.ID,
		reviewWorking2.Version, reviewClaim2.Version,
		"Cycle one evidence must not be reused.", actor,
		domain.SessionOperation(userA, "cycle-premature-accept"), now,
	)
	require.ErrorIs(t, err, domain.ErrConflict)

	check2, err := workflow.RecordAcceptanceCheck(
		ctx, created.Task.Number, reviewClaim2.ID,
		reviewWorking2.Version, reviewClaim2.Version,
		domain.AcceptanceCheck{
			CriterionID: criterion.ID, CriterionRevision: criterion.Revision,
			Outcome: domain.AcceptanceOutcomePassed, Evidence: "Cycle two passed.",
		},
		actor, domain.SessionOperation(userA, "cycle-check-2"), now,
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), *check2.TaskReviewCycle)
	done, _, err := workflow.AcceptTask(
		ctx, created.Task.Number, reviewClaim2.ID,
		reviewWorking2.Version, reviewClaim2.Version,
		"Current-cycle acceptance passed.", actor,
		domain.SessionOperation(userA, "cycle-accept"), now,
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseDone, done.Lifecycle.Phase)
}

func TestTaskWorkflowExpiresClaimOnlyWhenWorkflowIsAccessed(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 6, 0, 0, 0, time.UTC)
	workflow := store.NewTaskWorkflowStore(db)
	claimStore := store.NewTaskStageClaimStore(db)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Lazy Claim expiration", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)

	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "ready-expiry"), now,
	)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, firstClaim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "first-claim"), "browser", "", now,
	)
	require.NoError(t, err)

	storedBeforeAccess, err := claimStore.Get(ctx, firstClaim.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StageClaimStatusActive, storedBeforeAccess.Status)

	reclaimed, secondClaim, err := workflow.Claim(
		ctx, created.Task.Number, working.Version, actor,
		domain.SessionOperation(userA, "claim-after-deadline"), "browser", "",
		now.Add(domain.TaskClaimActiveLifetime+time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskActivityWorking, reclaimed.Lifecycle.Activity)
	require.NotEqual(t, firstClaim.ID, secondClaim.ID)

	expired, err := claimStore.Get(ctx, firstClaim.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StageClaimStatusExpired, expired.Status)
	require.Equal(t, domain.TaskClaimOutcomeDeadlineElapsed, expired.Outcome)
}

func TestTaskWorkflowCancellationClosesOpenIssue(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC)
	workflow := store.NewTaskWorkflowStore(db)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Cancel blocked Task", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "cancel-ready"), now,
	)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "cancel-claim"), "browser", "", now.Add(time.Minute),
	)
	require.NoError(t, err)
	blocked, _, issue, err := workflow.RequestResolution(
		ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
		domain.IssueThreadTypeDependencyRequired, "The required environment is unavailable.",
		actor, domain.SessionOperation(userA, "cancel-block"), now.Add(2*time.Minute),
	)
	require.NoError(t, err)

	result, err := workflow.CancelTask(
		ctx, created.Task.Number, blocked.Version,
		"The dependency will not be provided.", actor,
		domain.SessionOperation(userA, "cancel-task"), now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseCancelled, result.Task.Lifecycle.Phase)
	require.Nil(t, result.Task.ActiveIssueThreadID)
	require.NotNil(t, result.ResolvedIssue)
	require.Equal(t, issue.ID, result.ResolvedIssue.ID)
	require.Equal(t, domain.IssueThreadStatusResolved, result.ResolvedIssue.IssueStatus)
}

func TestTaskWorkflowArchiveRequiresNoOwnerOrOpenIssue(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 7, 30, 0, 0, time.UTC)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	created := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Archive workflow boundary", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "archive-ready"), now,
	)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "archive-claim"), "browser", "", now,
	)
	require.NoError(t, err)

	_, err = tasks.SetArchived(ctx, created.Task.Number, true, userA)
	require.ErrorIs(t, err, domain.ErrConflict)

	available, _, err := workflow.ReleaseClaim(
		ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
		"Pause before archiving.", actor,
		domain.SessionOperation(userA, "archive-release"), now,
	)
	require.NoError(t, err)
	archived, err := tasks.SetArchived(ctx, created.Task.Number, true, userA)
	require.NoError(t, err)
	require.NotNil(t, archived.Task.ArchivedAt)
	restored, err := tasks.SetArchived(ctx, created.Task.Number, false, userA)
	require.NoError(t, err)

	blockedWorking, nextClaim, err := workflow.Claim(
		ctx, created.Task.Number, restored.Task.Version, actor,
		domain.SessionOperation(userA, "archive-reclaim"), "browser", "", now,
	)
	require.NoError(t, err)
	blocked, _, _, err := workflow.RequestResolution(
		ctx, created.Task.Number, nextClaim.ID, blockedWorking.Version, nextClaim.Version,
		domain.IssueThreadTypeDecisionRequired, "Should this Task remain active?",
		actor, domain.SessionOperation(userA, "archive-block"), now,
	)
	require.NoError(t, err)

	_, err = tasks.SetArchived(ctx, created.Task.Number, true, userA)
	require.ErrorIs(t, err, domain.ErrConflict)

	cancelled, err := workflow.CancelTask(
		ctx, created.Task.Number, blocked.Version, "No longer required.", actor,
		domain.SessionOperation(userA, "archive-cancel"), now,
	)
	require.NoError(t, err)
	archived, err = tasks.SetArchived(ctx, created.Task.Number, true, userA)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseCancelled, cancelled.Task.Lifecycle.Phase)
	require.NotNil(t, archived.Task.ArchivedAt)
	require.Equal(t, available.Lifecycle.Phase, domain.TaskPhaseInProgress)
}

func TestReadyTaskRejectsUnfinishedDependencyUntilReadinessIsWithdrawn(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 7, 45, 0, 0, time.UTC)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	task := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Ready dependency owner", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, task.Task.ID)
	dependency := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Unfinished dependency", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, dependency.Task.ID)
	ready, err := workflow.MarkReady(
		ctx, task.Task.Number, task.Task.Version,
		domain.SessionOperation(userA, "dependency-ready"), now,
	)
	require.NoError(t, err)

	_, err = tasks.Update(ctx, task.Task.Number, domain.TaskPatch{
		DependenciesSet: true,
		DependencyIDs:   []uuid.UUID{dependency.Task.ID},
	}, userA)
	require.ErrorIs(t, err, domain.ErrInvalidTransition)

	backlog, err := workflow.WithdrawReadiness(
		ctx, task.Task.Number, ready.Version, "Add an unfinished dependency first.",
		domain.SessionOperation(userA, "dependency-withdraw"), now,
	)
	require.NoError(t, err)
	updated, err := tasks.UpdateVersionedWithOperation(
		ctx, task.Task.Number, backlog.Version,
		domain.TaskPatch{
			DependenciesSet: true,
			DependencyIDs:   []uuid.UUID{dependency.Task.ID},
		},
		domain.SessionOperation(userA, "dependency-add"),
	)
	require.NoError(t, err)
	require.Len(t, updated.Dependencies, 1)
}

func TestTaskProjectIsImmutableAtStoreAndDatabaseBoundaries(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tasks := store.NewTaskStore(db)
	created := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Permanent Project boundary", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	otherProject, err := store.NewProjectStore(db).Create(ctx, domain.Project{
		Name: "Other Project " + uuid.NewString(), CreatorID: userA,
	})
	require.NoError(t, err)
	cleanupProject(t, db, otherProject.Project.ID)

	_, err = tasks.Update(ctx, created.Task.Number, domain.TaskPatch{
		ProjectSet: true, ProjectID: &otherProject.Project.ID,
	}, userA)
	require.ErrorIs(t, err, domain.ErrConflict)

	_, err = db.Pool.Exec(ctx, `UPDATE tasks SET project_id=$1 WHERE id=$2`,
		otherProject.Project.ID, created.Task.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Task Project is immutable")
}

func TestTaskWorkflowResolutionEndsClaimAndDoesNotRestoreIt(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 5, 0, 0, 0, time.UTC)
	workflow := store.NewTaskWorkflowStore(db)
	threads := store.NewTaskThreadStore(db)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Resolve one concrete blocker", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)

	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "ready-resolution"), now,
	)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "claim-resolution"), "browser", "", now.Add(time.Minute),
	)
	require.NoError(t, err)

	blocked, releasedClaim, issue, err := workflow.RequestResolution(
		ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
		domain.IssueThreadTypeDecisionRequired,
		"Should the response retain the deprecated field?",
		actor, domain.SessionOperation(userA, "request-resolution"), now.Add(2*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskActivityNeedsResolution, blocked.Lifecycle.Activity)
	require.Equal(t, domain.StageClaimStatusReleased, releasedClaim.Status)
	require.Equal(t, domain.TaskClaimOutcomeNeedsResolution, releasedClaim.Outcome)
	require.Equal(t, &issue.ID, blocked.ActiveIssueThreadID)

	available, resolvedIssue, err := workflow.ResolveIssue(
		ctx, created.Task.Number, blocked.Version, issue.ID, issue.Version,
		"Retain it for one compatibility release and document removal.",
		actor, domain.SessionOperation(userA, "resolve-issue"), now.Add(3*time.Minute),
	)
	require.NoError(t, err)
	require.Equal(t, domain.TaskPhaseInProgress, available.Lifecycle.Phase)
	require.Equal(t, domain.TaskActivityAvailable, available.Lifecycle.Activity)
	require.Nil(t, available.ActiveIssueThreadID)
	require.Equal(t, domain.IssueThreadStatusResolved, resolvedIssue.IssueStatus)

	_, err = store.NewTaskStageClaimStore(db).GetActiveForTaskNumber(ctx, created.Task.Number)
	require.ErrorIs(t, err, domain.ErrNotFound)

	main, err := threads.GetMainByTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	mainItems, err := threads.ListItems(ctx, main.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ThreadItemKindIssueResolution, mainItems[len(mainItems)-1].Kind)
	require.NotNil(t, mainItems[len(mainItems)-1].IssueResolution)
	require.Equal(t,
		"Should the response retain the deprecated field?",
		mainItems[len(mainItems)-1].IssueResolution.Request,
	)

	issueItems, err := threads.ListItems(ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, []domain.ThreadItemKind{
		domain.ThreadItemKindResolutionRequest,
		domain.ThreadItemKindResolution,
	}, threadItemKinds(issueItems))
}

func TestTaskWorkflowConcurrentClaimCreatesOneOwner(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	tasks := store.NewTaskStore(db)
	workflow := store.NewTaskWorkflowStore(db)
	created := mustCreateTask(t, db, tasks, domain.Task{
		Title: "Concurrent Claim", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "concurrent-ready"), now,
	)
	require.NoError(t, err)

	type claimResult struct {
		claim domain.TaskStageClaim
		err   error
	}
	results := make(chan claimResult, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index, userID := range []uuid.UUID{userA, userB} {
		wait.Add(1)
		go func(index int, userID uuid.UUID) {
			defer wait.Done()
			<-start
			actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}
			_, claim, claimErr := workflow.Claim(
				ctx, created.Task.Number, ready.Version, actor,
				domain.SessionOperation(userID, fmt.Sprintf("concurrent-claim-%d", index)),
				"browser", "", now,
			)
			results <- claimResult{claim: claim, err: claimErr}
		}(index, userID)
	}
	close(start)
	wait.Wait()
	close(results)

	successes, conflicts := 0, 0
	for result := range results {
		if result.err == nil {
			successes++
			require.NotEqual(t, uuid.Nil, result.claim.ID)
			continue
		}
		if errors.Is(result.err, domain.ErrVersionConflict) ||
			errors.Is(result.err, domain.ErrActiveClaim) {
			conflicts++
			continue
		}
		require.NoError(t, result.err)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	var activeClaims int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM task_stage_claims
		WHERE task_id=$1 AND status='active'`, created.Task.ID).Scan(&activeClaims))
	require.Equal(t, 1, activeClaims)
	state, err := workflow.Get(ctx, created.Task.Number)
	require.NoError(t, err)
	require.Equal(t, domain.TaskActivityWorking, state.Lifecycle.Activity)
}

func TestTaskWorkflowReleaseRacingSubmitHasOneTerminalOutcome(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 10, 30, 0, 0, time.UTC)
	workflow := store.NewTaskWorkflowStore(db)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Concurrent Claim finish", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "finish-ready"), now,
	)
	require.NoError(t, err)
	actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, actor,
		domain.SessionOperation(userA, "finish-claim"), "browser", "", now,
	)
	require.NoError(t, err)

	errorsOut := make(chan error, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, _, releaseErr := workflow.ReleaseClaim(
			ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
			"Leave a coherent handoff.", actor,
			domain.SessionOperation(userA, "finish-release"), now,
		)
		errorsOut <- releaseErr
	}()
	go func() {
		defer wait.Done()
		<-start
		_, _, submitErr := workflow.SubmitWork(
			ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
			"Submit one coherent result.", actor,
			domain.SessionOperation(userA, "finish-submit"), now,
		)
		errorsOut <- submitErr
	}()
	close(start)
	wait.Wait()
	close(errorsOut)

	successes, conflicts := 0, 0
	for finishErr := range errorsOut {
		if finishErr == nil {
			successes++
		} else if errors.Is(finishErr, domain.ErrVersionConflict) ||
			errors.Is(finishErr, domain.ErrConflict) {
			conflicts++
		} else {
			require.NoError(t, finishErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	storedClaim, err := store.NewTaskStageClaimStore(db).Get(ctx, claim.ID)
	require.NoError(t, err)
	require.True(t,
		storedClaim.Outcome == domain.TaskClaimOutcomeWorkSubmitted ||
			storedClaim.Outcome == domain.TaskClaimOutcomeVoluntarilyReleased,
	)
	var terminalItems int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*)
		FROM task_thread_items item
		JOIN task_threads thread ON thread.id=item.thread_id
		WHERE thread.task_id=$1 AND item.kind IN ('handoff','work_submission')`,
		created.Task.ID,
	).Scan(&terminalItems))
	require.Equal(t, 1, terminalItems)
}

func TestTaskWorkflowConcurrentResolutionCreatesOneConclusion(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 11, 0, 0, 0, time.UTC)
	workflow := store.NewTaskWorkflowStore(db)
	threads := store.NewTaskThreadStore(db)
	created := mustCreateTask(t, db, store.NewTaskStore(db), domain.Task{
		Title: "Concurrent resolution", CreatorID: userA,
	}, nil)
	cleanupTask(t, db, created.Task.ID)
	ready, err := workflow.MarkReady(
		ctx, created.Task.Number, created.Task.Version,
		domain.SessionOperation(userA, "resolve-ready"), now,
	)
	require.NoError(t, err)
	owner := domain.Actor{Type: domain.ActorTypeUser, UserID: &userA}
	working, claim, err := workflow.Claim(
		ctx, created.Task.Number, ready.Version, owner,
		domain.SessionOperation(userA, "resolve-claim"), "browser", "", now,
	)
	require.NoError(t, err)
	blocked, _, issue, err := workflow.RequestResolution(
		ctx, created.Task.Number, claim.ID, working.Version, claim.Version,
		domain.IssueThreadTypeDecisionRequired, "Which option should we choose?",
		owner, domain.SessionOperation(userA, "resolve-request"), now,
	)
	require.NoError(t, err)

	errorsOut := make(chan error, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index, userID := range []uuid.UUID{userA, userB} {
		wait.Add(1)
		go func(index int, userID uuid.UUID) {
			defer wait.Done()
			<-start
			actor := domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}
			_, _, resolveErr := workflow.ResolveIssue(
				ctx, created.Task.Number, blocked.Version, issue.ID, issue.Version,
				fmt.Sprintf("Choose option %d.", index+1), actor,
				domain.SessionOperation(userID, fmt.Sprintf("concurrent-resolve-%d", index)), now,
			)
			errorsOut <- resolveErr
		}(index, userID)
	}
	close(start)
	wait.Wait()
	close(errorsOut)

	successes, conflicts := 0, 0
	for resolveErr := range errorsOut {
		if resolveErr == nil {
			successes++
		} else if errors.Is(resolveErr, domain.ErrVersionConflict) ||
			errors.Is(resolveErr, domain.ErrConflict) {
			conflicts++
		} else {
			require.NoError(t, resolveErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	main, err := threads.GetMainByTaskNumber(ctx, created.Task.Number)
	require.NoError(t, err)
	mainItems, err := threads.ListItems(ctx, main.ID)
	require.NoError(t, err)
	issueItems, err := threads.ListItems(ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, 1, countThreadItems(mainItems, domain.ThreadItemKindIssueResolution))
	require.Equal(t, 1, countThreadItems(issueItems, domain.ThreadItemKindResolution))
}

func countThreadItems(items []domain.ThreadItem, kind domain.ThreadItemKind) int {
	count := 0
	for _, item := range items {
		if item.Kind == kind {
			count++
		}
	}
	return count
}

func threadItemKinds(items []domain.ThreadItem) []domain.ThreadItemKind {
	kinds := make([]domain.ThreadItemKind, len(items))
	for index, item := range items {
		kinds[index] = item.Kind
	}
	return kinds
}
