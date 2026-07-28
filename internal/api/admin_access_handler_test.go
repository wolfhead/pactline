package api_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type adminTokenListJSON struct {
	Items []struct {
		Token issuedTokenJSON `json:"token"`
	} `json:"items"`
}

type auditPageJSON struct {
	Items      []access.RequestAuditEvent `json:"items"`
	NextCursor string                     `json:"next_cursor"`
}

func TestAdminAccessCanListAndRevokeButCannotCreateSecrets(t *testing.T) {
	handler, db := newTaskTestServer(t)
	setTestAdmin(t, db, uuid.MustParse(userA))

	memberDenied := do(t, handler, http.MethodGet, "/api/admin/api-tokens", userB, nil)
	require.Equal(t, http.StatusForbidden, memberDenied.Code)

	created := do(t, handler, http.MethodPost, "/api/account/tokens", userB, map[string]any{
		"name":   "admin-revoke-" + uuid.NewString(),
		"scopes": []string{"work:read"}, "expires_in_days": 365,
	})
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var issued issuedTokenJSON
	decodeJSON(t, created, &issued)
	t.Cleanup(func() { cleanupAPIToken(t, db, issued.ID) })

	listed := do(t, handler, http.MethodGet, "/api/admin/api-tokens", userA, nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.NotContains(t, listed.Body.String(), issued.Token)
	var list adminTokenListJSON
	decodeJSON(t, listed, &list)
	var found bool
	for _, item := range list.Items {
		if item.Token.ID == issued.ID {
			found = true
		}
	}
	require.True(t, found)

	createDenied := do(t, handler, http.MethodPost, "/api/admin/api-tokens", userA, map[string]any{})
	require.Equal(t, http.StatusMethodNotAllowed, createDenied.Code)

	revoked := do(t, handler, http.MethodDelete,
		"/api/admin/api-tokens/"+issued.ID.String(), userA, nil)
	require.Equal(t, http.StatusNoContent, revoked.Code, revoked.Body.String())
}

func TestAccessAuditQueriesForceAccountOwnerAndSupportAdminFiltersAndCursor(t *testing.T) {
	handler, db := newTaskTestServer(t)
	setTestAdmin(t, db, uuid.MustParse(userA))
	repository := store.NewAccessAuditStore(db)
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	userBID, userCID := uuid.MustParse(userB), uuid.MustParse(userC)
	events := []access.RequestAuditEvent{
		auditFixture(uuid.New(), userBID, now.Add(-time.Minute), "request-b-latest"),
		auditFixture(uuid.New(), userCID, now.Add(-2*time.Minute), "request-c"),
		auditFixture(uuid.New(), userBID, now.Add(-3*time.Minute), "request-b-oldest"),
	}
	for _, event := range events {
		require.NoError(t, repository.RecordAccessAudit(context.Background(), event))
	}
	t.Cleanup(func() {
		ids := []uuid.UUID{events[0].ID, events[1].ID, events[2].ID}
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM api_request_audit_events WHERE id=ANY($1)`, ids)
		require.NoError(t, err)
	})

	firstPage := do(t, handler, http.MethodGet,
		"/api/account/api-activity?page_size=1", userB, nil)
	require.Equal(t, http.StatusOK, firstPage.Code, firstPage.Body.String())
	var first auditPageJSON
	decodeJSON(t, firstPage, &first)
	require.Len(t, first.Items, 1)
	require.Equal(t, userBID, *first.Items[0].UserID)
	require.NotEmpty(t, first.NextCursor)

	secondPage := do(t, handler, http.MethodGet,
		"/api/account/api-activity?page_size=1&cursor="+url.QueryEscape(first.NextCursor),
		userB, nil)
	require.Equal(t, http.StatusOK, secondPage.Code, secondPage.Body.String())
	var second auditPageJSON
	decodeJSON(t, secondPage, &second)
	require.Len(t, second.Items, 1)
	require.Equal(t, "request-b-oldest", second.Items[0].RequestID)

	adminFiltered := do(t, handler, http.MethodGet,
		"/api/admin/api-activity?user_id="+userC, userA, nil)
	require.Equal(t, http.StatusOK, adminFiltered.Code, adminFiltered.Body.String())
	var adminPage auditPageJSON
	decodeJSON(t, adminFiltered, &adminPage)
	require.Len(t, adminPage.Items, 1)
	require.Equal(t, userCID, *adminPage.Items[0].UserID)
}

func setTestAdmin(t *testing.T, db *store.DB, userID uuid.UUID) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET platform_role='ADMIN' WHERE id=$1`, userID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`UPDATE users SET platform_role='MEMBER' WHERE id=$1`, userID)
		require.NoError(t, cleanupErr)
	})
}

func auditFixture(
	id uuid.UUID,
	userID uuid.UUID,
	occurredAt time.Time,
	requestIDValue string,
) access.RequestAuditEvent {
	return access.RequestAuditEvent{
		ID: id, OccurredAt: occurredAt, RequestID: requestIDValue,
		AuthMethod:  access.AuthenticationMethodSession,
		AuthOutcome: access.AuthOutcomeAuthenticated, UserID: &userID,
		Method: http.MethodGet, RoutePattern: "/api/v1/tasks",
		StatusCode: http.StatusOK, UserAgent: "admin-access-test",
	}
}
