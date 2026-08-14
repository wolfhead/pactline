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
	Connections  *store.RepositoryConnectionStore
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
	links = s.refreshLinks(ctx, links, requestID)
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
	links = s.refreshLinks(ctx, links, requestID)
	snapshots := make([]domain.CodeChangeSnapshot, len(links))
	for index, link := range links {
		snapshots[index] = domain.CodeChangeSnapshot{
			TaskCodeChangeID: link.CodeChange.ID, ProjectRepositoryID: link.Repository.ID,
			Provider: link.CodeChange.Provider, Kind: link.CodeChange.Kind,
			ChangeNumber: link.CodeChange.ChangeNumber, WebURL: link.CodeChange.WebURL,
			ProviderEvidence: link.CodeChange.ProviderEvidence,
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
) []store.TaskCodeChangeWithRepository {
	if len(links) == 0 {
		return links
	}
	results := append([]store.TaskCodeChangeWithRepository(nil), links...)
	refreshContext, cancelRefresh := context.WithTimeout(ctx, taskDeliveryRefreshTimeout)
	defer cancelRefresh()
	semaphore := make(chan struct{}, 4)
	var waitGroup sync.WaitGroup
	for index := range results {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
				results[index] = s.refreshProviderEvidence(refreshContext, results[index], requestID)
			case <-refreshContext.Done():
				if results[index].CodeChange.ProviderEvidence != nil {
					verification := domain.CodeChangeVerification{
						Status: domain.CodeChangeVerificationUnreachable, AttemptedAt: s.now(),
					}
					s.persistVerification(ctx, &results[index], verification, requestID)
				}
			}
		}(index)
	}
	waitGroup.Wait()
	return results
}

func (s *TaskDeliveryService) refreshProviderEvidence(
	ctx context.Context,
	item store.TaskCodeChangeWithRepository,
	requestID string,
) store.TaskCodeChangeWithRepository {
	now := s.now()
	if s.Connections == nil {
		return item
	}
	connection, err := s.Connections.FindActiveByRepository(ctx, item.Repository.Reference())
	if errors.Is(err, domain.ErrNotFound) {
		if item.CodeChange.ProviderEvidence != nil {
			s.persistVerification(ctx, &item, domain.CodeChangeVerification{
				Status: domain.CodeChangeVerificationDisconnected, AttemptedAt: now,
			}, requestID)
		}
		return item
	}
	if err != nil {
		slog.WarnContext(ctx, "match repository connection for delivery evidence",
			"project_repository_id", item.Repository.ID, "provider", item.Repository.Provider,
			"request_id", requestID, "error", err)
		return item
	}
	if s.Providers == nil || s.Cipher == nil {
		s.persistVerification(ctx, &item, domain.CodeChangeVerification{
			Status: domain.CodeChangeVerificationDisconnected, AttemptedAt: now,
		}, requestID)
		return item
	}
	provider, err := s.Providers.Provider(connection.Provider)
	if err != nil {
		s.persistVerification(ctx, &item, domain.CodeChangeVerification{
			Status: domain.CodeChangeVerificationDisconnected, AttemptedAt: now,
		}, requestID)
		return item
	}
	credential, err := s.Cipher.Decrypt(
		connection.EncryptionKeyID, connection.CredentialCiphertext,
	)
	if err != nil {
		slog.ErrorContext(ctx, "decrypt repository delivery credential",
			"connection_id", connection.ID, "provider", connection.Provider,
			"provider_repository_id", connection.ProviderRepositoryID,
			"change_number", item.CodeChange.ChangeNumber,
			"request_id", requestID, "error", err)
		s.persistVerification(ctx, &item, domain.CodeChangeVerification{
			Status: domain.CodeChangeVerificationDisconnected, AttemptedAt: now,
		}, requestID)
		return item
	}
	defer clear(credential)
	codeChange, err := provider.GetCodeChange(
		ctx, item.Repository.Reference(), connection.ProviderRepositoryID,
		item.CodeChange.Kind, item.CodeChange.ChangeNumber, credential, requestID,
	)
	if err != nil {
		mapped := mapRepositoryProviderError(connection.Provider, err)
		status := domain.CodeChangeVerificationUnreachable
		switch {
		case errors.Is(mapped, domain.ErrNotFound):
			status = domain.CodeChangeVerificationMissing
		case errors.Is(mapped, domain.ErrProviderUnauthorized):
			status = domain.CodeChangeVerificationUnauthorized
		}
		s.persistVerification(ctx, &item, domain.CodeChangeVerification{Status: status, AttemptedAt: now}, requestID)
		return item
	}
	providerReference, parseErr := provider.ParseCodeChangeURL(codeChange.WebURL)
	if parseErr != nil || providerReference.Repository.Provider != item.Repository.Provider ||
		providerReference.Repository.Origin != item.Repository.Origin ||
		providerReference.Repository.PathLookupKey != item.Repository.PathLookupKey ||
		(item.CodeChange.ProviderEvidence != nil &&
			codeChange.ProviderChangeID != item.CodeChange.ProviderEvidence.ProviderChangeID) ||
		codeChange.ChangeNumber != item.CodeChange.ChangeNumber || codeChange.Kind != item.CodeChange.Kind {
		slog.WarnContext(ctx, "repository delivery identity changed",
			"connection_id", connection.ID, "provider", connection.Provider,
			"provider_repository_id", connection.ProviderRepositoryID,
			"kind", item.CodeChange.Kind, "change_number", item.CodeChange.ChangeNumber,
			"request_id", requestID)
		s.persistVerification(ctx, &item, domain.CodeChangeVerification{
			Status: domain.CodeChangeVerificationMissing, AttemptedAt: now,
		}, requestID)
		return item
	}
	observation := codeChange.Observation
	if observation.Status != domain.CodeChangeObservationConfirmed {
		s.persistVerification(ctx, &item, domain.CodeChangeVerification{
			Status: domain.CodeChangeVerificationUnreachable, AttemptedAt: now,
		}, requestID)
		return item
	}
	evidence := domain.CodeChangeProviderEvidence{
		ConnectionID: connection.ID, ProviderRepositoryID: connection.ProviderRepositoryID,
		ProviderChangeID: codeChange.ProviderChangeID, Title: observation.Title,
		State: observation.State, Draft: observation.Draft,
		SourceBranch: observation.SourceBranch, TargetBranch: observation.TargetBranch,
		HeadSHA: observation.HeadSHA, MergeCommitSHA: observation.MergeCommitSHA,
		MergedAt: observation.MergedAt, ProviderUpdatedAt: observation.ProviderUpdatedAt,
		ObservedAt: observation.ObservedAt,
	}
	verification := domain.CodeChangeVerification{
		Status: domain.CodeChangeVerificationVerified, AttemptedAt: now,
	}
	if err := s.CodeChanges.UpdateProviderEvidence(ctx, item.CodeChange.ID, evidence, verification, now); err != nil {
		slog.WarnContext(ctx, "persist repository delivery evidence",
			"task_code_change_id", item.CodeChange.ID, "connection_id", connection.ID,
			"provider", connection.Provider, "request_id", requestID, "error", err)
		return item
	}
	item.CodeChange.ProviderEvidence = &evidence
	item.CodeChange.ProviderVerification = &verification
	return item
}

