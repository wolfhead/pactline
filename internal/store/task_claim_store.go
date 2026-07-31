package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TaskClaimStore struct{ db *DB }

func NewTaskClaimStore(db *DB) *TaskClaimStore { return &TaskClaimStore{db: db} }

const taskClaimColumns = `c.id, c.task_id, t.number, c.claimed_by_user_id,
	c.claimed_via_token_id, c.token_name_snapshot, c.client_kind,
	c.client_session_id, c.status, c.version, c.expires_at, c.terminal_reason,
	c.created_at, c.updated_at, c.completed_at`

func scanTaskClaim(s scanner) (domain.TaskClaim, error) {
	var claim domain.TaskClaim
	var terminalReason *string
	if err := s.Scan(
		&claim.ID,
		&claim.TaskID,
		&claim.TaskNumber,
		&claim.ClaimedByUserID,
		&claim.ClaimedViaTokenID,
		&claim.TokenNameSnapshot,
		&claim.ClientKind,
		&claim.ClientSessionID,
		&claim.Status,
		&claim.Version,
		&claim.ExpiresAt,
		&terminalReason,
		&claim.CreatedAt,
		&claim.UpdatedAt,
		&claim.CompletedAt,
	); err != nil {
		return domain.TaskClaim{}, err
	}
	if terminalReason != nil {
		claim.TerminalReason = *terminalReason
	}
	return claim, nil
}

