package runtime

import (
	"errors"
	"fmt"
	"testing"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	agenttools "github.com/wolfhead/pactline/internal/agent/tools"

	"github.com/cloudwego/eino/adk"
	"github.com/stretchr/testify/require"
)

func TestDrainEventsFullyConsumesInterruptedIterator(t *testing.T) {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(&adk.AgentEvent{
		Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{
			InterruptContexts: []*adk.InterruptCtx{{
				ID: "interrupt-1",
				Info: map[string]any{
					"question":   "Which single Task?",
					"candidates": []string{"A", "B"},
				},
				IsRootCause: true,
			}},
		}},
	})
	generator.Send(&adk.AgentEvent{Err: errors.New("checkpoint flush failed")})
	generator.Close()

	_, err := drainEvents(iterator)
	require.EqualError(t, err, "checkpoint flush failed")
}

func TestDrainEventsReturnsClarificationAfterIteratorCloses(t *testing.T) {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(&adk.AgentEvent{
		Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{
			InterruptContexts: []*adk.InterruptCtx{{
				ID: "interrupt-1",
				Info: map[string]any{
					"question":   "Which single Task?",
					"candidates": []string{"A", "B"},
				},
				IsRootCause: true,
			}},
		}},
	})
	generator.Close()

	outcome, err := drainEvents(iterator)
	require.NoError(t, err)
	require.Equal(t, outcomeWaiting, outcome.kind)
	require.Equal(t, "interrupt-1", outcome.interruptID)
	require.Equal(t, "Which single Task?", outcome.question)
	require.Equal(t, []string{"A", "B"}, outcome.candidates)
}

func TestDescribeRunFailureUsesSafeActionableReasons(t *testing.T) {
	evidenceFailure := describeRunFailure(fmt.Errorf(
		"%w: %w: provider details stay internal",
		pactagent.ErrToolCallProtocol,
		agenttools.ErrResponseEvidence,
	))
	require.Equal(t, "response_evidence_invalid", evidenceFailure.code)
	require.Equal(
		t,
		"Agent 生成回复时未能正确引用查询结果。",
		evidenceFailure.message,
	)

	summaryFailure := describeRunFailure(fmt.Errorf(
		"%w: %w",
		agenttools.ErrToolInput,
		agenttools.ErrResponseSummary,
	))
	require.Equal(t, "response_summary_missing", summaryFailure.code)

	internalFailure := describeRunFailure(errors.New("database password must not surface"))
	require.Equal(t, "internal_execution_error", internalFailure.code)
	require.NotContains(t, internalFailure.message, "password")
}
