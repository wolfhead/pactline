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
	ReviewCycle int64
	CodeChanges []domain.CodeChangeSnapshot
}

type TaskCodeChangeWithRepository struct {
	CodeChange domain.TaskCodeChange
	Repository domain.ProjectRepository
	Connection domain.RepositoryConnection
}

type TaskCodeChangeMutation struct {
	Task       TaskWorkflowSnapshot
	CodeChange TaskCodeChangeWithRepository
}

type TaskCodeChangeStore struct{ db *DB }

func NewTaskCodeChangeStore(db *DB) *TaskCodeChangeStore {
	return &TaskCodeChangeStore{db: db}
}

func (s *TaskCodeChangeStore) ListActive(
	ctx context.Context,
	taskID uuid.UUID,
) ([]TaskCodeChangeWithRepository, error) {
	rows, err := s.db.Pool.Query(ctx, taskCodeChangeSelect+`
		WHERE code_change.task_id=$1 AND code_change.unlinked_at IS NULL
		ORDER BY connection.provider, connection.origin, connection.provider_repository_id,
			code_change.kind, code_change.change_number, code_change.id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list active Task code changes: %w", err)
	}
	defer rows.Close()
	items := []TaskCodeChangeWithRepository{}
	for rows.Next() {
		item, err := scanTaskCodeChangeWithRepository(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *TaskCodeChangeStore) GetReviewSnapshot(
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
	if payload.CodeChanges == nil {
		payload.CodeChanges = []domain.CodeChangeSnapshot{}
	}
	for index := range payload.CodeChanges {
		payload.CodeChanges[index].ObservedAt = payload.CodeChanges[index].ObservedAt.UTC()
		if payload.CodeChanges[index].MergedAt != nil {
			mergedAt := payload.CodeChanges[index].MergedAt.UTC()
			payload.CodeChanges[index].MergedAt = &mergedAt
		}
	}
	return &TaskDeliverySnapshot{
		ReviewCycle: payload.ReviewCycle, CodeChanges: payload.CodeChanges,
	}, nil
}

func (s *TaskCodeChangeStore) Link(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	projectRepositoryID uuid.UUID,
	codeChange domain.CodeChange,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskCodeChangeMutation, error) {
	if err := codeChange.Validate(); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if codeChange.Observation.Status != domain.CodeChangeObservationConfirmed {
		return TaskCodeChangeMutation{}, fmt.Errorf(
			"%w: a code change must be confirmed before linking", domain.ErrInvalidInput,
		)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskCodeChangeMutation{}, fmt.Errorf("begin Task code change link: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, claim, err := lockOwnedWorkflowClaim(
		ctx, tx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion, actor, operation,
	)
	if err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if task.ArchivedAt != nil || task.Lifecycle.Phase != domain.TaskPhaseInProgress ||
		task.Lifecycle.Activity != domain.TaskActivityWorking || claim.Stage != domain.TaskClaimStageExecution {
		return TaskCodeChangeMutation{}, fmt.Errorf(
			"%w: code changes can be linked only during claimed execution", domain.ErrInvalidTransition,
		)
	}
	repository, connection, err := lockActiveProjectRepository(
		ctx, tx, projectRepositoryID, task.ProjectID,
	)
	if err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if connection.Provider != codeChange.Provider {
		return TaskCodeChangeMutation{}, fmt.Errorf("%w: code change provider does not match Repository Connection", domain.ErrConflict)
	}
	link := domain.TaskCodeChange{
		ID: uuid.New(), TaskID: task.TaskID, ProjectID: task.ProjectID,
		ProjectRepositoryID: repository.ID,
		Provider:            codeChange.Provider, Kind: codeChange.Kind,
		ChangeNumber: codeChange.ChangeNumber, ProviderChangeID: codeChange.ProviderChangeID,
		WebURL: codeChange.WebURL, LinkedBy: actor, LinkedThroughClaimID: claim.ID,
		LinkedAt: now, LatestObservation: codeChange.Observation,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := link.Validate(); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_code_changes (
			id, task_id, project_id, project_repository_id,
			kind, change_number, provider_change_id, web_url,
			linked_by_type, linked_by_user_id, linked_by_ref,
			linked_through_claim_id, linked_at,
			observation_status, observed_at, title, state, draft,
			source_branch, target_branch, head_sha, merge_commit_sha,
			merged_at, provider_updated_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
			$19,$20,$21,$22,$23,$24,$25,$26
		)`,
		link.ID, link.TaskID, link.ProjectID, link.ProjectRepositoryID,
		link.Kind, link.ChangeNumber, link.ProviderChangeID, link.WebURL,
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
		return TaskCodeChangeMutation{}, mapPgError(err)
	}
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if err := insertActivity(
		ctx, tx, task.TaskID, operation, domain.ActivityFieldCodeChanges, nil, &link.WebURL,
	); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	newValue, _ := json.Marshal(taskCodeChangeAuditValue(link, connection))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "task_code_change",
		EntityID: link.ID, EntityNumber: &task.TaskNumber,
		Action: "linked", NewValue: newValue,
	}); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskCodeChangeMutation{}, fmt.Errorf("commit Task code change link: %w", err)
	}
	return TaskCodeChangeMutation{
		Task: task.TaskWorkflowSnapshot,
		CodeChange: TaskCodeChangeWithRepository{
			CodeChange: link, Repository: repository, Connection: connection,
		},
	}, nil
}

