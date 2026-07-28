package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

type testSecrets struct {
	value []byte
	err   error
}

func (s testSecrets) Bytes(size int) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.value[:size]...), nil
}

type testRepository struct {
	created     Token
	bundle      TokenWithUser
	createErr   error
	getErr      error
	touchErr    error
	touchCalls  int
	touchedAt   time.Time
	touchBefore time.Time
	revokedID   uuid.UUID
	revokedBy   uuid.UUID
	revokedAt   time.Time
	listed      []Token
	listErr     error
	revokeErr   error
}

func (r *testRepository) CreateToken(_ context.Context, token Token) error {
	r.created = token
	return r.createErr
}

func (r *testRepository) GetToken(_ context.Context, _ uuid.UUID) (TokenWithUser, error) {
	return r.bundle, r.getErr
}

func (r *testRepository) ListUserTokens(_ context.Context, _ uuid.UUID) ([]Token, error) {
	return r.listed, r.listErr
}

func (r *testRepository) RevokeToken(_ context.Context, tokenID, actorID uuid.UUID, now time.Time) error {
	r.revokedID = tokenID
	r.revokedBy = actorID
	r.revokedAt = now
	return r.revokeErr
}

func (r *testRepository) TouchToken(_ context.Context, _ uuid.UUID, now, before time.Time) error {
	r.touchCalls++
	r.touchedAt = now
	r.touchBefore = before
	return r.touchErr
}

func TestIssueTokenHashesSecretAndNormalizesScopes(t *testing.T) {
	now := time.Date(2026, 7, 28, 2, 3, 4, 0, time.UTC)
	userID := uuid.New()
	repository := &testRepository{}
	service := NewService(repository, testClock{now: now}, testSecrets{value: make([]byte, SecretSize)})

	issued, err := service.Issue(context.Background(), IssueRequest{
		UserID:   userID,
		Name:     " Nightly agent ",
		Scopes:   []Scope{ScopeWorkWrite, ScopeWorkWrite},
		Lifetime: 90 * 24 * time.Hour,
	})

	require.NoError(t, err)
	require.Contains(t, issued.Value, "bb_pat_")
	require.NotContains(t, issued.Value, string(repository.created.SecretHash))
	require.Len(t, repository.created.SecretHash, 32)
	require.Equal(t, "Nightly agent", repository.created.Name)
	require.Equal(t, []Scope{ScopeWorkRead, ScopeWorkWrite}, repository.created.Scopes)
	require.Equal(t, now.Add(90*24*time.Hour), repository.created.ExpiresAt)
	require.Equal(t, repository.created.Metadata(), issued.Token)
}

func TestIssueTokenRejectsUnsupportedLifetimeAndScope(t *testing.T) {
	service := NewService(&testRepository{}, testClock{}, testSecrets{value: make([]byte, SecretSize)})

	_, err := service.Issue(context.Background(), IssueRequest{
		UserID: uuid.New(), Name: "agent", Scopes: []Scope{ScopeWorkRead}, Lifetime: time.Hour,
	})
	require.ErrorIs(t, err, ErrInvalidLifetime)

	_, err = service.Issue(context.Background(), IssueRequest{
		UserID: uuid.New(), Name: "agent", Scopes: []Scope{"admin"}, Lifetime: 30 * 24 * time.Hour,
	})
	require.ErrorIs(t, err, ErrInvalidScope)
}

