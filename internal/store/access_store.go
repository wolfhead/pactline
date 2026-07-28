package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"bountyboard/internal/access"
	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccessStore struct{ db *DB }

func NewAccessStore(db *DB) *AccessStore { return &AccessStore{db: db} }

const tokenColumns = `
	id, user_id, name, secret_hash, display_prefix, scopes, expires_at,
	last_used_at, revoked_at, revoked_by_user_id, created_at`

func (s *AccessStore) CreateToken(ctx context.Context, token access.Token) error {
	scopes := make([]string, len(token.Scopes))
	for i, scope := range token.Scopes {
		scopes[i] = string(scope)
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO api_tokens (
			id, user_id, name, secret_hash, display_prefix, scopes, expires_at,
			last_used_at, revoked_at, revoked_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		token.ID, token.UserID, token.Name, token.SecretHash, token.DisplayPrefix,
		scopes, token.ExpiresAt, token.LastUsedAt, token.RevokedAt,
		token.RevokedByUserID, token.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert API token: %w", err)
	}
	return nil
}

func (s *AccessStore) GetToken(ctx context.Context, id uuid.UUID) (access.TokenWithUser, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT
			t.id, t.user_id, t.name, t.secret_hash, t.display_prefix, t.scopes,
			t.expires_at, t.last_used_at, t.revoked_at, t.revoked_by_user_id,
			t.created_at,
			u.id, u.name, u.email, u.avatar_url, u.platform_role, u.roles,
			u.active, u.created_at, u.updated_at
		FROM api_tokens t
		JOIN users u ON u.id=t.user_id
		WHERE t.id=$1`, id)
	bundle, err := scanTokenWithUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return access.TokenWithUser{}, access.ErrTokenNotFound
	}
	if err != nil {
		return access.TokenWithUser{}, fmt.Errorf("get API token: %w", err)
	}
	return bundle, nil
}

func (s *AccessStore) ListUserTokens(ctx context.Context, userID uuid.UUID) ([]access.Token, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+tokenColumns+`
		FROM api_tokens
		WHERE user_id=$1
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("query API tokens: %w", err)
	}
	defer rows.Close()

	var tokens []access.Token
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("list API tokens: %w", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	return tokens, nil
}

func (s *AccessStore) ListAllTokens(ctx context.Context) ([]access.TokenWithUser, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT
			t.id, t.user_id, t.name, t.secret_hash, t.display_prefix, t.scopes,
			t.expires_at, t.last_used_at, t.revoked_at, t.revoked_by_user_id,
			t.created_at,
			u.id, u.name, u.email, u.avatar_url, u.platform_role, u.roles,
			u.active, u.created_at, u.updated_at
		FROM api_tokens t
		JOIN users u ON u.id=t.user_id
		ORDER BY t.created_at DESC, t.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query all API tokens: %w", err)
	}
	defer rows.Close()

	var tokens []access.TokenWithUser
	for rows.Next() {
		token, err := scanTokenWithUser(rows)
		if err != nil {
			return nil, fmt.Errorf("list all API tokens: %w", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all API tokens: %w", err)
	}
	return tokens, nil
}

func (s *AccessStore) RevokeToken(
	ctx context.Context,
	tokenID uuid.UUID,
	actorID uuid.UUID,
	now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE api_tokens
		SET revoked_at=$3, revoked_by_user_id=$2
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`,
		tokenID, actorID, now)
	if err != nil {
		return fmt.Errorf("revoke API token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return access.ErrTokenNotFound
	}
	return nil
}

func (s *AccessStore) RevokeTokenAsAdmin(
	ctx context.Context,
	tokenID uuid.UUID,
	adminID uuid.UUID,
	now time.Time,
) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE api_tokens
		SET revoked_at=$3, revoked_by_user_id=$2
		WHERE id=$1 AND revoked_at IS NULL`,
		tokenID, adminID, now)
	if err != nil {
		return fmt.Errorf("revoke API token as administrator: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return access.ErrTokenNotFound
	}
	return nil
}

func (s *AccessStore) TouchToken(
	ctx context.Context,
	tokenID uuid.UUID,
	now time.Time,
	before time.Time,
) error {
	if _, err := s.db.Pool.Exec(ctx, `
		UPDATE api_tokens
		SET last_used_at=$2
		WHERE id=$1
		  AND (last_used_at IS NULL OR last_used_at < $3)`,
		tokenID, now, before); err != nil {
		return fmt.Errorf("touch API token: %w", err)
	}
	return nil
}

func scanToken(s scanner) (access.Token, error) {
	var token access.Token
	var scopes []string
	if err := s.Scan(
		&token.ID, &token.UserID, &token.Name, &token.SecretHash,
		&token.DisplayPrefix, &scopes, &token.ExpiresAt, &token.LastUsedAt,
		&token.RevokedAt, &token.RevokedByUserID, &token.CreatedAt,
	); err != nil {
		return access.Token{}, err
	}
	token.Scopes = make([]access.Scope, len(scopes))
	for i, scope := range scopes {
		token.Scopes[i] = access.Scope(scope)
	}
	return token, nil
}

func scanTokenWithUser(s scanner) (access.TokenWithUser, error) {
	var token access.Token
	var scopes []string
	var user domain.User
	var roles []string
	if err := s.Scan(
		&token.ID, &token.UserID, &token.Name, &token.SecretHash,
		&token.DisplayPrefix, &scopes, &token.ExpiresAt, &token.LastUsedAt,
		&token.RevokedAt, &token.RevokedByUserID, &token.CreatedAt,
		&user.ID, &user.Name, &user.Email, &user.AvatarURL, &user.PlatformRole,
		&roles, &user.Active, &user.CreatedAt, &user.UpdatedAt,
	); err != nil {
		return access.TokenWithUser{}, err
	}
	token.Scopes = make([]access.Scope, len(scopes))
	for i, scope := range scopes {
		token.Scopes[i] = access.Scope(scope)
	}
	user.Roles = make([]domain.UserRole, len(roles))
	for i, role := range roles {
		user.Roles[i] = domain.UserRole(role)
	}
	return access.TokenWithUser{Token: token, User: user}, nil
}
