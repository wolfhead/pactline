package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type AgentStore struct{ db *DB }

func NewAgentStore(db *DB) *AgentStore { return &AgentStore{db: db} }

func (s *AgentStore) CreateRun(
	ctx context.Context,
	run pactagent.Run,
) (stored pactagent.Run, created bool, err error) {
	if err := run.Validate(); err != nil {
		return pactagent.Run{}, false, err
	}
	tag, err := s.db.Pool.Exec(ctx, `
		INSERT INTO agent_runs (
			id, provider, tenant_id, conversation_id, trigger_message_id,
			provider_event_id, thread_root_message_id, reply_parent_message_id,
			conversation_revision_id,
			trigger_occurred_at, initiating_user_id, initiating_subject_id,
			status, command_kind,
			model, prompt_version, attempt_count, clarification_rounds,
			context_messages_used, available_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22
		)
		ON CONFLICT DO NOTHING`,
		run.ID, run.Provider, run.TenantID, run.ConversationID,
		run.TriggerMessageID, run.ProviderEventID,
		nullIfEmpty(run.ThreadRootMessageID), nullIfEmpty(run.ReplyParentMessageID),
		run.ConversationRevisionID,
		run.TriggerOccurredAt, run.InitiatingUserID, run.InitiatingSubjectID,
		run.Status, run.CommandKind,
		run.Model, run.PromptVersion, run.AttemptCount, run.ClarificationRounds,
		run.ContextMessagesUsed, run.AvailableAt, run.CreatedAt, run.UpdatedAt,
	)
	if err != nil {
		return pactagent.Run{}, false, fmt.Errorf("insert agent run: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return run, true, nil
	}
	stored, err = s.findRunByProviderIdentity(
		ctx, run.Provider, run.TenantID, run.ProviderEventID, run.TriggerMessageID,
	)
	if err != nil {
		return pactagent.Run{}, false, err
	}
	return stored, false, nil
}

func (s *AgentStore) CreateRunWithInput(
	ctx context.Context,
	run pactagent.Run,
	input pactagent.RunInput,
	now time.Time,
) (stored pactagent.Run, created bool, err error) {
	if err := run.Validate(); err != nil {
		return pactagent.Run{}, false, err
	}
	if input.RunID != run.ID || input.EncryptionKeyID == "" ||
		len(input.CommandCiphertext) == 0 {
		return pactagent.Run{}, false, pactagent.ErrInvalidRun
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return pactagent.Run{}, false, fmt.Errorf("begin Agent run transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	tag, err := tx.Exec(ctx, `
		INSERT INTO agent_runs (
			id, provider, tenant_id, conversation_id, trigger_message_id,
			provider_event_id, thread_root_message_id, reply_parent_message_id,
			conversation_revision_id,
			trigger_occurred_at, initiating_user_id, initiating_subject_id,
			status, command_kind,
			model, prompt_version, attempt_count, clarification_rounds,
			context_messages_used, available_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22
		)
		ON CONFLICT DO NOTHING`,
		run.ID, run.Provider, run.TenantID, run.ConversationID,
		run.TriggerMessageID, run.ProviderEventID,
		nullIfEmpty(run.ThreadRootMessageID), nullIfEmpty(run.ReplyParentMessageID),
		run.ConversationRevisionID,
		run.TriggerOccurredAt, run.InitiatingUserID, run.InitiatingSubjectID,
		run.Status, run.CommandKind,
		run.Model, run.PromptVersion, run.AttemptCount, run.ClarificationRounds,
		run.ContextMessagesUsed, run.AvailableAt, run.CreatedAt, run.UpdatedAt,
	)
	if err != nil {
		return pactagent.Run{}, false, fmt.Errorf("insert Agent run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return pactagent.Run{}, false, fmt.Errorf("commit Agent run deduplication: %w", err)
		}
		stored, err := s.findRunByProviderIdentity(
			ctx,
			run.Provider,
			run.TenantID,
			run.ProviderEventID,
			run.TriggerMessageID,
		)
		return stored, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_run_inputs (
			run_id, encryption_key_id, command_ciphertext, updated_at
		) VALUES ($1,$2,$3,$4)`,
		input.RunID,
		input.EncryptionKeyID,
		input.CommandCiphertext,
		now.UTC(),
	); err != nil {
		return pactagent.Run{}, false, fmt.Errorf("insert Agent run input: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return pactagent.Run{}, false, fmt.Errorf("commit Agent run: %w", err)
	}
	return run, true, nil
}

func (s *AgentStore) GetRun(ctx context.Context, id uuid.UUID) (pactagent.Run, error) {
	row := s.db.Pool.QueryRow(ctx, agentRunSelect+` WHERE id=$1`, id)
	return scanAgentRun(row)
}

func (s *AgentStore) SaveRunInput(
	ctx context.Context,
	input pactagent.RunInput,
	now time.Time,
) error {
	if input.RunID == uuid.Nil || input.EncryptionKeyID == "" || len(input.CommandCiphertext) == 0 {
		return pactagent.ErrInvalidRun
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO agent_run_inputs (
			run_id, encryption_key_id, command_ciphertext, updated_at
		) VALUES ($1,$2,$3,$4)
		ON CONFLICT (run_id) DO NOTHING`,
		input.RunID, input.EncryptionKeyID, input.CommandCiphertext, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("save Agent run input: %w", err)
	}
	return nil
}

func (s *AgentStore) GetRunInput(
	ctx context.Context,
	runID uuid.UUID,
) (pactagent.RunInput, error) {
	var input pactagent.RunInput
	err := s.db.Pool.QueryRow(ctx, `
		SELECT run_id, encryption_key_id, command_ciphertext,
		       pending_resume_ciphertext
		FROM agent_run_inputs
		WHERE run_id=$1`,
		runID,
	).Scan(
		&input.RunID, &input.EncryptionKeyID, &input.CommandCiphertext,
		&input.PendingResumeCiphertext,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pactagent.RunInput{}, pactagent.ErrAgentRunNotFound
	}
	if err != nil {
		return pactagent.RunInput{}, fmt.Errorf("load Agent run input: %w", err)
	}
	return input, nil
}

func (s *AgentStore) SavePendingResumeInput(
	ctx context.Context,
	runID uuid.UUID,
	ciphertext []byte,
	now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_run_inputs
		SET pending_resume_ciphertext=$2, updated_at=$3
		WHERE run_id=$1`,
		runID, ciphertext, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("save Agent resume input: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentRunNotFound
	}
	return nil
}

func (s *AgentStore) ClearPendingResumeInput(ctx context.Context, runID uuid.UUID, now time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_run_inputs
		SET pending_resume_ciphertext=NULL, updated_at=$2
		WHERE run_id=$1`,
		runID, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("clear Agent resume input: %w", err)
	}
	return nil
}

func (s *AgentStore) GetRunForDelegate(
	ctx context.Context,
	id, userID uuid.UUID,
) (pactagent.Run, error) {
	row := s.db.Pool.QueryRow(ctx,
		agentRunSelect+` WHERE id=$1 AND initiating_user_id=$2`,
		id, userID,
	)
	return scanAgentRun(row)
}

func (s *AgentStore) FindWaitingRunByClarification(
	ctx context.Context,
	provider, tenantID, conversationID, clarificationMessageID string,
) (pactagent.Run, error) {
	row := s.db.Pool.QueryRow(ctx, agentRunSelect+`
		WHERE provider=$1 AND tenant_id=$2 AND conversation_id=$3
		  AND clarification_message_id=$4 AND status='waiting_user'
		ORDER BY created_at DESC
		LIMIT 1`,
		provider, tenantID, conversationID, clarificationMessageID,
	)
	return scanAgentRun(row)
}

func (s *AgentStore) ClaimRun(
	ctx context.Context,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
) (pactagent.Run, bool, error) {
	if workerID == "" {
		return pactagent.Run{}, false, pactagent.ErrInvalidRun
	}
	if leaseDuration <= 0 {
		leaseDuration = pactagent.DefaultLeaseDuration
	}
	leaseExpiresAt := now.UTC().Add(leaseDuration)
	row := s.db.Pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM agent_runs
			WHERE (
				(status='queued' AND available_at <= $1)
				OR
				(status='running' AND lease_expires_at <= $1)
			)
			ORDER BY available_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE agent_runs AS runs
		SET status='running',
		    lease_owner=$2,
		    lease_expires_at=$3,
		    attempt_count=runs.attempt_count+1,
		    updated_at=$1
		FROM candidate
		WHERE runs.id=candidate.id
		RETURNING `+agentRunColumns,
		now.UTC(), workerID, leaseExpiresAt,
	)
	run, err := scanAgentRun(row)
	if errors.Is(err, pactagent.ErrAgentRunNotFound) {
		return pactagent.Run{}, false, nil
	}
	if err != nil {
		return pactagent.Run{}, false, fmt.Errorf("claim agent run: %w", err)
	}
	return run, true, nil
}

func (s *AgentStore) RenewRunLease(
	ctx context.Context,
	runID uuid.UUID,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
) error {
	if leaseDuration <= 0 {
		leaseDuration = pactagent.DefaultLeaseDuration
	}
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_runs
		SET lease_expires_at=$4, updated_at=$3
		WHERE id=$1 AND status='running' AND lease_owner=$2
		  AND lease_expires_at > $3`,
		runID, workerID, now.UTC(), now.UTC().Add(leaseDuration),
	)
	if err != nil {
		return fmt.Errorf("renew agent run lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentRunLeaseLost
	}
	return nil
}

func (s *AgentStore) AddContextMessages(
	ctx context.Context,
	runID uuid.UUID,
	workerID string,
	count int,
	now time.Time,
) (int, error) {
	if count < 0 {
		return 0, pactagent.ErrContextLimit
	}
	var used int
	err := s.db.Pool.QueryRow(ctx, `
		UPDATE agent_runs
		SET context_messages_used=context_messages_used+$3, updated_at=$4
		WHERE id=$1 AND status='running' AND lease_owner=$2
		  AND context_messages_used+$3 <= $5
		RETURNING context_messages_used`,
		runID, workerID, count, now.UTC(), pactagent.MaxContextMessages,
	).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, pactagent.ErrContextLimit
	}
	if err != nil {
		return 0, fmt.Errorf("record Agent context usage: %w", err)
	}
	return used, nil
}

func (s *AgentStore) SaveCheckpoint(
	ctx context.Context,
	checkpoint pactagent.Checkpoint,
) error {
	if checkpoint.RunID == uuid.Nil || checkpoint.FormatVersion <= 0 ||
		checkpoint.EinoVersion == "" || checkpoint.Model == "" ||
		checkpoint.EncryptionKeyID == "" || len(checkpoint.Ciphertext) == 0 ||
		checkpoint.UpdatedAt.IsZero() {
		return pactagent.ErrInvalidRun
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO agent_run_checkpoints (
			run_id, format_version, eino_version, model, encryption_key_id,
			ciphertext, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (run_id) DO UPDATE
		SET format_version=excluded.format_version,
		    eino_version=excluded.eino_version,
		    model=excluded.model,
		    encryption_key_id=excluded.encryption_key_id,
		    ciphertext=excluded.ciphertext,
		    updated_at=excluded.updated_at`,
		checkpoint.RunID, checkpoint.FormatVersion, checkpoint.EinoVersion,
		checkpoint.Model, checkpoint.EncryptionKeyID, checkpoint.Ciphertext,
		checkpoint.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("save agent checkpoint: %w", err)
	}
	return nil
}

func (s *AgentStore) GetCheckpoint(
	ctx context.Context,
	runID uuid.UUID,
) (pactagent.Checkpoint, error) {
	var checkpoint pactagent.Checkpoint
	err := s.db.Pool.QueryRow(ctx, `
		SELECT run_id, format_version, eino_version, model, encryption_key_id,
		       ciphertext, updated_at
		FROM agent_run_checkpoints
		WHERE run_id=$1`,
		runID,
	).Scan(
		&checkpoint.RunID, &checkpoint.FormatVersion, &checkpoint.EinoVersion,
		&checkpoint.Model, &checkpoint.EncryptionKeyID, &checkpoint.Ciphertext,
		&checkpoint.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pactagent.Checkpoint{}, pactagent.ErrAgentCheckpointNotFound
	}
	if err != nil {
		return pactagent.Checkpoint{}, fmt.Errorf("load agent checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (s *AgentStore) DeleteCheckpoint(ctx context.Context, runID uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM agent_run_checkpoints WHERE run_id=$1`, runID)
	if err != nil {
		return fmt.Errorf("delete agent checkpoint: %w", err)
	}
	return nil
}

func (s *AgentStore) ClaimToolCall(
	ctx context.Context,
	call pactagent.ToolCall,
) (pactagent.ToolCallClaim, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		INSERT INTO agent_tool_calls (
			run_id, tool_call_id, tool_name, argument_hash, argument_summary,
			state, started_at
		) VALUES ($1,$2,$3,$4,$5,'running',$6)
		ON CONFLICT DO NOTHING`,
		call.RunID, call.ToolCallID, call.ToolName, call.ArgumentHash,
		json.RawMessage(call.ArgumentSummary), call.StartedAt.UTC(),
	)
	if err != nil {
		return pactagent.ToolCallClaim{}, fmt.Errorf("insert agent tool call: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimAcquired}, nil
	}

	var argumentHash, result []byte
	var toolName, state string
	err = s.db.Pool.QueryRow(ctx, `
		SELECT tool_name, argument_hash, state, result
		FROM agent_tool_calls
		WHERE run_id=$1 AND tool_call_id=$2`,
		call.RunID, call.ToolCallID,
	).Scan(&toolName, &argumentHash, &state, &result)
	if err != nil {
		return pactagent.ToolCallClaim{}, fmt.Errorf("load agent tool call: %w", err)
	}
	if toolName != call.ToolName || !bytes.Equal(argumentHash, call.ArgumentHash) {
		return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimConflict}, nil
	}
	switch pactagent.ToolCallState(state) {
	case pactagent.ToolCallCompleted:
		return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimReplay, Result: result}, nil
	case pactagent.ToolCallRunning:
		return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimRunning}, nil
	default:
		return pactagent.ToolCallClaim{Kind: pactagent.ToolCallClaimConflict}, nil
	}
}

func (s *AgentStore) CompleteToolCall(
	ctx context.Context,
	runID uuid.UUID,
	toolCallID string,
	result []byte,
	now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_tool_calls
		SET state='completed', result=$3, completed_at=$4
		WHERE run_id=$1 AND tool_call_id=$2 AND state='running'`,
		runID, toolCallID, json.RawMessage(result), now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("complete agent tool call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrToolCallProtocol
	}
	return nil
}

func (s *AgentStore) GetCompletedToolCall(
	ctx context.Context,
	runID uuid.UUID,
	toolCallID string,
) (pactagent.ToolCall, error) {
	var call pactagent.ToolCall
	var completedAt time.Time
	err := s.db.Pool.QueryRow(ctx, `
		SELECT tool_name, result, started_at, completed_at
		FROM agent_tool_calls
		WHERE run_id=$1 AND tool_call_id=$2 AND state='completed'`,
		runID, toolCallID,
	).Scan(&call.ToolName, &call.Result, &call.StartedAt, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return pactagent.ToolCall{}, pactagent.ErrToolEvidenceNotFound
	}
	if err != nil {
		return pactagent.ToolCall{}, fmt.Errorf("load completed Agent tool call: %w", err)
	}
	call.RunID = runID
	call.ToolCallID = toolCallID
	call.State = pactagent.ToolCallCompleted
	call.CompletedAt = &completedAt
	return call, nil
}

func (s *AgentStore) FailToolCall(
	ctx context.Context,
	runID uuid.UUID,
	toolCallID, category string,
	now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_tool_calls
		SET state='failed', error_category=$3, completed_at=$4
		WHERE run_id=$1 AND tool_call_id=$2 AND state='running'`,
		runID, toolCallID, category, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("fail agent tool call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrToolCallProtocol
	}
	return nil
}

func (s *AgentStore) AttachTask(
	ctx context.Context,
	runID uuid.UUID,
	workerID string,
	taskID uuid.UUID,
	taskNumber int64,
	now time.Time,
) (existingTaskID uuid.UUID, existingTaskNumber int64, attached bool, err error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE agent_runs
		SET created_task_id=$3, created_task_number=$4, updated_at=$5
		WHERE id=$1 AND status='running' AND lease_owner=$2
		  AND created_task_id IS NULL
		RETURNING created_task_id, created_task_number`,
		runID, workerID, taskID, taskNumber, now.UTC(),
	)
	err = row.Scan(&existingTaskID, &existingTaskNumber)
	if err == nil {
		return existingTaskID, existingTaskNumber, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, false, fmt.Errorf("attach task to agent run: %w", err)
	}
	err = s.db.Pool.QueryRow(ctx, `
		SELECT created_task_id, created_task_number
		FROM agent_runs
		WHERE id=$1 AND created_task_id IS NOT NULL`,
		runID,
	).Scan(&existingTaskID, &existingTaskNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, false, pactagent.ErrAgentRunLeaseLost
	}
	if err != nil {
		return uuid.Nil, 0, false, fmt.Errorf("load attached agent task: %w", err)
	}
	return existingTaskID, existingTaskNumber, false, nil
}

func (s *AgentStore) MarkRunWaiting(
	ctx context.Context,
	runID uuid.UUID,
	workerID string,
	interruptID string,
	outbox pactagent.OutboxMessage,
	now time.Time,
) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent clarification: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	run, err := scanAgentRun(tx.QueryRow(ctx, agentRunSelect+` WHERE id=$1 FOR UPDATE`, runID))
	if err != nil {
		return err
	}
	if run.LeaseOwner != workerID {
		return pactagent.ErrAgentRunLeaseLost
	}
	pendingMessageID := "pending:" + outbox.ID.String()
	if err := run.WaitForUser(pendingMessageID, interruptID, now); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status=$2, clarification_rounds=$3,
		    clarification_message_id=$4, clarification_interrupt_id=$5,
		    clarification_expires_at=$6,
		    lease_owner=NULL, lease_expires_at=NULL, updated_at=$7
		WHERE id=$1 AND status='running' AND lease_owner=$8`,
		run.ID, run.Status, run.ClarificationRounds, run.ClarificationMessageID,
		run.ClarificationInterruptID, run.ClarificationExpiresAt,
		run.UpdatedAt, workerID,
	)
	if err != nil {
		return fmt.Errorf("mark agent run waiting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentRunLeaseLost
	}
	if err := insertOutbox(ctx, tx, outbox); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent clarification: %w", err)
	}
	return nil
}

func (s *AgentStore) ResumeWaitingRun(
	ctx context.Context,
	runID, userID uuid.UUID,
	clarificationMessageID string,
	now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_runs
		SET status='queued', available_at=$4,
		    clarification_message_id=NULL,
		    clarification_expires_at=NULL,
		    updated_at=$4
		WHERE id=$1 AND status='waiting_user'
		  AND initiating_user_id=$2
		  AND clarification_message_id=$3
		  AND clarification_expires_at > $4`,
		runID, userID, clarificationMessageID, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("resume waiting agent run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		run, loadErr := s.GetRun(ctx, runID)
		if loadErr != nil {
			return loadErr
		}
		switch {
		case run.Status != pactagent.RunWaitingUser:
			return pactagent.ErrRunNotWaiting
		case run.InitiatingUserID != userID:
			return pactagent.ErrClarificationUserMismatch
		case run.ClarificationExpiresAt == nil || !now.UTC().Before(*run.ClarificationExpiresAt):
			return pactagent.ErrClarificationExpired
		default:
			return pactagent.ErrInvalidTransition
		}
	}
	return nil
}

func (s *AgentStore) ResumeWaitingRunWithInput(
	ctx context.Context,
	runID, userID uuid.UUID,
	clarificationMessageID string,
	ciphertext []byte,
	now time.Time,
) error {
	if len(ciphertext) == 0 {
		return pactagent.ErrInvalidRun
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin resume waiting Agent run: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status='queued', available_at=$4,
		    clarification_message_id=NULL,
		    clarification_expires_at=NULL,
		    updated_at=$4
		WHERE id=$1 AND status='waiting_user'
		  AND initiating_user_id=$2
		  AND clarification_message_id=$3
		  AND clarification_expires_at > $4`,
		runID, userID, clarificationMessageID, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("resume waiting Agent run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrInvalidTransition
	}
	tag, err = tx.Exec(ctx, `
		UPDATE agent_run_inputs
		SET pending_resume_ciphertext=$2, updated_at=$3
		WHERE run_id=$1`,
		runID, ciphertext, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("save Agent clarification input: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentRunNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit resume waiting Agent run: %w", err)
	}
	return nil
}

func (s *AgentStore) FinishRun(
	ctx context.Context,
	runID uuid.UUID,
	workerID string,
	status pactagent.RunStatus,
	errorCategory, errorDetail string,
	outbox *pactagent.OutboxMessage,
	now time.Time,
) error {
	if status != pactagent.RunSucceeded && status != pactagent.RunFailed && status != pactagent.RunCancelled {
		return pactagent.ErrInvalidTransition
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin finish agent run: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status=$3, terminal_error_category=$4, terminal_error_detail=$5,
		    lease_owner=NULL, lease_expires_at=NULL,
		    clarification_message_id=NULL, clarification_interrupt_id=NULL,
		    clarification_expires_at=NULL,
		    completed_at=$6, updated_at=$6
		WHERE id=$1 AND status='running' AND lease_owner=$2`,
		runID, workerID, status, nullIfEmpty(errorCategory), nullIfEmpty(errorDetail),
		now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("finish agent run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentRunLeaseLost
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_run_checkpoints WHERE run_id=$1`, runID); err != nil {
		return fmt.Errorf("delete terminal agent checkpoint: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_run_inputs WHERE run_id=$1`, runID); err != nil {
		return fmt.Errorf("delete terminal Agent input: %w", err)
	}
	if outbox != nil {
		if err := insertOutbox(ctx, tx, *outbox); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit finish agent run: %w", err)
	}
	return nil
}

func (s *AgentStore) RetryRun(
	ctx context.Context,
	runID uuid.UUID,
	workerID string,
	availableAt, now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_runs
		SET status='queued', available_at=$3,
		    lease_owner=NULL, lease_expires_at=NULL, updated_at=$4
		WHERE id=$1 AND status='running' AND lease_owner=$2`,
		runID, workerID, availableAt.UTC(), now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("retry agent run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentRunLeaseLost
	}
	return nil
}

func (s *AgentStore) EnqueueOutbox(
	ctx context.Context,
	message pactagent.OutboxMessage,
) error {
	return insertOutbox(ctx, s.db.Pool, message)
}

func (s *AgentStore) ClaimOutbox(
	ctx context.Context,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
) (pactagent.OutboxMessage, bool, error) {
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	row := s.db.Pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM agent_message_outbox
			WHERE (
				(state IN ('pending','failed') AND available_at <= $1)
				OR
				(state='delivering' AND lease_expires_at <= $1)
			)
			ORDER BY available_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE agent_message_outbox AS messages
		SET state='delivering', lease_owner=$2, lease_expires_at=$3,
		    attempt_count=messages.attempt_count+1, updated_at=$1
		FROM candidate
		WHERE messages.id=candidate.id
		RETURNING `+outboxColumns,
		now.UTC(), workerID, now.UTC().Add(leaseDuration),
	)
	message, err := scanOutbox(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return pactagent.OutboxMessage{}, false, nil
	}
	if err != nil {
		return pactagent.OutboxMessage{}, false, fmt.Errorf("claim agent outbox: %w", err)
	}
	return message, true, nil
}

func (s *AgentStore) MarkOutboxDelivered(
	ctx context.Context,
	id uuid.UUID,
	workerID, providerMessageID string,
	now time.Time,
) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deliver agent outbox: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var runID uuid.UUID
	var kind pactagent.OutboxKind
	tag, err := tx.Exec(ctx, `
		UPDATE agent_message_outbox
		SET state='delivered', provider_message_id=$3, delivered_at=$4,
		    lease_owner=NULL, lease_expires_at=NULL, updated_at=$4
		WHERE id=$1 AND state='delivering' AND lease_owner=$2`,
		id, workerID, providerMessageID, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("mark agent outbox delivered: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentOutboxDeliveryClaimed
	}
	if err := tx.QueryRow(ctx,
		`SELECT run_id, kind FROM agent_message_outbox WHERE id=$1`, id,
	).Scan(&runID, &kind); err != nil {
		return fmt.Errorf("load delivered agent outbox: %w", err)
	}
	if kind == pactagent.OutboxClarification {
		if _, err := tx.Exec(ctx, `
			UPDATE agent_runs
			SET clarification_message_id=$2, updated_at=$3
			WHERE id=$1 AND status='waiting_user'
			  AND clarification_message_id=$4`,
			runID, providerMessageID, now.UTC(), "pending:"+id.String(),
		); err != nil {
			return fmt.Errorf("bind agent clarification reply: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent outbox delivery: %w", err)
	}
	return nil
}

func (s *AgentStore) MarkOutboxFailed(
	ctx context.Context,
	id uuid.UUID,
	workerID, providerErrorCode string,
	availableAt, now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE agent_message_outbox
		SET state='failed', provider_error_code=$3, available_at=$4,
		    lease_owner=NULL, lease_expires_at=NULL, updated_at=$5
		WHERE id=$1 AND state='delivering' AND lease_owner=$2`,
		id, workerID, nullIfEmpty(providerErrorCode), availableAt.UTC(), now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("mark agent outbox failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pactagent.ErrAgentOutboxDeliveryClaimed
	}
	return nil
}

func (s *AgentStore) ListExpiredClarifications(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]pactagent.Run, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Pool.Query(ctx, agentRunSelect+`
		WHERE runs.status='waiting_user'
		  AND runs.clarification_expires_at <= $1
		ORDER BY runs.clarification_expires_at, runs.id
		LIMIT $2`,
		now.UTC(), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list expired Agent clarifications: %w", err)
	}
	defer rows.Close()
	runs := make([]pactagent.Run, 0)
	for rows.Next() {
		run, scanErr := scanAgentRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired Agent clarifications: %w", err)
	}
	return runs, nil
}

func (s *AgentStore) CancelExpiredClarification(
	ctx context.Context,
	runID uuid.UUID,
	outbox pactagent.OutboxMessage,
	now time.Time,
) (bool, error) {
	if outbox.RunID != runID || outbox.Kind != pactagent.OutboxExpired {
		return false, pactagent.ErrInvalidRun
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin expiring Agent clarification: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status='cancelled',
		    terminal_error_category='clarification_expired',
		    terminal_error_detail='clarification expired',
		    clarification_message_id=NULL,
		    clarification_interrupt_id=NULL,
		    clarification_expires_at=NULL,
		    completed_at=$2,
		    updated_at=$2
		WHERE id=$1
		  AND status='waiting_user'
		  AND clarification_expires_at <= $2`,
		runID, now.UTC(),
	)
	if err != nil {
		return false, fmt.Errorf("cancel expired Agent clarification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM agent_run_checkpoints WHERE run_id=$1`, runID,
	); err != nil {
		return false, fmt.Errorf("delete expired Agent checkpoint: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM agent_run_inputs WHERE run_id=$1`, runID,
	); err != nil {
		return false, fmt.Errorf("delete expired Agent input: %w", err)
	}
	if err := insertOutbox(ctx, tx, outbox); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit expiring Agent clarification: %w", err)
	}
	return true, nil
}

func (s *AgentStore) findRunByProviderIdentity(
	ctx context.Context,
	provider, tenantID, eventID, messageID string,
) (pactagent.Run, error) {
	row := s.db.Pool.QueryRow(ctx, agentRunSelect+`
		WHERE provider=$1 AND tenant_id=$2
		  AND (provider_event_id=$3 OR trigger_message_id=$4)
		ORDER BY created_at
		LIMIT 1`,
		provider, tenantID, eventID, messageID,
	)
	return scanAgentRun(row)
}

const agentRunColumns = `
	runs.id, runs.provider, runs.tenant_id, runs.conversation_id,
	runs.trigger_message_id, runs.provider_event_id,
	runs.thread_root_message_id, runs.reply_parent_message_id,
	runs.conversation_revision_id,
	runs.trigger_occurred_at, runs.initiating_user_id, runs.initiating_subject_id, runs.status,
	runs.command_kind, runs.model, runs.prompt_version, runs.attempt_count,
	runs.clarification_rounds, runs.clarification_message_id,
	runs.clarification_interrupt_id, runs.clarification_expires_at,
	runs.context_messages_used,
	runs.lease_owner, runs.lease_expires_at, runs.available_at,
	runs.created_task_id, runs.created_task_number,
	runs.terminal_error_category, runs.terminal_error_detail,
	runs.created_at, runs.updated_at, runs.completed_at`

const agentRunSelect = `SELECT ` + agentRunColumns + ` FROM agent_runs AS runs`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgentRun(row rowScanner) (pactagent.Run, error) {
	var run pactagent.Run
	var (
		threadRoot, replyParent, clarificationMessage, clarificationInterrupt *string
		leaseOwner, terminalCategory, terminalDetail                          *string
	)
	err := row.Scan(
		&run.ID, &run.Provider, &run.TenantID, &run.ConversationID,
		&run.TriggerMessageID, &run.ProviderEventID, &threadRoot, &replyParent,
		&run.ConversationRevisionID,
		&run.TriggerOccurredAt, &run.InitiatingUserID, &run.InitiatingSubjectID, &run.Status,
		&run.CommandKind, &run.Model, &run.PromptVersion, &run.AttemptCount,
		&run.ClarificationRounds, &clarificationMessage,
		&clarificationInterrupt, &run.ClarificationExpiresAt, &run.ContextMessagesUsed,
		&leaseOwner, &run.LeaseExpiresAt, &run.AvailableAt,
		&run.CreatedTaskID, &run.CreatedTaskNumber,
		&terminalCategory, &terminalDetail,
		&run.CreatedAt, &run.UpdatedAt, &run.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pactagent.Run{}, pactagent.ErrAgentRunNotFound
	}
	if err != nil {
		return pactagent.Run{}, fmt.Errorf("scan agent run: %w", err)
	}
	run.ThreadRootMessageID = stringValue(threadRoot)
	run.ReplyParentMessageID = stringValue(replyParent)
	run.ClarificationMessageID = stringValue(clarificationMessage)
	run.ClarificationInterruptID = stringValue(clarificationInterrupt)
	run.LeaseOwner = stringValue(leaseOwner)
	run.TerminalErrorCategory = stringValue(terminalCategory)
	run.TerminalErrorDetail = stringValue(terminalDetail)
	return run, nil
}

func insertOutbox(
	ctx context.Context,
	executor interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	message pactagent.OutboxMessage,
) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO agent_message_outbox (
			id, run_id, deduplication_key, kind, target_message_id, body, state,
			attempt_count, available_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (deduplication_key) DO NOTHING`,
		message.ID, message.RunID, message.DeduplicationKey, message.Kind,
		message.TargetMessageID, message.Body, message.State,
		message.AttemptCount, message.AvailableAt.UTC(),
		message.CreatedAt.UTC(), message.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert agent outbox: %w", err)
	}
	return nil
}

const outboxColumns = `
	messages.id, messages.run_id, messages.deduplication_key, messages.kind,
	messages.target_message_id, messages.body, messages.state,
	messages.attempt_count, messages.available_at, messages.lease_owner,
	messages.lease_expires_at, messages.provider_message_id,
	messages.provider_error_code, messages.created_at, messages.updated_at,
	messages.delivered_at`

func scanOutbox(row rowScanner) (pactagent.OutboxMessage, error) {
	var message pactagent.OutboxMessage
	var leaseOwner, providerMessageID, providerErrorCode *string
	err := row.Scan(
		&message.ID, &message.RunID, &message.DeduplicationKey, &message.Kind,
		&message.TargetMessageID, &message.Body, &message.State,
		&message.AttemptCount, &message.AvailableAt, &leaseOwner,
		&message.LeaseExpiresAt, &providerMessageID, &providerErrorCode,
		&message.CreatedAt, &message.UpdatedAt, &message.DeliveredAt,
	)
	if err != nil {
		return pactagent.OutboxMessage{}, err
	}
	message.LeaseOwner = stringValue(leaseOwner)
	message.ProviderMessageID = stringValue(providerMessageID)
	message.ProviderErrorCode = stringValue(providerErrorCode)
	return message, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
