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

// CommentStore reads and writes comments on tasks. A comment is editable and
// deletable only by its own author — enforced here, the one place that
// decides it, rather than in every handler that touches a comment.
type CommentStore struct{ db *DB }

// NewCommentStore wires a CommentStore to the pool.
func NewCommentStore(db *DB) *CommentStore { return &CommentStore{db: db} }

const commentColumns = `id, task_id, author_id, body, created_at, updated_at`

// List returns every comment on a task, oldest first.
func (s *CommentStore) List(ctx context.Context, taskID uuid.UUID) ([]domain.Comment, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+commentColumns+` FROM task_comments WHERE task_id=$1 ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list comments for task %s: %w", taskID, err)
	}
	defer rows.Close()

	out := []domain.Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Create adds a comment to a task.
func (s *CommentStore) Create(ctx context.Context, taskID, authorID uuid.UUID, body string) (domain.Comment, error) {
	return s.CreateWithOperation(
		ctx, taskID, authorID, body, domain.SessionOperation(authorID, "internal"),
	)
}

func (s *CommentStore) CreateWithOperation(
	ctx context.Context,
	taskID, authorID uuid.UUID,
	body string,
	actor domain.OperationActor,
) (domain.Comment, error) {
	if strings.TrimSpace(body) == "" {
		return domain.Comment{}, fmt.Errorf("%w: comment body is required", domain.ErrInvalidInput)
	}
	if err := actor.Validate(); err != nil {
		return domain.Comment{}, err
	}
	if authorID != actor.UserID {
		return domain.Comment{}, fmt.Errorf("%w: comment author must match operation actor", domain.ErrForbidden)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Comment{}, fmt.Errorf("begin create comment: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	row := tx.QueryRow(ctx,
		`INSERT INTO task_comments (id, task_id, author_id, body) VALUES ($1,$2,$3,$4) RETURNING `+commentColumns,
		uuid.New(), taskID, authorID, body)
	c, err := scanComment(row)
	if err != nil {
		return domain.Comment{}, mapPgError(err)
	}
	newValue, _ := json.Marshal(map[string]any{"task_id": taskID, "body": c.Body})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "comment",
		EntityID: c.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return domain.Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, fmt.Errorf("commit create comment: %w", err)
	}
	slog.Info("comment created", "comment_id", c.ID, "task_id", taskID, "author_id", authorID)
	return c, nil
}

// Update rewrites a comment's body. taskID scopes the lookup to the task the
// caller believes the comment belongs to, so a comment ID from one task can
// never be edited through another task's URL. Returns domain.ErrNotFound if
// no such comment exists under that task, or domain.ErrForbidden if actorID
// is not the comment's author.
func (s *CommentStore) Update(ctx context.Context, taskID, id, actorID uuid.UUID, body string) (domain.Comment, error) {
	return s.UpdateWithOperation(
		ctx, taskID, id, body, domain.SessionOperation(actorID, "internal"),
	)
}

func (s *CommentStore) UpdateWithOperation(
	ctx context.Context,
	taskID, id uuid.UUID,
	body string,
	actor domain.OperationActor,
) (domain.Comment, error) {
	if strings.TrimSpace(body) == "" {
		return domain.Comment{}, fmt.Errorf("%w: comment body is required", domain.ErrInvalidInput)
	}
	if err := actor.Validate(); err != nil {
		return domain.Comment{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Comment{}, fmt.Errorf("begin update comment: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var author uuid.UUID
	var oldBody string
	err = tx.QueryRow(ctx,
		`SELECT author_id, body FROM task_comments WHERE id=$1 AND task_id=$2 FOR UPDATE`,
		id, taskID,
	).Scan(&author, &oldBody)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Comment{}, fmt.Errorf("lock comment %s: %w", id, err)
	}
	if author != actor.UserID {
		return domain.Comment{}, domain.ErrForbidden
	}
	row := tx.QueryRow(ctx, `UPDATE task_comments SET body=$2, updated_at=now() WHERE id=$1 RETURNING `+commentColumns, id, body)
	c, err := scanComment(row)
	if err != nil {
		return domain.Comment{}, mapPgError(err)
	}
	oldValue, _ := json.Marshal(map[string]any{"body": oldBody})
	newValue, _ := json.Marshal(map[string]any{"body": c.Body})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "comment",
		EntityID: c.ID, Action: "updated", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, fmt.Errorf("commit update comment: %w", err)
	}
	slog.Info("comment updated", "comment_id", id, "task_id", taskID, "actor_id", actor.UserID)
	return c, nil
}

// Delete removes a comment. See Update for the taskID scoping and error
// semantics.
func (s *CommentStore) Delete(ctx context.Context, taskID, id, actorID uuid.UUID) error {
	return s.DeleteWithOperation(
		ctx, taskID, id, domain.SessionOperation(actorID, "internal"),
	)
}

func (s *CommentStore) DeleteWithOperation(
	ctx context.Context,
	taskID, id uuid.UUID,
	actor domain.OperationActor,
) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete comment: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var author uuid.UUID
	var body string
	err = tx.QueryRow(ctx,
		`SELECT author_id, body FROM task_comments WHERE id=$1 AND task_id=$2 FOR UPDATE`,
		id, taskID,
	).Scan(&author, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock comment %s: %w", id, err)
	}
	if author != actor.UserID {
		return domain.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `DELETE FROM task_comments WHERE id=$1`, id); err != nil {
		return fmt.Errorf("delete comment %s: %w", id, err)
	}
	oldValue, _ := json.Marshal(map[string]any{"task_id": taskID, "body": body})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "comment",
		EntityID: id, Action: "deleted", OldValue: oldValue,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete comment: %w", err)
	}
	slog.Info("comment deleted", "comment_id", id, "task_id", taskID, "actor_id", actor.UserID)
	return nil
}

func scanComment(s scanner) (domain.Comment, error) {
	var c domain.Comment
	if err := s.Scan(&c.ID, &c.TaskID, &c.AuthorID, &c.Body, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return domain.Comment{}, fmt.Errorf("scan comment: %w", err)
	}
	return c, nil
}
