package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func createTaskClaimToken(
	t *testing.T,
	db *store.DB,
	userID uuid.UUID,
	now time.Time,
) access.Token {
	t.Helper()
	token := access.Token{
		ID: uuid.New(), UserID: userID, Name: "Codex worker",
		SecretHash:    access.HashSecret([]byte(uuid.NewString())),
		DisplayPrefix: "bb_pat_claim", Scopes: []access.Scope{access.ScopeWorkExecute},
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
	}
	require.NoError(t, store.NewAccessStore(db).CreateToken(context.Background(), token))
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := db.Pool.Exec(ctx, `DELETE FROM business_audit_events WHERE token_id=$1`, token.ID)
		require.NoError(t, err)
		_, err = db.Pool.Exec(ctx, `DELETE FROM api_tokens WHERE id=$1`, token.ID)
		require.NoError(t, err)
	})
	return token
}

func taskClaimActor(
	userID uuid.UUID,
	token access.Token,
	requestID string,
) domain.OperationActor {
	tokenID := token.ID
	return domain.OperationActor{
		UserID: userID, AuthMethod: domain.AuthenticationMethodAPIToken,
		TokenID: &tokenID, TokenName: token.Name, RequestID: requestID,
	}
}
