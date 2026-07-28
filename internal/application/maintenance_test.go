package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type maintenanceStore struct {
	accessBefore       time.Time
	idempotencyBefore  time.Time
	accessRemoved      int64
	idempotencyRemoved int64
}

func (s *maintenanceStore) DeleteAccessAuditBefore(_ context.Context, before time.Time) (int64, error) {
	s.accessBefore = before
	return s.accessRemoved, nil
}

func (s *maintenanceStore) DeleteIdempotencyBefore(_ context.Context, before time.Time) (int64, error) {
	s.idempotencyBefore = before
	return s.idempotencyRemoved, nil
}

func TestMaintenanceDeletesExpiredTransientRecords(t *testing.T) {
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	store := &maintenanceStore{accessRemoved: 2, idempotencyRemoved: 3}
	maintenance := Maintenance{Store: store}

	result, err := maintenance.RunOnce(context.Background(), now)

	require.NoError(t, err)
	require.Equal(t, now.Add(-90*24*time.Hour), store.accessBefore)
	require.Equal(t, now, store.idempotencyBefore)
	require.Equal(t, MaintenanceResult{AccessAuditRemoved: 2, IdempotencyRemoved: 3}, result)
}
