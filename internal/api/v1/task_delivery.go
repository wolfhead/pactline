package v1

import (
	"context"
	"fmt"
	"net/url"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

func (h *Handler) GetTaskDelivery(
	ctx context.Context,
	params generated.GetTaskDeliveryParams,
) (generated.GetTaskDeliveryRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	delivery, err := h.Delivery.GetDelivery(
		ctx, params.Number, subject, baseapi.RequestIDFromContext(ctx),
	)
	if err != nil {
		return nil, err
	}
	activeLinks := make([]generated.TaskMergeRequest, len(delivery.ActiveLinks))
	currentByID := make(map[uuid.UUID]generated.TaskMergeRequest, len(delivery.ActiveLinks))
	currentDomainByID := make(map[uuid.UUID]domain.TaskMergeRequest, len(delivery.ActiveLinks))
	for index, item := range delivery.ActiveLinks {
		converted, err := taskMergeRequestFromDomain(item)
		if err != nil {
			return nil, err
		}
		activeLinks[index] = converted
		currentByID[item.MergeRequest.ID] = converted
		currentDomainByID[item.MergeRequest.ID] = item.MergeRequest
	}
	response := generated.TaskDelivery{ActiveLinks: activeLinks}
	if delivery.Review != nil {
		comparisons := make([]generated.TaskDeliveryComparison, len(delivery.Review.MergeRequests))
		for index, snapshot := range delivery.Review.MergeRequests {
			comparison := generated.TaskDeliveryComparison{
				Snapshot: mergeRequestSnapshotFromDomain(snapshot),
				Comparison: generated.TaskDeliveryComparisonComparison(
					application.DeliveryComparison(snapshot, currentMergeRequest(
						currentDomainByID, snapshot.TaskMergeRequestID,
					))),
			}
			if current, ok := currentByID[snapshot.TaskMergeRequestID]; ok {
				comparison.Current = generated.NewOptTaskMergeRequest(current)
			}
			comparisons[index] = comparison
		}
		response.Review = generated.NewOptTaskDeliveryReview(generated.TaskDeliveryReview{
			ReviewCycle:   delivery.Review.ReviewCycle,
			MergeRequests: comparisons,
		})
	}
	return &generated.TaskDeliveryHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func currentMergeRequest(
	items map[uuid.UUID]domain.TaskMergeRequest,
	id uuid.UUID,
) *domain.TaskMergeRequest {
	item, ok := items[id]
	if !ok {
		return nil
	}
	return &item
}

func (h *Handler) LinkTaskMergeRequest(
	ctx context.Context,
	req *generated.TaskMergeRequestLink,
	params generated.LinkTaskMergeRequestParams,
) (generated.LinkTaskMergeRequestRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(
		ctx, params.Number, params.IfMatch,
	)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := h.Delivery.LinkMergeRequest(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion,
		req.MergeRequestURL.String(), subject, actor, operation,
	)
	if err != nil {
		return nil, err
	}
	return taskMergeRequestMutationResponse(ctx, mutation)
}

func (h *Handler) UnlinkTaskMergeRequest(
	ctx context.Context,
	req *generated.TaskMergeRequestUnlink,
	params generated.UnlinkTaskMergeRequestParams,
) (generated.UnlinkTaskMergeRequestRes, error) {
	expectedVersion, operation, actor, err := h.workflowTaskCommandContext(
		ctx, params.Number, params.IfMatch,
	)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := h.Delivery.UnlinkMergeRequest(
		ctx, params.Number, params.ID, expectedVersion, req.ClaimVersion, params.LinkID,
		subject, actor, operation,
	)
	if err != nil {
		return nil, err
	}
	return taskMergeRequestMutationResponse(ctx, mutation)
}

func taskMergeRequestMutationResponse(
	ctx context.Context,
	mutation store.TaskMergeRequestMutation,
) (*generated.TaskMergeRequestChangedHeaders, error) {
	mergeRequest, err := taskMergeRequestFromDomain(mutation.MergeRequest)
	if err != nil {
		return nil, err
	}
	return &generated.TaskMergeRequestChangedHeaders{
		Etag:       generated.NewOptString(formatETag(mutation.Task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.TaskMergeRequestMutation{
			Task:         taskWorkflowFromDomain(mutation.Task),
			MergeRequest: mergeRequest,
		},
	}, nil
}

func taskMergeRequestFromDomain(
	item store.TaskMergeRequestWithRepository,
) (generated.TaskMergeRequest, error) {
	repositoryURL, err := url.Parse(item.Connection.CanonicalWebURL)
	if err != nil {
		return generated.TaskMergeRequest{}, fmt.Errorf("parse GitLab repository URL: %w", err)
	}
	mergeRequestURL, err := url.Parse(item.MergeRequest.WebURL)
	if err != nil {
		return generated.TaskMergeRequest{}, fmt.Errorf("parse GitLab merge request URL: %w", err)
	}
	return generated.TaskMergeRequest{
		ID:                   item.MergeRequest.ID,
		ProjectRepositoryID:  item.MergeRequest.ProjectRepositoryID,
		RepositoryURL:        *repositoryURL,
		MergeRequestIid:      item.MergeRequest.MergeRequestIID,
		GitlabMergeRequestID: item.MergeRequest.GitLabMergeRequestID,
		WebURL:               *mergeRequestURL,
		LinkedBy:             actorFromDomain(item.MergeRequest.LinkedBy),
		LinkedThroughClaimID: item.MergeRequest.LinkedThroughClaimID,
		LinkedAt:             item.MergeRequest.LinkedAt,
		LatestObservation:    gitLabObservationFromDomain(item.MergeRequest.LatestObservation),
	}, nil
}

func gitLabObservationFromDomain(
	observation domain.GitLabMergeRequestObservation,
) generated.GitLabMergeRequestObservation {
	out := generated.GitLabMergeRequestObservation{
		Status:     generated.GitLabObservationStatus(observation.Status),
		ObservedAt: observation.ObservedAt, Title: observation.Title,
		State: generated.GitLabMergeRequestState(observation.State), Draft: observation.Draft,
		SourceBranch: observation.SourceBranch, TargetBranch: observation.TargetBranch,
		HeadSha: observation.HeadSHA, ProviderUpdatedAt: observation.ProviderUpdatedAt,
	}
	if observation.MergeCommitSHA != nil {
		out.MergeCommitSha = generated.NewOptString(*observation.MergeCommitSHA)
	}
	if observation.MergedAt != nil {
		out.MergedAt = generated.NewOptDateTime(*observation.MergedAt)
	}
	return out
}

func mergeRequestSnapshotFromDomain(
	snapshot domain.MergeRequestSnapshot,
) generated.MergeRequestSnapshot {
	webURL, _ := url.Parse(snapshot.WebURL)
	out := generated.MergeRequestSnapshot{
		TaskMergeRequestID:  snapshot.TaskMergeRequestID,
		ProjectRepositoryID: snapshot.ProjectRepositoryID,
		ConnectionID:        snapshot.ConnectionID,
		GitlabProjectID:     snapshot.GitLabProjectID,
		MergeRequestIid:     snapshot.MergeRequestIID,
		WebURL:              *webURL,
		Title:               snapshot.Title,
		State:               generated.GitLabMergeRequestState(snapshot.State),
		Draft:               snapshot.Draft,
		SourceBranch:        snapshot.SourceBranch,
		TargetBranch:        snapshot.TargetBranch,
		HeadSha:             snapshot.HeadSHA,
		ObservationStatus:   generated.GitLabObservationStatus(snapshot.ObservationStatus),
		ObservedAt:          snapshot.ObservedAt,
	}
	if snapshot.MergeCommitSHA != nil {
		out.MergeCommitSha = generated.NewOptString(*snapshot.MergeCommitSHA)
	}
	if snapshot.MergedAt != nil {
		out.MergedAt = generated.NewOptDateTime(*snapshot.MergedAt)
	}
	return out
}
