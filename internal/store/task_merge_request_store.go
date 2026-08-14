package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TaskDeliverySnapshot struct {
	ReviewCycle   int64
	MergeRequests []domain.MergeRequestSnapshot
}

type TaskMergeRequestWithRepository struct {
	MergeRequest domain.TaskMergeRequest
	Repository   domain.ProjectRepository
	Connection   domain.GitLabConnection
}

type TaskMergeRequestMutation struct {
	Task         TaskWorkflowSnapshot
	MergeRequest TaskMergeRequestWithRepository
}

type TaskMergeRequestStore struct{ db *DB }

func NewTaskMergeRequestStore(db *DB) *TaskMergeRequestStore {
	return &TaskMergeRequestStore{db: db}
}

func (s *TaskMergeRequestStore) ListActive(
	ctx context.Context,
	taskID uuid.UUID,
) ([]TaskMergeRequestWithRepository, error) {
	rows, err := s.db.Pool.Query(ctx, taskMergeRequestSelect+`
		WHERE merge_request.task_id=$1 AND merge_request.unlinked_at IS NULL
		ORDER BY connection.origin, connection.gitlab_project_id,
			merge_request.merge_request_iid, merge_request.id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list active Task merge requests: %w", err)
	}
	defer rows.Close()
	items := []TaskMergeRequestWithRepository{}
	for rows.Next() {
		item, err := scanTaskMergeRequestWithRepository(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *TaskMergeRequestStore) GetReviewSnapshot(
	ctx context.Context,
	taskID uuid.UUID,
	reviewCycle int64,
) (*TaskDeliverySnapshot, error) {
	if reviewCycle < 1 {
		return nil, nil
	}
	var payloadJSON []byte
	err := s.db.Pool.QueryRow(ctx, `
		SELECT item.typed_payload
		FROM task_thread_items item
		JOIN task_threads thread ON thread.id=item.thread_id
		WHERE thread.task_id=$1
		  AND item.kind='execution_completed'
		  AND item.task_review_cycle=$2
		ORDER BY item.created_at DESC,item.id DESC
		LIMIT 1`, taskID, reviewCycle).Scan(&payloadJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get Task delivery review snapshot: %w", err)
	}
	var payload domain.ExecutionCompletedPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("decode Task delivery review snapshot: %w", err)
	}
	if payload.MergeRequests == nil {
		payload.MergeRequests = []domain.MergeRequestSnapshot{}
	}
	return &TaskDeliverySnapshot{
		ReviewCycle: payload.ReviewCycle, MergeRequests: payload.MergeRequests,
	}, nil
}

func (s *TaskMergeRequestStore) Link(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	projectRepositoryID uuid.UUID,
	mergeRequest domain.GitLabMergeRequest,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskMergeRequestMutation, error) {
	if err := mergeRequest.Validate(); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if mergeRequest.Observation.Status != domain.GitLabObservationConfirmed {
		return TaskMergeRequestMutation{}, fmt.Errorf(
			"%w: a merge request must be confirmed before linking", domain.ErrInvalidInput,
		)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskMergeRequestMutation{}, fmt.Errorf("begin Task merge request link: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, claim, err := lockOwnedWorkflowClaim(
		ctx, tx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion, actor, operation,
	)
	if err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if task.ArchivedAt != nil || task.Lifecycle.Phase != domain.TaskPhaseInProgress ||
		task.Lifecycle.Activity != domain.TaskActivityWorking || claim.Stage != domain.TaskClaimStageExecution {
		return TaskMergeRequestMutation{}, fmt.Errorf(
			"%w: merge requests can be linked only during claimed execution", domain.ErrInvalidTransition,
		)
	}
	repository, connection, err := lockActiveProjectRepository(
		ctx, tx, projectRepositoryID, task.ProjectID,
	)
	if err != nil {
		return TaskMergeRequestMutation{}, err
	}
	link := domain.TaskMergeRequest{
		ID: uuid.New(), TaskID: task.TaskID, ProjectID: task.ProjectID,
		ProjectRepositoryID: repository.ID,
		MergeRequestIID:     mergeRequest.IID, GitLabMergeRequestID: mergeRequest.ID,
		WebURL: mergeRequest.WebURL, LinkedBy: actor, LinkedThroughClaimID: claim.ID,
		LinkedAt: now, LatestObservation: mergeRequest.Observation,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := link.Validate(); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_merge_requests (
			id, task_id, project_id, project_repository_id,
			merge_request_iid, gitlab_merge_request_id, web_url,
			linked_by_type, linked_by_user_id, linked_by_ref,
			linked_through_claim_id, linked_at,
			observation_status, observed_at, title, state, draft,
			source_branch, target_branch, head_sha, merge_commit_sha,
			merged_at, provider_updated_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
			$18,$19,$20,$21,$22,$23,$24,$25
		)`,
		link.ID, link.TaskID, link.ProjectID, link.ProjectRepositoryID,
		link.MergeRequestIID, link.GitLabMergeRequestID, link.WebURL,
		link.LinkedBy.Type, link.LinkedBy.UserID, nullIfEmpty(link.LinkedBy.Ref),
		link.LinkedThroughClaimID, link.LinkedAt,
		link.LatestObservation.Status, link.LatestObservation.ObservedAt,
		link.LatestObservation.Title, link.LatestObservation.State, link.LatestObservation.Draft,
		link.LatestObservation.SourceBranch, link.LatestObservation.TargetBranch,
		link.LatestObservation.HeadSHA, link.LatestObservation.MergeCommitSHA,
		link.LatestObservation.MergedAt, link.LatestObservation.ProviderUpdatedAt,
		link.CreatedAt, link.UpdatedAt,
	)
	if err != nil {
		return TaskMergeRequestMutation{}, mapPgError(err)
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if err := insertActivity(
		ctx, tx, task.TaskID, operation, domain.ActivityFieldMergeRequests, nil, &link.WebURL,
	); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	newValue, _ := json.Marshal(taskMergeRequestAuditValue(link, connection))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "task_merge_request",
		EntityID: link.ID, EntityNumber: &task.TaskNumber,
		Action: "linked", NewValue: newValue,
	}); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskMergeRequestMutation{}, fmt.Errorf("commit Task merge request link: %w", err)
	}
	return TaskMergeRequestMutation{
		Task: task.TaskWorkflowSnapshot,
		MergeRequest: TaskMergeRequestWithRepository{
			MergeRequest: link, Repository: repository, Connection: connection,
		},
	}, nil
}

