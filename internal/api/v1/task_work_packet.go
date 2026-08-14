package v1

import (
	"context"
	"time"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
)

func (h *Handler) GetTaskWorkPacket(
	ctx context.Context,
	params generated.GetTaskWorkPacketParams,
) (generated.GetTaskWorkPacketRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	packet, err := h.WorkPackets.Get(
		ctx, params.Number, nil,
		params.ThreadItemsLimit.Or(application.DefaultWorkPacketThreadItemLimit),
		subject, baseapi.RequestIDFromContext(ctx),
	)
	if err != nil {
		return nil, err
	}
	response, err := taskWorkPacketFromApplication(packet)
	if err != nil {
		return nil, err
	}
	return &generated.TaskWorkPacketHeaders{
		Etag:       generated.NewOptString(formatETag(packet.Task.Task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) GetClaimWorkPacket(
	ctx context.Context,
	params generated.GetClaimWorkPacketParams,
) (generated.GetClaimWorkPacketRes, error) {
	claim, err := h.StageClaims.Get(ctx, params.ClaimID)
	if err != nil {
		return nil, err
	}
	if err := h.requireTaskNumberAccess(ctx, claim.TaskNumber, application.ProjectPermissionRead); err != nil {
		return nil, err
	}
	operation, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	if expired, err := h.expireDueClaims(ctx, []domain.TaskStageClaim{claim}, operation, time.Now().UTC()); err != nil {
		return nil, err
	} else if expired {
		claim, err = h.StageClaims.Get(ctx, params.ClaimID)
		if err != nil {
			return nil, err
		}
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	packet, err := h.WorkPackets.Get(
		ctx, claim.TaskNumber, &claim,
		params.ThreadItemsLimit.Or(application.DefaultWorkPacketThreadItemLimit),
		subject, baseapi.RequestIDFromContext(ctx),
	)
	if err != nil {
		return nil, err
	}
	taskPacket, err := taskWorkPacketFromApplication(packet)
	if err != nil {
		return nil, err
	}
	return &generated.ClaimWorkPacketHeaders{
		Etag:       generated.NewOptString(formatETag(packet.Task.Task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.ClaimWorkPacket{
			Task: taskPacket.Task, Claim: taskStageClaimFromDomain(claim),
			Criteria: taskPacket.Criteria, Delivery: taskPacket.Delivery,
			MainThread:               taskPacket.MainThread,
			ActiveIssueThread:        taskPacket.ActiveIssueThread,
			ResolvedIssueThreadCount: taskPacket.ResolvedIssueThreadCount,
		},
	}, nil
}

func taskWorkPacketFromApplication(
	packet application.TaskWorkPacket,
) (generated.TaskWorkPacket, error) {
	delivery, err := taskDeliveryFromApplication(packet.Delivery)
	if err != nil {
		return generated.TaskWorkPacket{}, err
	}
	criteria := make([]generated.AcceptanceCriterion, len(packet.Criteria))
	for index := range packet.Criteria {
		criteria[index] = acceptanceCriterionFromDomain(packet.Criteria[index])
	}
	response := generated.TaskWorkPacket{
		Task: taskFromDomain(packet.Task), Criteria: criteria, Delivery: delivery,
		MainThread:               compactThreadFromApplication(packet.MainThread),
		ResolvedIssueThreadCount: packet.ResolvedIssueThreadCount,
	}
	if packet.ActiveIssueThread != nil {
		response.ActiveIssueThread = generated.NewOptCompactThread(
			compactThreadFromApplication(*packet.ActiveIssueThread),
		)
	}
	return response, nil
}

func compactThreadFromApplication(thread application.CompactThread) generated.CompactThread {
	items := make([]generated.TaskThreadItem, len(thread.Items))
	for index := range thread.Items {
		items[index] = taskThreadItemFromDomain(thread.Items[index])
	}
	return generated.CompactThread{
		Thread: taskThreadFromDomain(thread.Thread), Items: items,
		TotalCount: thread.TotalCount, ReturnedCount: len(items),
		Truncated: len(items) < thread.TotalCount,
	}
}
