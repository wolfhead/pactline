package store_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/wolfhead/pactline/internal/larkaudit"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLarkAPIAuditStorePersistsSafeMetadataAndExpiresRecords(t *testing.T) {
	db := newTestDB(t)
	repository := store.NewAccessAuditStore(db)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 8, 30, 0, 0, time.UTC)
	status, providerCode := http.StatusOK, 0
	event := larkaudit.Event{
		ID: uuid.New(), OccurredAt: now, Operation: "send_notification",
		Category: "notification", Method: http.MethodPost,
		RoutePattern:   "/open-apis/im/v1/messages",
		CredentialKind: string(larkaudit.CredentialTenant),
		Outcome:        larkaudit.OutcomeSucceeded, HTTPStatus: &status,
		ProviderCode: &providerCode, ProviderRequestID: "provider-request-1",
		DurationMS: 18, RequestBytes: 120, ResponseBytes: 48,
	}
	require.NoError(t, repository.RecordLarkAPIAudit(ctx, event))
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(ctx, `DELETE FROM lark_api_audit_events WHERE id=$1`, event.ID)
	})

	items, err := repository.ListLarkAPIAudit(ctx, larkaudit.Filter{
		Operation: "send_notification", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, event, items[0])

	removed, err := repository.DeleteLarkAPIAuditBefore(ctx, now.Add(time.Second))
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)
}