func TestAuthenticateTokenReturnsPrincipalAndThrottlesTouch(t *testing.T) {
	now := time.Date(2026, 7, 28, 2, 3, 4, 0, time.UTC)
	owner := domain.User{ID: uuid.New(), Name: "Owner", Active: true}
	repository := &testRepository{}
	issuer := NewService(repository, testClock{now: now}, testSecrets{value: make([]byte, SecretSize)})
	issued, err := issuer.Issue(context.Background(), IssueRequest{
		UserID: owner.ID, Name: "agent", Scopes: []Scope{ScopeWorkWrite}, Lifetime: 30 * 24 * time.Hour,
	})
	require.NoError(t, err)
	repository.bundle = TokenWithUser{Token: repository.created, User: owner}

	principal, err := issuer.Authenticate(context.Background(), issued.Value)

	require.NoError(t, err)
	require.Equal(t, owner, principal.User)
	require.Equal(t, AuthenticationMethodAPIToken, principal.Method)
	require.Equal(t, repository.created.ID, *principal.TokenID)
	require.Equal(t, "agent", principal.TokenName)
	require.True(t, principal.HasScope(ScopeWorkRead))
	require.True(t, principal.HasScope(ScopeWorkWrite))
	require.Equal(t, 1, repository.touchCalls)
	require.Equal(t, now, repository.touchedAt)
	require.Equal(t, now.Add(-LastUsedTouchInterval), repository.touchBefore)
}

func TestAuthenticateTokenRejectsSpecificInactiveStates(t *testing.T) {
	now := time.Date(2026, 7, 28, 2, 3, 4, 0, time.UTC)
	owner := domain.User{ID: uuid.New(), Active: true}
	baseToken := Token{
		ID: uuid.New(), UserID: owner.ID, Name: "agent",
		SecretHash: HashSecret(make([]byte, SecretSize)),
		Scopes:     []Scope{ScopeWorkRead}, ExpiresAt: now.Add(time.Hour),
	}
	raw := FormatToken(baseToken.ID, make([]byte, SecretSize))

	tests := []struct {
		name  string
		token Token
		user  domain.User
		want  error
	}{
		{name: "expired", token: func() Token { v := baseToken; v.ExpiresAt = now; return v }(), user: owner, want: ErrTokenExpired},
		{name: "revoked", token: func() Token { v := baseToken; at := now.Add(-time.Minute); v.RevokedAt = &at; return v }(), user: owner, want: ErrTokenRevoked},
		{name: "inactive owner", token: baseToken, user: func() domain.User { v := owner; v.Active = false; return v }(), want: ErrUserInactive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &testRepository{bundle: TokenWithUser{Token: tt.token, User: tt.user}}
			service := NewService(repository, testClock{now: now}, testSecrets{})
			_, err := service.Authenticate(context.Background(), raw)
			require.ErrorIs(t, err, tt.want)
			require.Zero(t, repository.touchCalls)
		})
	}
}

func TestAuthenticateTokenRejectsMalformedUnknownAndWrongSecrets(t *testing.T) {
	now := time.Date(2026, 7, 28, 2, 3, 4, 0, time.UTC)
	token := Token{ID: uuid.New(), SecretHash: HashSecret(make([]byte, SecretSize)), ExpiresAt: now.Add(time.Hour)}

	for _, test := range []struct {
		name       string
		raw        string
		repository *testRepository
	}{
		{name: "malformed", raw: "not-a-token", repository: &testRepository{}},
		{name: "unknown", raw: FormatToken(token.ID, make([]byte, SecretSize)), repository: &testRepository{getErr: ErrTokenNotFound}},
		{name: "wrong secret", raw: FormatToken(token.ID, make([]byte, SecretSize)), repository: &testRepository{
			bundle: TokenWithUser{Token: func() Token {
				v := token
				v.SecretHash = HashSecret([]byte("01234567890123456789012345678901"))
				return v
			}(), User: domain.User{Active: true}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(test.repository, testClock{now: now}, testSecrets{})
			_, err := service.Authenticate(context.Background(), test.raw)
			require.ErrorIs(t, err, ErrTokenInvalid)
		})
	}
}

func TestIssueTokenPropagatesSecretGenerationFailure(t *testing.T) {
	service := NewService(&testRepository{}, testClock{}, testSecrets{err: errors.New("entropy unavailable")})
	_, err := service.Issue(context.Background(), IssueRequest{
		UserID: uuid.New(), Name: "agent", Scopes: []Scope{ScopeWorkRead}, Lifetime: 30 * 24 * time.Hour,
	})
	require.ErrorContains(t, err, "generate token secret")
}
