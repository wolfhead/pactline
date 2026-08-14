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
	delivery, err := h.Delivery.GetDelivery(ctx, params.Number, subject, baseapi.RequestIDFromContext(ctx))
	if err != nil {
		return nil, err
	}
	response, err := taskDeliveryFromApplication(delivery)
	if err != nil {
		return nil, err
	}
	return &generated.TaskDeliveryHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)), Response: response,
	}, nil
}

func taskDeliveryFromApplication(delivery application.TaskDelivery) (generated.TaskDelivery, error) {
	activeLinks := make([]generated.TaskCodeChange, len(delivery.ActiveLinks))
	currentByID := make(map[uuid.UUID]generated.TaskCodeChange, len(delivery.ActiveLinks))
	currentDomainByID := make(map[uuid.UUID]domain.TaskCodeChange, len(delivery.ActiveLinks))
	for index, item := range delivery.ActiveLinks {
		converted, err := taskCodeChangeFromDomain(item)
		if err != nil {
			return generated.TaskDelivery{}, err
		}
		activeLinks[index] = converted
		currentByID[item.CodeChange.ID] = converted
		currentDomainByID[item.CodeChange.ID] = item.CodeChange
	}
	response := generated.TaskDelivery{ActiveLinks: activeLinks}
	if delivery.Review != nil {
		comparisons := make([]generated.TaskDeliveryComparison, len(delivery.Review.CodeChanges))
		for index, snapshot := range delivery.Review.CodeChanges {
			comparison := generated.TaskDeliveryComparison{
				Snapshot: codeChangeSnapshotFromDomain(snapshot),
				Comparison: generated.TaskDeliveryComparisonComparison(
					application.DeliveryComparison(snapshot, currentCodeChange(
						currentDomainByID, snapshot.TaskCodeChangeID,
					)),
				),
			}
			if current, ok := currentByID[snapshot.TaskCodeChangeID]; ok {
				comparison.Current = generated.NewOptTaskCodeChange(current)
			}
			comparisons[index] = comparison
		}
		response.Review = generated.NewOptTaskDeliveryReview(generated.TaskDeliveryReview{
			ReviewCycle: delivery.Review.ReviewCycle, CodeChanges: comparisons,
		})
	}
	return response, nil
}

func currentCodeChange(items map[uuid.UUID]domain.TaskCodeChange, id uuid.UUID) *domain.TaskCodeChange {
	item, ok := items[id]
	if !ok {
		return nil
	}
	return &item
}

func (h *Handler) LinkTaskCodeChange(
	ctx context.Context,
	req *generated.TaskCodeChangeLink,
	params generated.LinkTaskCodeChangeParams,
) (generated.LinkTaskCodeChangeRes, error) {
	expectedVersion, operation, actor, claim, err := h.claimCommandContext(ctx, params.ClaimID, params.IfMatch)
	if err != nil {
		return nil, err
	}
	if _, _, err := claimClientProvenance(actor, params.PactlineClientKind.Or(""), params.PactlineClientSessionID.Or("")); err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := h.Delivery.LinkCodeChange(
		ctx, claim.TaskNumber, params.ClaimID, expectedVersion, claim.Version,
		req.CodeChangeURL.String(), subject, actor, operation,
	)
	if err != nil {
		return nil, err
	}
	return taskCodeChangeMutationResponse(ctx, mutation)
}

func (h *Handler) UnlinkTaskCodeChange(
	ctx context.Context,
	params generated.UnlinkTaskCodeChangeParams,
) (generated.UnlinkTaskCodeChangeRes, error) {
	expectedVersion, operation, actor, claim, err := h.claimCommandContext(ctx, params.ClaimID, params.IfMatch)
	if err != nil {
		return nil, err
	}
	if _, _, err := claimClientProvenance(actor, params.PactlineClientKind.Or(""), params.PactlineClientSessionID.Or("")); err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := h.Delivery.UnlinkCodeChange(
		ctx, claim.TaskNumber, params.ClaimID, expectedVersion, claim.Version, params.LinkID,
		subject, actor, operation,
	)
	if err != nil {
		return nil, err
	}
	return taskCodeChangeMutationResponse(ctx, mutation)
}

