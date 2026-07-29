// Package access owns personal API token policy and request authentication
// provenance. It is independent of HTTP and persistence concerns.
package access

import (
	"crypto/sha256"
	"errors"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
)

type Scope string

const (
	ScopeWorkRead  Scope = "work:read"
	ScopeWorkWrite Scope = "work:write"
)

type AuthenticationMethod = domain.AuthenticationMethod

const (
	AuthenticationMethodSession  = domain.AuthenticationMethodSession
	AuthenticationMethodAPIToken = domain.AuthenticationMethodAPIToken
)

const (
	SecretSize            = 32
	LastUsedTouchInterval = 5 * time.Minute
)

var (
	ErrInvalidName     = errors.New("token name is invalid")
	ErrInvalidScope    = errors.New("token scope is invalid")
	ErrInvalidLifetime = errors.New("token lifetime is invalid")
	ErrTokenInvalid    = errors.New("API token is invalid")
	ErrTokenNotFound   = errors.New("API token not found")
	ErrTokenExpired    = errors.New("API token expired")
	ErrTokenRevoked    = errors.New("API token revoked")
	ErrUserInactive    = errors.New("token owner is inactive")
)

type Token struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Name            string
	SecretHash      []byte
	DisplayPrefix   string
	Scopes          []Scope
	ExpiresAt       time.Time
	LastUsedAt      *time.Time
	RevokedAt       *time.Time
	RevokedByUserID *uuid.UUID
	CreatedAt       time.Time
}

type TokenMetadata struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"user_id"`
	Name            string     `json:"name"`
	DisplayPrefix   string     `json:"display_prefix"`
	Scopes          []Scope    `json:"scopes"`
	ExpiresAt       time.Time  `json:"expires_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
	RevokedByUserID *uuid.UUID `json:"revoked_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (t Token) Metadata() TokenMetadata {
	return TokenMetadata{
		ID: t.ID, UserID: t.UserID, Name: t.Name, DisplayPrefix: t.DisplayPrefix,
		Scopes: append([]Scope(nil), t.Scopes...), ExpiresAt: t.ExpiresAt,
		LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt,
		RevokedByUserID: t.RevokedByUserID, CreatedAt: t.CreatedAt,
	}
}

type TokenWithUser struct {
	Token Token
	User  domain.User
}

type IssuedToken struct {
	Token TokenMetadata `json:"token"`
	Value string        `json:"value"`
}

type Principal struct {
	User      domain.User
	Method    AuthenticationMethod
	TokenID   *uuid.UUID
	TokenName string
	Scopes    []Scope
}

func (p Principal) HasScope(required Scope) bool {
	for _, scope := range p.Scopes {
		if scope == required || (required == ScopeWorkRead && scope == ScopeWorkWrite) {
			return true
		}
	}
	return false
}

type IssueRequest struct {
	UserID   uuid.UUID
	Name     string
	Scopes   []Scope
	Lifetime time.Duration
}

func HashSecret(secret []byte) []byte {
	digest := sha256.Sum256(secret)
	return digest[:]
}
