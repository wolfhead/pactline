package application

import (
	"context"
	"fmt"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"
)

const DefaultWorkPacketThreadItemLimit = 20

type CompactThread struct {
	Thread     domain.Thread
	Items      []domain.ThreadItem
	TotalCount int
}

type TaskWorkPacket struct {
	Task                     store.TaskWithRelations
	Claim                    *domain.TaskStageClaim
	Criteria                 []store.CriterionWithCurrentCheck
	Delivery                 TaskDelivery
	MainThread               CompactThread
	ActiveIssueThread        *CompactThread
	ResolvedIssueThreadCount int
}

type TaskWorkPacketService struct {
	Access     *ProjectAccessService
	Acceptance *store.AcceptanceStore
	Threads    *store.TaskThreadStore
	Delivery   *TaskDeliveryService
}

func (s *TaskWorkPacketService) Get(
	ctx context.Context,
	taskNumber int64,
	claim *domain.TaskStageClaim,
	threadItemLimit int,
	subject domain.ProjectAccessSubject,
	requestID string,
) (TaskWorkPacket, error) {
	if threadItemLimit < 1 || threadItemLimit > 100 {
		return TaskWorkPacket{}, fmt.Errorf("%w: Thread Item limit must be between 1 and 100", domain.ErrInvalidInput)
	}
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return TaskWorkPacket{}, err
	}
	if claim != nil && (claim.TaskID != task.Task.ID || claim.TaskNumber != taskNumber) {
		return TaskWorkPacket{}, fmt.Errorf("%w: Claim does not belong to Task", domain.ErrConflict)
	}
	criteria, err := s.Acceptance.ListForTask(ctx, task.Task.ID)
	if claim != nil {
		criteria, err = s.Acceptance.ListForTaskClaim(ctx, task.Task.ID, claim.ID)
	}
	if err != nil {
		return TaskWorkPacket{}, err
	}
	threadContext, err := s.Threads.GetCompactContextForTaskNumber(ctx, taskNumber)
	if err != nil {
		return TaskWorkPacket{}, err
	}
	mainThread, err := s.compactThread(ctx, threadContext.Main, threadItemLimit, false)
	if err != nil {
		return TaskWorkPacket{}, err
	}
	var activeIssue *CompactThread
	if threadContext.ActiveIssue != nil {
		issue, err := s.compactThread(ctx, *threadContext.ActiveIssue, threadItemLimit, true)
		if err != nil {
			return TaskWorkPacket{}, err
		}
		activeIssue = &issue
	}
	delivery, err := s.Delivery.GetDelivery(ctx, taskNumber, subject, requestID)
	if err != nil {
		return TaskWorkPacket{}, err
	}
	return TaskWorkPacket{
		Task: task, Claim: claim, Criteria: criteria, Delivery: delivery,
		MainThread: mainThread, ActiveIssueThread: activeIssue,
		ResolvedIssueThreadCount: threadContext.ResolvedIssueCount,
	}, nil
}

func (s *TaskWorkPacketService) compactThread(
	ctx context.Context,
	thread domain.Thread,
	limit int,
	includeResolutionRequest bool,
) (CompactThread, error) {
	recent, err := s.Threads.ListRecentItems(ctx, thread.ID, limit)
	if err != nil {
		return CompactThread{}, err
	}
	items := recent.Items
	if includeResolutionRequest {
		request, err := s.Threads.GetFirstItemByKind(ctx, thread.ID, domain.ThreadItemKindResolutionRequest)
		if err != nil {
			return CompactThread{}, err
		}
		items = includeOriginalThreadItem(items, request)
	}
	return CompactThread{Thread: thread, Items: items, TotalCount: recent.TotalCount}, nil
}

func includeOriginalThreadItem(
	items []domain.ThreadItem,
	original domain.ThreadItem,
) []domain.ThreadItem {
	for index := range items {
		if items[index].ID == original.ID {
			return items
		}
	}
	// The original resolution request must precede the recent window, so
	// prepend it instead of appending it.
	return append([]domain.ThreadItem{original}, items...)
}
