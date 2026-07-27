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

// LabelStore reads and writes labels: named, reusable, renameable tags on
// tasks. Renaming updates the one row every task wearing it already points
// at, which is the entire reason labels are a real table instead of a
// string array on the task.
type LabelStore struct{ db *DB }

// NewLabelStore wires a LabelStore to the pool.
func NewLabelStore(db *DB) *LabelStore { return &LabelStore{db: db} }

const labelColumns = `id, name, created_at`

// List returns every label, ordered by name.
func (s *LabelStore) List(ctx context.Context) ([]domain.Label, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+labelColumns+` FROM labels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	defer rows.Close()

	out := []domain.Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Create inserts a new label. A duplicate name (case-sensitive, matching
// the column's UNIQUE constraint) returns domain.ErrConflict.
func (s *LabelStore) Create(ctx context.Context, name string) (domain.Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Label{}, fmt.Errorf("%w: label name is required", domain.ErrInvalidInput)
	}
	row := s.db.Pool.QueryRow(ctx, `INSERT INTO labels (id, name) VALUES ($1,$2) RETURNING `+labelColumns, uuid.New(), name)
	l, err := scanLabel(row)
	if err != nil {
		return domain.Label{}, mapPgError(err)
	}
	slog.Info("label created", "label_id", l.ID, "name", l.Name)
	return l, nil
}

// Rename changes a label's name in place, or returns domain.ErrNotFound.
func (s *LabelStore) Rename(ctx context.Context, id uuid.UUID, name string) (domain.Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Label{}, fmt.Errorf("%w: label name is required", domain.ErrInvalidInput)
	}
	row := s.db.Pool.QueryRow(ctx, `UPDATE labels SET name=$2 WHERE id=$1 RETURNING `+labelColumns, id, name)
	l, err := scanLabel(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Label{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Label{}, mapPgError(err)
	}
	slog.Info("label renamed", "label_id", id, "name", name)
	return l, nil
}

// Delete removes a label, along with its task_labels associations (ON
// DELETE CASCADE), or returns domain.ErrNotFound.
func (s *LabelStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM labels WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete label %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	slog.Info("label deleted", "label_id", id)
	return nil
}

func scanLabel(s scanner) (domain.Label, error) {
	var l domain.Label
	if err := s.Scan(&l.ID, &l.Name, &l.CreatedAt); err != nil {
		return domain.Label{}, fmt.Errorf("scan label: %w", err)
	}
	return l, nil
}
