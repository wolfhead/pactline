package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// GetByID returns one user, or domain.ErrNotFound. Deliberately does not
// filter on active: whether a caller may act on this identity is decided by
// withIdentity (see its comment on the meaning of "active"), and whether a
// credited person may still be named is decided by ListAll — GetByID itself
// is a plain lookup either concern can build on.
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row := s.db.Pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// ListAll returns every user regardless of active status, ordered by name.
//
// This backs credit-name resolution (feed_handler.decorate): a person who
// has left the team must still be nameable on the work they are credited
// on — the archive keeps naming people after they are deactivated. Do NOT
// use this for the identity switcher or the nomination picker (GET
// /api/users); those must keep using ListActive.
func (s *UserStore) ListAll(ctx context.Context) ([]domain.User, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY name`)
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

// SetActive flips a user's active flag.
//
// "active" governs who may act (mutating and read endpoints alike, enforced
// in withIdentity) and who appears in pickers (ListActive) — never who is
// remembered (ListAll, used for credit names). There is no HTTP endpoint for
// this yet in Phase 1; it exists so tests can exercise deactivation, and so a
// future admin endpoint has somewhere to call into.
func (s *UserStore) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE users SET active=$2 WHERE id=$1`, id, active)
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	slog.Info("user active flag changed", "user_id", id, "active", active)
	return nil
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
