package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"bountyboard/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AnchorStore reads and writes the definitional anchor list (spec §4.7):
// plain CRUD over reference examples, one list per dimension and level, no
// suggestion logic and no auto-promotion.
type AnchorStore struct{ db *DB }

// NewAnchorStore wires an AnchorStore to the pool.
func NewAnchorStore(db *DB) *AnchorStore { return &AnchorStore{db: db} }

const anchorColumns = `id, dimension, level, bounty_id, note, created_at`

// AnchorFilter narrows a List query. Zero value means "everything".
type AnchorFilter struct {
	Dimension domain.AnchorDimension
	Level     string
}

// Create inserts an anchor example, assigning an ID when absent.
func (s *AnchorStore) Create(ctx context.Context, a domain.AnchorExample) (domain.AnchorExample, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO anchor_examples (id, dimension, level, bounty_id, note)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING `+anchorColumns,
		a.ID, a.Dimension, a.Level, a.BountyID, a.Note)
	out, err := scanAnchor(row)
	if err != nil {
		return domain.AnchorExample{}, err
	}
	slog.Info("anchor example created",
		"anchor_id", out.ID, "dimension", out.Dimension, "level", out.Level, "bounty_id", out.BountyID)
	return out, nil
}

// GetByID returns one anchor example, or domain.ErrNotFound.
func (s *AnchorStore) GetByID(ctx context.Context, id uuid.UUID) (domain.AnchorExample, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+anchorColumns+` FROM anchor_examples WHERE id=$1`, id)
	a, err := scanAnchor(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnchorExample{}, domain.ErrNotFound
	}
	return a, err
}

// List returns anchor examples matching the filter, oldest first within each
// dimension/level so the earliest-established precedent sorts first.
func (s *AnchorStore) List(ctx context.Context, f AnchorFilter) ([]domain.AnchorExample, error) {
	var (
		where []string
		args  []any
	)
	add := func(clause string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.Dimension != "" {
		add("dimension = $%d", f.Dimension)
	}
	if f.Level != "" {
		add("level = $%d", f.Level)
	}

	q := `SELECT ` + anchorColumns + ` FROM anchor_examples`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY dimension, level, created_at"

	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query anchor examples: %w", err)
	}
	defer rows.Close()

	out := []domain.AnchorExample{}
	for rows.Next() {
		a, err := scanAnchor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Update overwrites an anchor example's mutable fields (level and note; the
// dimension and bounty pinned to it are the identity of the precedent and are
// not changed in place — delete and recreate instead if those are wrong).
func (s *AnchorStore) Update(ctx context.Context, a domain.AnchorExample) (domain.AnchorExample, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE anchor_examples SET level=$2, note=$3
		WHERE id=$1
		RETURNING `+anchorColumns,
		a.ID, a.Level, a.Note)
	out, err := scanAnchor(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnchorExample{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AnchorExample{}, err
	}
	slog.Info("anchor example updated", "anchor_id", out.ID, "level", out.Level)
	return out, nil
}

// Delete removes an anchor example.
func (s *AnchorStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM anchor_examples WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete anchor example: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	slog.Info("anchor example deleted", "anchor_id", id)
	return nil
}

func scanAnchor(s scanner) (domain.AnchorExample, error) {
	var a domain.AnchorExample
	if err := s.Scan(&a.ID, &a.Dimension, &a.Level, &a.BountyID, &a.Note, &a.CreatedAt); err != nil {
		return domain.AnchorExample{}, fmt.Errorf("scan anchor example: %w", err)
	}
	return a, nil
}
