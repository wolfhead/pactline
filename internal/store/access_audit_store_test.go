package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccessAuditStorePersistsSafeFieldsAndAppliesRetention(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAccessAuditStore(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	recentID, expiredID := uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM api_request_audit_events WHERE id=ANY($1)`,
			[]uuid.UUID{recentID, expiredID})
		require.NoError(t, err)
	})

	base := access.RequestAuditEvent{
		RequestID: "audit-request", AuthMethod: access.AuthenticationMethodSession,
		AuthOutcome: access.AuthOutcomeAuthenticated, UserID: pointerUUID(userA),
		Method: httpMethodGet, RoutePattern: "/api/v1/tasks", StatusCode: 200,
		DurationMS: 12, ResponseBytes: 34, UserAgent: "store-test",
		NetworkAddress: "192.0.2.20", ClientKind: "pactline-cli",
		ClientSessionID: "session-a",
	}
	recent := base
	recent.ID, recent.OccurredAt = recentID, now.Add(-89*24*time.Hour)
	expired := base
	expired.ID, expired.OccurredAt = expiredID, now.Add(-91*24*time.Hour)
	require.NoError(t, repository.RecordAccessAudit(ctx, recent))
	require.NoError(t, repository.RecordAccessAudit(ctx, expired))

	removed, err := repository.DeleteAccessAuditBefore(ctx, now.Add(-90*24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)

	var remaining []uuid.UUID
	rows, err := db.Pool.Query(ctx, `
		SELECT id FROM api_request_audit_events
		WHERE id=ANY($1) ORDER BY id`, []uuid.UUID{recentID, expiredID})
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		remaining = append(remaining, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []uuid.UUID{recentID}, remaining)
	events, err := repository.ListAccessAudit(ctx, access.RequestAuditFilter{
		RequestID: "audit-request", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "pactline-cli", events[0].ClientKind)
	require.Equal(t, "session-a", events[0].ClientSessionID)
}

func TestAccessAuditImportantFilterExcludesOnlySuccessfulReads(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAccessAuditStore(db)
	ctx := context.Background()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(),
			`DELETE FROM api_request_audit_events WHERE id=ANY($1)`, ids)
		require.NoError(t, err)
	})

	base := access.RequestAuditEvent{
		OccurredAt: time.Now().UTC(), RequestID: "important-filter",
		AuthMethod:  access.AuthenticationMethodSession,
		AuthOutcome: access.AuthOutcomeAuthenticated, UserID: pointerUUID(userA),
		RoutePattern: "/api/v1/tasks", DurationMS: 1, UserAgent: "store-test",
	}
	for index, request := range []struct {
		method string
		status int
	}{
		{method: httpMethodGet, status: 200},
		{method: httpMethodGet, status: 403},
		{method: "PATCH", status: 200},
	} {
		event := base
		event.ID = ids[index]
		event.Method = request.method
		event.StatusCode = request.status
		require.NoError(t, repository.RecordAccessAudit(ctx, event))
	}

	events, err := repository.ListAccessAudit(ctx, access.RequestAuditFilter{
		RequestID: "important-filter", ImportantOnly: true, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.ElementsMatch(t, []int{403, 200}, []int{events[0].StatusCode, events[1].StatusCode})
}

const httpMethodGet = "GET"

func pointerUUID(value uuid.UUID) *uuid.UUID { return &value }
