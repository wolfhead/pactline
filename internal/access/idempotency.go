package access

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const IdempotencyRetention = 24 * time.Hour

type ClaimKind string

const (
	ClaimAcquired   ClaimKind = "acquired"
	ClaimReplay     ClaimKind = "replay"
	ClaimInProgress ClaimKind = "in_progress"
	ClaimReused     ClaimKind = "reused"
)

var ErrIdempotencyNotClaimed = errors.New("idempotency key is not claimed")

type IdempotencyKey struct {
	UserID         uuid.UUID
	CredentialKind AuthenticationMethod
	CredentialID   uuid.UUID
	TokenID        *uuid.UUID
	AgentRunID     *uuid.UUID
	Method         string
	RoutePattern   string
	Value          string
}

type StoredResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type Claim struct {
	Kind     ClaimKind
	Response StoredResponse
}
