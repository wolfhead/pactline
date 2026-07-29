package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type issuedTokenJSON struct {
	ID            uuid.UUID      `json:"id"`
	UserID        uuid.UUID      `json:"user_id"`
	Name          string         `json:"name"`
	DisplayPrefix string         `json:"display_prefix"`
	Scopes        []access.Scope `json:"scopes"`
	Token         string         `json:"token"`
}

type tokenListJSON struct {
	Items []issuedTokenJSON `json:"items"`
}

func TestAccountTokenCreateListAndOwnership(t *testing.T) {
	handler, db := newTaskTestServer(t)
	name := "account-test-" + uuid.NewString()
	created := do(t, handler, http.MethodPost, "/api/account/tokens", userA, map[string]any{
		"name": name, "scopes": []string{"work:write"}, "expires_in_days": 90,
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, created, &issued)
	require.Equal(t, uuid.MustParse(userA), issued.UserID)
	require.True(t, strings.HasPrefix(issued.Token, "bb_pat_"))
	require.NotEmpty(t, issued.DisplayPrefix)
	t.Cleanup(func() { cleanupAPIToken(t, db, issued.ID) })

	listed := do(t, handler, http.MethodGet, "/api/account/tokens", userA, nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var list tokenListJSON
	decodeJSON(t, listed, &list)
	require.Contains(t, tokenJSONIDs(list.Items), issued.ID)
	require.NotContains(t, listed.Body.String(), issued.Token,
		"the complete bearer value must only appear in the create response")

	other := do(t, handler, http.MethodPost, "/api/account/tokens", userB, map[string]any{
		"name":   "other-" + uuid.NewString(),
		"scopes": []string{"work:read"}, "expires_in_days": 30,
	})
	require.Equal(t, http.StatusCreated, other.Code, other.Body.String())
	var otherIssued issuedTokenJSON
	decodeJSON(t, other, &otherIssued)
	t.Cleanup(func() { cleanupAPIToken(t, db, otherIssued.ID) })

	crossUser := do(t, handler, http.MethodDelete,
		"/api/account/tokens/"+otherIssued.ID.String(), userA, nil)
	require.Equal(t, http.StatusNotFound, crossUser.Code)

	revoked := do(t, handler, http.MethodDelete,
		"/api/account/tokens/"+issued.ID.String(), userA, nil)
	require.Equal(t, http.StatusNoContent, revoked.Code, revoked.Body.String())
	var revokedAt *string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT revoked_at::text FROM api_tokens WHERE id=$1`, issued.ID).Scan(&revokedAt))
	require.NotNil(t, revokedAt)
}

func tokenJSONIDs(tokens []issuedTokenJSON) []uuid.UUID {
	ids := make([]uuid.UUID, len(tokens))
	for i, token := range tokens {
		ids[i] = token.ID
	}
	return ids
}

func cleanupAPIToken(t *testing.T, db *store.DB, tokenID uuid.UUID) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(),
		`DELETE FROM idempotency_records WHERE token_id=$1`, tokenID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(),
		`DELETE FROM api_request_audit_events WHERE token_id=$1`, tokenID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(), `DELETE FROM api_tokens WHERE id=$1`, tokenID)
	require.NoError(t, err)
}
