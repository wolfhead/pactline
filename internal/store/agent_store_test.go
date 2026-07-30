package store_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAgentStoreDurableRunToolCheckpointClarificationAndOutbox(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAgentStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	run := newStoredAgentRun(t, now)
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM agent_runs WHERE id=$1`, run.ID)
		require.NoError(t, err)
	})

	stored, created, err := repository.CreateRun(ctx, run)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, run.ID, stored.ID)
	duplicate := run
	duplicate.ID = uuid.New()
	stored, created, err = repository.CreateRun(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, run.ID, stored.ID)

	require.NoError(t, repository.SaveRunInput(ctx, pactagent.RunInput{
		RunID: run.ID, EncryptionKeyID: "input-key",
		CommandCiphertext: []byte("encrypted-command"),
	}, now))
	claimed, ok, err := repository.ClaimRun(ctx, "worker-1", now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, pactagent.RunRunning, claimed.Status)
	require.Equal(t, 1, claimed.AttemptCount)
	require.NoError(t, repository.RenewRunLease(
		ctx, run.ID, "worker-1", now.Add(10*time.Second), time.Minute,
	))

	require.NoError(t, repository.SaveCheckpoint(ctx, pactagent.Checkpoint{
		RunID: run.ID, FormatVersion: 1, EinoVersion: "v0.9.13",
		Model: "deepseek-v4-pro", EncryptionKeyID: "checkpoint-key",
		Ciphertext: []byte("ciphertext-1"), UpdatedAt: now,
	}))
	require.NoError(t, repository.SaveCheckpoint(ctx, pactagent.Checkpoint{
		RunID: run.ID, FormatVersion: 1, EinoVersion: "v0.9.13",
		Model: "deepseek-v4-pro", EncryptionKeyID: "checkpoint-key",
		Ciphertext: []byte("ciphertext-2"), UpdatedAt: now.Add(time.Second),
	}))
	checkpoint, err := repository.GetCheckpoint(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("ciphertext-2"), checkpoint.Ciphertext)

	argumentsHash := sha256.Sum256([]byte(`{"query":"Pactline"}`))
	call := pactagent.ToolCall{
		RunID: run.ID, ToolCallID: "call-1", ToolName: "search_projects",
		ArgumentHash: argumentsHash[:], ArgumentSummary: []byte(`{"bytes":20}`),
		State: pactagent.ToolCallRunning, StartedAt: now,
	}
	claim, err := repository.ClaimToolCall(ctx, call)
	require.NoError(t, err)
	require.Equal(t, pactagent.ToolCallClaimAcquired, claim.Kind)
	require.NoError(t, repository.CompleteToolCall(
		ctx, run.ID, call.ToolCallID, []byte(`{"candidates":[]}`), now.Add(time.Second),
	))
	claim, err = repository.ClaimToolCall(ctx, call)
	require.NoError(t, err)
	require.Equal(t, pactagent.ToolCallClaimReplay, claim.Kind)
	require.JSONEq(t, `{"candidates":[]}`, string(claim.Result))

	conflicting := call
	differentHash := sha256.Sum256([]byte(`{"query":"Other"}`))
	conflicting.ArgumentHash = differentHash[:]
	claim, err = repository.ClaimToolCall(ctx, conflicting)
	require.NoError(t, err)
	require.Equal(t, pactagent.ToolCallClaimConflict, claim.Kind)

	outboxID := uuid.New()
	clarification := pactagent.OutboxMessage{
		ID: outboxID, RunID: run.ID,
		DeduplicationKey: "run:" + run.ID.String() + ":clarification:1",
		Kind:             pactagent.OutboxClarification, TargetMessageID: run.TriggerMessageID,
		Body: "clarification", State: pactagent.OutboxPending,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repository.MarkRunWaiting(
		ctx, run.ID, "worker-1", "interrupt-1", clarification, now.Add(2*time.Second),
	))
	waiting, err := repository.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, pactagent.RunWaitingUser, waiting.Status)
	require.Equal(t, "interrupt-1", waiting.ClarificationInterruptID)
	require.Equal(t, "pending:"+outboxID.String(), waiting.ClarificationMessageID)

	message, ok, err := repository.ClaimOutbox(
		ctx, "outbox-1", now.Add(2*time.Second), time.Minute,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, outboxID, message.ID)
	require.NoError(t, repository.MarkOutboxDelivered(
		ctx, outboxID, "outbox-1", "provider-clarification-1", now.Add(3*time.Second),
	))
	waiting, err = repository.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "provider-clarification-1", waiting.ClarificationMessageID)

	require.Error(t, repository.ResumeWaitingRunWithInput(
		ctx, run.ID, uuid.New(), waiting.ClarificationMessageID,
		[]byte("encrypted-answer"), now.Add(4*time.Second),
	))
	unchanged, err := repository.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, pactagent.RunWaitingUser, unchanged.Status)

	require.NoError(t, repository.ResumeWaitingRunWithInput(
		ctx, run.ID, run.InitiatingUserID, waiting.ClarificationMessageID,
		[]byte("encrypted-answer"), now.Add(4*time.Second),
	))
	resumed, err := repository.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, pactagent.RunQueued, resumed.Status)
	require.Equal(t, "interrupt-1", resumed.ClarificationInterruptID)
	input, err := repository.GetRunInput(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("encrypted-answer"), input.PendingResumeCiphertext)
}

func TestAgentStoreCreatesRunAndEncryptedInputAtomically(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAgentStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	run := newStoredAgentRun(t, now)
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM agent_runs WHERE id=$1`, run.ID)
		require.NoError(t, err)
	})
	input := pactagent.RunInput{
		RunID: run.ID, EncryptionKeyID: "input-key",
		CommandCiphertext: []byte("encrypted-command"),
	}

	stored, created, err := repository.CreateRunWithInput(ctx, run, input, now)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, run.ID, stored.ID)
	persisted, err := repository.GetRunInput(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, input.CommandCiphertext, persisted.CommandCiphertext)

	duplicate := run
	duplicate.ID = uuid.New()
	duplicateInput := pactagent.RunInput{
		RunID: duplicate.ID, EncryptionKeyID: "different-key",
		CommandCiphertext: []byte("different-command"),
	}
	stored, created, err = repository.CreateRunWithInput(
		ctx,
		duplicate,
		duplicateInput,
		now.Add(time.Minute),
	)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, run.ID, stored.ID)
	persisted, err = repository.GetRunInput(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, input.CommandCiphertext, persisted.CommandCiphertext)
	require.Equal(t, input.EncryptionKeyID, persisted.EncryptionKeyID)
}

