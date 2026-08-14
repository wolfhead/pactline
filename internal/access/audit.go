package access

import (
	"time"

	"github.com/google/uuid"
)

const AuthenticationMethodUnknown AuthenticationMethod = "unknown"

type AuthOutcome string

const (
	AuthOutcomeAuthenticated AuthOutcome = "authenticated"
	AuthOutcomeRejected      AuthOutcome = "rejected"
)

type RequestAuditEvent struct {
	ID                  uuid.UUID            `json:"id"`
	OccurredAt          time.Time            `json:"occurred_at"`
	RequestID           string               `json:"request_id"`
	AuthMethod          AuthenticationMethod `json:"auth_method"`
	AuthOutcome         AuthOutcome          `json:"auth_outcome"`
	UserID              *uuid.UUID           `json:"user_id,omitempty"`
	TokenID             *uuid.UUID           `json:"token_id,omitempty"`
	TokenName           string               `json:"token_name,omitempty"`
	AgentRunID          *uuid.UUID           `json:"agent_run_id,omitempty"`
	ClientKind          string               `json:"client_kind,omitempty"`
	ClientSessionID     string               `json:"client_session_id,omitempty"`
	Method              string               `json:"method"`
	RoutePattern        string               `json:"route_pattern"`
	StatusCode          int                  `json:"status_code"`
	ProblemCode         string               `json:"problem_code,omitempty"`
	DurationMS          int64                `json:"duration_ms"`
	ResponseBytes       int64                `json:"response_bytes"`
	IdempotencyReplayed bool                 `json:"idempotency_replayed"`
	UserAgent           string               `json:"user_agent"`
	NetworkAddress      string               `json:"network_address,omitempty"`
}

type RequestAuditCursor struct {
	OccurredAt time.Time
	ID         uuid.UUID
}

type RequestAuditFilter struct {
	UserID        *uuid.UUID
	TokenID       *uuid.UUID
	AgentRunID    *uuid.UUID
	Method        string
	RoutePattern  string
	StatusCode    *int
	RequestID     string
	ImportantOnly bool
	From          *time.Time
	To            *time.Time
	Before        *RequestAuditCursor
	Limit         int
}
