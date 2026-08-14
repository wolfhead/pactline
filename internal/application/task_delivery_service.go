package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

const taskDeliveryRefreshTimeout = 15 * time.Second

type TaskDeliveryService struct {
	CodeChanges  *store.TaskCodeChangeStore
	Repositories *store.ProjectRepositoryStore
	Access       *ProjectAccessService
	Providers    *RepositoryProviderRegistry
	Cipher       *identity.CredentialCipher
	Now          func() time.Time
}

type TaskDelivery struct {
	ActiveLinks []store.TaskCodeChangeWithRepository
	Review      *store.TaskDeliverySnapshot
}

func (s *TaskDeliveryService) GetDelivery(
	ctx context.Context,
	taskNumber int64,
	subject domain.ProjectAccessSubject,
	requestID string,
) (TaskDelivery, error) {
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return TaskDelivery{}, err
	}
	links, err := s.CodeChanges.ListActive(ctx, task.Task.ID)
	if err != nil {
		return TaskDelivery{}, err
	}
	links, err = s.refreshLinks(ctx, links, requestID)
	if err != nil {
		return TaskDelivery{}, err
	}
	var review *store.TaskDeliverySnapshot
	if task.Task.Phase == domain.TaskPhaseInReview || task.Task.Phase == domain.TaskPhaseDone {
		review, err = s.CodeChanges.GetReviewSnapshot(ctx, task.Task.ID, task.Task.ReviewCycle)
		if err != nil {
			return TaskDelivery{}, err
		}
	}
	return TaskDelivery{ActiveLinks: links, Review: review}, nil
}

func (s *TaskDeliveryService) PrepareCompletion(
	ctx context.Context,
	taskNumber int64,
	subject domain.ProjectAccessSubject,
	requestID string,
) ([]domain.CodeChangeSnapshot, error) {
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return nil, err
	}
	links, err := s.CodeChanges.ListActive(ctx, task.Task.ID)
	if err != nil {
		return nil, err
	}
	links, err = s.refreshLinks(ctx, links, requestID)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.CodeChangeSnapshot, len(links))
	for index, link := range links {
		observation := link.CodeChange.LatestObservation
		snapshots[index] = domain.CodeChangeSnapshot{
			TaskCodeChangeID:     link.CodeChange.ID,
			ProjectRepositoryID:  link.Repository.ID,
			ConnectionID:         link.Connection.ID,
			Provider:             link.Connection.Provider,
			ProviderRepositoryID: link.Connection.ProviderRepositoryID,
			Kind:                 link.CodeChange.Kind,
			ChangeNumber:         link.CodeChange.ChangeNumber,
			ProviderChangeID:     link.CodeChange.ProviderChangeID,
			WebURL:               link.CodeChange.WebURL, Title: observation.Title,
			State: observation.State, Draft: observation.Draft,
			SourceBranch: observation.SourceBranch, TargetBranch: observation.TargetBranch,
			HeadSHA: observation.HeadSHA, MergeCommitSHA: observation.MergeCommitSHA,
			MergedAt: observation.MergedAt, ObservationStatus: observation.Status,
			ObservedAt: observation.ObservedAt,
		}
		if err := snapshots[index].Validate(); err != nil {
			return nil, err
		}
	}
	return snapshots, nil
}

func (s *TaskDeliveryService) refreshLinks(
	ctx context.Context,
	links []store.TaskCodeChangeWithRepository,
	requestID string,
) ([]store.TaskCodeChangeWithRepository, error) {
	if len(links) == 0 {
		return links, nil
	}
	results := append([]store.TaskCodeChangeWithRepository(nil), links...)
	refreshContext, cancelRefresh := context.WithTimeout(ctx, taskDeliveryRefreshTimeout)
	defer cancelRefresh()
	semaphore := make(chan struct{}, 4)
	var waitGroup sync.WaitGroup
	var firstError error
	var errorLock sync.Mutex
	for index := range results {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			var observation domain.CodeChangeObservation
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
				observation = s.refreshObservation(refreshContext, results[index], requestID)
			case <-refreshContext.Done():
				observation = domain.CodeChangeObservation{
					Status: domain.CodeChangeObservationUnreachable, ObservedAt: s.now(),
				}
			}
			if err := s.CodeChanges.UpdateObservation(
				ctx, results[index].CodeChange.ID, observation, s.now(),
			); err != nil {
				errorLock.Lock()
				if firstError == nil {
					firstError = err
				}
				errorLock.Unlock()
				return
			}
			if observation.Status == domain.CodeChangeObservationConfirmed {
				results[index].CodeChange.LatestObservation = observation
			} else {
				results[index].CodeChange.LatestObservation.Status = observation.Status
				results[index].CodeChange.LatestObservation.ObservedAt = observation.ObservedAt
			}
		}(index)
	}
	waitGroup.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if firstError != nil {
		return nil, firstError
	}
	return results, nil
}

