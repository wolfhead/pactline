package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	AccessAuditRetention = 90 * 24 * time.Hour
	MaintenanceInterval  = 10 * time.Minute
)

type MaintenanceStore interface {
	DeleteAccessAuditBefore(context.Context, time.Time) (int64, error)
	DeleteLarkAPIAuditBefore(context.Context, time.Time) (int64, error)
	DeleteIdempotencyBefore(context.Context, time.Time) (int64, error)
	DeleteAgentRunsBefore(context.Context, time.Time) (int64, error)
}

type Maintenance struct {
	Store  MaintenanceStore
	Claims interface {
		ExpireDue(context.Context, time.Time, int) (int, error)
	}
}

type MaintenanceResult struct {
	AccessAuditRemoved int64
	LarkAuditRemoved   int64
	IdempotencyRemoved int64
	AgentRunsRemoved   int64
	TaskClaimsExpired  int
}

func (m Maintenance) RunOnce(ctx context.Context, now time.Time) (MaintenanceResult, error) {
	accessRemoved, err := m.Store.DeleteAccessAuditBefore(ctx, now.Add(-AccessAuditRetention))
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("expire API access audit: %w", err)
	}
	larkRemoved, err := m.Store.DeleteLarkAPIAuditBefore(ctx, now.Add(-AccessAuditRetention))
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("expire Lark API audit: %w", err)
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
	claimsExpired := 0
	if m.Claims != nil {
		claimsExpired, err = m.Claims.ExpireDue(ctx, now, 200)
		if err != nil {
			return MaintenanceResult{}, fmt.Errorf("expire Task Claims: %w", err)
		}
	}
	return MaintenanceResult{
		AccessAuditRemoved: accessRemoved, LarkAuditRemoved: larkRemoved,
		IdempotencyRemoved: idempotencyRemoved,
		AgentRunsRemoved:   agentRunsRemoved, TaskClaimsExpired: claimsExpired,
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
			"lark_api_audit_removed", result.LarkAuditRemoved,
			"idempotency_removed", result.IdempotencyRemoved,
			"agent_runs_removed", result.AgentRunsRemoved,
			"task_claims_expired", result.TaskClaimsExpired)
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