func (s *TaskClaimStore) Claim(
	ctx context.Context,
	taskNumber int64,
	clientKind, clientSessionID string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaim, error) {
	if err := actor.Validate(); err != nil {
		return domain.TaskClaim{}, err
	}
	now = now.UTC()
	// Expire a previous Claim for this session in its own transaction. Keeping
	// that cleanup separate prevents two sessions that swap target Tasks from
	// holding their old Claim/Task locks while waiting on each other's target.
	if err := s.expireDueSessionClaim(
		ctx, actor.UserID, clientKind, clientSessionID, now,
	); err != nil {
		return domain.TaskClaim{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.TaskClaim{}, fmt.Errorf("begin Task Claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var task domain.Task
	err = tx.QueryRow(ctx, `
		SELECT id, number, version, status, execution_mode, assignee_id, archived_at
		FROM tasks
		WHERE number=$1`,
		taskNumber,
	).Scan(
		&task.ID,
		&task.Number,
		&task.Version,
		&task.Status,
		&task.ExecutionMode,
		&task.AssigneeID,
		&task.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskClaim{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskClaim{}, fmt.Errorf("lock Task %d for Claim: %w", taskNumber, err)
	}

	if err := expireDueTaskClaim(ctx, tx, task, now); err != nil {
		return domain.TaskClaim{}, err
	}
	// Claim expiry consistently locks Claim before Task. Lock and reload the
	// Task only after stale Claim cleanup to preserve that global lock order.
	if err := tx.QueryRow(ctx, `
		SELECT version, status, execution_mode, assignee_id, archived_at
		FROM tasks WHERE id=$1
		FOR UPDATE`,
		task.ID,
	).Scan(
		&task.Version,
		&task.Status,
		&task.ExecutionMode,
		&task.AssigneeID,
		&task.ArchivedAt,
	); err != nil {
		return domain.TaskClaim{}, fmt.Errorf("reload Task %d after Claim expiry: %w", taskNumber, err)
	}

	var unfinishedForSession bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM task_claims
			WHERE claimed_by_user_id=$1
			  AND client_kind=$2
			  AND client_session_id=$3
			  AND status IN ('active','waiting_human')
		)`,
		actor.UserID,
		strings.TrimSpace(clientKind),
		strings.TrimSpace(clientSessionID),
	).Scan(&unfinishedForSession); err != nil {
		return domain.TaskClaim{}, fmt.Errorf("check current session Claim: %w", err)
	}
	if unfinishedForSession {
		return domain.TaskClaim{}, fmt.Errorf(
			"%w: client session already has an unfinished Claim", domain.ErrConflict,
		)
	}

	claim, err := domain.NewTaskClaim(task, actor, clientKind, clientSessionID, now)
	if err != nil {
		return domain.TaskClaim{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_claims (
			id, task_id, claimed_by_user_id, claimed_via_token_id,
			token_name_snapshot, client_kind, client_session_id, status,
			version, expires_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		claim.ID,
		claim.TaskID,
		claim.ClaimedByUserID,
		claim.ClaimedViaTokenID,
		claim.TokenNameSnapshot,
		claim.ClientKind,
		claim.ClientSessionID,
		claim.Status,
		claim.Version,
		claim.ExpiresAt,
		claim.CreatedAt,
		claim.UpdatedAt,
	)
	if err != nil {
		return domain.TaskClaim{}, mapPgError(err)
	}

	if err := updateTaskStatusForClaim(
		ctx, tx, task, domain.TaskStatusTodo, domain.TaskStatusInProgress, actor, now,
	); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := insertClaimAudit(ctx, tx, claim, actor, "claimed", nil); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaim{}, fmt.Errorf("commit Task Claim: %w", err)
	}
	slog.Info(
		"Task claimed by external client",
		"task_number", claim.TaskNumber,
		"claim_id", claim.ID,
		"actor_id", actor.UserID,
		"client_kind", claim.ClientKind,
		"status", claim.Status,
	)
	return claim, nil
}

func (s *TaskClaimStore) GetCurrent(
	ctx context.Context,
	userID uuid.UUID,
	clientKind, clientSessionID string,
) (domain.TaskClaim, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE c.claimed_by_user_id=$1
		  AND c.client_kind=$2
		  AND c.client_session_id=$3
		  AND c.status IN ('active','waiting_human')
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT 1`,
		userID,
		strings.TrimSpace(clientKind),
		strings.TrimSpace(clientSessionID),
	)
	claim, err := scanTaskClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskClaim{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskClaim{}, fmt.Errorf("get current Task Claim: %w", err)
	}
	return claim, nil
}

func (s *TaskClaimStore) Get(ctx context.Context, id uuid.UUID) (domain.TaskClaim, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE c.id=$1`,
		id,
	)
	claim, err := scanTaskClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskClaim{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskClaim{}, fmt.Errorf("get Task Claim %s: %w", id, err)
	}
	return claim, nil
}

func (s *TaskClaimStore) ListForTaskNumber(
	ctx context.Context,
	taskNumber int64,
) ([]domain.TaskClaim, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE t.number=$1
		ORDER BY c.created_at, c.id`,
		taskNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("list Task Claims for Task %d: %w", taskNumber, err)
	}
	defer rows.Close()
	claims := []domain.TaskClaim{}
	for rows.Next() {
		claim, scanErr := scanTaskClaim(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Task Claims for Task %d: %w", taskNumber, err)
	}
	return claims, nil
}

func (s *TaskClaimStore) Extend(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	clientKind, clientSessionID string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaim, error) {
	return s.mutateOwnedClaim(
		ctx, id, expectedVersion, clientKind, clientSessionID, actor, now,
		func(claim *domain.TaskClaim, _ pgx.Tx) error {
			return claim.Extend(now)
		},
		"extended",
	)
}

func (s *TaskClaimStore) AddProgress(
	ctx context.Context,
	id uuid.UUID,
	clientKind, clientSessionID, body string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaimMessage, error) {
	tx, claim, err := s.lockOwnedClaim(
		ctx, id, clientKind, clientSessionID, actor, now,
	)
	if err != nil {
		return domain.TaskClaimMessage{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if claim.Status != domain.TaskClaimStatusActive {
		return domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: progress requires an active Claim", domain.ErrConflict,
		)
	}
	message, err := insertAgentClaimMessage(
		ctx, tx, claim, domain.TaskClaimMessageProgress, body, nil, actor, now,
	)
	if err != nil {
		return domain.TaskClaimMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaimMessage{}, fmt.Errorf("commit Claim progress: %w", err)
	}
	return message, nil
}

func (s *TaskClaimStore) Ask(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	clientKind, clientSessionID, body string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaim, domain.TaskClaimMessage, error) {
	tx, claim, err := s.lockOwnedClaim(
		ctx, id, clientKind, clientSessionID, actor, now,
	)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if claim.Version != expectedVersion {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			domain.VersionConflictError{CurrentVersion: claim.Version}
	}
	message, err := insertAgentClaimMessage(
		ctx, tx, claim, domain.TaskClaimMessageQuestion, body, nil, actor, now,
	)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := claim.WaitForHuman(now); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := insertClaimAudit(ctx, tx, claim, actor, "waiting_for_human", nil); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			fmt.Errorf("commit Claim question: %w", err)
	}
	return claim, message, nil
}

func (s *TaskClaimStore) Answer(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	body string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaim, domain.TaskClaimMessage, error) {
	if err := actor.Validate(); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if actor.AuthMethod != domain.AuthenticationMethodSession {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: a browser user must answer the Agent", domain.ErrForbidden,
		)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			fmt.Errorf("begin answer Claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	claim, err := lockClaim(ctx, tx, id)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if claim.ClaimedByUserID != actor.UserID {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: only the assigned user may answer this Claim", domain.ErrForbidden,
		)
	}
	if claim.Version != expectedVersion {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			domain.VersionConflictError{CurrentVersion: claim.Version}
	}
	if claim.Status != domain.TaskClaimStatusWaitingHuman {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: Claim is not waiting for a human", domain.ErrConflict,
		)
	}
	var questionID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM task_claim_messages
		WHERE claim_id=$1 AND kind='question'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		claim.ID,
	).Scan(&questionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: waiting Claim has no question", domain.ErrConflict,
		)
	}
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			fmt.Errorf("find Claim question: %w", err)
	}
	message, err := insertHumanClaimAnswer(
		ctx, tx, claim, body, questionID, actor, now,
	)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := claim.Resume(now); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := insertClaimAudit(ctx, tx, claim, actor, "resumed", nil); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			fmt.Errorf("commit Claim answer: %w", err)
	}
	return claim, message, nil
}

