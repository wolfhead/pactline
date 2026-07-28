package store

import (
	"errors"
	"fmt"

	"bountyboard/internal/domain"

	"github.com/jackc/pgx/v5/pgconn"
)

// mapPgError translates a raw PostgreSQL error into the shared domain error
// vocabulary so handlers never have to know PostgreSQL error codes: a
// foreign-key violation (e.g. an assignee_id or label_id that names no row)
// becomes domain.ErrInvalidInput, a uniqueness violation (e.g. a duplicate
// label name) becomes domain.ErrConflict. Anything else is wrapped, never
// swallowed.
func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.Message)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", domain.ErrInvalidInput, pgErr.Message)
		case "23514": // check_violation
			return fmt.Errorf("%w: %s", domain.ErrInvalidInput, pgErr.Message)
		}
	}
	return fmt.Errorf("query: %w", err)
}
