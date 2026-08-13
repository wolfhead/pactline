package larkaudit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Outcome string

const (
	OutcomeSucceeded     Outcome = "succeeded"
	OutcomeRejected      Outcome = "rejected"
	OutcomeRateLimited   Outcome = "rate_limited"
	OutcomeUnavailable   Outcome = "unavailable"
	OutcomeCancelled     Outcome = "cancelled"
	OutcomeContractError Outcome = "contract_error"
)

type CredentialKind string

const (
	CredentialNone   CredentialKind = "none"
	CredentialApp    CredentialKind = "app"
	CredentialTenant CredentialKind = "tenant"
	CredentialUser   CredentialKind = "user"
)

type Call struct {
	Operation      string
	Category       string
	Method         string
	RoutePattern   string
	CredentialKind CredentialKind
}

func (call Call) Validate() error {
	if strings.TrimSpace(call.Operation) == "" || strings.TrimSpace(call.Category) == "" ||
		strings.TrimSpace(call.Method) == "" || strings.TrimSpace(call.RoutePattern) == "" {
		return fmt.Errorf("invalid Lark API call descriptor")
	}
	switch call.CredentialKind {
	case CredentialNone, CredentialApp, CredentialTenant, CredentialUser:
		return nil
	default:
		return fmt.Errorf("invalid Lark API credential kind")
	}
}

type Event struct {
	ID                 uuid.UUID  `json:"id"`
	OccurredAt         time.Time  `json:"occurred_at"`
	Operation          string     `json:"operation"`
	Category           string     `json:"category"`
	Method             string     `json:"method"`
	RoutePattern       string     `json:"route_pattern"`
	CredentialKind     string     `json:"credential_kind"`
	Outcome            Outcome    `json:"outcome"`
	HTTPStatus         *int       `json:"http_status,omitempty"`
	ProviderCode       *int       `json:"provider_code,omitempty"`
	ProviderRequestID  string     `json:"provider_request_id,omitempty"`
	ErrorCategory      string     `json:"error_category,omitempty"`
	DurationMS         int64      `json:"duration_ms"`
	RequestBytes       int64      `json:"request_bytes"`
	ResponseBytes      int64      `json:"response_bytes"`
	RequestID          string     `json:"request_id,omitempty"`
	ActorUserID        *uuid.UUID `json:"actor_user_id,omitempty"`
	SubjectUserID      *uuid.UUID `json:"subject_user_id,omitempty"`
	AgentRunID         *uuid.UUID `json:"agent_run_id,omitempty"`
	ApplicationEventID *uuid.UUID `json:"application_event_id,omitempty"`
}

func (event Event) Validate() error {
	call := Call{
		Operation: event.Operation, Category: event.Category, Method: event.Method,
		RoutePattern: event.RoutePattern, CredentialKind: CredentialKind(event.CredentialKind),
	}
	if event.ID == uuid.Nil || event.OccurredAt.IsZero() || call.Validate() != nil ||
		event.DurationMS < 0 || event.RequestBytes < 0 || event.ResponseBytes < 0 {
		return fmt.Errorf("invalid Lark API audit event")
	}
	switch event.Outcome {
	case OutcomeSucceeded, OutcomeRejected, OutcomeRateLimited, OutcomeUnavailable,
		OutcomeCancelled, OutcomeContractError:
	default:
		return fmt.Errorf("invalid Lark API audit outcome")
	}
	if event.HTTPStatus != nil && (*event.HTTPStatus < 100 || *event.HTTPStatus > 599) {
		return fmt.Errorf("invalid Lark API HTTP status")
	}
	return nil
}

type Cursor struct {
	OccurredAt time.Time
	ID         uuid.UUID
}

type Filter struct {
	Operation          string
	Category           string
	Outcome            Outcome
	HTTPStatus         *int
	ProviderRequestID  string
	RequestID          string
	ActorUserID        *uuid.UUID
	AgentRunID         *uuid.UUID
	ApplicationEventID *uuid.UUID
	From               *time.Time
	To                 *time.Time
	Before             *Cursor
	Limit              int
}

type Correlation struct {
	RequestID          string
	ActorUserID        *uuid.UUID
	SubjectUserID      *uuid.UUID
	AgentRunID         *uuid.UUID
	ApplicationEventID *uuid.UUID
}

type correlationContextKey struct{}

func WithCorrelation(ctx context.Context, update Correlation) context.Context {
	current := CorrelationFromContext(ctx)
	if update.RequestID != "" {
		current.RequestID = update.RequestID
	}
	if update.ActorUserID != nil {
		current.ActorUserID = update.ActorUserID
	}
	if update.SubjectUserID != nil {
		current.SubjectUserID = update.SubjectUserID
	}
	if update.AgentRunID != nil {
		current.AgentRunID = update.AgentRunID
	}
	if update.ApplicationEventID != nil {
		current.ApplicationEventID = update.ApplicationEventID
	}
	return context.WithValue(ctx, correlationContextKey{}, current)
}

func CorrelationFromContext(ctx context.Context) Correlation {
	value, _ := ctx.Value(correlationContextKey{}).(Correlation)
	return value
}

type Writer interface {
	RecordLarkAPIAudit(context.Context, Event) error
}
