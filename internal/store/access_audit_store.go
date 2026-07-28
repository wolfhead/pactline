package store

import (
	"context"
	"fmt"
	"time"

	"bountyboard/internal/access"
)

type AccessAuditStore struct{ db *DB }

func NewAccessAuditStore(db *DB) *AccessAuditStore { return &AccessAuditStore{db: db} }

func (s *AccessAuditStore) RecordAccessAudit(
	ctx context.Context,
	event access.RequestAuditEvent,
) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO api_request_audit_events (
			id, occurred_at, request_id, auth_method, auth_outcome, user_id,
			token_id, token_name, method, route_pattern, status_code, problem_code,
			duration_ms, response_bytes, idempotency_replayed, user_agent,
			network_address
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
		)`,
		event.ID, event.OccurredAt, event.RequestID, event.AuthMethod,
		event.AuthOutcome, event.UserID, event.TokenID, nullIfEmpty(event.TokenName),
		event.Method, event.RoutePattern, event.StatusCode,
		nullIfEmpty(event.ProblemCode), event.DurationMS, event.ResponseBytes,
		event.IdempotencyReplayed, event.UserAgent,
		nullIfEmpty(event.NetworkAddress),
	)
	if err != nil {
		return fmt.Errorf("insert API access audit: %w", err)
	}
	return nil
}

func (s *AccessAuditStore) DeleteAccessAuditBefore(
	ctx context.Context,
	before time.Time,
) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM api_request_audit_events WHERE occurred_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired API access audit: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *AccessAuditStore) DeleteIdempotencyBefore(
	ctx context.Context,
	before time.Time,
) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM idempotency_records WHERE expires_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired idempotency records: %w", err)
	}
	return tag.RowsAffected(), nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