func (s *TaskClaimStore) Release(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	clientKind, clientSessionID, handoff string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaim, error) {
	if err := actor.Validate(); err != nil {
		return domain.TaskClaim{}, err
	}
	if actor.AuthMethod != domain.AuthenticationMethodAPIToken &&
		actor.AuthMethod != domain.AuthenticationMethodSession {
		return domain.TaskClaim{}, fmt.Errorf(
			"%w: only the owning personal Token or browser user may release a Claim",
			domain.ErrForbidden,
		)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.TaskClaim{}, fmt.Errorf("begin release Claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	claim, err := lockClaim(ctx, tx, id)
	if err != nil {
		return domain.TaskClaim{}, err
	}
	if claim.ClaimedByUserID != actor.UserID {
		return domain.TaskClaim{}, fmt.Errorf(
			"%w: only the assigned user may release this Claim", domain.ErrForbidden,
		)
	}
	if actor.AuthMethod == domain.AuthenticationMethodAPIToken &&
		!claim.OwnedBy(actor.UserID, clientKind, clientSessionID) {
		return domain.TaskClaim{}, fmt.Errorf(
			"%w: Claim belongs to another client session", domain.ErrForbidden,
		)
	}
	if actor.AuthMethod == domain.AuthenticationMethodAPIToken &&
		!now.UTC().Before(claim.ExpiresAt) {
		return domain.TaskClaim{}, fmt.Errorf("%w: Claim has expired", domain.ErrConflict)
	}
	if claim.Version != expectedVersion {
		return domain.TaskClaim{},
			domain.VersionConflictError{CurrentVersion: claim.Version}
	}
	if actor.AuthMethod == domain.AuthenticationMethodAPIToken &&
		strings.TrimSpace(handoff) != "" {
		if _, err := insertAgentClaimMessage(
			ctx, tx, claim, domain.TaskClaimMessageHandoff, handoff, nil, actor, now,
		); err != nil {
			return domain.TaskClaim{}, err
		}
	}
	if err := claim.Release("released", now); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := returnClaimTaskToTodo(ctx, tx, claim, actor, now); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := insertClaimAudit(ctx, tx, claim, actor, "released", nil); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaim{}, fmt.Errorf("commit release Claim: %w", err)
	}
	return claim, nil
}

func (s *TaskClaimStore) Submit(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	clientKind, clientSessionID, report string,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaim, domain.TaskClaimMessage, error) {
	tx, claim, err := s.lockOwnedClaim(
		ctx, id, clientKind, clientSessionID, actor, now,
	)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if claim.Version != expectedVersion {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			domain.VersionConflictError{CurrentVersion: claim.Version}
	}
	if claim.Status != domain.TaskClaimStatusActive {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: only an active Claim may be submitted", domain.ErrConflict,
		)
	}
	task, err := lockClaimTask(ctx, tx, claim.TaskID)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if task.Status != domain.TaskStatusInProgress {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: Task is no longer in progress", domain.ErrConflict,
		)
	}
	readiness, err := taskCompletionReadiness(ctx, tx, claim.TaskID)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := readiness.ValidateAcceptance(); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	message, err := insertAgentClaimMessage(
		ctx, tx, claim, domain.TaskClaimMessageSubmission, report, nil, actor, now,
	)
	if err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := claim.Submit(now); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := updateTaskStatusForClaim(
		ctx, tx, task, domain.TaskStatusInProgress, domain.TaskStatusInReview, actor, now,
	); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := insertClaimAudit(ctx, tx, claim, actor, "submitted", nil); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaim{}, domain.TaskClaimMessage{},
			fmt.Errorf("commit submit Claim: %w", err)
	}
	return claim, message, nil
}

