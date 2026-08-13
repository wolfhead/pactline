package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TaskStageClaimStore struct{ db *DB }

func NewTaskStageClaimStore(db *DB) *TaskStageClaimStore {
	return &TaskStageClaimStore{db: db}
}

const taskStageClaimColumns = `claim.id,claim.task_id,claim.task_number,claim.stage,
	claim.claimed_by_type,claim.claimed_by_user_id,claim.claimed_by_ref,
	claim.subject_user_id,claim.auth_method,claim.api_token_id,
	claim.token_name_snapshot,claim.agent_run_id,claim.client_kind,
	claim.client_session_id,claim.status,claim.outcome,claim.version,
	claim.expires_at,claim.created_at,claim.updated_at,claim.completed_at`

func scanTaskStageClaim(row scanner) (domain.TaskStageClaim, error) {
	var (
		claim           domain.TaskStageClaim
		claimedByType   string
		claimedByUserID *uuid.UUID
		claimedByRef    *string
		outcome         *string
		tokenName       *string
	)
	if err := row.Scan(
		&claim.ID, &claim.TaskID, &claim.TaskNumber, &claim.Stage,
		&claimedByType, &claimedByUserID, &claimedByRef,
		&claim.SubjectUserID, &claim.AuthMethod, &claim.APITokenID,
		&tokenName, &claim.AgentRunID, &claim.ClientKind,
		&claim.ClientSessionID, &claim.Status, &outcome, &claim.Version,
		&claim.ExpiresAt, &claim.CreatedAt, &claim.UpdatedAt, &claim.CompletedAt,
	); err != nil {
		return domain.TaskStageClaim{}, err
	}
	claim.ClaimedBy = actorFromColumns(claimedByType, claimedByUserID, claimedByRef)
	if outcome != nil {
		claim.Outcome = domain.TaskClaimOutcome(*outcome)
	}
	if tokenName != nil {
		claim.TokenName = *tokenName
	}
	return claim, nil
}

func (s *TaskStageClaimStore) Get(
	ctx context.Context,
	claimID uuid.UUID,
) (domain.TaskStageClaim, error) {
	claim, err := scanTaskStageClaim(s.db.Pool.QueryRow(ctx, `
		SELECT `+taskStageClaimColumns+`
		FROM task_stage_claims claim
		WHERE claim.id=$1`, claimID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskStageClaim{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskStageClaim{}, fmt.Errorf("get Task Claim %s: %w", claimID, err)
	}
	return claim, nil
}

func (s *TaskStageClaimStore) GetActiveForTaskNumber(
	ctx context.Context,
	taskNumber int64,
) (domain.TaskStageClaim, error) {
	claim, err := scanTaskStageClaim(s.db.Pool.QueryRow(ctx, `
		SELECT `+taskStageClaimColumns+`
		FROM task_stage_claims claim
		JOIN tasks task ON task.id=claim.task_id
		WHERE task.number=$1 AND claim.status='active'`, taskNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskStageClaim{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskStageClaim{}, fmt.Errorf("get active Claim for Task %d: %w", taskNumber, err)
	}
	return claim, nil
}

func (s *TaskStageClaimStore) GetCurrentForClient(
	ctx context.Context,
	operation domain.OperationActor,
	clientKind, clientSessionID string,
) (domain.TaskStageClaim, error) {
	if err := operation.Validate(); err != nil {
		return domain.TaskStageClaim{}, err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+taskStageClaimColumns+`
		FROM task_stage_claims claim
		WHERE claim.status='active'
		  AND claim.subject_user_id=$1
		  AND claim.auth_method=$2
		  AND claim.api_token_id IS NOT DISTINCT FROM $3
		  AND claim.agent_run_id IS NOT DISTINCT FROM $4
		  AND claim.client_kind=$5
		  AND claim.client_session_id=$6
		ORDER BY claim.created_at,claim.id
		LIMIT 2`,
		operation.UserID, operation.AuthMethod, operation.TokenID,
		operation.AgentRunID, clientKind, clientSessionID,
	)
	if err != nil {
		return domain.TaskStageClaim{}, fmt.Errorf("query current Task Claim: %w", err)
	}
	defer rows.Close()
	claims := make([]domain.TaskStageClaim, 0, 2)
	for rows.Next() {
		claim, scanErr := scanTaskStageClaim(rows)
		if scanErr != nil {
			return domain.TaskStageClaim{}, fmt.Errorf("scan current Task Claim: %w", scanErr)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return domain.TaskStageClaim{}, fmt.Errorf("iterate current Task Claims: %w", err)
	}
	if len(claims) == 0 {
		return domain.TaskStageClaim{}, domain.ErrNotFound
	}
	if len(claims) > 1 {
		return domain.TaskStageClaim{}, fmt.Errorf(
			"%w: client session owns more than one active Task Claim",
			domain.ErrConflict,
		)
	}
	return claims[0], nil
}

func (s *TaskStageClaimStore) ListForTaskNumber(
	ctx context.Context,
	taskNumber int64,
) ([]domain.TaskStageClaim, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+taskStageClaimColumns+`
		FROM task_stage_claims claim
		JOIN tasks task ON task.id=claim.task_id
		WHERE task.number=$1
		ORDER BY claim.created_at,claim.id`, taskNumber)
	if err != nil {
		return nil, fmt.Errorf("list Claims for Task %d: %w", taskNumber, err)
	}
	defer rows.Close()
	claims := []domain.TaskStageClaim{}
	for rows.Next() {
		claim, scanErr := scanTaskStageClaim(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Claim for Task %d: %w", taskNumber, scanErr)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Claims for Task %d: %w", taskNumber, err)
	}
	return claims, nil
}

func insertTaskStageClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskStageClaim,
) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	claimedByUserID, claimedByRef := actorColumns(claim.ClaimedBy)
	_, err := tx.Exec(ctx, `
		INSERT INTO task_stage_claims (
			id,task_id,task_number,stage,claimed_by_type,claimed_by_user_id,
			claimed_by_ref,subject_user_id,auth_method,api_token_id,
			token_name_snapshot,agent_run_id,client_kind,client_session_id,
			status,outcome,version,expires_at,created_at,updated_at,completed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
			$18,$19,$20,$21
		)`,
		claim.ID, claim.TaskID, claim.TaskNumber, claim.Stage,
		claim.ClaimedBy.Type, claimedByUserID, claimedByRef,
		claim.SubjectUserID, claim.AuthMethod, claim.APITokenID,
		nullIfEmpty(claim.TokenName), claim.AgentRunID, claim.ClientKind,
		claim.ClientSessionID, claim.Status, nullIfEmpty(string(claim.Outcome)),
		claim.Version, claim.ExpiresAt, claim.CreatedAt, claim.UpdatedAt,
		claim.CompletedAt,
	)
	return mapPgError(err)
}

func updateTaskStageClaim(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskStageClaim,
	expectedVersion int64,
) error {
	commandTag, err := tx.Exec(ctx, `
		UPDATE task_stage_claims
		SET status=$1,outcome=$2,version=$3,updated_at=$4,completed_at=$5
		WHERE id=$6 AND version=$7`,
		claim.Status, nullIfEmpty(string(claim.Outcome)), claim.Version,
		claim.UpdatedAt, claim.CompletedAt, claim.ID, expectedVersion,
	)
	if err != nil {
		return mapPgError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("%w: Task Claim version changed", domain.ErrConflict)
	}
	return nil
}
