// Package store provides PostgreSQL-backed persistence.
package store

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB owns the connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// migrationLockKey is an arbitrary, fixed key for the PostgreSQL advisory
// lock used to serialize concurrent Migrate callers. Its numeric value has
// no meaning beyond being unique to this application's migration runner.
const migrationLockKey = 727433

// Connect opens a pool and verifies it with a ping.
func Connect(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	slog.Info("database connected", "max_conns", pool.Config().MaxConns)
	return &DB{Pool: pool}, nil
}

// Close releases all pooled connections.
func (db *DB) Close() {
	db.Pool.Close()
}

// Migrate applies every migration file not yet recorded in schema_migrations.
func (db *DB) Migrate(ctx context.Context, fsys fs.FS) error {
	if _, err := db.Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`,
	); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := db.applyMigration(ctx, fsys, name)
		if err != nil {
			return err
		}
		if applied {
			slog.Info("migration applied", "name", name)
		} else {
			slog.Debug("migration already applied", "name", name)
		}
	}
	return nil
}

// applyMigration applies a single migration file, if not already recorded,
// inside one transaction: acquiring a transaction-scoped advisory lock,
// checking whether the migration was already applied, running the
// migration body, and recording it in schema_migrations all commit or roll
// back together. This makes apply-and-record atomic (a crash between the
// two can no longer leave the database applied-but-unrecorded) and
// serializes concurrent callers against the same migration file, since the
// advisory lock is held for the duration of the check-then-apply sequence
// and is released automatically on commit or rollback.
//
// It reports whether the migration was newly applied (true) or had already
// been recorded by an earlier call (false).
func (db *DB) applyMigration(ctx context.Context, fsys fs.FS, name string) (bool, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction for migration %s: %w", name, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed; nothing actionable if the connection is already gone

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
		return false, fmt.Errorf("acquire migration lock for %s: %w", name, err)
	}

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1)`, name,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	if exists {
		return false, nil
	}

	body, err := fs.ReadFile(fsys, "migrations/"+name)
	if err != nil {
		return false, fmt.Errorf("read migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, string(body)); err != nil {
		return false, fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (name) VALUES ($1)`, name,
	); err != nil {
		return false, fmt.Errorf("record migration %s: %w", name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", name, err)
	}
	return true, nil
}
