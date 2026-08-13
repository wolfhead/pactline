package v1

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

func (h *Handler) MarkTaskReady(
	ctx context.Context,
	params generated.MarkTaskReadyParams,
) (generated.MarkTaskReadyRes, error) {
	expectedVersion, actor, _, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, err := h.Workflow.MarkReady(ctx, params.Number, expectedVersion, actor, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return taskWorkflowResponse(ctx, task), nil
}

func (h *Handler) WithdrawTaskReadiness(
	ctx context.Context,
	req *generated.ReasonWrite,
	params generated.WithdrawTaskReadinessParams,
) (generated.WithdrawTaskReadinessRes, error) {
	expectedVersion, actor, _, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, err := h.Workflow.WithdrawReadiness(
		ctx, params.Number, expectedVersion, req.Reason, actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskWorkflowResponse(ctx, task), nil
}

func (h *Handler) ListTaskStageClaims(
	ctx context.Context,
	params generated.ListTaskStageClaimsParams,
) (generated.ListTaskStageClaimsRes, error) {
	if err := h.requireTaskNumberAccess(ctx, params.Number, application.ProjectPermissionRead); err != nil {
		return nil, err
	}
	claims, err := h.StageClaims.ListForTaskNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	items := make([]generated.TaskStageClaim, len(claims))
	for index := range claims {
		items[index] = taskStageClaimFromDomain(claims[index])
	}
	return &generated.TaskStageClaimListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.TaskStageClaimList{Items: items},
	}, nil
}

func (h *Handler) GetCurrentTaskStageClaim(
	ctx context.Context,
	params generated.GetCurrentTaskStageClaimParams,
) (generated.GetCurrentTaskStageClaimRes, error) {
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, err := h.StageClaims.GetCurrentForClient(
		ctx, operation, params.ClientKind, params.ClientSessionID,
	)
	if err != nil {
		return nil, err
	}
	if err := h.requireTaskNumberAccess(
		ctx, claim.TaskNumber, application.ProjectPermissionRead,
	); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if !claim.ExpiresAt.After(now) {
		if _, err := h.Workflow.ExpireDueClaim(ctx, claim.TaskNumber, operation, now); err != nil {
			return nil, err
		}
		return nil, domain.ErrNotFound
	}
	response := taskStageClaimFromDomain(claim)
	return &generated.TaskStageClaimHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) CreateTaskStageClaim(
	ctx context.Context,
	req *generated.TaskStageClaimCreate,
	params generated.CreateTaskStageClaimParams,
) (generated.CreateTaskStageClaimRes, error) {
	expectedVersion, operation, claimedBy, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	clientKind := req.ClientKind.Or("")
	clientSessionID := req.ClientSessionID.Or("")
	if claimedBy.Type == domain.ActorTypeAgent &&
		(strings.TrimSpace(clientKind) == "" || strings.TrimSpace(clientSessionID) == "") {
		return nil, fmt.Errorf(
			"%w: Agent Claims require client_kind and client_session_id",
			domain.ErrInvalidInput,
		)
	}
	task, claim, err := h.Workflow.Claim(
		ctx, params.Number, expectedVersion, claimedBy, operation,
		clientKind, clientSessionID, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskStageClaimCommandResponse(ctx, task, claim), nil
}

func (h *Handler) ReleaseTaskStageClaim(
	ctx context.Context,
	req *generated.TaskStageClaimFinish,
	params generated.ReleaseTaskStageClaimParams,
) (generated.ReleaseTaskStageClaimRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, claim, err := h.Workflow.ReleaseClaim(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		req.Body, actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskStageClaimCommandResponse(ctx, task, claim), nil
}

func (h *Handler) SubmitTaskWork(
	ctx context.Context,
	req *generated.TaskStageClaimFinish,
	params generated.SubmitTaskWorkParams,
) (generated.SubmitTaskWorkRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, claim, err := h.Workflow.SubmitWork(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		req.Body, actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskStageClaimCommandResponse(ctx, task, claim), nil
}

func (h *Handler) RequestTaskChanges(
	ctx context.Context,
	req *generated.TaskStageClaimFinish,
	params generated.RequestTaskChangesParams,
) (generated.RequestTaskChangesRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, claim, err := h.Workflow.RequestChanges(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		req.Body, actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskStageClaimCommandResponse(ctx, task, claim), nil
}

func (h *Handler) AcceptTask(
	ctx context.Context,
	req *generated.TaskStageClaimFinish,
	params generated.AcceptTaskParams,
) (generated.AcceptTaskRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, claim, err := h.Workflow.AcceptTask(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		req.Body, actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskStageClaimCommandResponse(ctx, task, claim), nil
}

func (h *Handler) RequestTaskResolution(
	ctx context.Context,
	req *generated.TaskResolutionRequest,
	params generated.RequestTaskResolutionParams,
) (generated.RequestTaskResolutionRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, claim, issue, err := h.Workflow.RequestResolution(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		domain.IssueThreadType(req.IssueType), req.Request,
		actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return &generated.TaskResolutionRequestedHeaders{
		Etag:       generated.NewOptString(formatETag(task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.TaskResolutionRequested{
			Task: taskWorkflowFromDomain(task), Claim: taskStageClaimFromDomain(claim),
			Issue: taskThreadFromDomain(issue),
		},
	}, nil
}

func (h *Handler) ResolveTaskIssue(
	ctx context.Context,
	req *generated.TaskIssueResolve,
	params generated.ResolveTaskIssueParams,
) (generated.ResolveTaskIssueRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	task, issue, err := h.Workflow.ResolveIssue(
		ctx, params.Number, expectedVersion, params.ID, req.ThreadVersion,
		req.Resolution, actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return &generated.TaskIssueResolvedHeaders{
		Etag:       generated.NewOptString(formatETag(task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.TaskIssueResolved{
			Task: taskWorkflowFromDomain(task), Issue: taskThreadFromDomain(issue),
		},
	}, nil
}

func (h *Handler) RecordTaskStageAcceptanceCheck(
	ctx context.Context,
	req *generated.TaskStageAcceptanceCheckWrite,
	params generated.RecordTaskStageAcceptanceCheckParams,
) (generated.RecordTaskStageAcceptanceCheckRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	check, err := h.Workflow.RecordAcceptanceCheck(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		domain.AcceptanceCheck{
			CriterionID: params.CriterionID, CriterionRevision: req.CriterionRevision,
			Outcome: domain.AcceptanceOutcome(req.Outcome), Evidence: req.Evidence,
		},
		actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return &generated.AcceptanceCheckCreatedHeaders{
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/acceptance-checks/%s", check.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   acceptanceCheckFromDomain(check),
	}, nil
}

func (h *Handler) CancelTask(
	ctx context.Context,
	req *generated.ReasonWrite,
	params generated.CancelTaskParams,
) (generated.CancelTaskRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(ctx, params.Number, params.IfMatch)
	if err != nil {
		return nil, err
	}
	result, err := h.Workflow.CancelTask(
		ctx, params.Number, expectedVersion, req.Reason, actor, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	response := generated.TaskCancelResult{Task: taskWorkflowFromDomain(result.Task)}
	if result.EndedClaim != nil {
		response.EndedClaim = generated.NewOptTaskStageClaim(taskStageClaimFromDomain(*result.EndedClaim))
	}
	if result.ResolvedIssue != nil {
		response.ResolvedIssue = generated.NewOptTaskThread(taskThreadFromDomain(*result.ResolvedIssue))
	}
	return &generated.TaskCancelResultHeaders{
		Etag:       generated.NewOptString(formatETag(result.Task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) ListTaskThreads(
	ctx context.Context,
	params generated.ListTaskThreadsParams,
) (generated.ListTaskThreadsRes, error) {
	if err := h.requireTaskNumberAccess(ctx, params.Number, application.ProjectPermissionRead); err != nil {
		return nil, err
	}
	threads, err := h.Threads.ListForTaskNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	items := make([]generated.TaskThread, len(threads))
	for index := range threads {
		items[index] = taskThreadFromDomain(threads[index])
	}
	return &generated.TaskThreadListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.TaskThreadList{Items: items},
	}, nil
}

func (h *Handler) ListTaskThreadItems(
	ctx context.Context,
	params generated.ListTaskThreadItemsParams,
) (generated.ListTaskThreadItemsRes, error) {
	if _, err := h.requireThreadAccess(ctx, params.ID, application.ProjectPermissionRead); err != nil {
		return nil, err
	}
	items, err := h.Threads.ListItems(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	offset, end, next, err := pageBounds(len(items), params.Cursor, params.Limit)
	if err != nil {
		return nil, err
	}
	responseItems := make([]generated.TaskThreadItem, end-offset)
	for index := offset; index < end; index++ {
		responseItems[index-offset] = taskThreadItemFromDomain(items[index])
	}
	response := generated.TaskThreadItemList{Items: responseItems}
	if next != "" {
		response.NextCursor = generated.NewOptString(next)
	}
	return &generated.TaskThreadItemListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) CreateTaskThreadMessage(
	ctx context.Context,
	req *generated.TaskThreadMessageWrite,
	params generated.CreateTaskThreadMessageParams,
) (generated.CreateTaskThreadMessageRes, error) {
	if _, err := h.requireThreadAccess(ctx, params.ID, application.ProjectPermissionWrite); err != nil {
		return nil, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	author, err := workflowActor(ctx, operation)
	if err != nil {
		return nil, err
	}
	var replyTo *uuid.UUID
	if value, ok := req.ReplyToItemID.Get(); ok {
		replyTo = &value
	}
	item, err := h.Threads.AddItem(
		ctx, params.ID, domain.ThreadItemKind(req.Kind), req.Body, replyTo, req.MentionedUserIds,
		author, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskThreadItemResponse(ctx, item), nil
}

func (h *Handler) UpdateTaskThreadMessage(
	ctx context.Context,
	req *generated.TaskThreadMessageUpdate,
	params generated.UpdateTaskThreadMessageParams,
) (generated.UpdateTaskThreadMessageRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	if _, err := h.requireThreadItemAccess(ctx, params.ID, application.ProjectPermissionWrite); err != nil {
		return nil, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	author, err := workflowActor(ctx, operation)
	if err != nil {
		return nil, err
	}
	item, err := h.Threads.EditMessage(
		ctx, params.ID, expectedVersion, req.Body, req.MentionedUserIds,
		author, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskThreadItemResponse(ctx, item), nil
}

func (h *Handler) DeleteTaskThreadMessage(
	ctx context.Context,
	params generated.DeleteTaskThreadMessageParams,
) (generated.DeleteTaskThreadMessageRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	if _, err := h.requireThreadItemAccess(ctx, params.ID, application.ProjectPermissionWrite); err != nil {
		return nil, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	author, err := workflowActor(ctx, operation)
	if err != nil {
		return nil, err
	}
	item, err := h.Threads.DeleteMessage(
		ctx, params.ID, expectedVersion, author, operation, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskThreadItemResponse(ctx, item), nil
}

func (h *Handler) workflowTaskCommandContext(
	ctx context.Context,
	taskNumber int64,
	ifMatch string,
) (int64, domain.OperationActor, domain.Actor, error) {
	expectedVersion, err := parseIfMatch(ifMatch)
	if err != nil {
		return 0, domain.OperationActor{}, domain.Actor{}, err
	}
	if err := h.requireTaskNumberAccess(ctx, taskNumber, application.ProjectPermissionWrite); err != nil {
		return 0, domain.OperationActor{}, domain.Actor{}, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return 0, domain.OperationActor{}, domain.Actor{}, err
	}
	actor, err := workflowActor(ctx, operation)
	if err != nil {
		return 0, domain.OperationActor{}, domain.Actor{}, err
	}
	return expectedVersion, operation, actor, nil
}

func workflowActor(ctx context.Context, operation domain.OperationActor) (domain.Actor, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return domain.Actor{}, ErrAuthenticationRequired
	}
	switch current.AuthenticationMethod {
	case access.AuthenticationMethodSession:
		userID := current.Subject.ID
		return domain.Actor{Type: domain.ActorTypeUser, UserID: &userID}, nil
	case access.AuthenticationMethodAPIToken:
		return domain.Actor{
			Type: domain.ActorTypeAgent, Ref: "api-token/" + operation.TokenName,
		}, nil
	case access.AuthenticationMethodAgentDelegate:
		if operation.AgentRunID == nil {
			return domain.Actor{}, ErrAuthenticationRequired
		}
		return domain.Actor{
			Type: domain.ActorTypeAgent, Ref: "agent-run/" + operation.AgentRunID.String(),
		}, nil
	default:
		return domain.Actor{}, ErrAuthenticationRequired
	}
}

func (h *Handler) requireTaskNumberAccess(
	ctx context.Context,
	taskNumber int64,
	permission application.ProjectPermission,
) error {
	subject, err := accessSubject(ctx)
	if err != nil {
		return err
	}
	_, err = h.Access.RequireTaskByNumber(ctx, taskNumber, subject, permission)
	return err
}

func (h *Handler) requireThreadAccess(
	ctx context.Context,
	threadID uuid.UUID,
	permission application.ProjectPermission,
) (domain.Thread, error) {
	thread, err := h.Threads.Get(ctx, threadID)
	if err != nil {
		return domain.Thread{}, err
	}
	if err := h.requireTaskIDAccess(ctx, thread.TaskID, permission); err != nil {
		return domain.Thread{}, err
	}
	return thread, nil
}

func (h *Handler) requireThreadItemAccess(
	ctx context.Context,
	itemID uuid.UUID,
	permission application.ProjectPermission,
) (domain.Thread, error) {
	thread, err := h.Threads.GetThreadForItem(ctx, itemID)
	if err != nil {
		return domain.Thread{}, err
	}
	if err := h.requireTaskIDAccess(ctx, thread.TaskID, permission); err != nil {
		return domain.Thread{}, err
	}
	return thread, nil
}

func (h *Handler) requireTaskIDAccess(
	ctx context.Context,
	taskID uuid.UUID,
	permission application.ProjectPermission,
) error {
	subject, err := accessSubject(ctx)
	if err != nil {
		return err
	}
	task, err := h.Tasks.Tasks.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	_, err = h.Access.RequireProjectByNumber(ctx, task.Project.Number, subject, permission)
	return err
}

func taskWorkflowResponse(
	ctx context.Context,
	task store.TaskWorkflowSnapshot,
) *generated.TaskWorkflowHeaders {
	return &generated.TaskWorkflowHeaders{
		Etag:       generated.NewOptString(formatETag(task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   taskWorkflowFromDomain(task),
	}
}

func taskWorkflowFromDomain(task store.TaskWorkflowSnapshot) generated.TaskWorkflow {
	out := generated.TaskWorkflow{
		TaskID: task.TaskID, TaskNumber: task.TaskNumber, Version: task.Version,
		Phase:       generated.TaskPhase(task.Lifecycle.Phase),
		ReviewCycle: task.Lifecycle.ReviewCycle, MainThreadID: task.MainThreadID,
	}
	if task.Lifecycle.Activity != "" {
		out.Activity = generated.NewOptTaskActivityState(
			generated.TaskActivityState(task.Lifecycle.Activity),
		)
	}
	if task.ActiveIssueThreadID != nil {
		out.ActiveIssueThreadID = generated.NewOptUUID(*task.ActiveIssueThreadID)
	}
	return out
}

func taskStageClaimCommandResponse(
	ctx context.Context,
	task store.TaskWorkflowSnapshot,
	claim domain.TaskStageClaim,
) *generated.TaskStageClaimCommandHeaders {
	return &generated.TaskStageClaimCommandHeaders{
		Etag:       generated.NewOptString(formatETag(task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.TaskStageClaimCommand{
			Task: taskWorkflowFromDomain(task), Claim: taskStageClaimFromDomain(claim),
		},
	}
}

func actorFromDomain(actor domain.Actor) generated.Actor {
	out := generated.Actor{Type: generated.ActorType(actor.Type)}
	if actor.UserID != nil {
		out.UserID = generated.NewOptUUID(*actor.UserID)
	}
	if actor.Ref != "" {
		out.Ref = generated.NewOptString(actor.Ref)
	}
	return out
}

func taskStageClaimFromDomain(claim domain.TaskStageClaim) generated.TaskStageClaim {
	out := generated.TaskStageClaim{
		ID: claim.ID, TaskID: claim.TaskID, TaskNumber: claim.TaskNumber,
		Stage: generated.TaskClaimStage(claim.Stage), ClaimedBy: actorFromDomain(claim.ClaimedBy),
		SubjectUserID:        claim.SubjectUserID,
		AuthenticationMethod: generated.TaskStageClaimAuthenticationMethod(claim.AuthMethod),
		ClientKind:           claim.ClientKind,
		Status:               generated.TaskStageClaimStatus(claim.Status), Version: claim.Version,
		ExpiresAt: claim.ExpiresAt, CreatedAt: claim.CreatedAt, UpdatedAt: claim.UpdatedAt,
	}
	if claim.TokenName != "" {
		out.TokenName = generated.NewOptString(claim.TokenName)
	}
	if claim.Outcome != "" {
		out.Outcome = generated.NewOptTaskStageClaimOutcome(
			generated.TaskStageClaimOutcome(claim.Outcome),
		)
	}
	if claim.CompletedAt != nil {
		out.CompletedAt = generated.NewOptDateTime(*claim.CompletedAt)
	}
	return out
}

func taskThreadFromDomain(thread domain.Thread) generated.TaskThread {
	out := generated.TaskThread{
		ID: thread.ID, TaskID: thread.TaskID, Role: generated.TaskThreadRole(thread.Role),
		Version: thread.Version, CreatedAt: thread.CreatedAt, UpdatedAt: thread.UpdatedAt,
	}
	if thread.Role == domain.ThreadRoleIssue {
		out.IssueType = generated.NewOptIssueThreadType(generated.IssueThreadType(thread.IssueType))
		out.IssueStatus = generated.NewOptIssueThreadStatus(generated.IssueThreadStatus(thread.IssueStatus))
		out.OpenedFromPhase = generated.NewOptTaskPhase(generated.TaskPhase(thread.OpenedFromPhase))
		out.OpenedBy = generated.NewOptActor(actorFromDomain(thread.OpenedBy))
	}
	if thread.ResolvedBy != nil {
		out.ResolvedBy = generated.NewOptActor(actorFromDomain(*thread.ResolvedBy))
	}
	if thread.ResolvedAt != nil {
		out.ResolvedAt = generated.NewOptDateTime(*thread.ResolvedAt)
	}
	return out
}

func taskThreadItemFromDomain(item domain.ThreadItem) generated.TaskThreadItem {
	mentionedUserIDs := append([]uuid.UUID(nil), item.MentionedUserIDs...)
	if mentionedUserIDs == nil {
		mentionedUserIDs = []uuid.UUID{}
	}
	out := generated.TaskThreadItem{
		ID: item.ID, ThreadID: item.ThreadID, Kind: generated.TaskThreadItemKind(item.Kind),
		Author:           actorFromDomain(item.Author),
		MentionedUserIds: mentionedUserIDs,
		Version:          item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	if item.Body != "" {
		out.Body = generated.NewOptString(item.Body)
	}
	if item.ReplyToItemID != nil {
		out.ReplyToItemID = generated.NewOptUUID(*item.ReplyToItemID)
	}
	if item.DeletedAt != nil {
		out.DeletedAt = generated.NewOptDateTime(*item.DeletedAt)
	}
	if item.IssueResolution != nil {
		payload := item.IssueResolution
		out.IssueResolution = generated.NewOptIssueResolutionPayload(generated.IssueResolutionPayload{
			IssueThreadID: payload.IssueThreadID,
			IssueType:     generated.IssueThreadType(payload.IssueType),
			Request:       payload.Request,
			Resolution:    payload.Resolution,
			OpenedBy:      actorFromDomain(payload.OpenedBy),
			ResolvedBy:    actorFromDomain(payload.ResolvedBy),
			OpenedAt:      payload.OpenedAt,
			ResolvedAt:    payload.ResolvedAt,
		})
	}
	return out
}

func taskThreadItemResponse(
	ctx context.Context,
	item domain.ThreadItem,
) *generated.TaskThreadItemHeaders {
	return &generated.TaskThreadItemHeaders{
		Etag:       generated.NewOptString(formatETag(item.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   taskThreadItemFromDomain(item),
	}
}
