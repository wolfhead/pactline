package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/larkaudit"
)

func (s *AccessAuditStore) RecordLarkAPIAudit(
	ctx context.Context,
	event larkaudit.Event,
) error {
	if err := event.Validate(); err != nil {
		return err
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO lark_api_audit_events (
			id, occurred_at, operation, category, method, route_pattern,
			credential_kind, outcome, http_status, provider_code,
			provider_request_id, error_category, duration_ms, request_bytes,
			response_bytes, request_id, actor_user_id, subject_user_id,
			agent_run_id, application_event_id
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
		)`,
		event.ID, event.OccurredAt, event.Operation, event.Category, event.Method,
		event.RoutePattern, event.CredentialKind, event.Outcome, event.HTTPStatus,
		event.ProviderCode, nullIfEmpty(event.ProviderRequestID),
		nullIfEmpty(event.ErrorCategory), event.DurationMS, event.RequestBytes,
		event.ResponseBytes, nullIfEmpty(event.RequestID), event.ActorUserID,
		event.SubjectUserID, event.AgentRunID, event.ApplicationEventID,
	)
	if err != nil {
		return fmt.Errorf("insert Lark API audit: %w", err)
	}
	return nil
}

func (s *AccessAuditStore) DeleteLarkAPIAuditBefore(
	ctx context.Context,
	before time.Time,
) (int64, error) {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM lark_api_audit_events WHERE occurred_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired Lark API audit: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *AccessAuditStore) ListLarkAPIAudit(
	ctx context.Context,
	filter larkaudit.Filter,
) ([]larkaudit.Event, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	var query strings.Builder
	query.WriteString(`
		SELECT id, occurred_at, operation, category, method, route_pattern,
		       credential_kind, outcome, http_status, provider_code,
		       provider_request_id, error_category, duration_ms, request_bytes,
		       response_bytes, request_id, actor_user_id, subject_user_id,
		       agent_run_id, application_event_id
		FROM lark_api_audit_events
		WHERE true`)
	args := make([]any, 0, 12)
	add := func(column string, value any) {
		args = append(args, value)
		fmt.Fprintf(&query, " AND %s=$%d", column, len(args))
	}
	if filter.Operation != "" {
		add("operation", filter.Operation)
	}
	if filter.Category != "" {
		add("category", filter.Category)
	}
	if filter.Outcome != "" {
		add("outcome", filter.Outcome)
	}
	if filter.HTTPStatus != nil {
		add("http_status", *filter.HTTPStatus)
	}
	if filter.ProviderRequestID != "" {
		add("provider_request_id", filter.ProviderRequestID)
	}
	if filter.RequestID != "" {
		add("request_id", filter.RequestID)
	}
	if filter.ActorUserID != nil {
		add("actor_user_id", *filter.ActorUserID)
	}
	if filter.AgentRunID != nil {
		add("agent_run_id", *filter.AgentRunID)
	}
	if filter.ApplicationEventID != nil {
		add("application_event_id", *filter.ApplicationEventID)
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
		fmt.Fprintf(&query, " AND (occurred_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit)
	fmt.Fprintf(&query, " ORDER BY occurred_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.db.Pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query Lark API audit: %w", err)
	}
	defer rows.Close()
	var events []larkaudit.Event
	for rows.Next() {
		var event larkaudit.Event
		var providerRequestID, errorCategory, requestID *string
		if err := rows.Scan(
			&event.ID, &event.OccurredAt, &event.Operation, &event.Category,
			&event.Method, &event.RoutePattern, &event.CredentialKind,
			&event.Outcome, &event.HTTPStatus, &event.ProviderCode,
			&providerRequestID, &errorCategory, &event.DurationMS,
			&event.RequestBytes, &event.ResponseBytes, &requestID,
			&event.ActorUserID, &event.SubjectUserID, &event.AgentRunID,
			&event.ApplicationEventID,
		); err != nil {
			return nil, fmt.Errorf("scan Lark API audit: %w", err)
		}
		if providerRequestID != nil {
			event.ProviderRequestID = *providerRequestID
		}
		if errorCategory != nil {
			event.ErrorCategory = *errorCategory
		}
		if requestID != nil {
			event.RequestID = *requestID
		}
		event.OccurredAt = event.OccurredAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Lark API audit: %w", err)
	}
	return events, nil
}
