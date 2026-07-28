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
		NetworkAddress: "192.0.2.20",
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
}

const httpMethodGet = "GET"

func pointerUUID(value uuid.UUID) *uuid.UUID { return &value }
