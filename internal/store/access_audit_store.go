package store

import (
	"context"
	"fmt"
	"strings"
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

func (s *AccessAuditStore) ListAccessAudit(
	ctx context.Context,
	filter access.RequestAuditFilter,
) ([]access.RequestAuditEvent, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	var query strings.Builder
	query.WriteString(`
		SELECT id, occurred_at, request_id, auth_method, auth_outcome, user_id,
		       token_id, token_name, method, route_pattern, status_code,
		       problem_code, duration_ms, response_bytes, idempotency_replayed,
		       user_agent, host(network_address)
		FROM api_request_audit_events
		WHERE true`)
	args := make([]any, 0, 10)
	add := func(clause string, value any) {
		args = append(args, value)
		fmt.Fprintf(&query, " AND %s=$%d", clause, len(args))
	}
	if filter.UserID != nil {
		add("user_id", *filter.UserID)
	}
	if filter.TokenID != nil {
		add("token_id", *filter.TokenID)
	}
	if filter.Method != "" {
		add("method", filter.Method)
	}
	if filter.RoutePattern != "" {
		add("route_pattern", filter.RoutePattern)
	}
	if filter.StatusCode != nil {
		add("status_code", *filter.StatusCode)
	}
	if filter.RequestID != "" {
		add("request_id", filter.RequestID)
	}
	if filter.From != nil {
		args = append(args, *filter.From)
		fmt.Fprintf(&query, " AND occurred_at >= $%d", len(args))
	}
	if filter.To != nil {
		args = append(args, *filter.To)
		fmt.Fprintf(&query, " AND occurred_at <= $%d", len(args))
	}
	if filter.Before != nil {
		args = append(args, filter.Before.OccurredAt, filter.Before.ID)
		fmt.Fprintf(&query,
			" AND (occurred_at, id) < ($%d, $%d)",
			len(args)-1, len(args))
	}
	args = append(args, limit)
	fmt.Fprintf(&query, " ORDER BY occurred_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.db.Pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query API access audit: %w", err)
	}
	defer rows.Close()
	var events []access.RequestAuditEvent
	for rows.Next() {
		var event access.RequestAuditEvent
		var tokenName, problemCode, networkAddress *string
		if err := rows.Scan(
			&event.ID, &event.OccurredAt, &event.RequestID, &event.AuthMethod,
			&event.AuthOutcome, &event.UserID, &event.TokenID, &tokenName,
			&event.Method, &event.RoutePattern, &event.StatusCode, &problemCode,
			&event.DurationMS, &event.ResponseBytes, &event.IdempotencyReplayed,
			&event.UserAgent, &networkAddress,
		); err != nil {
			return nil, fmt.Errorf("scan API access audit: %w", err)
		}
		if tokenName != nil {
			event.TokenName = *tokenName
		}
		if problemCode != nil {
			event.ProblemCode = *problemCode
		}
		if networkAddress != nil {
			event.NetworkAddress = *networkAddress
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query API access audit: %w", err)
	}
	return events, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