func (s *TaskDeliveryService) persistVerification(
	ctx context.Context,
	item *store.TaskCodeChangeWithRepository,
	verification domain.CodeChangeVerification,
	requestID string,
) {
	if err := s.CodeChanges.UpdateProviderVerification(ctx, item.CodeChange.ID, verification, s.now()); err != nil {
		slog.WarnContext(ctx, "persist repository delivery verification",
			"task_code_change_id", item.CodeChange.ID, "provider", item.CodeChange.Provider,
			"verification_status", verification.Status, "request_id", requestID, "error", err)
		return
	}
	item.CodeChange.ProviderVerification = &verification
}

func DeliveryComparison(snapshot domain.CodeChangeSnapshot, current *domain.TaskCodeChange) string {
	if current == nil {
		return "missing"
	}
	if snapshot.ProviderEvidence == nil || current.ProviderEvidence == nil {
		return "unverified"
	}
	if current.ProviderVerification != nil {
		switch current.ProviderVerification.Status {
		case domain.CodeChangeVerificationMissing:
			return "missing"
		case domain.CodeChangeVerificationUnauthorized:
			return "unauthorized"
		case domain.CodeChangeVerificationUnreachable:
			return "unreachable"
		case domain.CodeChangeVerificationDisconnected:
			return "disconnected"
		}
	}
	if current.ProviderEvidence.State == domain.CodeChangeStateMerged {
		return "merged"
	}
	if current.ProviderEvidence.HeadSHA != snapshot.ProviderEvidence.HeadSHA {
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
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	candidates, err := s.Providers.CodeChangeURLCandidates(codeChangeURL)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	var reference domain.CodeChangeReference
	var repository domain.ProjectRepository
	matchCount := 0
	for _, candidate := range candidates {
		matched, findErr := s.Repositories.FindActiveByReference(ctx, task.Task.ProjectID, candidate.Repository)
		if errors.Is(findErr, domain.ErrNotFound) {
			continue
		}
		if findErr != nil {
			return store.TaskCodeChangeMutation{}, findErr
		}
		reference, repository = candidate, matched
		matchCount++
	}
	if matchCount == 0 {
		return store.TaskCodeChangeMutation{}, domain.ErrNotFound
	}
	if matchCount > 1 {
		return store.TaskCodeChangeMutation{}, fmt.Errorf(
			"%w: code change URL matches more than one repository bound to the Project", domain.ErrConflict,
		)
	}
	mutation, err := s.CodeChanges.Link(
		ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		repository.ID, reference, actor, operation, s.now(),
	)
	if err != nil {
		return store.TaskCodeChangeMutation{}, err
	}
	mutation.CodeChange = s.refreshProviderEvidence(ctx, mutation.CodeChange, operation.RequestID)
	return mutation, nil
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