func (s *TaskClaimStore) ListMessages(
	ctx context.Context,
	claimID uuid.UUID,
) ([]domain.TaskClaimMessage, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT m.id, m.claim_id, c.task_id, m.author_type, m.author_user_id,
		       m.kind, m.body, m.reply_to_message_id, m.request_id,
		       m.api_token_id, m.token_name_snapshot, m.created_at
		FROM task_claim_messages m
		JOIN task_claims c ON c.id=m.claim_id
		WHERE m.claim_id=$1
		ORDER BY m.created_at, m.id`,
		claimID,
	)
	if err != nil {
		return nil, fmt.Errorf("list Task Claim messages: %w", err)
	}
	defer rows.Close()
	messages := []domain.TaskClaimMessage{}
	for rows.Next() {
		message, err := scanTaskClaimMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Task Claim messages: %w", err)
	}
	return messages, nil
}

func (s *TaskClaimStore) ListMessagesForTaskNumber(
	ctx context.Context,
	taskNumber int64,
) ([]domain.TaskClaimMessage, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT m.id, m.claim_id, c.task_id, m.author_type, m.author_user_id,
		       m.kind, m.body, m.reply_to_message_id, m.request_id,
		       m.api_token_id, m.token_name_snapshot, m.created_at
		FROM task_claim_messages m
		JOIN task_claims c ON c.id=m.claim_id
		JOIN tasks t ON t.id=c.task_id
		WHERE t.number=$1
		ORDER BY m.created_at, m.id`,
		taskNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("list Agent messages for Task %d: %w", taskNumber, err)
	}
	defer rows.Close()
	messages := []domain.TaskClaimMessage{}
	for rows.Next() {
		message, scanErr := scanTaskClaimMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Agent messages for Task %d: %w", taskNumber, err)
	}
	return messages, nil
}

