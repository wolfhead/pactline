package larkaudit

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEventValidationRejectsUnsafeIncompleteDescriptors(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	status := http.StatusOK
	event := Event{
		ID: uuid.New(), OccurredAt: now,
		Operation: "send_notification", Category: "notification",
		Method: http.MethodPost, RoutePattern: "/open-apis/im/v1/messages",
		CredentialKind: string(CredentialTenant), Outcome: OutcomeSucceeded,
		HTTPStatus: &status,
	}
	require.NoError(t, event.Validate())

	event.RoutePattern = ""
	require.Error(t, event.Validate())
}

func TestCorrelationUpdatesPreserveExistingValues(t *testing.T) {
	actorID, runID, eventID := uuid.New(), uuid.New(), uuid.New()
	ctx := WithCorrelation(context.Background(), Correlation{
		RequestID: "request-1", ActorUserID: &actorID,
	})
	ctx = WithCorrelation(ctx, Correlation{
		AgentRunID: &runID, ApplicationEventID: &eventID,
	})

	correlation := CorrelationFromContext(ctx)
	require.Equal(t, "request-1", correlation.RequestID)
	require.Equal(t, actorID, *correlation.ActorUserID)
	require.Equal(t, runID, *correlation.AgentRunID)
	require.Equal(t, eventID, *correlation.ApplicationEventID)
}
