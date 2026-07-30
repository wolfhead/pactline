package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AuthenticationMethod string

const (
	AuthenticationMethodSession       AuthenticationMethod = "session"
	AuthenticationMethodAPIToken      AuthenticationMethod = "api_token"
	AuthenticationMethodAgentDelegate AuthenticationMethod = "agent_delegate"
)

var ErrInvalidOperationActor = errors.New("operation actor is invalid")

type OperationActor struct {
	UserID     uuid.UUID
	AuthMethod AuthenticationMethod
	TokenID    *uuid.UUID
	TokenName  string
	AgentRunID *uuid.UUID
	RequestID  string
}

func (a OperationActor) Validate() error {
	if a.UserID == uuid.Nil || strings.TrimSpace(a.RequestID) == "" {
		return ErrInvalidOperationActor
	}
	switch a.AuthMethod {
	case AuthenticationMethodSession:
		if a.TokenID != nil || a.TokenName != "" || a.AgentRunID != nil {
			return ErrInvalidOperationActor
		}
	case AuthenticationMethodAPIToken:
		if a.TokenID == nil || *a.TokenID == uuid.Nil || a.AgentRunID != nil {
			return ErrInvalidOperationActor
		}
	case AuthenticationMethodAgentDelegate:
		if a.TokenID != nil || a.TokenName != "" ||
			a.AgentRunID == nil || *a.AgentRunID == uuid.Nil {
			return ErrInvalidOperationActor
		}
	default:
		return ErrInvalidOperationActor
	}
	return nil
}

func SessionOperation(userID uuid.UUID, requestID string) OperationActor {
	if strings.TrimSpace(requestID) == "" {
		requestID = "internal"
	}
	return OperationActor{
		UserID: userID, AuthMethod: AuthenticationMethodSession, RequestID: requestID,
	}
}

type BusinessAuditEvent struct {
	ID           uuid.UUID
	OccurredAt   time.Time
	Actor        OperationActor
	EntityType   string
	EntityID     uuid.UUID
	EntityNumber *int64
	Action       string
	OldValue     json.RawMessage
	NewValue     json.RawMessage
}