func TestAgentStoreClaimsOnlyOneWorkerAndTerminalDeletesSensitiveState(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAgentStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)
	run := newStoredAgentRun(t, now)
	run.ProviderEventID += "-terminal"
	run.TriggerMessageID += "-terminal"
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM agent_runs WHERE id=$1`, run.ID)
		require.NoError(t, err)
	})
	_, _, err := repository.CreateRun(ctx, run)
	require.NoError(t, err)
	require.NoError(t, repository.SaveRunInput(ctx, pactagent.RunInput{
		RunID: run.ID, EncryptionKeyID: "input-key",
		CommandCiphertext: []byte("encrypted-command"),
	}, now))
	_, claimed, err := repository.ClaimRun(ctx, "worker-a", now, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.ErrorIs(t, repository.RenewRunLease(
		ctx, run.ID, "worker-b", now.Add(time.Second), time.Minute,
	), pactagent.ErrAgentRunLeaseLost)

	require.NoError(t, repository.SaveCheckpoint(ctx, pactagent.Checkpoint{
		RunID: run.ID, FormatVersion: 1, EinoVersion: "v0.9.13",
		Model: "deepseek-v4-pro", EncryptionKeyID: "checkpoint-key",
		Ciphertext: []byte("checkpoint"), UpdatedAt: now,
	}))
	require.NoError(t, repository.FinishRun(
		ctx, run.ID, "worker-a", pactagent.RunFailed,
		"test_failure", "test_failure", nil, now.Add(time.Second),
	))
	terminal, err := repository.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, pactagent.RunFailed, terminal.Status)
	_, err = repository.GetCheckpoint(ctx, run.ID)
	require.ErrorIs(t, err, pactagent.ErrAgentCheckpointNotFound)
	_, err = repository.GetRunInput(ctx, run.ID)
	require.ErrorIs(t, err, pactagent.ErrAgentRunNotFound)
}

func TestAgentStoreExpiresClarificationAndQueuesReplyAtomically(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAgentStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	run := newStoredAgentRun(t, now)
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM agent_runs WHERE id=$1`, run.ID)
		require.NoError(t, err)
	})
	_, _, err := repository.CreateRun(ctx, run)
	require.NoError(t, err)
	require.NoError(t, repository.SaveRunInput(ctx, pactagent.RunInput{
		RunID: run.ID, EncryptionKeyID: "input-key",
		CommandCiphertext: []byte("encrypted-command"),
	}, now))
	_, claimed, err := repository.ClaimRun(ctx, "worker-expiry", now, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	clarification := pactagent.OutboxMessage{
		ID: uuid.New(), RunID: run.ID, DeduplicationKey: "expiry-clarification-" + run.ID.String(),
		Kind: pactagent.OutboxClarification, TargetMessageID: run.TriggerMessageID,
		Body: "clarification", State: pactagent.OutboxPending,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repository.MarkRunWaiting(
		ctx, run.ID, "worker-expiry", "interrupt-expiry", clarification, now,
	))
	expiresAt := now.Add(pactagent.ClarificationLifetime)
	expired, err := repository.ListExpiredClarifications(ctx, expiresAt, 10)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, run.ID, expired[0].ID)

	expiryMessage := pactagent.OutboxMessage{
		ID: uuid.New(), RunID: run.ID, DeduplicationKey: "expired-" + run.ID.String(),
		Kind: pactagent.OutboxExpired, TargetMessageID: run.TriggerMessageID,
		Body: "expired", State: pactagent.OutboxPending,
		AvailableAt: expiresAt, CreatedAt: expiresAt, UpdatedAt: expiresAt,
	}
	cancelled, err := repository.CancelExpiredClarification(
		ctx, run.ID, expiryMessage, expiresAt,
	)
	require.NoError(t, err)
	require.True(t, cancelled)
	cancelled, err = repository.CancelExpiredClarification(
		ctx, run.ID, expiryMessage, expiresAt,
	)
	require.NoError(t, err)
	require.False(t, cancelled)

	stored, err := repository.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, pactagent.RunCancelled, stored.Status)
	require.Equal(t, "clarification_expired", stored.TerminalErrorCategory)
	_, err = repository.GetRunInput(ctx, run.ID)
	require.ErrorIs(t, err, pactagent.ErrAgentRunNotFound)
	var count int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_message_outbox
		WHERE run_id=$1 AND kind='expired'`,
		run.ID,
	).Scan(&count))
	require.Equal(t, 1, count)
}

func newStoredAgentRun(t *testing.T, now time.Time) pactagent.Run {
	t.Helper()
	run, err := pactagent.NewRun(pactagent.NewRunInput{
		Provider:            "lark",
		TenantID:            "tenant-agent-store",
		ConversationID:      "conversation-agent-store",
		TriggerMessageID:    "message-" + uuid.NewString(),
		ProviderEventID:     "event-" + uuid.NewString(),
		TriggerOccurredAt:   now,
		InitiatingUserID:    userA,
		InitiatingSubjectID: "ou_agent_store",
		CommandKind:         pactagent.CommandDirect,
		Model:               "deepseek-v4-pro",
		PromptVersion:       "first-party-task-v1",
	}, now)
	require.NoError(t, err)
	return run
}