func (s *TaskClaimStore) ExpireDue(
	ctx context.Context,
	now time.Time,
	limit int,
) (int, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	now = now.UTC()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire Task Claims: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	rows, err := tx.Query(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE c.status IN ('active','waiting_human') AND c.expires_at <= $1
		ORDER BY c.expires_at, c.id
		LIMIT $2
		FOR UPDATE OF c SKIP LOCKED`,
		now,
		limit,
	)
	if err != nil {
		return 0, fmt.Errorf("list due Task Claims: %w", err)
	}
	var claims []domain.TaskClaim
	for rows.Next() {
		claim, scanErr := scanTaskClaim(rows)
		if scanErr != nil {
			rows.Close()
			return 0, fmt.Errorf("scan due Task Claim: %w", scanErr)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate due Task Claims: %w", err)
	}
	rows.Close()

	for i := range claims {
		claim := claims[i]
		actor := claimActor(claim, "maintenance:Task-Claim-expiry:"+claim.ID.String())
		if err := claim.Expire(now); err != nil {
			return 0, err
		}
		if err := updateClaimRow(ctx, tx, &claim); err != nil {
			return 0, err
		}
		if err := returnClaimTaskToTodo(ctx, tx, claim, actor, now); err != nil {
			return 0, err
		}
		if err := insertClaimAudit(ctx, tx, claim, actor, "expired", nil); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expired Task Claims: %w", err)
	}
	if len(claims) > 0 {
		slog.Info("expired Task Claims", "count", len(claims))
	}
	return len(claims), nil
}

func (s *TaskClaimStore) mutateOwnedClaim(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	clientKind, clientSessionID string,
	actor domain.OperationActor,
	now time.Time,
	mutate func(*domain.TaskClaim, pgx.Tx) error,
	action string,
) (domain.TaskClaim, error) {
	tx, claim, err := s.lockOwnedClaim(
		ctx, id, clientKind, clientSessionID, actor, now,
	)
	if err != nil {
		return domain.TaskClaim{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if claim.Version != expectedVersion {
		return domain.TaskClaim{},
			domain.VersionConflictError{CurrentVersion: claim.Version}
	}
	if err := mutate(&claim, tx); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := insertClaimAudit(ctx, tx, claim, actor, action, nil); err != nil {
		return domain.TaskClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TaskClaim{}, fmt.Errorf("commit Claim %s: %w", action, err)
	}
	return claim, nil
}

func (s *TaskClaimStore) lockOwnedClaim(
	ctx context.Context,
	id uuid.UUID,
	clientKind, clientSessionID string,
	actor domain.OperationActor,
	now time.Time,
) (pgx.Tx, domain.TaskClaim, error) {
	if err := actor.Validate(); err != nil {
		return nil, domain.TaskClaim{}, err
	}
	if actor.AuthMethod != domain.AuthenticationMethodAPIToken {
		return nil, domain.TaskClaim{}, fmt.Errorf(
			"%w: a personal API Token is required", domain.ErrForbidden,
		)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, domain.TaskClaim{}, fmt.Errorf("begin mutate Task Claim: %w", err)
	}
	claim, err := lockClaim(ctx, tx, id)
	if err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		return nil, domain.TaskClaim{}, err
	}
	if !claim.OwnedBy(actor.UserID, clientKind, clientSessionID) {
		tx.Rollback(ctx) //nolint:errcheck
		return nil, domain.TaskClaim{}, fmt.Errorf(
			"%w: Claim belongs to another client session", domain.ErrForbidden,
		)
	}
	if !now.UTC().Before(claim.ExpiresAt) {
		tx.Rollback(ctx) //nolint:errcheck
		return nil, domain.TaskClaim{}, fmt.Errorf(
			"%w: Claim has expired", domain.ErrConflict,
		)
	}
	return tx, claim, nil
}

func lockClaim(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.TaskClaim, error) {
	row := tx.QueryRow(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE c.id=$1
		FOR UPDATE OF c`,
		id,
	)
	claim, err := scanTaskClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskClaim{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskClaim{}, fmt.Errorf("lock Task Claim %s: %w", id, err)
	}
	return claim, nil
}

func lockClaimTask(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.Task, error) {
	var task domain.Task
	err := tx.QueryRow(ctx, `
		SELECT id, number, version, status, execution_mode, assignee_id, archived_at
		FROM tasks
		WHERE id=$1
		FOR UPDATE`,
		id,
	).Scan(
		&task.ID,
		&task.Number,
		&task.Version,
		&task.Status,
		&task.ExecutionMode,
		&task.AssigneeID,
		&task.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("lock claimed Task %s: %w", id, err)
	}
	return task, nil
}

func updateClaimRow(ctx context.Context, tx pgx.Tx, claim *domain.TaskClaim) error {
	var version int64
	err := tx.QueryRow(ctx, `
		UPDATE task_claims
		SET status=$2, version=version+1, expires_at=$3,
		    terminal_reason=$4, updated_at=$5, completed_at=$6
		WHERE id=$1 AND version=$7
		RETURNING version`,
		claim.ID,
		claim.Status,
		claim.ExpiresAt,
		nullIfEmpty(claim.TerminalReason),
		claim.UpdatedAt,
		claim.CompletedAt,
		claim.Version,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("update Task Claim %s: %w", claim.ID, err)
	}
	claim.Version = version
	return nil
}

