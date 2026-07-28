package store_test

import (
	"context"
	"testing"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccessStoreCreatesLoadsListsTouchesAndRevokesToken(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAccessStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 3, 4, 5, 0, time.UTC)
	token := access.Token{
		ID: uuid.New(), UserID: userA, Name: "Store test",
		SecretHash:    access.HashSecret([]byte("01234567890123456789012345678901")),
		DisplayPrefix: "bb_pat_fixture", Scopes: []access.Scope{access.ScopeWorkRead},
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
	}
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, err)
	})

	require.NoError(t, repository.CreateToken(ctx, token))

	bundle, err := repository.GetToken(ctx, token.ID)
	require.NoError(t, err)
	require.Equal(t, token.ID, bundle.Token.ID)
	require.Equal(t, token.SecretHash, bundle.Token.SecretHash)
	require.Equal(t, []access.Scope{access.ScopeWorkRead}, bundle.Token.Scopes)
	require.Equal(t, userA, bundle.User.ID)
	require.True(t, bundle.User.Active)

	list, err := repository.ListUserTokens(ctx, userA)
	require.NoError(t, err)
	require.Contains(t, tokenIDs(list), token.ID)

	firstUse := now.Add(time.Hour)
	require.NoError(t, repository.TouchToken(ctx, token.ID, firstUse, firstUse.Add(-access.LastUsedTouchInterval)))
	require.NoError(t, repository.TouchToken(ctx, token.ID, firstUse.Add(time.Minute), firstUse.Add(-4*time.Minute)))
	bundle, err = repository.GetToken(ctx, token.ID)
	require.NoError(t, err)
	require.NotNil(t, bundle.Token.LastUsedAt)
	require.WithinDuration(t, firstUse, *bundle.Token.LastUsedAt, time.Microsecond,
		"a touch inside the five-minute interval must not write again")

	revokedAt := firstUse.Add(2 * time.Minute)
	require.NoError(t, repository.RevokeToken(ctx, token.ID, userA, revokedAt))
	bundle, err = repository.GetToken(ctx, token.ID)
	require.NoError(t, err)
	require.WithinDuration(t, revokedAt, *bundle.Token.RevokedAt, time.Microsecond)
	require.Equal(t, userA, *bundle.Token.RevokedByUserID)
}

func TestAccessStoreRejectsUnknownAndCrossUserRevocation(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAccessStore(db)
	ctx := context.Background()

	_, err := repository.GetToken(ctx, uuid.New())
	require.ErrorIs(t, err, access.ErrTokenNotFound)

	now := time.Now().UTC()
	token := access.Token{
		ID: uuid.New(), UserID: userA, Name: "Ownership test",
		SecretHash:    access.HashSecret([]byte("abcdefghijklmnopqrstuvwxyzABCDEF")),
		DisplayPrefix: "bb_pat_fixture", Scopes: []access.Scope{access.ScopeWorkRead},
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
	}
	require.NoError(t, repository.CreateToken(ctx, token))
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, cleanupErr)
	})

	err = repository.RevokeToken(ctx, token.ID, userB, now.Add(time.Minute))
	require.ErrorIs(t, err, access.ErrTokenNotFound)
}

func tokenIDs(tokens []access.Token) []uuid.UUID {
	ids := make([]uuid.UUID, len(tokens))
	for i, token := range tokens {
		ids[i] = token.ID
	}
	return ids
}