func (s *TaskMergeRequestStore) Unlink(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	linkID uuid.UUID,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskMergeRequestMutation, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskMergeRequestMutation{}, fmt.Errorf("begin Task merge request unlink: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, claim, err := lockOwnedWorkflowClaim(
		ctx, tx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion, actor, operation,
	)
	if err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if task.ArchivedAt != nil || task.Lifecycle.Phase != domain.TaskPhaseInProgress ||
		task.Lifecycle.Activity != domain.TaskActivityWorking || claim.Stage != domain.TaskClaimStageExecution {
		return TaskMergeRequestMutation{}, fmt.Errorf(
			"%w: merge requests can be unlinked only during claimed execution", domain.ErrInvalidTransition,
		)
	}
	item, err := scanTaskMergeRequestWithRepository(tx.QueryRow(ctx, taskMergeRequestSelect+`
		WHERE merge_request.id=$1 AND merge_request.task_id=$2
		FOR UPDATE OF merge_request`, linkID, task.TaskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskMergeRequestMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if !item.MergeRequest.Active() {
		return TaskMergeRequestMutation{}, fmt.Errorf("%w: merge request is already unlinked", domain.ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		UPDATE task_merge_requests SET
			unlinked_by_type=$2, unlinked_by_user_id=$3, unlinked_by_ref=$4,
			unlinked_through_claim_id=$5, unlinked_at=$6, updated_at=$6
		WHERE id=$1`,
		linkID, actor.Type, actor.UserID, nullIfEmpty(actor.Ref), claim.ID, now,
	)
	if err != nil {
		return TaskMergeRequestMutation{}, mapPgError(err)
	}
	item.MergeRequest.UnlinkedBy = &actor
	item.MergeRequest.UnlinkedThroughClaimID = &claim.ID
	item.MergeRequest.UnlinkedAt = &now
	item.MergeRequest.UpdatedAt = now
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if err := insertActivity(
		ctx, tx, task.TaskID, operation, domain.ActivityFieldMergeRequests,
		&item.MergeRequest.WebURL, nil,
	); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	oldValue, _ := json.Marshal(taskMergeRequestAuditValue(item.MergeRequest, item.Connection))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "task_merge_request",
		EntityID: item.MergeRequest.ID, EntityNumber: &task.TaskNumber,
		Action: "unlinked", OldValue: oldValue,
	}); err != nil {
		return TaskMergeRequestMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskMergeRequestMutation{}, fmt.Errorf("commit Task merge request unlink: %w", err)
	}
	return TaskMergeRequestMutation{Task: task.TaskWorkflowSnapshot, MergeRequest: item}, nil
}

func (s *TaskMergeRequestStore) UpdateObservation(
	ctx context.Context,
	linkID uuid.UUID,
	observation domain.GitLabMergeRequestObservation,
	now time.Time,
) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	commandTag, err := s.db.Pool.Exec(ctx, `
		UPDATE task_merge_requests SET
			observation_status=$2, observed_at=$3,
			title=CASE WHEN $2='confirmed' THEN $4 ELSE title END,
			state=CASE WHEN $2='confirmed' THEN $5 ELSE state END,
			draft=CASE WHEN $2='confirmed' THEN $6 ELSE draft END,
			source_branch=CASE WHEN $2='confirmed' THEN $7 ELSE source_branch END,
			target_branch=CASE WHEN $2='confirmed' THEN $8 ELSE target_branch END,
			head_sha=CASE WHEN $2='confirmed' THEN $9 ELSE head_sha END,
			merge_commit_sha=CASE WHEN $2='confirmed' THEN $10 ELSE merge_commit_sha END,
			merged_at=CASE WHEN $2='confirmed' THEN $11 ELSE merged_at END,
			provider_updated_at=CASE WHEN $2='confirmed' THEN $12 ELSE provider_updated_at END,
			updated_at=$13
		WHERE id=$1`,
		linkID, observation.Status, observation.ObservedAt,
		observation.Title, observation.State, observation.Draft,
		observation.SourceBranch, observation.TargetBranch, observation.HeadSHA,
		observation.MergeCommitSHA, observation.MergedAt, observation.ProviderUpdatedAt, now,
	)
	if err != nil {
		return mapPgError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func lockActiveProjectRepository(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID uuid.UUID,
	projectID uuid.UUID,
) (domain.ProjectRepository, domain.GitLabConnection, error) {
	item, err := scanProjectRepositoryWithConnection(tx.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.connection_id,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
			`+prefixedGitLabConnectionColumns("connection")+`
		FROM project_repositories repository
		JOIN gitlab_connections connection ON connection.id=repository.connection_id
		WHERE repository.id=$1 AND repository.project_id=$2
		  AND repository.unbound_at IS NULL AND connection.status='active'
		FOR SHARE OF repository, connection`, repositoryID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectRepository{}, domain.GitLabConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ProjectRepository{}, domain.GitLabConnection{}, err
	}
	return item.Repository, item.Connection, nil
}

func taskMergeRequestAuditValue(
	mergeRequest domain.TaskMergeRequest,
	connection domain.GitLabConnection,
) map[string]any {
	return map[string]any{
		"project_repository_id": mergeRequest.ProjectRepositoryID,
		"gitlab_project_id":     connection.GitLabProjectID,
		"merge_request_iid":     mergeRequest.MergeRequestIID,
		"web_url":               mergeRequest.WebURL,
		"head_sha":              mergeRequest.LatestObservation.HeadSHA,
		"observation_status":    mergeRequest.LatestObservation.Status,
	}
}

var taskMergeRequestSelect = `
	SELECT
		merge_request.id, merge_request.task_id, merge_request.project_id,
		merge_request.project_repository_id, merge_request.merge_request_iid,
		merge_request.gitlab_merge_request_id, merge_request.web_url,
		merge_request.linked_by_type, merge_request.linked_by_user_id,
		merge_request.linked_by_ref, merge_request.linked_through_claim_id,
		merge_request.linked_at, merge_request.unlinked_by_type,
		merge_request.unlinked_by_user_id, merge_request.unlinked_by_ref,
		merge_request.unlinked_through_claim_id, merge_request.unlinked_at,
		merge_request.observation_status, merge_request.observed_at,
		merge_request.title, merge_request.state, merge_request.draft,
		merge_request.source_branch, merge_request.target_branch,
		merge_request.head_sha, merge_request.merge_commit_sha,
		merge_request.merged_at, merge_request.provider_updated_at,
		merge_request.created_at, merge_request.updated_at,
		repository.id, repository.project_id, repository.connection_id,
		repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
		` + prefixedGitLabConnectionColumns("connection") + `
	FROM task_merge_requests merge_request
	JOIN project_repositories repository ON repository.id=merge_request.project_repository_id
	JOIN gitlab_connections connection ON connection.id=repository.connection_id
`

type taskMergeRequestScanner interface {
	Scan(dest ...any) error
}

func scanTaskMergeRequestWithRepository(row taskMergeRequestScanner) (TaskMergeRequestWithRepository, error) {
	var item TaskMergeRequestWithRepository
	var linkedType string
	var linkedUserID *uuid.UUID
	var linkedRef *string
	var unlinkedType *string
	var unlinkedUserID *uuid.UUID
	var unlinkedRef *string
	err := row.Scan(
		&item.MergeRequest.ID, &item.MergeRequest.TaskID, &item.MergeRequest.ProjectID,
		&item.MergeRequest.ProjectRepositoryID, &item.MergeRequest.MergeRequestIID,
		&item.MergeRequest.GitLabMergeRequestID, &item.MergeRequest.WebURL,
		&linkedType, &linkedUserID, &linkedRef, &item.MergeRequest.LinkedThroughClaimID,
		&item.MergeRequest.LinkedAt, &unlinkedType, &unlinkedUserID, &unlinkedRef,
		&item.MergeRequest.UnlinkedThroughClaimID, &item.MergeRequest.UnlinkedAt,
		&item.MergeRequest.LatestObservation.Status, &item.MergeRequest.LatestObservation.ObservedAt,
		&item.MergeRequest.LatestObservation.Title, &item.MergeRequest.LatestObservation.State,
		&item.MergeRequest.LatestObservation.Draft,
		&item.MergeRequest.LatestObservation.SourceBranch,
		&item.MergeRequest.LatestObservation.TargetBranch,
		&item.MergeRequest.LatestObservation.HeadSHA,
		&item.MergeRequest.LatestObservation.MergeCommitSHA,
		&item.MergeRequest.LatestObservation.MergedAt,
		&item.MergeRequest.LatestObservation.ProviderUpdatedAt,
		&item.MergeRequest.CreatedAt, &item.MergeRequest.UpdatedAt,
		&item.Repository.ID, &item.Repository.ProjectID, &item.Repository.ConnectionID,
		&item.Repository.BoundBy, &item.Repository.BoundAt,
		&item.Repository.UnboundBy, &item.Repository.UnboundAt,
		&item.Connection.ID, &item.Connection.Version, &item.Connection.Label,
		&item.Connection.Origin, &item.Connection.GitLabProjectID,
		&item.Connection.PathWithNamespace, &item.Connection.PathLookupKey,
		&item.Connection.CanonicalWebURL, &item.Connection.DefaultBranch,
		&item.Connection.CredentialCiphertext, &item.Connection.EncryptionKeyID,
		&item.Connection.CredentialExpiresAt, &item.Connection.Status,
		&item.Connection.LastValidatedAt, &item.Connection.CreatedBy,
		&item.Connection.DisabledBy, &item.Connection.DisabledAt,
		&item.Connection.CreatedAt, &item.Connection.UpdatedAt,
	)
	if err != nil {
		return TaskMergeRequestWithRepository{}, err
	}
	item.MergeRequest.LinkedBy = actorFromStored(linkedType, linkedUserID, linkedRef)
	if unlinkedType != nil {
		actor := actorFromStored(*unlinkedType, unlinkedUserID, unlinkedRef)
		item.MergeRequest.UnlinkedBy = &actor
	}
	return item, nil
}

func actorFromStored(actorType string, userID *uuid.UUID, ref *string) domain.Actor {
	actor := domain.Actor{Type: domain.ActorType(actorType), UserID: userID}
	if ref != nil {
		actor.Ref = *ref
	}
	return actor
}
