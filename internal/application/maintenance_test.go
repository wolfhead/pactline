package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type maintenanceStore struct {
	accessBefore       time.Time
	larkBefore         time.Time
	idempotencyBefore  time.Time
	accessRemoved      int64
	larkRemoved        int64
	idempotencyRemoved int64
	agentBefore        time.Time
	agentRunsRemoved   int64
}

type maintenanceClaimStore struct {
	now     time.Time
	limit   int
	expired int
}

func (s *maintenanceClaimStore) ExpireDue(
	_ context.Context,
	now time.Time,
	limit int,
) (int, error) {
	s.now = now
	s.limit = limit
	return s.expired, nil
}

func (s *maintenanceStore) DeleteAccessAuditBefore(_ context.Context, before time.Time) (int64, error) {
	s.accessBefore = before
	return s.accessRemoved, nil
}

func (s *maintenanceStore) DeleteLarkAPIAuditBefore(_ context.Context, before time.Time) (int64, error) {
	s.larkBefore = before
	return s.larkRemoved, nil
}

func (s *maintenanceStore) DeleteIdempotencyBefore(_ context.Context, before time.Time) (int64, error) {
	s.idempotencyBefore = before
	return s.idempotencyRemoved, nil
}

func (s *maintenanceStore) DeleteAgentRunsBefore(_ context.Context, before time.Time) (int64, error) {
	s.agentBefore = before
	return s.agentRunsRemoved, nil
}

func TestMaintenanceDeletesExpiredTransientRecords(t *testing.T) {
	now := time.Date(2026, 7, 28, 18, 0, 0, 0, time.UTC)
	store := &maintenanceStore{
		accessRemoved: 2, larkRemoved: 6, idempotencyRemoved: 3, agentRunsRemoved: 4,
	}
	claims := &maintenanceClaimStore{expired: 5}
	maintenance := Maintenance{Store: store, Claims: claims}

	result, err := maintenance.RunOnce(context.Background(), now)

	require.NoError(t, err)
	require.Equal(t, now.Add(-90*24*time.Hour), store.accessBefore)
	require.Equal(t, now.Add(-90*24*time.Hour), store.larkBefore)
	require.Equal(t, now, store.idempotencyBefore)
	require.Equal(t, now.Add(-90*24*time.Hour), store.agentBefore)
	require.Equal(t, now, claims.now)
	require.Equal(t, 200, claims.limit)
	require.Equal(t, MaintenanceResult{
		AccessAuditRemoved: 2, LarkAuditRemoved: 6,
		IdempotencyRemoved: 3, AgentRunsRemoved: 4,
		TaskClaimsExpired: 5,
	}, result)
}