func (s *TaskDeliveryService) refreshObservation(
	ctx context.Context,
	item store.TaskCodeChangeWithRepository,
	requestID string,
) domain.CodeChangeObservation {
	now := s.now()
	if item.Connection.Status != domain.RepositoryConnectionStatusActive || s.Providers == nil || s.Cipher == nil {
		return domain.CodeChangeObservation{Status: domain.CodeChangeObservationDisconnected, ObservedAt: now}
	}
	provider, err := s.Providers.Provider(item.Connection.Provider)
	if err != nil {
		return domain.CodeChangeObservation{Status: domain.CodeChangeObservationDisconnected, ObservedAt: now}
	}
	credential, err := s.Cipher.Decrypt(
		item.Connection.EncryptionKeyID, item.Connection.CredentialCiphertext,
	)
	if err != nil {
		slog.ErrorContext(ctx, "decrypt repository delivery credential",
			"connection_id", item.Connection.ID,
			"provider", item.Connection.Provider,
			"provider_repository_id", item.Connection.ProviderRepositoryID,
			"change_number", item.CodeChange.ChangeNumber,
			"request_id", requestID, "error", err)
		return domain.CodeChangeObservation{Status: domain.CodeChangeObservationDisconnected, ObservedAt: now}
	}
	defer clear(credential)
	codeChange, err := provider.GetCodeChange(
		ctx, item.Connection.Origin, item.Connection.ProviderRepositoryID,
		item.CodeChange.Kind, item.CodeChange.ChangeNumber, credential, requestID,
	)
	if err != nil {
		mapped := mapRepositoryProviderError(item.Connection.Provider, err)
		status := domain.CodeChangeObservationUnreachable
		switch {
		case errors.Is(mapped, domain.ErrNotFound):
			status = domain.CodeChangeObservationMissing
		case errors.Is(mapped, domain.ErrProviderUnauthorized):
			status = domain.CodeChangeObservationUnauthorized
		}
		return domain.CodeChangeObservation{Status: status, ObservedAt: now}
	}
	if codeChange.ProviderChangeID != item.CodeChange.ProviderChangeID ||
		codeChange.ChangeNumber != item.CodeChange.ChangeNumber || codeChange.Kind != item.CodeChange.Kind {
		slog.WarnContext(ctx, "repository delivery identity changed",
			"connection_id", item.Connection.ID,
			"provider", item.Connection.Provider,
			"provider_repository_id", item.Connection.ProviderRepositoryID,
			"kind", item.CodeChange.Kind, "change_number", item.CodeChange.ChangeNumber,
			"request_id", requestID)
		return domain.CodeChangeObservation{Status: domain.CodeChangeObservationMissing, ObservedAt: now}
	}
	return codeChange.Observation
}

func DeliveryComparison(snapshot domain.CodeChangeSnapshot, current *domain.TaskCodeChange) string {
	if current == nil {
		return "missing"
	}
	switch current.LatestObservation.Status {
	case domain.CodeChangeObservationMissing:
		return "missing"
	case domain.CodeChangeObservationUnauthorized:
		return "unauthorized"
	case domain.CodeChangeObservationUnreachable:
		return "unreachable"
	case domain.CodeChangeObservationDisconnected:
		return "disconnected"
	}
	if current.LatestObservation.State == domain.CodeChangeStateMerged {
		return "merged"
	}
	if current.LatestObservation.HeadSHA != snapshot.HeadSHA {
		return "moved"
	}
	return "unchanged"
}

func (s *TaskDeliveryService) LinkCodeChange(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	codeChangeURL string,
	subject domain.ProjectAccessSubject,
	actor domain.Actor,
	operation domain.OperationActor,
) (store.TaskCodeChangeMutation, error) {
	if s.Providers == nil || s.Cipher == nil {
		return store.TaskCodeChangeMutation{}, domain.ErrIntegrationNotConfigured
	}
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	reference, err := s.Providers.MatchCodeChangeURL(codeChangeURL)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	repository, err := s.Repositories.FindActiveByReference(ctx, task.Task.ProjectID, reference.Repository)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	provider, err := s.Providers.Provider(repository.Connection.Provider)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	credential, err := s.Cipher.Decrypt(
		repository.Connection.EncryptionKeyID, repository.Connection.CredentialCiphertext,
	)
	if err != nil {
		return store.TaskCodeChangeMutation{}, fmt.Errorf("decrypt repository credential: %w", err)
	}
	defer clear(credential)
	codeChange, err := provider.GetCodeChange(
		ctx, repository.Connection.Origin, repository.Connection.ProviderRepositoryID,
		reference.Kind, reference.ChangeNumber, credential, operation.RequestID,
	)
	if err != nil {
		return store.TaskCodeChangeMutation{}, mapRepositoryProviderError(repository.Connection.Provider, err)
	}
	providerReference, err := provider.ParseCodeChangeURL(codeChange.WebURL)
	if err != nil || providerReference.Repository.Provider != reference.Repository.Provider ||
		providerReference.Repository.Origin != reference.Repository.Origin ||
		providerReference.Repository.PathLookupKey != reference.Repository.PathLookupKey ||
		providerReference.Kind != reference.Kind || providerReference.ChangeNumber != reference.ChangeNumber {
		return store.TaskCodeChangeMutation{}, fmt.Errorf(
			"%w: provider returned a different code change identity", domain.ErrProviderRejected,
		)
	}
	codeChange.WebURL = providerReference.WebURL
	return s.CodeChanges.Link(
		ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		repository.Repository.ID, codeChange, actor, operation, s.now(),
	)
}

func (s *TaskDeliveryService) UnlinkCodeChange(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	linkID uuid.UUID,
	subject domain.ProjectAccessSubject,
	actor domain.Actor,
	operation domain.OperationActor,
) (store.TaskCodeChangeMutation, error) {
	if _, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead); err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	return s.CodeChanges.Unlink(
		ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		linkID, actor, operation, s.now(),
	)
}

func (s *TaskDeliveryService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
