package store

import (
	"context"
	"errors"
	"fmt"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserStore reads team members.
type UserStore struct{ db *DB }

// NewUserStore wires a UserStore to the pool.
func NewUserStore(db *DB) *UserStore { return &UserStore{db: db} }

const userColumns = `id, name, email, roles, active`

// ListActive returns every active user ordered by name.
func (s *UserStore) ListActive(ctx context.Context) ([]domain.User, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+userColumns+` FROM users WHERE active ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetByID returns one user, or domain.ErrNotFound.
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row := s.db.Pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

type scanner interface{ Scan(dest ...any) error }

func scanUser(s scanner) (domain.User, error) {
	var u domain.User
	var roles []string
	if err := s.Scan(&u.ID, &u.Name, &u.Email, &roles, &u.Active); err != nil {
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	u.Roles = make([]domain.UserRole, len(roles))
	for i, r := range roles {
		u.Roles[i] = domain.UserRole(r)
	}
	return u, nil
}