func updateTaskStatusForClaim(
	ctx context.Context,
	tx pgx.Tx,
	task domain.Task,
	expected, next domain.TaskStatus,
	actor domain.OperationActor,
	now time.Time,
) error {
	if task.Status != expected {
		return fmt.Errorf(
			"%w: Task status is %s, expected %s", domain.ErrConflict, task.Status, expected,
		)
	}
	var version int64
	err := tx.QueryRow(ctx, `
		UPDATE tasks
		SET status=$2, version=version+1, updated_at=$3,
		    completed_at=CASE WHEN $2='done' THEN $3::timestamptz ELSE NULL END
		WHERE id=$1 AND version=$4
		RETURNING version`,
		task.ID,
		next,
		now,
		task.Version,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("update claimed Task status: %w", err)
	}
	if err := recordFieldChange(
		ctx, tx, task.ID, actor, domain.ActivityFieldStatus, string(expected), string(next),
	); err != nil {
		return err
	}
	oldValue, _ := json.Marshal(map[string]any{"status": expected})
	newValue, _ := json.Marshal(map[string]any{"status": next})
	return InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt:   now,
		Actor:        actor,
		EntityType:   "task",
		EntityID:     task.ID,
		EntityNumber: &task.Number,
		Action:       "claim_status_transition",
		OldValue:     oldValue,
		NewValue:     newValue,
	})
}

func returnClaimTaskToTodo(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskClaim,
	actor domain.OperationActor,
	now time.Time,
) error {
	task, err := lockClaimTask(ctx, tx, claim.TaskID)
	if err != nil {
		return err
	}
	if task.Status != domain.TaskStatusInProgress {
		return nil
	}
	return updateTaskStatusForClaim(
		ctx, tx, task, domain.TaskStatusInProgress, domain.TaskStatusTodo, actor, now,
	)
}

func taskCompletionReadiness(
	ctx context.Context,
	tx pgx.Tx,
	taskID uuid.UUID,
) (domain.TaskCompletionReadiness, error) {
	var readiness domain.TaskCompletionReadiness
	err := tx.QueryRow(ctx, `
		SELECT count(*),
			count(*) FILTER (
				WHERE latest.outcome IS NULL
					OR latest.outcome NOT IN ('passed', 'waived')
			),
			(SELECT count(*)
			 FROM tasks child
			 WHERE child.parent_task_id=$1
			   AND child.status NOT IN ('done', 'cancelled')),
			(SELECT count(*)
			 FROM task_dependencies dependency
			 JOIN tasks predecessor ON predecessor.id=dependency.depends_on_task_id
			 WHERE dependency.task_id=$1
			   AND predecessor.status NOT IN ('done', 'cancelled'))
		FROM acceptance_criteria ac
		LEFT JOIN LATERAL (
			SELECT chk.outcome
			FROM acceptance_checks chk
			WHERE chk.criterion_id=ac.id
			  AND chk.criterion_revision=ac.revision
			ORDER BY chk.checked_at DESC, chk.id DESC
			LIMIT 1
		) latest ON true
		WHERE ac.task_id=$1 AND ac.archived_at IS NULL`,
		taskID,
	).Scan(
		&readiness.ActiveCriteria,
		&readiness.UnsatisfiedCriteria,
		&readiness.UnfinishedChildren,
		&readiness.UnfinishedDependencies,
	)
	if err != nil {
		return domain.TaskCompletionReadiness{},
			fmt.Errorf("read claimed Task readiness: %w", err)
	}
	return readiness, nil
}

