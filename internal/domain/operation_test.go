package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOperationActorValidatesAgentDelegationShape(t *testing.T) {
	userID, runID := uuid.New(), uuid.New()
	actor := OperationActor{
		UserID: userID, AuthMethod: AuthenticationMethodAgentDelegate,
		AgentRunID: &runID, RequestID: "request-1",
	}
	require.NoError(t, actor.Validate())

	tokenID := uuid.New()
	actor.TokenID = &tokenID
	require.ErrorIs(t, actor.Validate(), ErrInvalidOperationActor)
}
