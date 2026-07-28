package store_test

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestIdempotencyStoreClaimsCompletesReplaysAndRejectsReuse(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 19, 0, 0, 0, time.UTC)
	token := access.Token{
		ID: uuid.New(), UserID: userA, Name: "Idempotency test",
		SecretHash:    access.HashSecret([]byte("idempotency-secret-32-bytes....")),
		DisplayPrefix: "bb_pat_idem", Scopes: []access.Scope{access.ScopeWorkWrite},
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
	}
	require.NoError(t, store.NewAccessStore(db).CreateToken(ctx, token))
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE token_id=$1`, token.ID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, err)
	})
	repository := store.NewIdempotencyStore(db)
	key := access.IdempotencyKey{
		UserID: userA, TokenID: token.ID, Method: "POST",
		RoutePattern: "/api/v1/tasks", Value: "create-task",
	}
	firstHash := sha256.Sum256([]byte("first"))
	secondHash := sha256.Sum256([]byte("second"))

	claim, err := repository.Claim(ctx, key, firstHash[:], now, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, access.ClaimAcquired, claim.Kind)

	claim, err = repository.Claim(ctx, key, firstHash[:], now, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, access.ClaimInProgress, claim.Kind)

	response := access.StoredResponse{
		StatusCode: 201,
		Headers:    map[string][]string{"Content-Type": {"application/json"}, "ETag": {`"1"`}},
		Body:       []byte(`{"id":"task"}`),
	}
	require.NoError(t, repository.Complete(ctx, key, response))
	claim, err = repository.Claim(ctx, key, firstHash[:], now, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, access.ClaimReplay, claim.Kind)
	require.Equal(t, response, claim.Response)

	claim, err = repository.Claim(ctx, key, secondHash[:], now, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, access.ClaimReused, claim.Kind)
}

func TestIdempotencyStoreSerializesConcurrentClaimsAndCanRelease(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 19, 0, 0, 0, time.UTC)
	token := access.Token{
		ID: uuid.New(), UserID: userA, Name: "Concurrent idempotency",
		SecretHash:    access.HashSecret([]byte("concurrent-secret-32-bytes......")),
		DisplayPrefix: "bb_pat_concurrent", Scopes: []access.Scope{access.ScopeWorkWrite},
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
	}
	require.NoError(t, store.NewAccessStore(db).CreateToken(ctx, token))
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE token_id=$1`, token.ID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, err)
	})
	repository := store.NewIdempotencyStore(db)
	key := access.IdempotencyKey{
		UserID: userA, TokenID: token.ID, Method: "POST",
		RoutePattern: "/api/v1/tasks", Value: "concurrent-create",
	}
	hash := sha256.Sum256([]byte("same"))

	results := make(chan access.ClaimKind, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			claim, err := repository.Claim(ctx, key, hash[:], now, now.Add(24*time.Hour))
			require.NoError(t, err)
			results <- claim.Kind
		}()
	}
	group.Wait()
	close(results)
	kinds := map[access.ClaimKind]int{}
	for kind := range results {
		kinds[kind]++
	}
	require.Equal(t, 1, kinds[access.ClaimAcquired])
	require.Equal(t, 1, kinds[access.ClaimInProgress])

	require.NoError(t, repository.Release(ctx, key))
	claim, err := repository.Claim(ctx, key, hash[:], now, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, access.ClaimAcquired, claim.Kind)
}
