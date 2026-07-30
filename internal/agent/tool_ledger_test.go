package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type ledgerRepositoryStub struct {
	claim       ToolCallClaim
	claimed     ToolCall
	completed   []byte
	failed      string
	claimErr    error
	completeErr error
}

func (s *ledgerRepositoryStub) ClaimToolCall(_ context.Context, call ToolCall) (ToolCallClaim, error) {
	s.claimed = call
	return s.claim, s.claimErr
}

func (s *ledgerRepositoryStub) CompleteToolCall(
	_ context.Context, _ uuid.UUID, _ string, result []byte, _ time.Time,
) error {
	s.completed = append([]byte(nil), result...)
	return s.completeErr
}

func (s *ledgerRepositoryStub) FailToolCall(
	_ context.Context, _ uuid.UUID, _ string, category string, _ time.Time,
) error {
	s.failed = category
	return nil
}

func TestToolLedgerRecordsAndReplaysJSONResult(t *testing.T) {
	runID := uuid.New()
	repository := &ledgerRepositoryStub{
		claim: ToolCallClaim{Kind: ToolCallClaimAcquired},
	}
	endpoint := ToolLedger{RunID: runID, Repository: repository}.Middleware().Invokable(
		func(_ context.Context, _ *compose.ToolInput) (*compose.ToolOutput, error) {
			return &compose.ToolOutput{Result: `{"ok":true}`}, nil
		},
	)
	output, err := endpoint(context.Background(), &compose.ToolInput{
		CallID: "call-1", Name: "search_projects", Arguments: `{"query":"Pactline"}`,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true,"evidence_id":"call-1"}`, output.Result)
	require.JSONEq(t, `{"ok":true,"evidence_id":"call-1"}`, string(repository.completed))
	require.Equal(t, runID, repository.claimed.RunID)

	repository.claim = ToolCallClaim{
		Kind: ToolCallClaimReplay, Result: []byte(`{"ok":true}`),
	}
	called := false
	endpoint = ToolLedger{RunID: runID, Repository: repository}.Middleware().Invokable(
		func(_ context.Context, _ *compose.ToolInput) (*compose.ToolOutput, error) {
			called = true
			return nil, nil
		},
	)
	output, err = endpoint(context.Background(), &compose.ToolInput{
		CallID: "call-1", Name: "search_projects", Arguments: `{"query":"Pactline"}`,
	})
	require.NoError(t, err)
	require.False(t, called)
	require.JSONEq(t, `{"ok":true,"evidence_id":"call-1"}`, output.Result)
}

func TestToolLedgerReexecutesIncompleteCallAfterWorkerRecovery(t *testing.T) {
	repository := &ledgerRepositoryStub{
		claim: ToolCallClaim{Kind: ToolCallClaimRunning},
	}
	called := false
	endpoint := ToolLedger{
		RunID: uuid.New(), Repository: repository,
	}.Middleware().Invokable(
		func(_ context.Context, _ *compose.ToolInput) (*compose.ToolOutput, error) {
			called = true
			return &compose.ToolOutput{Result: `{"recovered":true}`}, nil
		},
	)
	output, err := endpoint(context.Background(), &compose.ToolInput{
		CallID: "call-recovery", Name: "create_task", Arguments: `{}`,
	})
	require.NoError(t, err)
	require.True(t, called)
	require.JSONEq(t, `{"recovered":true,"evidence_id":"call-recovery"}`, output.Result)
	require.JSONEq(t, `{"recovered":true,"evidence_id":"call-recovery"}`, string(repository.completed))
}

func TestToolLedgerRejectsProtocolViolations(t *testing.T) {
	require.ErrorIs(t,
		func() error {
			_, err := ValidateToolArguments(context.Background(), "tool", `[]`)
			return err
		}(),
		ErrToolCallProtocol,
	)
	unknown, err := UnknownToolResult(context.Background(), "invented", `{}`)
	require.NoError(t, err)
	require.JSONEq(t, `{"error":"unknown_tool","tool_name":"invented"}`, unknown)

	repository := &ledgerRepositoryStub{
		claim: ToolCallClaim{Kind: ToolCallClaimAcquired},
	}
	endpoint := ToolLedger{RunID: uuid.New(), Repository: repository}.Middleware().Invokable(
		func(_ context.Context, _ *compose.ToolInput) (*compose.ToolOutput, error) {
			return nil, errors.New("boom")
		},
	)
	_, err = endpoint(context.Background(), &compose.ToolInput{
		CallID: "call-2", Name: "create_task", Arguments: `{}`,
	})
	require.EqualError(t, err, "boom")
	require.Equal(t, "execution", repository.failed)
}
