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
			Task: taskWorkflowFromDomain(mutation.Task), CodeChange: codeChange, Changed: mutation.Changed,
		},
	}, nil
}

func taskCodeChangeFromDomain(item store.TaskCodeChangeWithRepository) (generated.TaskCodeChange, error) {
	repositoryURL, err := url.Parse(item.Repository.CanonicalWebURL)
	if err != nil {
		return generated.TaskCodeChange{}, fmt.Errorf("parse canonical repository URL: %w", err)
	}
	codeChangeURL, err := url.Parse(item.CodeChange.WebURL)
	if err != nil {
		return generated.TaskCodeChange{}, fmt.Errorf("parse code change URL: %w", err)
	}
	out := generated.TaskCodeChange{
		ID:                   item.CodeChange.ID,
		ProjectRepositoryID:  item.CodeChange.ProjectRepositoryID,
		Provider:             generated.RepositoryProvider(item.CodeChange.Provider),
		RepositoryURL:        *repositoryURL,
		Kind:                 generated.CodeChangeKind(item.CodeChange.Kind),
		ChangeNumber:         item.CodeChange.ChangeNumber,
		WebURL:               *codeChangeURL,
		LinkedBy:             actorFromDomain(item.CodeChange.LinkedBy),
		LinkedThroughClaimID: item.CodeChange.LinkedThroughClaimID,
		LinkedAt:             item.CodeChange.LinkedAt,
	}
	if item.CodeChange.ProviderEvidence != nil {
		out.ProviderEvidence = generated.NewOptCodeChangeProviderEvidence(
			codeChangeProviderEvidenceFromDomain(*item.CodeChange.ProviderEvidence),
		)
	}
	if item.CodeChange.ProviderVerification != nil {
		out.ProviderVerification = generated.NewOptCodeChangeVerification(generated.CodeChangeVerification{
			Status:      generated.CodeChangeVerificationStatus(item.CodeChange.ProviderVerification.Status),
			AttemptedAt: item.CodeChange.ProviderVerification.AttemptedAt,
		})
	}
	return out, nil
}

func codeChangeProviderEvidenceFromDomain(evidence domain.CodeChangeProviderEvidence) generated.CodeChangeProviderEvidence {
	out := generated.CodeChangeProviderEvidence{
		ConnectionID: evidence.ConnectionID, ProviderRepositoryID: evidence.ProviderRepositoryID,
		ProviderChangeID: evidence.ProviderChangeID, Title: evidence.Title,
		State: generated.CodeChangeState(evidence.State), Draft: evidence.Draft,
		SourceBranch: evidence.SourceBranch, TargetBranch: evidence.TargetBranch,
		HeadSha: evidence.HeadSHA, ProviderUpdatedAt: evidence.ProviderUpdatedAt,
		ObservedAt: evidence.ObservedAt,
	}
	if evidence.MergeCommitSHA != nil {
		out.MergeCommitSha = generated.NewOptString(*evidence.MergeCommitSHA)
	}
	if evidence.MergedAt != nil {
		out.MergedAt = generated.NewOptDateTime(*evidence.MergedAt)
	}
	return out
}

func codeChangeSnapshotFromDomain(snapshot domain.CodeChangeSnapshot) generated.CodeChangeSnapshot {
	webURL, _ := url.Parse(snapshot.WebURL)
	out := generated.CodeChangeSnapshot{
		TaskCodeChangeID:    snapshot.TaskCodeChangeID,
		ProjectRepositoryID: snapshot.ProjectRepositoryID,
		Provider:            generated.RepositoryProvider(snapshot.Provider),
		Kind:                generated.CodeChangeKind(snapshot.Kind), ChangeNumber: snapshot.ChangeNumber,
		WebURL: *webURL,
	}
	if snapshot.ProviderEvidence != nil {
		out.ProviderEvidence = generated.NewOptCodeChangeProviderEvidence(
			codeChangeProviderEvidenceFromDomain(*snapshot.ProviderEvidence),
		)
	}
	return out
}
