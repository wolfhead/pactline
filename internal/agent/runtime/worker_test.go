package runtime

import (
	"errors"
	"testing"

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
