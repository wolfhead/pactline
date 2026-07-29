package access

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const tokenPrefix = "bb_pat_"

var allowedLifetimes = map[time.Duration]struct{}{
	30 * 24 * time.Hour:  {},
	90 * 24 * time.Hour:  {},
	365 * 24 * time.Hour: {},
}

type Service struct {
	repository Repository
	clock      Clock
	secrets    SecretGenerator
}

func NewService(repository Repository, clock Clock, secrets SecretGenerator) *Service {
	return &Service{repository: repository, clock: clock, secrets: secrets}
}

func (s *Service) Issue(ctx context.Context, request IssueRequest) (IssuedToken, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" || len(name) > 100 {
		return IssuedToken{}, ErrInvalidName
	}
	if _, ok := allowedLifetimes[request.Lifetime]; !ok {
		return IssuedToken{}, ErrInvalidLifetime
	}
	scopes, err := normalizeScopes(request.Scopes)
	if err != nil {
		return IssuedToken{}, err
	}

	secret, err := s.secrets.Bytes(SecretSize)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("generate token secret: %w", err)
	}
	if len(secret) != SecretSize {
		return IssuedToken{}, fmt.Errorf("generate token secret: expected %d bytes, received %d", SecretSize, len(secret))
	}

	now := s.clock.Now().UTC()
	tokenID := uuid.New()
	raw := FormatToken(tokenID, secret)
	token := Token{
		ID: tokenID, UserID: request.UserID, Name: name,
		SecretHash: HashSecret(secret), DisplayPrefix: displayPrefix(tokenID),
		Scopes: scopes, ExpiresAt: now.Add(request.Lifetime), CreatedAt: now,
	}
	if err := s.repository.CreateToken(ctx, token); err != nil {
		return IssuedToken{}, fmt.Errorf("create API token: %w", err)
	}
	return IssuedToken{Token: token.Metadata(), Value: raw}, nil
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Principal, error) {
	tokenID, secret, err := ParseToken(raw)
	if err != nil {
		return Principal{}, ErrTokenInvalid
	}
	bundle, err := s.repository.GetToken(ctx, tokenID)
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			return Principal{}, ErrTokenInvalid
		}
		return Principal{}, fmt.Errorf("load API token: %w", err)
	}
	if len(bundle.Token.SecretHash) != 32 ||
		subtle.ConstantTimeCompare(HashSecret(secret), bundle.Token.SecretHash) != 1 {
		return Principal{}, ErrTokenInvalid
	}

	now := s.clock.Now().UTC()
	if bundle.Token.RevokedAt != nil {
		return Principal{}, ErrTokenRevoked
	}
	if !now.Before(bundle.Token.ExpiresAt) {
		return Principal{}, ErrTokenExpired
	}
	if !bundle.User.Active {
		return Principal{}, ErrUserInactive
	}
	if err := s.repository.TouchToken(ctx, bundle.Token.ID, now, now.Add(-LastUsedTouchInterval)); err != nil {
		return Principal{}, fmt.Errorf("touch API token: %w", err)
	}
	tokenIDCopy := bundle.Token.ID
	return Principal{
		User: bundle.User, Method: AuthenticationMethodAPIToken,
		TokenID: &tokenIDCopy, TokenName: bundle.Token.Name,
		Scopes: append([]Scope(nil), bundle.Token.Scopes...),
	}, nil
}

func (s *Service) ListUserTokens(ctx context.Context, userID uuid.UUID) ([]TokenMetadata, error) {
	tokens, err := s.repository.ListUserTokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	out := make([]TokenMetadata, len(tokens))
	for i, token := range tokens {
		out[i] = token.Metadata()
	}
	return out, nil
}

func (s *Service) Revoke(ctx context.Context, tokenID, actorID uuid.UUID) error {
	if err := s.repository.RevokeToken(ctx, tokenID, actorID, s.clock.Now().UTC()); err != nil {
		return fmt.Errorf("revoke API token: %w", err)
	}
	return nil
}

func (s *Service) ListAllTokens(ctx context.Context) ([]TokenWithUser, error) {
	tokens, err := s.repository.ListAllTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all API tokens: %w", err)
	}
	return tokens, nil
}

func (s *Service) RevokeAsAdmin(ctx context.Context, tokenID, adminID uuid.UUID) error {
	if err := s.repository.RevokeTokenAsAdmin(
		ctx, tokenID, adminID, s.clock.Now().UTC(),
	); err != nil {
		return fmt.Errorf("revoke API token as administrator: %w", err)
	}
	return nil
}

func FormatToken(id uuid.UUID, secret []byte) string {
	return tokenPrefix +
		base64.RawURLEncoding.EncodeToString(id[:]) + "_" +
		base64.RawURLEncoding.EncodeToString(secret)
}

func ParseToken(raw string) (uuid.UUID, []byte, error) {
	encoded, ok := strings.CutPrefix(raw, tokenPrefix)
	if !ok {
		return uuid.Nil, nil, ErrTokenInvalid
	}
	encodedIDLength := base64.RawURLEncoding.EncodedLen(16)
	encodedSecretLength := base64.RawURLEncoding.EncodedLen(SecretSize)
	if len(encoded) != encodedIDLength+1+encodedSecretLength ||
		encoded[encodedIDLength] != '_' {
		return uuid.Nil, nil, ErrTokenInvalid
	}
	encodedID := encoded[:encodedIDLength]
	encodedSecret := encoded[encodedIDLength+1:]
	idBytes, err := base64.RawURLEncoding.DecodeString(encodedID)
	if err != nil || len(idBytes) != 16 {
		return uuid.Nil, nil, ErrTokenInvalid
	}
	id, err := uuid.FromBytes(idBytes)
	if err != nil {
		return uuid.Nil, nil, ErrTokenInvalid
	}
	secret, err := base64.RawURLEncoding.DecodeString(encodedSecret)
	if err != nil || len(secret) != SecretSize {
		return uuid.Nil, nil, ErrTokenInvalid
	}
	return id, secret, nil
}

func normalizeScopes(values []Scope) ([]Scope, error) {
	if len(values) == 0 {
		return nil, ErrInvalidScope
	}
	present := make(map[Scope]bool, len(values)+1)
	for _, scope := range values {
		switch scope {
		case ScopeWorkRead, ScopeWorkWrite:
			present[scope] = true
		default:
			return nil, ErrInvalidScope
		}
	}
	if present[ScopeWorkWrite] {
		present[ScopeWorkRead] = true
	}
	scopes := make([]Scope, 0, len(present))
	for scope := range present {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	return scopes, nil
}

func displayPrefix(id uuid.UUID) string {
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(id[:])[:8]
}

type CryptoSecretGenerator struct{}

func (CryptoSecretGenerator) Bytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return nil, err
	}
	return value, nil
}
