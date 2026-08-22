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
}

type TaskCodeChangeMutation struct {
	Task       TaskWorkflowSnapshot
	CodeChange TaskCodeChangeWithRepository
	Changed    bool
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
		ORDER BY repository.provider, repository.origin, repository.path_lookup_key,
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
		evidence := payload.CodeChanges[index].ProviderEvidence
		if evidence != nil {
			evidence.ObservedAt = evidence.ObservedAt.UTC()
			evidence.ProviderUpdatedAt = evidence.ProviderUpdatedAt.UTC()
			if evidence.MergedAt != nil {
				mergedAt := evidence.MergedAt.UTC()
				evidence.MergedAt = &mergedAt
			}
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
	reference domain.CodeChangeReference,
	actor domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (TaskCodeChangeMutation, error) {
	if err := reference.Validate(); err != nil {
		return TaskCodeChangeMutation{}, err
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
	repository, err := lockActiveProjectRepository(
		ctx, tx, projectRepositoryID, task.ProjectID,
	)
	if err != nil {
		return TaskCodeChangeMutation{}, err
	}
	if repository.Provider != reference.Repository.Provider || repository.Origin != reference.Repository.Origin ||
		repository.PathLookupKey != reference.Repository.PathLookupKey {
		return TaskCodeChangeMutation{}, fmt.Errorf("%w: code change does not belong to the Project repository", domain.ErrConflict)
	}
	existing, err := scanTaskCodeChangeWithRepository(tx.QueryRow(ctx, taskCodeChangeSelect+`
		WHERE code_change.task_id=$1
		  AND code_change.project_repository_id=$2
		  AND code_change.kind=$3
		  AND code_change.change_number=$4
		  AND code_change.unlinked_at IS NULL
		FOR UPDATE`, task.TaskID, repository.ID, reference.Kind, reference.ChangeNumber))
	if err == nil {
		if existing.CodeChange.WebURL != reference.WebURL {
			return TaskCodeChangeMutation{}, fmt.Errorf(
				"%w: code-change identity is already linked with a different normalized URL", domain.ErrConflict,
			)
		}
		if err := tx.Commit(ctx); err != nil {
			return TaskCodeChangeMutation{}, fmt.Errorf("commit existing Task code change link: %w", err)
		}
		return TaskCodeChangeMutation{
			Task: task.TaskWorkflowSnapshot, CodeChange: existing, Changed: false,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TaskCodeChangeMutation{}, err
	}
	link := domain.TaskCodeChange{
		ID: uuid.New(), TaskID: task.TaskID, ProjectID: task.ProjectID,
		ProjectRepositoryID: repository.ID,
		Provider:            reference.Repository.Provider, Kind: reference.Kind,
		ChangeNumber: reference.ChangeNumber, WebURL: reference.WebURL,
		LinkedBy: actor, LinkedThroughClaimID: claim.ID, LinkedAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := link.Validate(); err != nil {
		return TaskCodeChangeMutation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_code_changes (
			id, task_id, project_id, project_repository_id,
			kind, change_number, web_url,
			linked_by_type, linked_by_user_id, linked_by_ref,
			linked_through_claim_id, linked_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14
		)`,
		link.ID, link.TaskID, link.ProjectID, link.ProjectRepositoryID,
		link.Kind, link.ChangeNumber, link.WebURL,
		link.LinkedBy.Type, link.LinkedBy.UserID, nullIfEmpty(link.LinkedBy.Ref),
		link.LinkedThroughClaimID, link.LinkedAt, link.CreatedAt, link.UpdatedAt,
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
	newValue, _ := json.Marshal(taskCodeChangeAuditValue(link))
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
		Task:       task.TaskWorkflowSnapshot,
		CodeChange: TaskCodeChangeWithRepository{CodeChange: link, Repository: repository},
		Changed:    true,
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
	oldValue, _ := json.Marshal(taskCodeChangeAuditValue(item.CodeChange))
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
	return TaskCodeChangeMutation{Task: task.TaskWorkflowSnapshot, CodeChange: item, Changed: true}, nil
}

func (s *TaskCodeChangeStore) UpdateProviderEvidence(
	ctx context.Context,
	linkID uuid.UUID,
	evidence domain.CodeChangeProviderEvidence,
	verification domain.CodeChangeVerification,
	now time.Time,
) error {
	if err := evidence.Validate(); err != nil {
		return err
	}
	if err := verification.Validate(); err != nil {
		return err
	}
	if verification.Status != domain.CodeChangeVerificationVerified {
		return fmt.Errorf("%w: successful provider evidence requires verified status", domain.ErrInvalidInput)
	}
	commandTag, err := s.db.Pool.Exec(ctx, `
		UPDATE task_code_changes SET
			evidence_connection_id=$2, evidence_provider_repository_id=$3,
			provider_change_id=$4, evidence_observed_at=$5,
			title=$6, state=$7, draft=$8, source_branch=$9, target_branch=$10,
			head_sha=$11, merge_commit_sha=$12, merged_at=$13, provider_updated_at=$14,
			verification_status=$15, verification_attempted_at=$16, updated_at=$17
		WHERE id=$1`,
		linkID, evidence.ConnectionID, evidence.ProviderRepositoryID, evidence.ProviderChangeID,
		evidence.ObservedAt, evidence.Title, evidence.State, evidence.Draft,
		evidence.SourceBranch, evidence.TargetBranch, evidence.HeadSHA,
		evidence.MergeCommitSHA, evidence.MergedAt, evidence.ProviderUpdatedAt,
		verification.Status, verification.AttemptedAt, now,
	)
	if err != nil {
		return mapPgError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *TaskCodeChangeStore) UpdateProviderVerification(
	ctx context.Context,
	linkID uuid.UUID,
	verification domain.CodeChangeVerification,
	now time.Time,
) error {
	if err := verification.Validate(); err != nil {
		return err
	}
	if verification.Status == domain.CodeChangeVerificationVerified {
		return fmt.Errorf("%w: verified status must be stored with provider evidence", domain.ErrInvalidInput)
	}
	commandTag, err := s.db.Pool.Exec(ctx, `
		UPDATE task_code_changes SET
			verification_status=$2, verification_attempted_at=$3, updated_at=$4
		WHERE id=$1`, linkID, verification.Status, verification.AttemptedAt, now)
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
) (domain.ProjectRepository, error) {
	repository, err := scanProjectRepository(tx.QueryRow(ctx, `
		SELECT
			repository.id, repository.project_id, repository.provider, repository.origin,
			repository.path_with_namespace, repository.path_lookup_key, repository.canonical_web_url,
			repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at
		FROM project_repositories repository
		WHERE repository.id=$1 AND repository.project_id=$2
		  AND repository.unbound_at IS NULL
		FOR SHARE OF repository`, repositoryID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectRepository{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ProjectRepository{}, err
	}
	return repository, nil
}

func taskCodeChangeAuditValue(codeChange domain.TaskCodeChange) map[string]any {
	value := map[string]any{
		"project_repository_id": codeChange.ProjectRepositoryID,
		"provider":              codeChange.Provider, "kind": codeChange.Kind,
		"change_number": codeChange.ChangeNumber, "web_url": codeChange.WebURL,
	}
	if codeChange.ProviderEvidence != nil {
		value["provider_change_id"] = codeChange.ProviderEvidence.ProviderChangeID
		value["head_sha"] = codeChange.ProviderEvidence.HeadSHA
	}
	if codeChange.ProviderVerification != nil {
		value["verification_status"] = codeChange.ProviderVerification.Status
	}
	return value
}

var taskCodeChangeSelect = `
	SELECT
		code_change.id, code_change.task_id, code_change.project_id,
		code_change.project_repository_id, repository.provider, code_change.kind,
		code_change.change_number, code_change.provider_change_id, code_change.web_url,
		code_change.linked_by_type, code_change.linked_by_user_id,
		code_change.linked_by_ref, code_change.linked_through_claim_id,
		code_change.linked_at, code_change.unlinked_by_type,
		code_change.unlinked_by_user_id, code_change.unlinked_by_ref,
		code_change.unlinked_through_claim_id, code_change.unlinked_at,
		code_change.verification_status, code_change.verification_attempted_at,
		code_change.evidence_connection_id, code_change.evidence_provider_repository_id,
		code_change.evidence_observed_at,
		code_change.title, code_change.state, code_change.draft,
		code_change.source_branch, code_change.target_branch,
		code_change.head_sha, code_change.merge_commit_sha,
		code_change.merged_at, code_change.provider_updated_at,
		code_change.created_at, code_change.updated_at,
		repository.id, repository.project_id, repository.provider, repository.origin,
		repository.path_with_namespace, repository.path_lookup_key, repository.canonical_web_url,
		repository.bound_by, repository.bound_at, repository.unbound_by, repository.unbound_at
	FROM task_code_changes code_change
	JOIN project_repositories repository ON repository.id=code_change.project_repository_id
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
	var providerChangeID *string
	var verificationStatus *domain.CodeChangeVerificationStatus
	var verificationAttemptedAt *time.Time
	var evidenceConnectionID *uuid.UUID
	var evidenceProviderRepositoryID *string
	var evidenceObservedAt *time.Time
	var title *string
	var state *domain.CodeChangeState
	var draft *bool
	var sourceBranch *string
	var targetBranch *string
	var headSHA *string
	var mergeCommitSHA *string
	var mergedAt *time.Time
	var providerUpdatedAt *time.Time
	err := row.Scan(
		&item.CodeChange.ID, &item.CodeChange.TaskID, &item.CodeChange.ProjectID,
		&item.CodeChange.ProjectRepositoryID, &item.CodeChange.Provider, &item.CodeChange.Kind,
		&item.CodeChange.ChangeNumber, &providerChangeID, &item.CodeChange.WebURL,
		&linkedType, &linkedUserID, &linkedRef, &item.CodeChange.LinkedThroughClaimID,
		&item.CodeChange.LinkedAt, &unlinkedType, &unlinkedUserID, &unlinkedRef,
		&item.CodeChange.UnlinkedThroughClaimID, &item.CodeChange.UnlinkedAt,
		&verificationStatus, &verificationAttemptedAt,
		&evidenceConnectionID, &evidenceProviderRepositoryID, &evidenceObservedAt,
		&title, &state, &draft, &sourceBranch, &targetBranch, &headSHA,
		&mergeCommitSHA, &mergedAt, &providerUpdatedAt,
		&item.CodeChange.CreatedAt, &item.CodeChange.UpdatedAt,
		&item.Repository.ID, &item.Repository.ProjectID, &item.Repository.Provider,
		&item.Repository.Origin, &item.Repository.PathWithNamespace,
		&item.Repository.PathLookupKey, &item.Repository.CanonicalWebURL,
		&item.Repository.BoundBy, &item.Repository.BoundAt,
		&item.Repository.UnboundBy, &item.Repository.UnboundAt,
	)
	if err != nil {
		return TaskCodeChangeWithRepository{}, err
	}
	item.CodeChange.LinkedBy = actorFromStored(linkedType, linkedUserID, linkedRef)
	if unlinkedType != nil {
		actor := actorFromStored(*unlinkedType, unlinkedUserID, unlinkedRef)
		item.CodeChange.UnlinkedBy = &actor
	}
	if verificationStatus != nil && verificationAttemptedAt != nil {
		item.CodeChange.ProviderVerification = &domain.CodeChangeVerification{
			Status: *verificationStatus, AttemptedAt: *verificationAttemptedAt,
		}
	}
	if evidenceConnectionID != nil && evidenceProviderRepositoryID != nil && providerChangeID != nil &&
		evidenceObservedAt != nil && title != nil && state != nil && draft != nil &&
		sourceBranch != nil && targetBranch != nil && headSHA != nil && providerUpdatedAt != nil {
		item.CodeChange.ProviderEvidence = &domain.CodeChangeProviderEvidence{
			ConnectionID: *evidenceConnectionID, ProviderRepositoryID: *evidenceProviderRepositoryID,
			ProviderChangeID: *providerChangeID, Title: *title, State: *state, Draft: *draft,
			SourceBranch: *sourceBranch, TargetBranch: *targetBranch, HeadSHA: *headSHA,
			MergeCommitSHA: mergeCommitSHA, MergedAt: mergedAt,
			ProviderUpdatedAt: *providerUpdatedAt, ObservedAt: *evidenceObservedAt,
		}
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
