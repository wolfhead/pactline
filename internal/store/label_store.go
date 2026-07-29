package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

const labelColumns = `id, name, version, created_at`

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
	return s.CreateWithOperation(ctx, name, domain.OperationActor{})
}

func (s *LabelStore) CreateWithOperation(
	ctx context.Context,
	name string,
	actor domain.OperationActor,
) (domain.Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Label{}, fmt.Errorf("%w: label name is required", domain.ErrInvalidInput)
	}
	if actor.UserID == uuid.Nil {
		row := s.db.Pool.QueryRow(ctx, `INSERT INTO labels (id, name) VALUES ($1,$2) RETURNING `+labelColumns, uuid.New(), name)
		l, err := scanLabel(row)
		if err != nil {
			return domain.Label{}, mapPgError(err)
		}
		slog.Info("label created", "label_id", l.ID, "name", l.Name)
		return l, nil
	}
	if err := actor.Validate(); err != nil {
		return domain.Label{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Label{}, fmt.Errorf("begin create label: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	row := tx.QueryRow(ctx, `INSERT INTO labels (id, name) VALUES ($1,$2) RETURNING `+labelColumns, uuid.New(), name)
	l, err := scanLabel(row)
	if err != nil {
		return domain.Label{}, mapPgError(err)
	}
	newValue, _ := json.Marshal(map[string]any{"name": l.Name})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "label",
		EntityID: l.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return domain.Label{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Label{}, fmt.Errorf("commit create label: %w", err)
	}
	slog.Info("label created", "label_id", l.ID, "name", l.Name)
	return l, nil
}

// Rename changes a label's name in place, or returns domain.ErrNotFound.
func (s *LabelStore) Rename(ctx context.Context, id uuid.UUID, name string) (domain.Label, error) {
	return s.RenameWithOperation(ctx, id, name, domain.OperationActor{})
}

func (s *LabelStore) RenameWithOperation(
	ctx context.Context,
	id uuid.UUID,
	name string,
	actor domain.OperationActor,
) (domain.Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Label{}, fmt.Errorf("%w: label name is required", domain.ErrInvalidInput)
	}
	if actor.UserID == uuid.Nil {
		row := s.db.Pool.QueryRow(ctx, `
			UPDATE labels SET name=$2, version=version+1
			WHERE id=$1 RETURNING `+labelColumns,
			id, name,
		)
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
	return s.renameVersionedWithOperation(ctx, id, nil, name, actor)
}

func (s *LabelStore) RenameVersionedWithOperation(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	name string,
	actor domain.OperationActor,
) (domain.Label, error) {
	return s.renameVersionedWithOperation(ctx, id, &expectedVersion, name, actor)
}

func (s *LabelStore) renameVersionedWithOperation(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion *int64,
	name string,
	actor domain.OperationActor,
) (domain.Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Label{}, fmt.Errorf("%w: label name is required", domain.ErrInvalidInput)
	}
	if err := actor.Validate(); err != nil {
		return domain.Label{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Label{}, fmt.Errorf("begin rename label: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var oldName string
	var oldVersion int64
	err = tx.QueryRow(ctx, `SELECT name, version FROM labels WHERE id=$1 FOR UPDATE`, id).
		Scan(&oldName, &oldVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Label{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Label{}, fmt.Errorf("lock label %s: %w", id, err)
	}
	if expectedVersion != nil && oldVersion != *expectedVersion {
		return domain.Label{}, domain.VersionConflictError{CurrentVersion: oldVersion}
	}
	row := tx.QueryRow(ctx, `
		UPDATE labels SET name=$2, version=version+1
		WHERE id=$1 AND version=$3 RETURNING `+labelColumns,
		id, name, oldVersion,
	)
	l, err := scanLabel(row)
	if err != nil {
		return domain.Label{}, mapPgError(err)
	}
	oldValue, _ := json.Marshal(map[string]any{"name": oldName})
	newValue, _ := json.Marshal(map[string]any{"name": l.Name})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "label",
		EntityID: l.ID, Action: "renamed", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.Label{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Label{}, fmt.Errorf("commit rename label: %w", err)
	}
	slog.Info("label renamed", "label_id", id, "name", name)
	return l, nil
}

// Delete removes a label, along with its task_labels associations (ON
// DELETE CASCADE), or returns domain.ErrNotFound.
func (s *LabelStore) Delete(ctx context.Context, id uuid.UUID) error {
	return s.DeleteWithOperation(ctx, id, domain.OperationActor{})
}

func (s *LabelStore) DeleteWithOperation(
	ctx context.Context,
	id uuid.UUID,
	actor domain.OperationActor,
) error {
	if actor.UserID == uuid.Nil {
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
	return s.deleteVersionedWithOperation(ctx, id, nil, actor)
}

func (s *LabelStore) DeleteVersionedWithOperation(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	actor domain.OperationActor,
) error {
	return s.deleteVersionedWithOperation(ctx, id, &expectedVersion, actor)
}

func (s *LabelStore) deleteVersionedWithOperation(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion *int64,
	actor domain.OperationActor,
) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete label: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var oldName string
	var oldVersion int64
	err = tx.QueryRow(ctx, `SELECT name, version FROM labels WHERE id=$1 FOR UPDATE`, id).
		Scan(&oldName, &oldVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock label %s: %w", id, err)
	}
	if expectedVersion != nil && oldVersion != *expectedVersion {
		return domain.VersionConflictError{CurrentVersion: oldVersion}
	}
	type affectedTask struct {
		ID      uuid.UUID
		Number  int64
		Version int64
	}
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.number, t.version
		FROM tasks t
		JOIN task_labels tl ON tl.task_id=t.id
		WHERE tl.label_id=$1
		ORDER BY t.id
		FOR UPDATE OF t`,
		id,
	)
	if err != nil {
		return fmt.Errorf("lock tasks affected by label %s: %w", id, err)
	}
	var affected []affectedTask
	for rows.Next() {
		var task affectedTask
		if err := rows.Scan(&task.ID, &task.Number, &task.Version); err != nil {
			rows.Close()
			return fmt.Errorf("scan task affected by label %s: %w", id, err)
		}
		affected = append(affected, task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list tasks affected by label %s: %w", id, err)
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `
		DELETE FROM labels WHERE id=$1 AND version=$2`,
		id, oldVersion,
	); err != nil {
		return fmt.Errorf("delete label %s: %w", id, err)
	}
	for _, task := range affected {
		if _, err := tx.Exec(ctx, `
			UPDATE tasks SET version=version+1, updated_at=now()
			WHERE id=$1 AND version=$2`,
			task.ID, task.Version,
		); err != nil {
			return fmt.Errorf("increment task %s after label deletion: %w", task.ID, err)
		}
		oldTaskValue, _ := json.Marshal(map[string]any{
			"label_id": id, "label_name": oldName,
		})
		if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
			OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "task",
			EntityID: task.ID, EntityNumber: &task.Number, Action: "label_removed",
			OldValue: oldTaskValue,
		}); err != nil {
			return err
		}
	}
	oldValue, _ := json.Marshal(map[string]any{"name": oldName})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "label",
		EntityID: id, Action: "deleted", OldValue: oldValue,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete label: %w", err)
	}
	slog.Info("label deleted", "label_id", id)
	return nil
}

func scanLabel(s scanner) (domain.Label, error) {
	var l domain.Label
	if err := s.Scan(&l.ID, &l.Name, &l.Version, &l.CreatedAt); err != nil {
		return domain.Label{}, fmt.Errorf("scan label: %w", err)
	}
	return l, nil
}