func (s *TaskCodeChangeStore) Unlink(
	ctx context.Context,
	taskNumber int64,
	claimID uuid.UUID,
	expectedTaskVersion int64,
	expectedClaimVersion int64,
	linkID uuid.UUID,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskCodeChangeMutation, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return TaskCodeChangeMutation{}, fmt.Errorf("begin Task code change unlink: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	task, claim, err := lockOwnedWorkflowClaim(
		ctx, tx, taskNumber, claimID, expectedTaskVersion, expectedClaimVersion, actor, operation,
	)
	if err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if task.ArchivedAt != nil || task.Lifecycle.Phase != domain.TaskPhaseInProgress ||
		task.Lifecycle.Activity != domain.TaskActivityWorking || claim.Stage != domain.TaskClaimStageExecution {
		return TaskCodeChangeMutation{}, fmt.Errorf(
			"%w: code changes can be unlinked only during claimed execution", domain.ErrInvalidTransition,
		)
	}
	item, err := scanTaskCodeChangeWithRepository(tx.QueryRow(ctx, taskCodeChangeSelect+`
		WHERE code_change.id=$1 AND code_change.task_id=$2
		FOR UPDATE OF code_change`, linkID, task.TaskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskCodeChangeMutation{}, domain.ErrNotFound
	}
	if err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if !item.CodeChange.Active() {
		return TaskCodeChangeMutation{}, fmt.Errorf("%w: code change is already unlinked", domain.ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		UPDATE task_code_changes SET
			unlinked_by_type=$2, unlinked_by_user_id=$3, unlinked_by_ref=$4,
			unlinked_through_claim_id=$5, unlinked_at=$6, updated_at=$6
		WHERE id=$1`,
		linkID, actor.Type, actor.UserID, nullIfEmpty(actor.Ref), claim.ID, now,
	)
	if err != nil {
		return TaskCodeChangeMutation{}, mapPgError(err)
	}
	item.CodeChange.UnlinkedBy = &actor
	item.CodeChange.UnlinkedThroughClaimID = &claim.ID
	item.CodeChange.UnlinkedAt = &now
	item.CodeChange.UpdatedAt = now
	if err := persistWorkflowTask(ctx, tx, &task, expectedTaskVersion, now); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if err := insertActivity(
		ctx, tx, task.TaskID, operation, domain.ActivityFieldCodeChanges,
		&item.CodeChange.WebURL, nil,
	); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	oldValue, _ := json.Marshal(taskCodeChangeAuditValue(item.CodeChange, item.Connection))
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now, Actor: operation, EntityType: "task_code_change",
		EntityID: item.CodeChange.ID, EntityNumber: &task.TaskNumber,
		Action: "unlinked", OldValue: oldValue,
	}); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TaskCodeChangeMutation{}, fmt.Errorf("commit Task code change unlink: %w", err)
	}
	return TaskCodeChangeMutation{Task: task.TaskWorkflowSnapshot, CodeChange: item}, nil
}

func (s *TaskCodeChangeStore) UpdateObservation(
	ctx context.Context,
	linkID uuid.UUID,
	observation domain.CodeChangeObservation,
	now time.Time,
) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	commandTag, err := s.db.Pool.Exec(ctx, `
		UPDATE task_code_changes SET
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
) (domain.ProjectRepository, domain.RepositoryConnection, error) {
	item, err := scanProjectRepositoryWithConnection(tx.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.connection_id,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
			`+prefixedRepositoryConnectionColumns("connection")+`
		FROM project_repositories repository
		JOIN repository_connections connection ON connection.id=repository.connection_id
		WHERE repository.id=$1 AND repository.project_id=$2
		  AND repository.unbound_at IS NULL AND connection.status='active'
		FOR SHARE OF repository, connection`, repositoryID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectRepository{}, domain.RepositoryConnection{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ProjectRepository{}, domain.RepositoryConnection{}, err
	}
	return item.Repository, item.Connection, nil
}

func taskCodeChangeAuditValue(
	codeChange domain.TaskCodeChange,
	connection domain.RepositoryConnection,
) map[string]any {
	return map[string]any{
		"project_repository_id":  codeChange.ProjectRepositoryID,
		"provider":               connection.Provider,
		"provider_repository_id": connection.ProviderRepositoryID,
		"kind":                   codeChange.Kind,
		"change_number":          codeChange.ChangeNumber,
		"provider_change_id":     codeChange.ProviderChangeID,
		"web_url":                codeChange.WebURL,
		"head_sha":               codeChange.LatestObservation.HeadSHA,
		"observation_status":     codeChange.LatestObservation.Status,
	}
}

var taskCodeChangeSelect = `
	SELECT
		code_change.id, code_change.task_id, code_change.project_id,
		code_change.project_repository_id, connection.provider, code_change.kind,
		code_change.change_number, code_change.provider_change_id, code_change.web_url,
		code_change.linked_by_type, code_change.linked_by_user_id,
		code_change.linked_by_ref, code_change.linked_through_claim_id,
		code_change.linked_at, code_change.unlinked_by_type,
		code_change.unlinked_by_user_id, code_change.unlinked_by_ref,
		code_change.unlinked_through_claim_id, code_change.unlinked_at,
		code_change.observation_status, code_change.observed_at,
		code_change.title, code_change.state, code_change.draft,
		code_change.source_branch, code_change.target_branch,
		code_change.head_sha, code_change.merge_commit_sha,
		code_change.merged_at, code_change.provider_updated_at,
		code_change.created_at, code_change.updated_at,
		repository.id, repository.project_id, repository.connection_id,
		repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at,
		` + prefixedRepositoryConnectionColumns("connection") + `
	FROM task_code_changes code_change
	JOIN project_repositories repository ON repository.id=code_change.project_repository_id
	JOIN repository_connections connection ON connection.id=repository.connection_id
`

type taskCodeChangeScanner interface {
	Scan(dest ...any) error
}

func scanTaskCodeChangeWithRepository(row taskCodeChangeScanner) (TaskCodeChangeWithRepository, error) {
	var item TaskCodeChangeWithRepository
	var linkedType string
	var linkedUserID *uuid.UUID
	var linkedRef *string
	var unlinkedType *string
	var unlinkedUserID *uuid.UUID
	var unlinkedRef *string
	err := row.Scan(
		&item.CodeChange.ID, &item.CodeChange.TaskID, &item.CodeChange.ProjectID,
		&item.CodeChange.ProjectRepositoryID, &item.CodeChange.Provider, &item.CodeChange.Kind,
		&item.CodeChange.ChangeNumber, &item.CodeChange.ProviderChangeID, &item.CodeChange.WebURL,
		&linkedType, &linkedUserID, &linkedRef, &item.CodeChange.LinkedThroughClaimID,
		&item.CodeChange.LinkedAt, &unlinkedType, &unlinkedUserID, &unlinkedRef,
		&item.CodeChange.UnlinkedThroughClaimID, &item.CodeChange.UnlinkedAt,
		&item.CodeChange.LatestObservation.Status, &item.CodeChange.LatestObservation.ObservedAt,
		&item.CodeChange.LatestObservation.Title, &item.CodeChange.LatestObservation.State,
		&item.CodeChange.LatestObservation.Draft,
		&item.CodeChange.LatestObservation.SourceBranch,
		&item.CodeChange.LatestObservation.TargetBranch,
		&item.CodeChange.LatestObservation.HeadSHA,
		&item.CodeChange.LatestObservation.MergeCommitSHA,
		&item.CodeChange.LatestObservation.MergedAt,
		&item.CodeChange.LatestObservation.ProviderUpdatedAt,
		&item.CodeChange.CreatedAt, &item.CodeChange.UpdatedAt,
		&item.Repository.ID, &item.Repository.ProjectID, &item.Repository.ConnectionID,
		&item.Repository.BoundBy, &item.Repository.BoundAt,
		&item.Repository.UnboundBy, &item.Repository.UnboundAt,
		&item.Connection.ID, &item.Connection.Version, &item.Connection.Label,
		&item.Connection.Origin, &item.Connection.Provider, &item.Connection.ProviderRepositoryID,
		&item.Connection.PathWithNamespace, &item.Connection.PathLookupKey,
		&item.Connection.CanonicalWebURL, &item.Connection.DefaultBranch,
		&item.Connection.CredentialCiphertext, &item.Connection.EncryptionKeyID,
		&item.Connection.CredentialExpiresAt, &item.Connection.Status,
		&item.Connection.LastValidatedAt, &item.Connection.CreatedBy,
		&item.Connection.DisabledBy, &item.Connection.DisabledAt,
		&item.Connection.CreatedAt, &item.Connection.UpdatedAt,
	)
	if err != nil {
		return TaskCodeChangeWithRepository{}, err
	}
	item.CodeChange.LinkedBy = actorFromStored(linkedType, linkedUserID, linkedRef)
	if unlinkedType != nil {
		actor := actorFromStored(*unlinkedType, unlinkedUserID, unlinkedRef)
		item.CodeChange.UnlinkedBy = &actor
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
