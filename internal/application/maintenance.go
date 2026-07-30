package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	AccessAuditRetention = 90 * 24 * time.Hour
	MaintenanceInterval  = 24 * time.Hour
)

type MaintenanceStore interface {
	DeleteAccessAuditBefore(context.Context, time.Time) (int64, error)
	DeleteIdempotencyBefore(context.Context, time.Time) (int64, error)
	DeleteAgentRunsBefore(context.Context, time.Time) (int64, error)
}

type Maintenance struct {
	Store MaintenanceStore
}

type MaintenanceResult struct {
	AccessAuditRemoved int64
	IdempotencyRemoved int64
	AgentRunsRemoved   int64
}

func (m Maintenance) RunOnce(ctx context.Context, now time.Time) (MaintenanceResult, error) {
	accessRemoved, err := m.Store.DeleteAccessAuditBefore(ctx, now.Add(-AccessAuditRetention))
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("expire API access audit: %w", err)
	}
	idempotencyRemoved, err := m.Store.DeleteIdempotencyBefore(ctx, now)
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("expire idempotency records: %w", err)
	}
	agentRunsRemoved, err := m.Store.DeleteAgentRunsBefore(
		ctx, now.Add(-AccessAuditRetention),
	)
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("expire Agent runs: %w", err)
	}
	return MaintenanceResult{
		AccessAuditRemoved: accessRemoved, IdempotencyRemoved: idempotencyRemoved,
		AgentRunsRemoved: agentRunsRemoved,
	}, nil
}

func (m Maintenance) Run(ctx context.Context) {
	run := func() {
		result, err := m.RunOnce(ctx, time.Now().UTC())
		if err != nil {
			slog.Error("maintenance failed", "error", err)
			return
		}
		slog.Info("maintenance completed",
			"access_audit_removed", result.AccessAuditRemoved,
			"idempotency_removed", result.IdempotencyRemoved,
			"agent_runs_removed", result.AgentRunsRemoved)
	}
	run()
	ticker := time.NewTicker(MaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