func insertAgentClaimMessage(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskClaim,
	kind domain.TaskClaimMessageKind,
	body string,
	replyToID *uuid.UUID,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaimMessage, error) {
	if actor.AuthMethod != domain.AuthenticationMethodAPIToken || actor.TokenID == nil {
		return domain.TaskClaimMessage{}, fmt.Errorf(
			"%w: an executor Token is required", domain.ErrForbidden,
		)
	}
	ref := actor.TokenName
	if strings.TrimSpace(ref) == "" {
		ref = "external Agent"
	}
	message := domain.TaskClaimMessage{
		ID:         uuid.New(),
		ClaimID:    claim.ID,
		TaskID:     claim.TaskID,
		Author:     domain.Actor{Type: domain.ActorTypeAgent, Ref: ref},
		Kind:       kind,
		Body:       body,
		ReplyToID:  replyToID,
		RequestID:  actor.RequestID,
		APITokenID: actor.TokenID,
		TokenName:  actor.TokenName,
		CreatedAt:  now.UTC(),
	}
	if err := message.Validate(); err != nil {
		return domain.TaskClaimMessage{}, err
	}
	if err := insertClaimMessage(ctx, tx, message, actor.UserID); err != nil {
		return domain.TaskClaimMessage{}, err
	}
	if err := insertMessageAudit(ctx, tx, claim, message, actor); err != nil {
		return domain.TaskClaimMessage{}, err
	}
	return message, nil
}

func insertHumanClaimAnswer(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskClaim,
	body string,
	replyToID uuid.UUID,
	actor domain.OperationActor,
	now time.Time,
) (domain.TaskClaimMessage, error) {
	userID := actor.UserID
	message := domain.TaskClaimMessage{
		ID:        uuid.New(),
		ClaimID:   claim.ID,
		TaskID:    claim.TaskID,
		Author:    domain.Actor{Type: domain.ActorTypeUser, UserID: &userID},
		Kind:      domain.TaskClaimMessageAnswer,
		Body:      body,
		ReplyToID: &replyToID,
		RequestID: actor.RequestID,
		CreatedAt: now.UTC(),
	}
	if err := message.Validate(); err != nil {
		return domain.TaskClaimMessage{}, err
	}
	if err := insertClaimMessage(ctx, tx, message, actor.UserID); err != nil {
		return domain.TaskClaimMessage{}, err
	}
	if err := insertMessageAudit(ctx, tx, claim, message, actor); err != nil {
		return domain.TaskClaimMessage{}, err
	}
	return message, nil
}

