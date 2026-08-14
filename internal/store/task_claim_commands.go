package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
)

// claimCommandTarget resolves the immutable Claim-to-Task association before
// the command transaction. The command then rechecks the exact active Claim
// after taking the existing Task-first locks.
func (s *TaskWorkflowStore) claimCommandTarget(
	ctx context.Context,
	claimID uuid.UUID,
	operation domain.OperationActor,
	now time.Time,
) (domain.TaskStageClaim, error) {
	claim, err := NewTaskStageClaimStore(s.db).Get(ctx, claimID)
	if err != nil {
		return domain.TaskStageClaim{}, err
	}
	if claim.Status == domain.StageClaimStatusActive && !claim.ExpiresAt.After(now.UTC()) {
		expired, expireErr := s.ExpireDueClaim(ctx, claim.TaskNumber, operation, now)
		if expireErr != nil {
			return domain.TaskStageClaim{}, expireErr
		}
		if expired {
			return domain.TaskStageClaim{}, fmt.Errorf("%w: Task Claim expired", domain.ErrConflict)
		}
		claim, err = NewTaskStageClaimStore(s.db).Get(ctx, claimID)
		if err != nil {
			return domain.TaskStageClaim{}, err
		}
	}
	if claim.Status != domain.StageClaimStatusActive {
		return domain.TaskStageClaim{}, fmt.Errorf("%w: Task Claim is no longer active", domain.ErrConflict)
	}
	return claim, nil
}

func (s *TaskWorkflowStore) ReleaseClaimByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	handoff string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	return s.ReleaseClaim(ctx, claim.TaskNumber, claimID, expectedTaskVersion,
		claim.Version, handoff, actor, operation, now)
}

func (s *TaskWorkflowStore) RecordWorkSubmissionByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	summary string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, domain.ThreadItem, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	return s.RecordWorkSubmission(ctx, claim.TaskNumber, claimID, expectedTaskVersion,
		claim.Version, summary, actor, operation, now)
}

func (s *TaskWorkflowStore) CompleteExecutionByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	summary string,
	codeChanges []domain.CodeChangeSnapshot,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, domain.ThreadItem, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	return s.CompleteExecutionWithDelivery(ctx, claim.TaskNumber, claimID,
		expectedTaskVersion, claim.Version, summary, codeChanges, actor, operation, now)
}

func (s *TaskWorkflowStore) RequestChangesByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	reason string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	return s.RequestChanges(ctx, claim.TaskNumber, claimID, expectedTaskVersion,
		claim.Version, reason, actor, operation, now)
}

func (s *TaskWorkflowStore) RequestResolutionByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	issueType domain.IssueThreadType,
	request string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, domain.Thread, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.Thread{}, err
	}
	return s.RequestResolution(ctx, claim.TaskNumber, claimID, expectedTaskVersion,
		claim.Version, issueType, request, actor, operation, now)
}

func (s *TaskWorkflowStore) RecordAcceptanceCheckByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	check domain.AcceptanceCheck,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (domain.AcceptanceCheck, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return domain.AcceptanceCheck{}, err
	}
	return s.RecordAcceptanceCheck(ctx, claim.TaskNumber, claimID,
		expectedTaskVersion, claim.Version, check, actor, operation, now)
}

func (s *TaskWorkflowStore) AcceptTaskByID(
	ctx context.Context,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	summary string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, error) {
	claim, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, err
	}
	return s.AcceptTask(ctx, claim.TaskNumber, claimID, expectedTaskVersion,
		claim.Version, summary, actor, operation, now)
}

// RecordClaimProgress appends immutable Main Thread progress while preserving
// both Task and Claim versions. It intentionally has no Task If-Match because
// it records an observation rather than a lifecycle decision.
func (s *TaskWorkflowStore) RecordClaimProgress(
	ctx context.Context,
	claimID uuid.UUID,
	body string,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskWorkflowSnapshot, domain.TaskStageClaim, domain.ThreadItem, error) {
	if strings.TrimSpace(body) == "" {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{},
			fmt.Errorf("%w: progress body is required", domain.ErrInvalidInput)
	}
	if err := validateThreadOperationActor(actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	target, err := s.claimCommandTarget(ctx, claimID, operation, now)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, fmt.Errorf("begin record Claim progress: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, err := lockWorkflowTask(ctx, tx, target.TaskNumber)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	claim, err := lockActiveTaskStageClaim(ctx, tx, task.TaskID)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	if claim.ID != claimID || claim.TaskID != target.TaskID {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{},
			fmt.Errorf("%w: Task Claim is no longer active", domain.ErrConflict)
	}
	if err := validateClaimActor(claim, actor, operation); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	if task.Lifecycle.Activity != domain.TaskActivityWorking {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{},
			fmt.Errorf("%w: progress requires a working Task", domain.ErrConflict)
	}
	item, err := insertDeliveryWorkflowItem(
		ctx, tx, task.MainThreadID, domain.ThreadItemKindProgress, actor, body,
		claim.ID, task.Lifecycle.ReviewCycle+executionCycleOffset(claim.Stage), nil,
		operation.RequestID, now,
	)
	if err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	if err := insertWorkflowAudit(ctx, tx, task, operation, "claim_progress_recorded", now); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskWorkflowSnapshot{}, domain.TaskStageClaim{}, domain.ThreadItem{}, fmt.Errorf("commit Claim progress: %w", err)
	}
	return task.TaskWorkflowSnapshot, claim, item, nil
}

func executionCycleOffset(stage domain.TaskClaimStage) int64 {
	if stage == domain.TaskClaimStageExecution {
		return 1
	}
	return 0
}
