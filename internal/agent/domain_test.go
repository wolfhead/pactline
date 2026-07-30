package agent

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRunLifecycleCreatesAtMostOneTask(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	run := testRun(t, now)

	require.NoError(t, run.Claim("worker-1", now, time.Minute))
	firstTaskID := uuid.New()
	require.NoError(t, run.AttachTask(firstTaskID, 12, now.Add(time.Second)))
	require.NoError(t, run.AttachTask(firstTaskID, 12, now.Add(2*time.Second)))
	require.ErrorIs(t, run.AttachTask(uuid.New(), 13, now.Add(3*time.Second)), ErrTaskAlreadyCreated)
	require.NoError(t, run.Succeed(now.Add(4*time.Second)))
	require.Equal(t, RunSucceeded, run.Status)
	require.True(t, run.IsTerminal())
	require.ErrorIs(t, run.Retry(now.Add(time.Minute), now.Add(5*time.Second)), ErrInvalidTransition)
	require.NoError(t, run.Validate())
}

func TestRunClarificationOnlyOriginalUserCanResume(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	run := testRun(t, now)
	require.NoError(t, run.Claim("worker-1", now, time.Minute))
	require.NoError(t, run.WaitForUser("clarification-1", "interrupt-1", now))

	require.ErrorIs(t, run.Resume(uuid.New(), now.Add(time.Minute)), ErrClarificationUserMismatch)
	require.Equal(t, RunWaitingUser, run.Status)
	require.NoError(t, run.Resume(run.InitiatingUserID, now.Add(time.Minute)))
	require.Equal(t, RunQueued, run.Status)
	require.NoError(t, run.Validate())
}

func TestRunClarificationLimitAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	run := testRun(t, now)
	current := now

	for round := 1; round <= MaxClarificationRounds; round++ {
		require.NoError(t, run.Claim("worker-1", current, time.Minute))
		require.NoError(t, run.WaitForUser(
			"clarification-"+string(rune('0'+round)),
			"interrupt-"+string(rune('0'+round)),
			current,
		))
		if round < MaxClarificationRounds {
			current = current.Add(time.Minute)
			require.NoError(t, run.Resume(run.InitiatingUserID, current))
		}
	}
	current = current.Add(time.Minute)
	require.NoError(t, run.Resume(run.InitiatingUserID, current))
	require.NoError(t, run.Claim("worker-1", current, time.Minute))
	require.ErrorIs(t, run.WaitForUser("clarification-4", "interrupt-4", current), ErrClarificationLimit)

	expiring := testRun(t, now)
	require.NoError(t, expiring.Claim("worker-2", now, time.Minute))
	require.NoError(t, expiring.WaitForUser("clarification-expiring", "interrupt-expiring", now))
	require.ErrorIs(
		t,
		expiring.Resume(expiring.InitiatingUserID, now.Add(ClarificationLifetime)),
		ErrClarificationExpired,
	)
}

func TestRunContextLimit(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	run := testRun(t, now)
	require.NoError(t, run.AddContextMessages(MaxContextMessages, now))
	require.ErrorIs(t, run.AddContextMessages(1, now), ErrContextLimit)
}

func testRun(t *testing.T, now time.Time) Run {
	t.Helper()
	run, err := NewRun(NewRunInput{
		Provider:            "lark",
		TenantID:            "tenant",
		ConversationID:      "conversation",
		TriggerMessageID:    "message",
		ProviderEventID:     "event",
		TriggerOccurredAt:   now,
		InitiatingUserID:    uuid.New(),
		InitiatingSubjectID: "open-id",
		CommandKind:         CommandDirect,
		Model:               "deepseek-v4-pro",
		PromptVersion:       "v1",
	}, now)
	require.NoError(t, err)
	return run
}