func taskCodeChangeMutationResponse(
	ctx context.Context,
	mutation store.TaskCodeChangeMutation,
) (*generated.TaskCodeChangeChangedHeaders, error) {
	codeChange, err := taskCodeChangeFromDomain(mutation.CodeChange)
	if err != nil {
		return nil, err
	}
	return &generated.TaskCodeChangeChangedHeaders{
		Etag:       generated.NewOptString(formatETag(mutation.Task.Version)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response: generated.TaskCodeChangeMutation{
			Task: taskWorkflowFromDomain(mutation.Task), CodeChange: codeChange,
		},
	}, nil
}

func taskCodeChangeFromDomain(item store.TaskCodeChangeWithRepository) (generated.TaskCodeChange, error) {
	repositoryURL, err := url.Parse(item.Connection.CanonicalWebURL)
	if err != nil {
		return generated.TaskCodeChange{}, fmt.Errorf("parse canonical repository URL: %w", err)
	}
	codeChangeURL, err := url.Parse(item.CodeChange.WebURL)
	if err != nil {
		return generated.TaskCodeChange{}, fmt.Errorf("parse code change URL: %w", err)
	}
	return generated.TaskCodeChange{
		ID:                   item.CodeChange.ID,
		ProjectRepositoryID:  item.CodeChange.ProjectRepositoryID,
		Provider:             generated.RepositoryProvider(item.Connection.Provider),
		RepositoryURL:        *repositoryURL,
		Kind:                 generated.CodeChangeKind(item.CodeChange.Kind),
		ChangeNumber:         item.CodeChange.ChangeNumber,
		ProviderChangeID:     item.CodeChange.ProviderChangeID,
		WebURL:               *codeChangeURL,
		LinkedBy:             actorFromDomain(item.CodeChange.LinkedBy),
		LinkedThroughClaimID: item.CodeChange.LinkedThroughClaimID,
		LinkedAt:             item.CodeChange.LinkedAt,
		LatestObservation:    codeChangeObservationFromDomain(item.CodeChange.LatestObservation),
	}, nil
}

func codeChangeObservationFromDomain(observation domain.CodeChangeObservation) generated.CodeChangeObservation {
	out := generated.CodeChangeObservation{
		Status:     generated.CodeChangeObservationStatus(observation.Status),
		ObservedAt: observation.ObservedAt, Title: observation.Title,
		State: generated.CodeChangeState(observation.State), Draft: observation.Draft,
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

func codeChangeSnapshotFromDomain(snapshot domain.CodeChangeSnapshot) generated.CodeChangeSnapshot {
	webURL, _ := url.Parse(snapshot.WebURL)
	out := generated.CodeChangeSnapshot{
		TaskCodeChangeID:     snapshot.TaskCodeChangeID,
		ProjectRepositoryID:  snapshot.ProjectRepositoryID,
		ConnectionID:         snapshot.ConnectionID,
		Provider:             generated.RepositoryProvider(snapshot.Provider),
		ProviderRepositoryID: snapshot.ProviderRepositoryID,
		Kind:                 generated.CodeChangeKind(snapshot.Kind),
		ChangeNumber:         snapshot.ChangeNumber,
		ProviderChangeID:     snapshot.ProviderChangeID,
		WebURL:               *webURL, Title: snapshot.Title,
		State: generated.CodeChangeState(snapshot.State), Draft: snapshot.Draft,
		SourceBranch: snapshot.SourceBranch, TargetBranch: snapshot.TargetBranch,
		HeadSha:           snapshot.HeadSHA,
		ObservationStatus: generated.CodeChangeObservationStatus(snapshot.ObservationStatus),
		ObservedAt:        snapshot.ObservedAt,
	}
	if snapshot.MergeCommitSHA != nil {
		out.MergeCommitSha = generated.NewOptString(*snapshot.MergeCommitSHA)
	}
	if snapshot.MergedAt != nil {
		out.MergedAt = generated.NewOptDateTime(*snapshot.MergedAt)
	}
	return out
}
