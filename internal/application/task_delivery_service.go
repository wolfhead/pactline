package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"
	gitlabintegration "github.com/wolfhead/pactline/internal/integrations/gitlab"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

const taskDeliveryRefreshTimeout = 15 * time.Second

type taskDeliveryProvider interface {
	GetMergeRequest(
		context.Context, string, int64, int64, []byte, string,
	) (domain.GitLabMergeRequest, error)
}

type TaskDeliveryService struct {
	MergeRequests *store.TaskMergeRequestStore
	Repositories  *store.ProjectRepositoryStore
	Access        *ProjectAccessService
	Provider      taskDeliveryProvider
	Cipher        *identity.CredentialCipher
	Now           func() time.Time
}

type TaskDelivery struct {
	ActiveLinks []store.TaskMergeRequestWithRepository
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
	links, err := s.MergeRequests.ListActive(ctx, task.Task.ID)
	if err != nil {
		return TaskDelivery{}, err
	}
	links, err = s.refreshLinks(ctx, links, requestID)
	if err != nil {
		return TaskDelivery{}, err
	}
	var review *store.TaskDeliverySnapshot
	if task.Task.Phase == domain.TaskPhaseInReview || task.Task.Phase == domain.TaskPhaseDone {
		review, err = s.MergeRequests.GetReviewSnapshot(ctx, task.Task.ID, task.Task.ReviewCycle)
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
) ([]domain.MergeRequestSnapshot, error) {
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return nil, err
	}
	links, err := s.MergeRequests.ListActive(ctx, task.Task.ID)
	if err != nil {
		return nil, err
	}
	links, err = s.refreshLinks(ctx, links, requestID)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.MergeRequestSnapshot, len(links))
	for index, link := range links {
		observation := link.MergeRequest.LatestObservation
		snapshots[index] = domain.MergeRequestSnapshot{
			TaskMergeRequestID:  link.MergeRequest.ID,
			ProjectRepositoryID: link.Repository.ID,
			ConnectionID:        link.Connection.ID,
			GitLabProjectID:     link.Connection.GitLabProjectID,
			MergeRequestIID:     link.MergeRequest.MergeRequestIID,
			WebURL:              link.MergeRequest.WebURL, Title: observation.Title,
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
	links []store.TaskMergeRequestWithRepository,
	requestID string,
) ([]store.TaskMergeRequestWithRepository, error) {
	if len(links) == 0 {
		return links, nil
	}
	results := make([]store.TaskMergeRequestWithRepository, len(links))
	copy(results, links)
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
			observation := domain.GitLabMergeRequestObservation{}
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
				observation = s.refreshObservation(refreshContext, results[index], requestID)
			case <-refreshContext.Done():
				observation = domain.GitLabMergeRequestObservation{
					Status: domain.GitLabObservationUnreachable, ObservedAt: s.now(),
				}
			}
			if err := s.MergeRequests.UpdateObservation(
				ctx, results[index].MergeRequest.ID, observation, s.now(),
			); err != nil {
				errorLock.Lock()
				if firstError == nil {
					firstError = err
				}
				errorLock.Unlock()
				return
			}
			if observation.Status == domain.GitLabObservationConfirmed {
				results[index].MergeRequest.LatestObservation = observation
			} else {
				results[index].MergeRequest.LatestObservation.Status = observation.Status
				results[index].MergeRequest.LatestObservation.ObservedAt = observation.ObservedAt
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
	item store.TaskMergeRequestWithRepository,
	requestID string,
) domain.GitLabMergeRequestObservation {
	now := s.now()
	if item.Connection.Status != domain.GitLabConnectionStatusActive || s.Provider == nil || s.Cipher == nil {
		return domain.GitLabMergeRequestObservation{
			Status: domain.GitLabObservationDisconnected, ObservedAt: now,
		}
	}
	credential, err := s.Cipher.Decrypt(
		item.Connection.EncryptionKeyID, item.Connection.CredentialCiphertext,
	)
	if err != nil {
		slog.ErrorContext(ctx, "decrypt GitLab delivery credential",
			"connection_id", item.Connection.ID,
			"gitlab_project_id", item.Connection.GitLabProjectID,
			"merge_request_iid", item.MergeRequest.MergeRequestIID,
			"request_id", requestID, "error", err)
		return domain.GitLabMergeRequestObservation{
			Status: domain.GitLabObservationDisconnected, ObservedAt: now,
		}
	}
	defer clear(credential)
	mergeRequest, err := s.Provider.GetMergeRequest(
		ctx, item.Connection.Origin, item.Connection.GitLabProjectID,
		item.MergeRequest.MergeRequestIID, credential, requestID,
	)
	if err != nil {
		status := domain.GitLabObservationUnreachable
		switch gitlabintegration.ErrorCategoryOf(err) {
		case gitlabintegration.ErrorNotFound:
			status = domain.GitLabObservationMissing
		case gitlabintegration.ErrorUnauthorized:
			status = domain.GitLabObservationUnauthorized
		}
		return domain.GitLabMergeRequestObservation{Status: status, ObservedAt: now}
	}
	if mergeRequest.ID != item.MergeRequest.GitLabMergeRequestID ||
		mergeRequest.IID != item.MergeRequest.MergeRequestIID {
		slog.WarnContext(ctx, "GitLab delivery identity changed",
			"connection_id", item.Connection.ID,
			"gitlab_project_id", item.Connection.GitLabProjectID,
			"merge_request_iid", item.MergeRequest.MergeRequestIID,
			"request_id", requestID)
		return domain.GitLabMergeRequestObservation{
			Status: domain.GitLabObservationMissing, ObservedAt: now,
		}
	}
	return mergeRequest.Observation
}

func DeliveryComparison(
	snapshot domain.MergeRequestSnapshot,
	current *domain.TaskMergeRequest,
) string {
	if current == nil {
		return "missing"
	}
	switch current.LatestObservation.Status {
	case domain.GitLabObservationMissing:
		return "missing"
	case domain.GitLabObservationUnauthorized:
		return "unauthorized"
	case domain.GitLabObservationUnreachable:
		return "unreachable"
	case domain.GitLabObservationDisconnected:
		return "disconnected"
	}
	if current.LatestObservation.State == domain.GitLabMergeRequestMerged {
		return "merged"
	}
	if current.LatestObservation.HeadSHA != snapshot.HeadSHA {
		return "moved"
	}
	return "unchanged"
}

func (s *TaskDeliveryService) LinkMergeRequest(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	mergeRequestURL string,
	subject domain.ProjectAccessSubject,
	actor domain.Actor,
	operation domain.OperationActor,
) (store.TaskMergeRequestMutation, error) {
	if s.Provider == nil || s.Cipher == nil {
		return store.TaskMergeRequestMutation{}, domain.ErrIntegrationNotConfigured
	}
	task, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead)
	if err != nil {
		return store.TaskMergeRequestMutation{}, err
	}
	reference, err := domain.ParseGitLabMergeRequestURL(mergeRequestURL)
	if err != nil {
		return store.TaskMergeRequestMutation{}, err
	}
	repository, err := s.Repositories.FindActiveByReference(
		ctx, task.Task.ProjectID, reference.Repository,
	)
	if err != nil {
		return store.TaskMergeRequestMutation{}, err
	}
	credential, err := s.Cipher.Decrypt(
		repository.Connection.EncryptionKeyID, repository.Connection.CredentialCiphertext,
	)
	if err != nil {
		return store.TaskMergeRequestMutation{}, fmt.Errorf("decrypt GitLab credential: %w", err)
	}
	defer clear(credential)
	mergeRequest, err := s.Provider.GetMergeRequest(
		ctx, repository.Connection.Origin, repository.Connection.GitLabProjectID,
		reference.IID, credential, operation.RequestID,
	)
	if err != nil {
		return store.TaskMergeRequestMutation{}, mapGitLabProviderError(err)
	}
	providerReference, err := domain.ParseGitLabMergeRequestURL(mergeRequest.WebURL)
	if err != nil || providerReference.Repository.Origin != reference.Repository.Origin ||
		providerReference.Repository.PathLookupKey != reference.Repository.PathLookupKey ||
		providerReference.IID != reference.IID {
		return store.TaskMergeRequestMutation{}, fmt.Errorf(
			"%w: GitLab returned a different merge request identity", domain.ErrProviderRejected,
		)
	}
	mergeRequest.WebURL = providerReference.WebURL
	return s.MergeRequests.Link(
		ctx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion,
		repository.Repository.ID, mergeRequest, actor, operation, s.now(),
	)
}

func (s *TaskDeliveryService) UnlinkMergeRequest(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	linkID uuid.UUID,
	subject domain.ProjectAccessSubject,
	actor domain.Actor,
	operation domain.OperationActor,
) (store.TaskMergeRequestMutation, error) {
	if _, err := s.Access.RequireTaskByNumber(ctx, taskNumber, subject, ProjectPermissionRead); err != nil {
		return store.TaskMergeRequestMutation{}, err
	}
	return s.MergeRequests.Unlink(
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
