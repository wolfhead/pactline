package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const workflowSystemActorRef = "pactline"

type TaskWorkflowStore struct{ db *DB }

func NewTaskWorkflowStore(db *DB) *TaskWorkflowStore {
	return &TaskWorkflowStore{db: db}
}

type TaskWorkflowSnapshot struct {
	TaskID              uuid.UUID
	TaskNumber          int64
	Version             int64
	Lifecycle           domain.TaskLifecycle
	ActiveIssueThreadID *uuid.UUID
	MainThreadID        uuid.UUID
	ArchivedAt          *time.Time
}

type workflowTask struct {
	TaskWorkflowSnapshot
	ProjectID uuid.UUID
}

func (s *TaskWorkflowStore) Get(
	ctx context.Context,
	taskNumber int64,
) (TaskWorkflowSnapshot, error) {
	task, err := scanWorkflowTask(s.db.Pool.QueryRow(ctx, `
		SELECT task.id,task.number,task.version,task.phase,task.activity_state,
			task.review_cycle,task.active_issue_thread_id,main.id,
			task.archived_at,task.project_id
		FROM tasks task
		LEFT JOIN task_threads main
			ON main.task_id=task.id AND main.role='main'
		WHERE task.number=$1`, taskNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskWorkflowSnapshot{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskWorkflowSnapshot{}, fmt.Errorf("get Task %d workflow: %w", taskNumber, err)
	}
	return task.TaskWorkflowSnapshot, nil
}

func scanWorkflowTask(row scanner) (workflowTask, error) {
	var (
		task            workflowTask
		phase, activity *string
		reviewCycle     *int64
		mainThreadID    *uuid.UUID
	)
	if err := row.Scan(
		&task.TaskID, &task.TaskNumber, &task.Version, &phase, &activity,
		&reviewCycle, &task.ActiveIssueThreadID, &mainThreadID,
		&task.ArchivedAt, &task.ProjectID,
	); err != nil {
		return workflowTask{}, err
	}
	if phase == nil || reviewCycle == nil || mainThreadID == nil {
		return workflowTask{}, fmt.Errorf(
			"%w: Task %d has not been classified into the target workflow",
			domain.ErrMigrationRequired,
			task.TaskNumber,
		)
	}
	task.Lifecycle = domain.TaskLifecycle{
		Phase: domain.TaskPhase(*phase), ReviewCycle: *reviewCycle,
	}
	if activity != nil {
		task.Lifecycle.Activity = domain.TaskActivityState(*activity)
	}
	task.MainThreadID = *mainThreadID
	if err := task.Lifecycle.Validate(); err != nil {
		return workflowTask{}, err
	}
	return task, nil
}

func lockWorkflowTask(
	ctx context.Context,
	tx pgx.Tx,
	taskNumber int64,
) (workflowTask, error) {
	task, err := scanWorkflowTask(tx.QueryRow(ctx, `
		SELECT task.id,task.number,task.version,task.phase,task.activity_state,
			task.review_cycle,task.active_issue_thread_id,main.id,
			task.archived_at,task.project_id
		FROM tasks task
		LEFT JOIN task_threads main
			ON main.task_id=task.id AND main.role='main'
		WHERE task.number=$1
		FOR UPDATE OF task`, taskNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowTask{}, domain.ErrNotFound
	}
	if err != nil {
		return workflowTask{}, fmt.Errorf("lock Task %d workflow: %w", taskNumber, err)
	}
	return task, nil
}

func (s *TaskWorkflowStore) MarkReady(
	ctx context.Context,
	taskNumber, expectedVersion int64,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, error) {
	if err := operation.Validate(); err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	return s.simpleLifecycleCommand(ctx, taskNumber, expectedVersion, operation, now,
		"marked_ready", "Task marked ready", func(ctx context.Context, tx pgx.Tx, task *workflowTask) error {
			var unfinishedDependencies bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1
					FROM task_dependencies dependency
					JOIN tasks prerequisite ON prerequisite.id=dependency.depends_on_task_id
					WHERE dependency.task_id=$1
					  AND (prerequisite.phase IS NULL OR prerequisite.phase NOT IN ('done','cancelled'))
				)`, task.TaskID).Scan(&unfinishedDependencies); err != nil {
				return fmt.Errorf("check Task readiness dependencies: %w", err)
			}
			return task.Lifecycle.MarkReady(task.ArchivedAt != nil, unfinishedDependencies)
		})
}

func (s *TaskWorkflowStore) WithdrawReadiness(
	ctx context.Context,
	taskNumber, expectedVersion int64,
	reason string,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, error) {
	if strings.TrimSpace(reason) == "" {
		return TaskWorkflowSnapshot{}, fmt.Errorf("%w: withdrawal reason is required", domain.ErrInvalidInput)
	}
	return s.simpleLifecycleCommand(ctx, taskNumber, expectedVersion, operation, now,
		"readiness_withdrawn", "Readiness withdrawn: "+reason,
		func(_ context.Context, _ pgx.Tx, task *workflowTask) error {
			return task.Lifecycle.WithdrawReadiness()
		})
}

type lifecycleMutation func(context.Context, pgx.Tx, *workflowTask) error

func (s *TaskWorkflowStore) simpleLifecycleCommand(
	ctx context.Context,
	taskNumber, expectedVersion int64,
	operation domain.OperationActor,
	now time.Time,
	action, eventBody string,
	mutate lifecycleMutation,
) (TaskWorkflowSnapshot, error) {
	if err := operation.Validate(); err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, fmt.Errorf("begin %s: %w", action, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	if task.Version != expectedVersion {
		return TaskWorkflowSnapshot{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	if err := mutate(ctx, tx, &task); err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedVersion, now); err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindSystemEvent,
		domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
		eventBody, nil, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, action, now); err != nil {
		return TaskWorkflowSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, fmt.Errorf("commit %s: %w", action, err)
	}
	return task.TaskWorkflowSnapshot, nil
}

func (s *TaskWorkflowStore) Claim(
	ctx context.Context,
	taskNumber, expectedVersion int64,
	claimedBy domain.Actor,
	operation domain.OperationActor,
	clientKind, clientSessionID string,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	if err := validateThreadOperationActor(claimedBy, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("begin claim Task: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if task.Version != expectedVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	if err := expireDueWorkflowClaim(ctx, tx, &task, operation, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	stage, err := task.Lifecycle.Claim(task.ArchivedAt != nil)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	claim, err := domain.NewTaskStageClaim(
		task.TaskID, task.TaskNumber, stage, claimedBy, operation,
		clientKind, clientSessionID, now,
	)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := insertTaskStageClaim(ctx, tx, claim); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedVersion, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindSystemEvent,
		domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
		fmt.Sprintf("%s Claim started", stage), nil, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "claimed", now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("commit claim Task: %w", err)
	}
	return task.TaskWorkflowSnapshot, claim, nil
}

func expireDueWorkflowClaim(
	ctx context.Context,
	tx pgx.Tx,
	task *workflowTask,
	operation domain.OperationActor,
	now time.Time,
) error {
	if task.Lifecycle.Activity != domain.TaskActivityWorking {
		return nil
	}
	claim, err := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
	if err != nil {
		return err
	}
	if claim.ExpiresAt.After(now.UTC()) {
		return nil
	}
	previousVersion := claim.Version
	if err := claim.Complete(domain.TaskClaimOutcomeDeadlineElapsed, now); err != nil {
		return err
	}
	if err := updateTaskStageClaim(ctx, tx, claim, previousVersion); err != nil {
		return err
	}
	if err := task.Lifecycle.Release(claim.Stage); err != nil {
		return err
	}
	_, err = insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindSystemEvent,
		domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
		fmt.Sprintf("%s Claim expired", claim.Stage), nil, operation.RequestID, now,
	)
	return err
}

func (s *TaskWorkflowStore) ExpireDueClaim(
	ctx context.Context,
	taskNumber int64,
	operation domain.OperationActor,
	now time.Time,
) (bool, error) {
	if err := operation.Validate(); err != nil {
		return false, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin expire due Task Claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return false, err
	}
	if task.Lifecycle.Activity != domain.TaskActivityWorking {
		return false, nil
	}
	previousVersion := task.Version
	previousActivity := task.Lifecycle.Activity
	if err := expireDueWorkflowClaim(ctx, tx, &task, operation, now); err != nil {
		return false, err
	}
	if task.Lifecycle.Activity == previousActivity {
		return false, nil
	}
	if err := persistWorkflowTask(ctx, tx, &task, previousVersion, now); err != nil {
		return false, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "claim_expired", now); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit due Task Claim expiry: %w", err)
	}
	return true, nil
}

func lockActiveTaskStageClaim(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
) (domain.TaskStageClaim, error) {
	claim, err := scanTaskStageClaim(tx.QueryRow(ctx, `
		SELECT `+taskStageClaimColumns+`
		FROM task_stage_claims claim
		WHERE claim.task_id=$1 AND claim.status='active'
		FOR UPDATE`, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskStageClaim{}, fmt.Errorf("%w: working Task has no active Claim", domain.ErrConflict)
	}
	if err != nil {
		return domain.TaskStageClaim{}, fmt.Errorf("lock active Task Claim: %w", err)
	}
	return claim, nil
}

func (s *TaskWorkflowStore) ReleaseClaim(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	handoff string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	if strings.TrimSpace(handoff) == "" {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("%w: handoff is required", domain.ErrInvalidInput)
	}
	return s.finishClaim(ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		domain.TaskClaimOutcomeVoluntarilyReleased, domain.ThreadItemKindHandoff,
		handoff, actor, operation, now, func(task *workflowTask, claim domain.TaskStageClaim) error {
			return task.Lifecycle.Release(claim.Stage)
		})
}

func (s *TaskWorkflowStore) SubmitWork(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	summary string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	if strings.TrimSpace(summary) == "" {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("%w: submission summary is required", domain.ErrInvalidInput)
	}
	return s.finishClaim(ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		domain.TaskClaimOutcomeWorkSubmitted, domain.ThreadItemKindWorkSubmission,
		summary, actor, operation, now, func(task *workflowTask, _ domain.TaskStageClaim) error {
			return task.Lifecycle.SubmitWork()
		})
}

func (s *TaskWorkflowStore) RequestChanges(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	reason string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	if strings.TrimSpace(reason) == "" {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("%w: review reason is required", domain.ErrInvalidInput)
	}
	return s.finishClaim(ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		domain.TaskClaimOutcomeChangesRequested, domain.ThreadItemKindReviewOutcome,
		reason, actor, operation, now, func(task *workflowTask, _ domain.TaskStageClaim) error {
			return task.Lifecycle.RequestChanges()
		})
}

type finishClaimMutation func(*workflowTask, domain.TaskStageClaim) error

func (s *TaskWorkflowStore) finishClaim(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	outcome domain.TaskClaimOutcome,
	itemKind domain.ThreadItemKind,
	body string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
	mutate finishClaimMutation,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("begin finish Task Claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if task.Version != expectedTaskVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	claim, err := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if claim.ID != claimID || claim.Version != expectedClaimVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("%w: Task Claim version changed", domain.ErrConflict)
	}
	if err := validateClaimActor(claim, actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := mutate(&task, claim); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	previousClaimVersion := claim.Version
	if err := claim.Complete(outcome, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := updateTaskStageClaim(ctx, tx, claim, previousClaimVersion); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, itemKind, actor, body, nil,
		operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, string(outcome), now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("commit finish Task Claim: %w", err)
	}
	return task.TaskWorkflowSnapshot, claim, nil
}

func validateClaimActor(
	claim domain.TaskStageClaim,
	actor domain.Actor,
	operation domain.OperationActor,
) error {
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return err
	}
	if claim.SubjectUserID != operation.UserID || claim.AuthMethod != operation.AuthMethod ||
		claim.ClaimedBy.Type != actor.Type || claim.ClaimedBy.Ref != actor.Ref ||
		!sameOptionalUUID(claim.ClaimedBy.UserID, actor.UserID) ||
		!sameOptionalUUID(claim.APITokenID, operation.TokenID) ||
		!sameOptionalUUID(claim.AgentRunID, operation.AgentRunID) {
		return fmt.Errorf("%w: caller does not own this Task Claim", domain.ErrForbidden)
	}
	return nil
}

func sameOptionalUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *TaskWorkflowStore) RequestResolution(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	issueType domain.IssueThreadType,
	request string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, domain.Thread, error) {
	if !issueType.Valid() {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{},
			fmt.Errorf("%w: %q", domain.ErrWrongIssueType, issueType)
	}
	if strings.TrimSpace(request) == "" {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{},
			fmt.Errorf("%w: resolution request is required", domain.ErrInvalidInput)
	}
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, fmt.Errorf("begin request resolution: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if task.Version != expectedTaskVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	claim, err := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if claim.ID != claimID || claim.Version != expectedClaimVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, fmt.Errorf("%w: Task Claim version changed", domain.ErrConflict)
	}
	if err := validateClaimActor(claim, actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	issue, err := domain.NewIssueThread(task.TaskID, issueType, task.Lifecycle.Phase, actor, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if err := task.Lifecycle.RequestResolution(claim.Stage); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if err := insertIssueThread(ctx, tx, issue); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, issue.ID, domain.ThreadItemKindResolutionRequest, actor,
		request, nil, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	previousClaimVersion := claim.Version
	if err := claim.Complete(domain.TaskClaimOutcomeNeedsResolution, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if err := updateTaskStageClaim(ctx, tx, claim, previousClaimVersion); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	task.ActiveIssueThreadID = &issue.ID
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindSystemEvent,
		domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
		fmt.Sprintf("Resolution requested in %s Issue Thread", issueType),
		nil, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "resolution_requested", now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, fmt.Errorf("commit request resolution: %w", err)
	}
	return task.TaskWorkflowSnapshot, claim, issue, nil
}

func (s *TaskWorkflowStore) ResolveIssue(
	ctx context.Context,
	taskNumber, expectedTaskVersion int64,
	issueThreadID uuid.UUID,
	expectedThreadVersion int64,
	resolution string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.Thread, error) {
	if strings.TrimSpace(resolution) == "" {
		return TaskWorkflowSnapshot{}, domain.Thread{}, fmt.Errorf("%w: Issue resolution is required", domain.ErrInvalidInput)
	}
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, fmt.Errorf("begin resolve Issue: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if task.Version != expectedTaskVersion {
		return TaskWorkflowSnapshot{}, domain.Thread{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	if task.ActiveIssueThreadID == nil || *task.ActiveIssueThreadID != issueThreadID {
		return TaskWorkflowSnapshot{}, domain.Thread{}, fmt.Errorf("%w: active Issue or Task version changed", domain.ErrConflict)
	}
	issue, err := lockIssueThread(ctx, tx, task.TaskID, issueThreadID)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if issue.Version != expectedThreadVersion || issue.OpenedFromPhase != task.Lifecycle.Phase {
		return TaskWorkflowSnapshot{}, domain.Thread{}, fmt.Errorf("%w: Issue Thread version or phase changed", domain.ErrConflict)
	}
	requestItem, err := getIssueRequest(ctx, tx, issue.ID)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, issue.ID, domain.ThreadItemKindResolution, actor,
		resolution, nil, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if err := issue.Resolve(actor, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if err := persistResolvedIssue(ctx, tx, issue, expectedThreadVersion); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	payload := &domain.IssueResolutionPayload{
		IssueThreadID: issue.ID, IssueType: issue.IssueType,
		Request: requestItem.Body, Resolution: resolution,
		OpenedBy: issue.OpenedBy, ResolvedBy: actor,
		OpenedAt: issue.CreatedAt, ResolvedAt: *issue.ResolvedAt,
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindIssueResolution,
		domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
		"", payload, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if err := task.Lifecycle.ResolveIssue(); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	task.ActiveIssueThreadID = nil
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "issue_resolved", now); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, domain.Thread{}, fmt.Errorf("commit resolve Issue: %w", err)
	}
	return task.TaskWorkflowSnapshot, issue, nil
}

func (s *TaskWorkflowStore) RecordAcceptanceCheck(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	check domain.AcceptanceCheck,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (domain.AcceptanceCheck, error) {
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return domain.AcceptanceCheck{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.AcceptanceCheck{}, fmt.Errorf("begin record acceptance check: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return domain.AcceptanceCheck{}, err
	}
	if task.Version != expectedTaskVersion {
		return domain.AcceptanceCheck{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	if task.Lifecycle.Activity != domain.TaskActivityWorking {
		return domain.AcceptanceCheck{}, fmt.Errorf("%w: Task version or activity changed", domain.ErrConflict)
	}
	claim, err := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
	if err != nil {
		return domain.AcceptanceCheck{}, err
	}
	if claim.ID != claimID || claim.Version != expectedClaimVersion {
		return domain.AcceptanceCheck{}, fmt.Errorf("%w: Task Claim version changed", domain.ErrConflict)
	}
	if err := validateClaimActor(claim, actor, operation); err != nil {
		return domain.AcceptanceCheck{}, err
	}
	if check.Checker.Type == "" {
		check.Checker = actor
	}
	if check.Checker.Type != actor.Type || check.Checker.Ref != actor.Ref ||
		!sameOptionalUUID(check.Checker.UserID, actor.UserID) {
		return domain.AcceptanceCheck{}, fmt.Errorf("%w: checker must match active Claim actor", domain.ErrForbidden)
	}
	criterion, err := lockTaskAcceptanceCriterion(ctx, tx, task.TaskID, check.CriterionID)
	if err != nil {
		return domain.AcceptanceCheck{}, err
	}
	check.ID = uuid.New()
	check.TaskClaimID = &claim.ID
	cycle := task.Lifecycle.ReviewCycle
	check.TaskReviewCycle = &cycle
	check.CheckedAt = now.UTC()
	if claim.Stage == domain.TaskClaimStageExecution {
		check.Purpose = domain.AcceptanceCheckPurposeExecutionVerification
	} else {
		check.Purpose = domain.AcceptanceCheckPurposeAcceptance
	}
	if err := check.ValidateForTaskClaim(criterion, claim.ID, claim.Stage, cycle); err != nil {
		return domain.AcceptanceCheck{}, err
	}
	checkerUserID, checkerRef := actorColumns(check.Checker)
	_, err = tx.Exec(ctx, `
		INSERT INTO acceptance_checks (
			id,criterion_id,criterion_revision,outcome,evidence,
			checker_type,checked_by_user_id,checker_ref,checked_at,
			purpose,task_stage_claim_id,task_review_cycle
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		check.ID, check.CriterionID, check.CriterionRevision, check.Outcome,
		check.Evidence, check.Checker.Type, checkerUserID, checkerRef,
		check.CheckedAt, check.Purpose, claim.ID, cycle,
	)
	if err != nil {
		return domain.AcceptanceCheck{}, mapPgError(err)
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "acceptance_check_recorded", now); err != nil {
		return domain.AcceptanceCheck{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AcceptanceCheck{}, fmt.Errorf("commit acceptance check: %w", err)
	}
	return check, nil
}

func (s *TaskWorkflowStore) AcceptTask(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion, expectedClaimVersion int64,
	summary string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	if strings.TrimSpace(summary) == "" {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("%w: acceptance summary is required", domain.ErrInvalidInput)
	}
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("begin accept Task: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if task.Version != expectedTaskVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	claim, err := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if claim.ID != claimID || claim.Version != expectedClaimVersion {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("%w: Task Claim version changed", domain.ErrConflict)
	}
	if err := validateClaimActor(claim, actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	readiness, err := taskCompletionReadinessForCycle(ctx, tx, task.TaskID, task.Lifecycle.ReviewCycle)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := task.Lifecycle.Accept(readiness); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	previousClaimVersion := claim.Version
	if err := claim.Complete(domain.TaskClaimOutcomeTaskAccepted, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := updateTaskStageClaim(ctx, tx, claim, previousClaimVersion); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindReviewOutcome,
		actor, summary, nil, operation.RequestID, now,
	); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "task_accepted", now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, fmt.Errorf("commit accept Task: %w", err)
	}
	return task.TaskWorkflowSnapshot, claim, nil
}

type CancelTaskResult struct {
	Task          TaskWorkflowSnapshot
	EndedClaim    *domain.TaskStageClaim
	ResolvedIssue *domain.Thread
}

func (s *TaskWorkflowStore) CancelTask(
	ctx context.Context,
	taskNumber, expectedTaskVersion int64,
	reason string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (CancelTaskResult, error) {
	if strings.TrimSpace(reason) == "" {
		return CancelTaskResult{}, fmt.Errorf("%w: cancellation reason is required", domain.ErrInvalidInput)
	}
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return CancelTaskResult{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return CancelTaskResult{}, fmt.Errorf("begin cancel Task: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, taskNumber)
	if err != nil {
		return CancelTaskResult{}, err
	}
	if task.Version != expectedTaskVersion {
		return CancelTaskResult{}, domain.VersionConflictError{CurrentVersion: task.Version}
	}
	result := CancelTaskResult{}
	if task.Lifecycle.Activity == domain.TaskActivityWorking {
		claim, lockErr := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
		if lockErr != nil {
			return CancelTaskResult{}, lockErr
		}
		previousClaimVersion := claim.Version
		if err := claim.Complete(domain.TaskClaimOutcomeTaskCancelled, now); err != nil {
			return CancelTaskResult{}, err
		}
		if err := updateTaskStageClaim(ctx, tx, claim, previousClaimVersion); err != nil {
			return CancelTaskResult{}, err
		}
		result.EndedClaim = &claim
	}
	if task.Lifecycle.Activity == domain.TaskActivityNeedsResolution {
		if task.ActiveIssueThreadID == nil {
			return CancelTaskResult{}, fmt.Errorf("%w: blocked Task has no active Issue", domain.ErrConflict)
		}
		issue, lockErr := lockIssueThread(ctx, tx, task.TaskID, *task.ActiveIssueThreadID)
		if lockErr != nil {
			return CancelTaskResult{}, lockErr
		}
		requestItem, requestErr := getIssueRequest(ctx, tx, issue.ID)
		if requestErr != nil {
			return CancelTaskResult{}, requestErr
		}
		resolution := "Task cancelled: " + reason
		if _, err := insertWorkflowItem(
			ctx, tx, issue.ID, domain.ThreadItemKindResolution, actor,
			resolution, nil, operation.RequestID, now,
		); err != nil {
			return CancelTaskResult{}, err
		}
		previousThreadVersion := issue.Version
		if err := issue.Resolve(actor, now); err != nil {
			return CancelTaskResult{}, err
		}
		if err := persistResolvedIssue(ctx, tx, issue, previousThreadVersion); err != nil {
			return CancelTaskResult{}, err
		}
		payload := &domain.IssueResolutionPayload{
			IssueThreadID: issue.ID, IssueType: issue.IssueType,
			Request: requestItem.Body, Resolution: resolution,
			OpenedBy: issue.OpenedBy, ResolvedBy: actor,
			OpenedAt: issue.CreatedAt, ResolvedAt: *issue.ResolvedAt,
		}
		if _, err := insertWorkflowItem(
			ctx, tx, task.MainThreadID, domain.ThreadItemKindIssueResolution,
			domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
			"", payload, operation.RequestID, now,
		); err != nil {
			return CancelTaskResult{}, err
		}
		task.ActiveIssueThreadID = nil
		result.ResolvedIssue = &issue
	}
	if err := task.Lifecycle.Cancel(); err != nil {
		return CancelTaskResult{}, err
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return CancelTaskResult{}, err
	}
	if _, err := insertWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindSystemEvent,
		domain.Actor{Type: domain.ActorTypeSystem, Ref: workflowSystemActorRef},
		"Task cancelled: "+reason, nil, operation.RequestID, now,
	); err != nil {
		return CancelTaskResult{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "task_cancelled", now); err != nil {
		return CancelTaskResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CancelTaskResult{}, fmt.Errorf("commit cancel Task: %w", err)
	}
	result.Task = task.TaskWorkflowSnapshot
	return result, nil
}

func persistWorkflowTask(
	ctx context.Context,
	tx pgx.Tx,
	task *workflowTask,
	expectedVersion int64,
	now time.Time,
) error {
	if err := task.Lifecycle.Validate(); err != nil {
		return err
	}
	task.Version = expectedVersion + 1
	var activity any
	if task.Lifecycle.Activity != "" {
		activity = task.Lifecycle.Activity
	}
	var completedAt any
	if task.Lifecycle.Phase == domain.TaskPhaseDone {
		completedAt = now.UTC()
	}
	commandTag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET phase=$1,activity_state=$2,review_cycle=$3,
			active_issue_thread_id=$4,version=$5,updated_at=$6,completed_at=$7
		WHERE id=$8 AND version=$9`,
		task.Lifecycle.Phase, activity, task.Lifecycle.ReviewCycle,
		task.ActiveIssueThreadID, task.Version, now.UTC(), completedAt,
		task.TaskID, expectedVersion,
	)
	if err != nil {
		return mapPgError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("%w: Task version changed", domain.ErrConflict)
	}
	return nil
}

func insertWorkflowItem(
	ctx context.Context,
	tx pgx.Tx,
	threadID uuid.UUID,
	kind domain.ThreadItemKind,
	author domain.Actor,
	body string,
	payload *domain.IssueResolutionPayload,
	requestID string,
	now time.Time,
) (domain.ThreadItem, error) {
	item := domain.ThreadItem{
		ID: uuid.New(), ThreadID: threadID, Kind: kind, Author: author,
		Body: body, IssueResolution: payload,
		Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	if err := insertTaskThreadItem(ctx, tx, item, requestID); err != nil {
		return domain.ThreadItem{}, err
	}
	return item, nil
}

func insertIssueThread(ctx context.Context, tx pgx.Tx, issue domain.Thread) error {
	if err := issue.Validate(); err != nil {
		return err
	}
	openedByUserID, openedByRef := actorColumns(issue.OpenedBy)
	_, err := tx.Exec(ctx, `
		INSERT INTO task_threads (
			id,task_id,role,issue_type,issue_status,opened_from_phase,
			opened_by_type,opened_by_user_id,opened_by_ref,version,
			created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		issue.ID, issue.TaskID, issue.Role, issue.IssueType, issue.IssueStatus,
		issue.OpenedFromPhase, issue.OpenedBy.Type, openedByUserID, openedByRef,
		issue.Version, issue.CreatedAt, issue.UpdatedAt,
	)
	return mapPgError(err)
}

func lockIssueThread(
	ctx context.Context,
	tx pgx.Tx,
	taskID, threadID uuid.UUID,
) (domain.Thread, error) {
	issue, err := scanTaskThread(tx.QueryRow(ctx, `
		SELECT `+taskThreadColumns+`
		FROM task_threads thread
		WHERE thread.id=$1 AND thread.task_id=$2 AND thread.role='issue'
		FOR UPDATE`, threadID, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Thread{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Thread{}, fmt.Errorf("lock Issue Thread: %w", err)
	}
	return issue, nil
}

func getIssueRequest(
	ctx context.Context,
	tx pgx.Tx,
	threadID uuid.UUID,
) (domain.ThreadItem, error) {
	item, err := scanTaskThreadItem(tx.QueryRow(ctx, `
		SELECT item.id,item.thread_id,item.kind,
			item.author_type,item.author_user_id,item.author_ref,
			item.body,item.typed_payload,item.reply_to_item_id,
			ARRAY[]::uuid[],item.version,item.created_at,item.updated_at,item.deleted_at
		FROM task_thread_items item
		WHERE item.thread_id=$1 AND item.kind='resolution_request'`, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ThreadItem{}, fmt.Errorf("%w: Issue Thread has no resolution request", domain.ErrConflict)
	}
	if err != nil {
		return domain.ThreadItem{}, fmt.Errorf("get Issue resolution request: %w", err)
	}
	return item, nil
}

func persistResolvedIssue(
	ctx context.Context,
	tx pgx.Tx,
	issue domain.Thread,
	expectedVersion int64,
) error {
	if issue.ResolvedBy == nil {
		return fmt.Errorf("%w: Issue resolver is required", domain.ErrInvalidInput)
	}
	resolvedByUserID, resolvedByRef := actorColumns(*issue.ResolvedBy)
	commandTag, err := tx.Exec(ctx, `
		UPDATE task_threads
		SET issue_status=$1,resolved_by_type=$2,resolved_by_user_id=$3,
			resolved_by_ref=$4,version=$5,updated_at=$6,resolved_at=$7
		WHERE id=$8 AND version=$9 AND issue_status='open'`,
		issue.IssueStatus, issue.ResolvedBy.Type, resolvedByUserID, resolvedByRef,
		issue.Version, issue.UpdatedAt, issue.ResolvedAt, issue.ID, expectedVersion,
	)
	if err != nil {
		return mapPgError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("%w: Issue Thread version changed", domain.ErrConflict)
	}
	return nil
}

func lockTaskAcceptanceCriterion(
	ctx context.Context,
	tx pgx.Tx,
	taskID, criterionID uuid.UUID,
) (domain.AcceptanceCriterion, error) {
	var criterion domain.AcceptanceCriterion
	if err := tx.QueryRow(ctx, `
		SELECT id,version,task_id,criterion,verification_instructions,
			revision,position,archived_at,created_at,updated_at
		FROM acceptance_criteria
		WHERE id=$1 AND task_id=$2 AND archived_at IS NULL
		FOR UPDATE`, criterionID, taskID).Scan(
		&criterion.ID, &criterion.Version, &criterion.TaskID,
		&criterion.Criterion, &criterion.VerificationInstructions,
		&criterion.Revision, &criterion.Position, &criterion.ArchivedAt,
		&criterion.CreatedAt, &criterion.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AcceptanceCriterion{}, domain.ErrNotFound
		}
		return domain.AcceptanceCriterion{}, fmt.Errorf("lock Task acceptance criterion: %w", err)
	}
	return criterion, nil
}

func taskCompletionReadinessForCycle(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
	reviewCycle int64,
) (domain.TaskCompletionReadiness, error) {
	var readiness domain.TaskCompletionReadiness
	if err := tx.QueryRow(ctx, `
		SELECT count(*),count(*) FILTER (WHERE NOT EXISTS (
			SELECT 1
			FROM acceptance_checks check_result
			WHERE check_result.criterion_id=criterion.id
			  AND check_result.criterion_revision=criterion.revision
			  AND check_result.purpose='acceptance'
			  AND check_result.task_review_cycle=$2
			  AND check_result.outcome IN ('passed','waived')
		))
		FROM acceptance_criteria criterion
		WHERE criterion.task_id=$1 AND criterion.archived_at IS NULL`,
		taskID, reviewCycle,
	).Scan(&readiness.ActiveCriteria, &readiness.UnsatisfiedCriteria); err != nil {
		return readiness, fmt.Errorf("calculate Task acceptance readiness: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT
			count(DISTINCT child.id) FILTER (
				WHERE child.id IS NOT NULL
				  AND (child.phase IS NULL OR child.phase NOT IN ('done','cancelled'))
			),
			count(DISTINCT prerequisite.id) FILTER (
				WHERE prerequisite.id IS NOT NULL
				  AND (prerequisite.phase IS NULL OR prerequisite.phase NOT IN ('done','cancelled'))
			)
		FROM tasks parent
		LEFT JOIN tasks child ON child.parent_task_id=parent.id
		LEFT JOIN task_dependencies dependency ON dependency.task_id=parent.id
		LEFT JOIN tasks prerequisite ON prerequisite.id=dependency.depends_on_task_id
		WHERE parent.id=$1`, taskID,
	).Scan(&readiness.UnfinishedChildren, &readiness.UnfinishedDependencies); err != nil {
		return readiness, fmt.Errorf("calculate Task relationship readiness: %w", err)
	}
	return readiness, nil
}

func insertWorkflowAudit(
	ctx context.Context,
	tx pgx.Tx,
	task workflowTask,
	operation domain.OperationActor,
	action string,
	now time.Time,
) error {
	stateJSON, err := json.Marshal(map[string]any{
		"phase": task.Lifecycle.Phase, "activity": task.Lifecycle.Activity,
		"review_cycle": task.Lifecycle.ReviewCycle,
	})
	if err != nil {
		return fmt.Errorf("encode Task workflow audit: %w", err)
	}
	return InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now.UTC(), Actor: operation, EntityType: "task",
		EntityID: task.TaskID, EntityNumber: &task.TaskNumber,
		Action: action, NewValue: stateJSON,
	})
}
