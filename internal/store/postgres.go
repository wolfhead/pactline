// Package store provides PostgreSQL-backed persistence.
package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB owns the connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

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
