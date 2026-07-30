package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/domain"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	AgentDelegateIssuer   = "pactline-agent"
	AgentDelegateAudience = "pactline-openapi"
	AgentDelegatePrefix   = "pact_agent_"
	AgentDelegateLifetime = 5 * time.Minute
)

var (
	ErrAgentDelegateInvalid = errors.New("agent delegation credential is invalid")
	ErrAgentDelegateExpired = errors.New("agent delegation credential expired")
	ErrAgentDelegateRun     = errors.New("agent delegation run is invalid")
)

type AgentRunReader interface {
	GetRunForDelegate(context.Context, uuid.UUID, uuid.UUID) (pactagent.Run, error)
}

type DelegationUserReader interface {
	GetByID(context.Context, uuid.UUID) (domain.User, error)
}

type DelegateConfig struct {
	ActiveKeyID string
	SigningKeys map[string][]byte
	Lifetime    time.Duration
}

type DelegateService struct {
	activeKeyID string
	signingKeys map[string][]byte
	lifetime    time.Duration
	runs        AgentRunReader
	users       DelegationUserReader
	clock       Clock
}

type delegateClaims struct {
	RunID  string  `json:"run_id"`
	Scopes []Scope `json:"scopes"`
	jwt.RegisteredClaims
}

func NewDelegateService(
	config DelegateConfig,
	runs AgentRunReader,
	users DelegationUserReader,
	clock Clock,
) (*DelegateService, error) {
	activeKeyID := strings.TrimSpace(config.ActiveKeyID)
	if activeKeyID == "" || runs == nil || users == nil || clock == nil {
		return nil, ErrAgentDelegateInvalid
	}
	keys := make(map[string][]byte, len(config.SigningKeys))
	for keyID, key := range config.SigningKeys {
		if strings.TrimSpace(keyID) == "" || len(key) < 32 {
			return nil, ErrAgentDelegateInvalid
		}
		keys[keyID] = append([]byte(nil), key...)
	}
	if _, ok := keys[activeKeyID]; !ok {
		return nil, ErrAgentDelegateInvalid
	}
	lifetime := config.Lifetime
	if lifetime <= 0 {
		lifetime = AgentDelegateLifetime
	}
	if lifetime > AgentDelegateLifetime {
		return nil, ErrAgentDelegateInvalid
	}
	return &DelegateService{
		activeKeyID: activeKeyID,
		signingKeys: keys,
		lifetime:    lifetime,
		runs:        runs,
		users:       users,
		clock:       clock,
	}, nil
}

func (s *DelegateService) Issue(
	ctx context.Context,
	runID, userID uuid.UUID,
) (string, error) {
	run, err := s.runs.GetRunForDelegate(ctx, runID, userID)
	if err != nil || run.Status != pactagent.RunRunning {
		return "", ErrAgentDelegateRun
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("load agent delegation user: %w", err)
	}
	if !user.Active {
		return "", ErrUserInactive
	}
	now := s.clock.Now().UTC()
	claims := delegateClaims{
		RunID:  runID.String(),
		Scopes: []Scope{ScopeWorkRead, ScopeWorkWrite},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    AgentDelegateIssuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{AgentDelegateAudience},
			ExpiresAt: jwt.NewNumericDate(now.Add(s.lifetime)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = s.activeKeyID
	signed, err := token.SignedString(s.signingKeys[s.activeKeyID])
	if err != nil {
		return "", fmt.Errorf("sign agent delegation credential: %w", err)
	}
	return AgentDelegatePrefix + signed, nil
}

func (s *DelegateService) Authenticate(
	ctx context.Context,
	raw string,
) (Principal, error) {
	encoded, ok := strings.CutPrefix(raw, AgentDelegatePrefix)
	if !ok || encoded == "" {
		return Principal{}, ErrAgentDelegateInvalid
	}
	claims := &delegateClaims{}
	token, err := jwt.ParseWithClaims(
		encoded,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrAgentDelegateInvalid
			}
			keyID, ok := token.Header["kid"].(string)
			if !ok || keyID == "" {
				return nil, ErrAgentDelegateInvalid
			}
			key, ok := s.signingKeys[keyID]
			if !ok {
				return nil, ErrAgentDelegateInvalid
			}
			return key, nil
		},
		jwt.WithAudience(AgentDelegateAudience),
		jwt.WithIssuer(AgentDelegateIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return s.clock.Now().UTC() }),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Principal{}, ErrAgentDelegateExpired
		}
		return Principal{}, ErrAgentDelegateInvalid
	}
	if !token.Valid || strings.TrimSpace(claims.ID) == "" {
		return Principal{}, ErrAgentDelegateInvalid
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return Principal{}, ErrAgentDelegateInvalid
	}
	runID, err := uuid.Parse(claims.RunID)
	if err != nil || runID == uuid.Nil {
		return Principal{}, ErrAgentDelegateInvalid
	}
	scopes, err := normalizeDelegateScopes(claims.Scopes)
	if err != nil {
		return Principal{}, ErrAgentDelegateInvalid
	}
	run, err := s.runs.GetRunForDelegate(ctx, runID, userID)
	if err != nil || run.Status != pactagent.RunRunning {
		return Principal{}, ErrAgentDelegateRun
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return Principal{}, fmt.Errorf("load agent delegation user: %w", err)
	}
	if !user.Active {
		return Principal{}, ErrUserInactive
	}
	return Principal{
		User:       user,
		Method:     AuthenticationMethodAgentDelegate,
		AgentRunID: &runID,
		Scopes:     scopes,
	}, nil
}

func normalizeDelegateScopes(scopes []Scope) ([]Scope, error) {
	if len(scopes) != 2 {
		return nil, ErrAgentDelegateInvalid
	}
	read, write := false, false
	for _, scope := range scopes {
		switch scope {
		case ScopeWorkRead:
			read = true
		case ScopeWorkWrite:
			write = true
		default:
			return nil, ErrAgentDelegateInvalid
		}
	}
	if !read || !write {
		return nil, ErrAgentDelegateInvalid
	}
	return []Scope{ScopeWorkRead, ScopeWorkWrite}, nil
}

type BearerService struct {
	Tokens    *Service
	Delegates *DelegateService
}

func (s BearerService) Authenticate(ctx context.Context, raw string) (Principal, error) {
	if strings.HasPrefix(raw, AgentDelegatePrefix) {
		if s.Delegates == nil {
			return Principal{}, ErrAgentDelegateInvalid
		}
		return s.Delegates.Authenticate(ctx, raw)
	}
	if s.Tokens == nil {
		return Principal{}, ErrTokenInvalid
	}
	return s.Tokens.Authenticate(ctx, raw)
}