func insertClaimMessage(
	ctx context.Context,
	tx pgx.Tx,
	message domain.TaskClaimMessage,
	authorUserID uuid.UUID,
) error {
	var tokenName any
	if strings.TrimSpace(message.TokenName) != "" {
		tokenName = message.TokenName
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO task_claim_messages (
			id, claim_id, author_type, author_user_id, kind, body,
			reply_to_message_id, request_id, api_token_id,
			token_name_snapshot, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		message.ID,
		message.ClaimID,
		message.Author.Type,
		authorUserID,
		message.Kind,
		message.Body,
		message.ReplyToID,
		message.RequestID,
		message.APITokenID,
		tokenName,
		message.CreatedAt,
	)
	if err != nil {
		return mapPgError(err)
	}
	return nil
}

func scanTaskClaimMessage(s scanner) (domain.TaskClaimMessage, error) {
	var message domain.TaskClaimMessage
	var authorUserID *uuid.UUID
	var tokenName *string
	if err := s.Scan(
		&message.ID,
		&message.ClaimID,
		&message.TaskID,
		&message.Author.Type,
		&authorUserID,
		&message.Kind,
		&message.Body,
		&message.ReplyToID,
		&message.RequestID,
		&message.APITokenID,
		&tokenName,
		&message.CreatedAt,
	); err != nil {
		return domain.TaskClaimMessage{}, fmt.Errorf("scan Task Claim message: %w", err)
	}
	message.Author.UserID = authorUserID
	if tokenName != nil {
		message.TokenName = *tokenName
		message.Author.Ref = *tokenName
	}
	if message.Author.Type == domain.ActorTypeSystem {
		message.Author.Ref = "system"
	}
	return message, nil
}

func updateClaimTaskFromInProgress(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskClaim,
	status domain.TaskStatus,
	actor domain.OperationActor,
	now time.Time,
) error {
	task, err := lockClaimTask(ctx, tx, claim.TaskID)
	if err != nil {
		return err
	}
	if task.Status != domain.TaskStatusInProgress {
		return nil
	}
	return updateTaskStatusForClaim(
		ctx, tx, task, domain.TaskStatusInProgress, status, actor, now,
	)
}

func expireDueTaskClaim(
	ctx context.Context,
	tx pgx.Tx,
	task domain.Task,
	now time.Time,
) error {
	row := tx.QueryRow(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE c.task_id=$1
		  AND c.status IN ('active','waiting_human')
		  AND c.expires_at <= $2
		FOR UPDATE OF c`,
		task.ID,
		now,
	)
	claim, err := scanTaskClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock expired Task Claim: %w", err)
	}
	actor := claimActor(claim, "claim:expire-on-reclaim:"+claim.ID.String())
	if err := claim.Expire(now); err != nil {
		return err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return err
	}
	if err := updateClaimTaskFromInProgress(
		ctx, tx, claim, domain.TaskStatusTodo, actor, now,
	); err != nil {
		return err
	}
	return insertClaimAudit(ctx, tx, claim, actor, "expired", nil)
}

func (s *TaskClaimStore) expireDueSessionClaim(
	ctx context.Context,
	userID uuid.UUID,
	clientKind, clientSessionID string,
	now time.Time,
) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin expire session Claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if err := expireDueSessionClaimInTx(
		ctx, tx, userID, clientKind, clientSessionID, now,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit expired session Claim: %w", err)
	}
	return nil
}

func expireDueSessionClaimInTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	clientKind, clientSessionID string,
	now time.Time,
) error {
	row := tx.QueryRow(ctx, `
		SELECT `+taskClaimColumns+`
		FROM task_claims c
		JOIN tasks t ON t.id=c.task_id
		WHERE c.claimed_by_user_id=$1
		  AND c.client_kind=$2
		  AND c.client_session_id=$3
		  AND c.status IN ('active','waiting_human')
		  AND c.expires_at <= $4
		FOR UPDATE OF c`,
		userID,
		strings.TrimSpace(clientKind),
		strings.TrimSpace(clientSessionID),
		now,
	)
	claim, err := scanTaskClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock expired session Claim: %w", err)
	}
	actor := claimActor(claim, "claim:expire-session:"+claim.ID.String())
	if err := claim.Expire(now); err != nil {
		return err
	}
	if err := updateClaimRow(ctx, tx, &claim); err != nil {
		return err
	}
	if err := updateClaimTaskFromInProgress(
		ctx, tx, claim, domain.TaskStatusTodo, actor, now,
	); err != nil {
		return err
	}
	return insertClaimAudit(ctx, tx, claim, actor, "expired", nil)
}

func claimActor(claim domain.TaskClaim, requestID string) domain.OperationActor {
	tokenID := claim.ClaimedViaTokenID
	return domain.OperationActor{
		UserID:     claim.ClaimedByUserID,
		AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID:    &tokenID,
		TokenName:  claim.TokenNameSnapshot,
		RequestID:  requestID,
	}
}

func insertClaimAudit(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskClaim,
	actor domain.OperationActor,
	action string,
	oldStatus *domain.TaskClaimStatus,
) error {
	var oldValue []byte
	if oldStatus != nil {
		oldValue, _ = json.Marshal(map[string]any{"status": *oldStatus})
	}
	newValue, _ := json.Marshal(map[string]any{
		"task_number":     claim.TaskNumber,
		"client_kind":     claim.ClientKind,
		"status":          claim.Status,
		"expires_at":      claim.ExpiresAt,
		"terminal_reason": claim.TerminalReason,
	})
	taskNumber := claim.TaskNumber
	return InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt:   claim.UpdatedAt,
		Actor:        actor,
		EntityType:   "task_claim",
		EntityID:     claim.ID,
		EntityNumber: &taskNumber,
		Action:       action,
		OldValue:     oldValue,
		NewValue:     newValue,
	})
}

func insertMessageAudit(
	ctx context.Context,
	tx pgx.Tx,
	claim domain.TaskClaim,
	message domain.TaskClaimMessage,
	actor domain.OperationActor,
) error {
	newValue, _ := json.Marshal(map[string]any{
		"claim_id": message.ClaimID,
		"kind":     message.Kind,
	})
	taskNumber := claim.TaskNumber
	return InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt:   message.CreatedAt,
		Actor:        actor,
		EntityType:   "task_claim_message",
		EntityID:     message.ID,
		EntityNumber: &taskNumber,
		Action:       "created",
		NewValue:     newValue,
	})
}
