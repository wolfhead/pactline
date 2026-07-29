// Package store provides PostgreSQL-backed persistence for the legacy
// mechanism (bounty, credit, calibration, anchor). See
// internal/legacy/README.md for why this package exists separately from
// internal/store.
package store

import pgstore "github.com/wolfhead/pactline/internal/store"

// DB aliases the shared connection pool type (internal/store.DB) so the
// stores in this package can keep writing "*DB", exactly as they did before
// the legacy split, when they lived in the same package as
// internal/store/postgres.go.
type DB = pgstore.DB

// scanner is a minimal cursor abstraction: either *pgx.Rows or *pgx.Row
// satisfies it. It is a local copy of the identical, unexported scanner
// interface declared in internal/store/user_store.go — that one cannot be
// referenced across the package boundary the legacy split introduced.
type scanner interface{ Scan(dest ...any) error }
