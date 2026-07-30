package store

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/access"

	"github.com/jackc/pgx/v5"
)

type IdempotencyStore struct{ db *DB }

func NewIdempotencyStore(db *DB) *IdempotencyStore { return &IdempotencyStore{db: db} }

func (s *IdempotencyStore) Claim(
	ctx context.Context,
	key access.IdempotencyKey,
	requestHash []byte,
	now time.Time,
	expiresAt time.Time,
) (access.Claim, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return access.Claim{}, fmt.Errorf("begin idempotency claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		DELETE FROM idempotency_records
		WHERE user_id=$1 AND credential_kind=$2 AND credential_id=$3
		  AND method=$4 AND route_pattern=$5
		  AND idempotency_key=$6 AND expires_at <= $7`,
		key.UserID, key.CredentialKind, key.CredentialID,
		key.Method, key.RoutePattern, key.Value, now); err != nil {
		return access.Claim{}, fmt.Errorf("delete expired idempotency claim: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_records (
			user_id, credential_kind, credential_id, token_id, agent_run_id,
			method, route_pattern, idempotency_key,
			request_hash, state, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'processing',$10,$11)
		ON CONFLICT DO NOTHING`,
		key.UserID, key.CredentialKind, key.CredentialID, key.TokenID, key.AgentRunID,
		key.Method, key.RoutePattern, key.Value, requestHash, now, expiresAt)
	if err != nil {
		return access.Claim{}, fmt.Errorf("insert idempotency claim: %w", err)
	}
	if tag.RowsAffected() == 1 {
		if err := tx.Commit(ctx); err != nil {
			return access.Claim{}, fmt.Errorf("commit idempotency claim: %w", err)
		}
		return access.Claim{Kind: access.ClaimAcquired}, nil
	}

	var existingHash []byte
	var state string
	var statusCode *int
	var headersJSON []byte
	var body []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, state, status_code, response_headers, response_body
		FROM idempotency_records
		WHERE user_id=$1 AND credential_kind=$2 AND credential_id=$3
		  AND method=$4 AND route_pattern=$5 AND idempotency_key=$6`,
		key.UserID, key.CredentialKind, key.CredentialID,
		key.Method, key.RoutePattern, key.Value,
	).Scan(&existingHash, &state, &statusCode, &headersJSON, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return access.Claim{}, fmt.Errorf("load conflicting idempotency claim: %w", err)
	}
	if err != nil {
		return access.Claim{}, fmt.Errorf("load idempotency claim: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return access.Claim{}, fmt.Errorf("commit idempotency lookup: %w", err)
	}
	if len(existingHash) != len(requestHash) ||
		subtle.ConstantTimeCompare(existingHash, requestHash) != 1 {
		return access.Claim{Kind: access.ClaimReused}, nil
	}
	if state == "processing" {
		return access.Claim{Kind: access.ClaimInProgress}, nil
	}
	if statusCode == nil {
		return access.Claim{}, errors.New("completed idempotency record has no status")
	}
	headers := map[string][]string{}
	if err := json.Unmarshal(headersJSON, &headers); err != nil {
		return access.Claim{}, fmt.Errorf("decode idempotency response headers: %w", err)
	}
	return access.Claim{
		Kind: access.ClaimReplay,
		Response: access.StoredResponse{
			StatusCode: *statusCode, Headers: headers, Body: body,
		},
	}, nil
}

func (s *IdempotencyStore) Complete(
	ctx context.Context,
	key access.IdempotencyKey,
	response access.StoredResponse,
) error {
	headers, err := json.Marshal(response.Headers)
	if err != nil {
		return fmt.Errorf("encode idempotency response headers: %w", err)
	}
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE idempotency_records
		SET state='completed', status_code=$7, response_headers=$8, response_body=$9
		WHERE user_id=$1 AND credential_kind=$2 AND credential_id=$3
		  AND method=$4 AND route_pattern=$5
		  AND idempotency_key=$6 AND state='processing'`,
		key.UserID, key.CredentialKind, key.CredentialID,
		key.Method, key.RoutePattern, key.Value,
		response.StatusCode, headers, response.Body)
	if err != nil {
		return fmt.Errorf("complete idempotency claim: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return access.ErrIdempotencyNotClaimed
	}
	return nil
}

func (s *IdempotencyStore) Release(ctx context.Context, key access.IdempotencyKey) error {
	_, err := s.db.Pool.Exec(ctx, `
		DELETE FROM idempotency_records
		WHERE user_id=$1 AND credential_kind=$2 AND credential_id=$3
		  AND method=$4 AND route_pattern=$5
		  AND idempotency_key=$6 AND state='processing'`,
		key.UserID, key.CredentialKind, key.CredentialID,
		key.Method, key.RoutePattern, key.Value)
	if err != nil {
		return fmt.Errorf("release idempotency claim: %w", err)
	}
	return nil
}
