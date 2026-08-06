package api_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/larkaudit"
	"github.com/wolfhead/pactline/internal/store"

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

type larkAuditPageJSON struct {
	Items      []larkaudit.Event `json:"items"`
	NextCursor string            `json:"next_cursor"`
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
	// Use an isolated future window so a concurrently running development
	// server cannot add the account owner's real requests between cursor
	// pages and make this deterministic pagination test flaky.
	now := time.Date(2040, 7, 29, 1, 0, 0, 0, time.UTC)
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

	timeWindow := "&from=" + url.QueryEscape(now.Add(-4*time.Minute).Format(time.RFC3339)) +
		"&to=" + url.QueryEscape(now.Add(time.Minute).Format(time.RFC3339))
	firstPage := do(t, handler, http.MethodGet,
		"/api/account/api-activity?page_size=1"+timeWindow, userB, nil)
	require.Equal(t, http.StatusOK, firstPage.Code, firstPage.Body.String())
	var first auditPageJSON
	decodeJSON(t, firstPage, &first)
	require.Len(t, first.Items, 1)
	require.Equal(t, userBID, *first.Items[0].UserID)
	require.NotEmpty(t, first.NextCursor)

	secondPage := do(t, handler, http.MethodGet,
		"/api/account/api-activity?page_size=1&cursor="+url.QueryEscape(first.NextCursor)+timeWindow,
		userB, nil)
	require.Equal(t, http.StatusOK, secondPage.Code, secondPage.Body.String())
	var second auditPageJSON
	decodeJSON(t, secondPage, &second)
	require.Len(t, second.Items, 1)
	require.Equal(t, "request-b-oldest", second.Items[0].RequestID)

	adminFiltered := do(t, handler, http.MethodGet,
		"/api/admin/api-activity?user_id="+userC+timeWindow, userA, nil)
	require.Equal(t, http.StatusOK, adminFiltered.Code, adminFiltered.Body.String())
	var adminPage auditPageJSON
	decodeJSON(t, adminFiltered, &adminPage)
	require.Len(t, adminPage.Items, 1)
	require.Equal(t, userCID, *adminPage.Items[0].UserID)
}

func TestLarkAPIAuditIsAdministratorOnlyAndSupportsSafeFilters(t *testing.T) {
	handler, db := newTaskTestServer(t)
	setTestAdmin(t, db, uuid.MustParse(userA))
	repository := store.NewAccessAuditStore(db)
	now := time.Date(2040, 8, 6, 8, 0, 0, 0, time.UTC)
	status, code := http.StatusOK, 0
	event := larkaudit.Event{
		ID: uuid.New(), OccurredAt: now, Operation: "send_notification",
		Category: "notification", Method: http.MethodPost,
		RoutePattern:   "/open-apis/im/v1/messages",
		CredentialKind: string(larkaudit.CredentialTenant),
		Outcome:        larkaudit.OutcomeSucceeded, HTTPStatus: &status,
		ProviderCode: &code, ProviderRequestID: "lark-log-1",
		DurationMS: 12, RequestBytes: 20, ResponseBytes: 30,
	}
	require.NoError(t, repository.RecordLarkAPIAudit(context.Background(), event))
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM lark_api_audit_events WHERE id=$1`, event.ID)
		require.NoError(t, err)
	})

	denied := do(t, handler, http.MethodGet,
		"/api/admin/lark-api-activity", userB, nil)
	require.Equal(t, http.StatusForbidden, denied.Code)

	window := "&from=" + url.QueryEscape(now.Add(-time.Minute).Format(time.RFC3339)) +
		"&to=" + url.QueryEscape(now.Add(time.Minute).Format(time.RFC3339))
	response := do(t, handler, http.MethodGet,
		"/api/admin/lark-api-activity?operation=send_notification&outcome=succeeded"+window,
		userA, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var page larkAuditPageJSON
	decodeJSON(t, response, &page)
	require.Len(t, page.Items, 1)
	require.Equal(t, "lark-log-1", page.Items[0].ProviderRequestID)
	require.NotContains(t, response.Body.String(), "Authorization")
}

func setTestAdmin(t *testing.T, db *store.DB, userID uuid.UUID) {
	t.Helper()
	var originalRole string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT platform_role FROM users WHERE id=$1`, userID).Scan(&originalRole)
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE users SET platform_role='ADMIN' WHERE id=$1`, userID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.Pool.Exec(context.Background(),
			`UPDATE users SET platform_role=$2 WHERE id=$1`, userID, originalRole)
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
