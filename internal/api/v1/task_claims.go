package v1

import (
	"context"
	"fmt"
	"time"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
)

func (h *Handler) GetCurrentTaskClaim(
	ctx context.Context,
	params generated.GetCurrentTaskClaimParams,
) (generated.GetCurrentTaskClaimRes, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return nil, ErrAuthenticationRequired
	}
	claim, err := h.Claims.GetCurrent(
		ctx, current.Subject.ID, params.ClientKind, params.ClientSessionID,
	)
	if err != nil {
		return nil, err
	}
	return taskClaimResponse(ctx, claim), nil
}

func (h *Handler) ClaimTask(
	ctx context.Context,
	req *generated.TaskClaimSession,
	params generated.ClaimTaskParams,
) (generated.ClaimTaskRes, error) {
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, err := h.Claims.Claim(
		ctx, params.Number, req.ClientKind, req.ClientSessionID, actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	response := taskClaimFromDomain(claim)
	return &generated.TaskClaimCreatedHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		Location:   generated.NewOptString(fmt.Sprintf("/api/v1/claims/%s", response.ID)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) GetTaskClaim(
	ctx context.Context,
	params generated.GetTaskClaimParams,
) (generated.GetTaskClaimRes, error) {
	claim, err := h.authorizedClaim(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return taskClaimResponse(ctx, claim), nil
}

func (h *Handler) ListTaskClaimMessages(
	ctx context.Context,
	params generated.ListTaskClaimMessagesParams,
) (generated.ListTaskClaimMessagesRes, error) {
	if _, err := h.authorizedClaim(ctx, params.ID); err != nil {
		return nil, err
	}
	messages, err := h.Claims.ListMessages(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	items := make([]generated.TaskClaimMessage, len(messages))
	for i := range messages {
		items[i] = taskClaimMessageFromDomain(messages[i])
	}
	return &generated.TaskClaimMessageListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.TaskClaimMessageList{Items: items},
	}, nil
}

func (h *Handler) ListTaskAgentConversations(
	ctx context.Context,
	params generated.ListTaskAgentConversationsParams,
) (generated.ListTaskAgentConversationsRes, error) {
	if _, err := h.Tasks.Tasks.GetByNumber(ctx, params.Number); err != nil {
		return nil, err
	}
	claims, err := h.Claims.ListForTaskNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	messages, err := h.Claims.ListMessagesForTaskNumber(ctx, params.Number)
	if err != nil {
		return nil, err
	}
	messagesByClaim := make(map[[16]byte][]generated.TaskClaimMessage, len(claims))
	for i := range messages {
		messagesByClaim[messages[i].ClaimID] = append(
			messagesByClaim[messages[i].ClaimID],
			taskClaimMessageFromDomain(messages[i]),
		)
	}
	items := make([]generated.TaskClaimConversation, len(claims))
	for i := range claims {
		generatedMessages := messagesByClaim[claims[i].ID]
		if generatedMessages == nil {
			generatedMessages = []generated.TaskClaimMessage{}
		}
		items[i] = generated.TaskClaimConversation{
			Claim: taskClaimFromDomain(claims[i]), Messages: generatedMessages,
		}
	}
	return &generated.TaskClaimConversationListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.TaskClaimConversationList{Items: items},
	}, nil
}

func (h *Handler) ExtendTaskClaim(
	ctx context.Context,
	req *generated.TaskClaimSession,
	params generated.ExtendTaskClaimParams,
) (generated.ExtendTaskClaimRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, err := h.Claims.Extend(
		ctx, params.ID, expectedVersion, req.ClientKind, req.ClientSessionID,
		actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskClaimResponse(ctx, claim), nil
}

func (h *Handler) AddTaskClaimProgress(
	ctx context.Context,
	req *generated.TaskClaimAgentMessage,
	params generated.AddTaskClaimProgressParams,
) (generated.AddTaskClaimProgressRes, error) {
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	message, err := h.Claims.AddProgress(
		ctx, params.ID, req.ClientKind, req.ClientSessionID, req.Body,
		actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	response := taskClaimMessageFromDomain(message)
	return &generated.TaskClaimMessageCreatedHeaders{
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/claims/%s/messages#%s", params.ID, response.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) AskTaskClaimQuestion(
	ctx context.Context,
	req *generated.TaskClaimAgentMessage,
	params generated.AskTaskClaimQuestionParams,
) (generated.AskTaskClaimQuestionRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, message, err := h.Claims.Ask(
		ctx, params.ID, expectedVersion, req.ClientKind, req.ClientSessionID,
		req.Body, actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskClaimActionResponse(ctx, claim, message), nil
}

func (h *Handler) AnswerTaskClaimQuestion(
	ctx context.Context,
	req *generated.TaskClaimHumanAnswer,
	params generated.AnswerTaskClaimQuestionParams,
) (generated.AnswerTaskClaimQuestionRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, message, err := h.Claims.Answer(
		ctx, params.ID, expectedVersion, req.Body, actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskClaimActionResponse(ctx, claim, message), nil
}

func (h *Handler) ReleaseTaskClaim(
	ctx context.Context,
	req *generated.TaskClaimRelease,
	params generated.ReleaseTaskClaimParams,
) (generated.ReleaseTaskClaimRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, err := h.Claims.Release(
		ctx, params.ID, expectedVersion,
		req.ClientKind.Or(""), req.ClientSessionID.Or(""), req.Handoff.Or(""),
		actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskClaimResponse(ctx, claim), nil
}

func (h *Handler) SubmitTaskClaim(
	ctx context.Context,
	req *generated.TaskClaimAgentMessage,
	params generated.SubmitTaskClaimParams,
) (generated.SubmitTaskClaimRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	claim, message, err := h.Claims.Submit(
		ctx, params.ID, expectedVersion, req.ClientKind, req.ClientSessionID,
		req.Body, actor, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return taskClaimActionResponse(ctx, claim, message), nil
}

func (h *Handler) RecordTaskClaimAcceptanceCheck(
	ctx context.Context,
	req *generated.TaskClaimAcceptanceCheckCreate,
	params generated.RecordTaskClaimAcceptanceCheckParams,
) (generated.RecordTaskClaimAcceptanceCheckRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	checker := domain.Actor{Type: domain.ActorTypeAgent, Ref: actor.TokenName}
	created, err := h.Projects.Acceptance.AddClaimCheckVersioned(
		ctx,
		params.ID,
		req.ClientKind,
		req.ClientSessionID,
		params.CriterionID,
		expectedVersion,
		domain.AcceptanceCheck{
			CriterionID:       params.CriterionID,
			CriterionRevision: req.CriterionRevision,
			Outcome:           domain.AcceptanceOutcome(req.Outcome),
			Evidence:          req.Evidence,
			Checker:           checker,
		},
		actor,
	)
	if err != nil {
		return nil, err
	}
	response := acceptanceCheckFromDomain(created.Check)
	return &generated.AcceptanceCheckCreatedHeaders{
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/criteria/%s/checks/%s", params.CriterionID, response.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) authorizedClaim(ctx context.Context, id [16]byte) (domain.TaskClaim, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return domain.TaskClaim{}, ErrAuthenticationRequired
	}
	claim, err := h.Claims.Get(ctx, id)
	if err != nil {
		return domain.TaskClaim{}, err
	}
	if claim.ClaimedByUserID != current.Subject.ID {
		return domain.TaskClaim{}, domain.ErrNotFound
	}
	return claim, nil
}

func taskClaimResponse(ctx context.Context, claim domain.TaskClaim) *generated.TaskClaimHeaders {
	response := taskClaimFromDomain(claim)
	return &generated.TaskClaimHeaders{
		Etag:       generated.NewOptString(formatETag(response.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}
}

func taskClaimActionResponse(
	ctx context.Context,
	claim domain.TaskClaim,
	message domain.TaskClaimMessage,
) *generated.TaskClaimActionHeaders {
	return &generated.TaskClaimActionHeaders{
		Etag:       generated.NewOptString(formatETag(claim.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.TaskClaimAction{
			Claim: taskClaimFromDomain(claim), Message: taskClaimMessageFromDomain(message),
		},
	}
}

func taskClaimFromDomain(claim domain.TaskClaim) generated.TaskClaim {
	out := generated.TaskClaim{
		ID: claim.ID, TaskID: claim.TaskID, TaskNumber: claim.TaskNumber,
		ClaimedByUserID: claim.ClaimedByUserID, TokenName: claim.TokenNameSnapshot,
		ClientKind: claim.ClientKind,
		Status:     generated.TaskClaimStatus(claim.Status), Version: claim.Version,
		ExpiresAt: claim.ExpiresAt, CreatedAt: claim.CreatedAt, UpdatedAt: claim.UpdatedAt,
	}
	if claim.TerminalReason != "" {
		out.TerminalReason = generated.NewOptString(claim.TerminalReason)
	}
	if claim.CompletedAt != nil {
		out.CompletedAt = generated.NewOptDateTime(*claim.CompletedAt)
	}
	return out
}

func taskClaimMessageFromDomain(message domain.TaskClaimMessage) generated.TaskClaimMessage {
	authorType := generated.TaskClaimMessageAuthorTypeAgent
	if message.Author.Type == domain.ActorTypeUser {
		authorType = generated.TaskClaimMessageAuthorTypeHuman
	}
	out := generated.TaskClaimMessage{
		ID: message.ID, ClaimID: message.ClaimID, TaskID: message.TaskID,
		AuthorType: authorType,
		Kind:       generated.TaskClaimMessageKind(message.Kind), Body: message.Body,
		CreatedAt: message.CreatedAt,
	}
	if message.Author.UserID != nil {
		out.AuthorUserID = generated.NewOptUUID(*message.Author.UserID)
	}
	if message.ReplyToID != nil {
		out.ReplyToMessageID = generated.NewOptUUID(*message.ReplyToID)
	}
	if message.TokenName != "" {
		out.TokenName = generated.NewOptString(message.TokenName)
	}
	return out
}
