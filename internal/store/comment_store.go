package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/events"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CommentStore owns comment authorship, thread integrity, Project-member
// mention validation, and the atomic creation of notification outbox events.
type CommentStore struct{ db *DB }

func NewCommentStore(db *DB) *CommentStore { return &CommentStore{db: db} }

const commentColumns = `id, task_id, author_id, body, reply_to_comment_id,
thread_root_id, deleted_at, version, created_at, updated_at`

type CommentCreation struct {
	Comment     domain.Comment
	TaskVersion int64
}

func (s *CommentStore) List(ctx context.Context, taskID uuid.UUID) ([]domain.Comment, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+commentColumns+`,
		ARRAY(SELECT cm.user_id FROM comment_mentions cm WHERE cm.comment_id=task_comments.id ORDER BY cm.user_id)
		FROM task_comments WHERE task_id=$1 ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list comments for task %s: %w", taskID, err)
	}
	defer rows.Close()
	comments := []domain.Comment{}
	for rows.Next() {
		comment, err := scanCommentWithMentions(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

func (s *CommentStore) Create(ctx context.Context, taskID, authorID uuid.UUID, body string) (domain.Comment, error) {
	return s.CreateWithOperation(ctx, taskID, authorID, body, domain.SessionOperation(authorID, "internal"))
}

func (s *CommentStore) CreateWithOperation(
	ctx context.Context, taskID, authorID uuid.UUID, body string, actor domain.OperationActor,
) (domain.Comment, error) {
	created, err := s.createWithExpectedTaskVersion(ctx, taskID, nil, authorID, body, nil, nil, actor)
	return created.Comment, err
}

func (s *CommentStore) CreateVersionedWithOperation(
	ctx context.Context, taskID uuid.UUID, expectedTaskVersion int64,
	authorID uuid.UUID, body string, actor domain.OperationActor,
) (CommentCreation, error) {
	return s.CreateVersionedThreadedWithOperation(
		ctx, taskID, expectedTaskVersion, authorID, body, nil, nil, actor,
	)
}

func (s *CommentStore) CreateVersionedThreadedWithOperation(
	ctx context.Context, taskID uuid.UUID, expectedTaskVersion int64,
	authorID uuid.UUID, body string, replyTo *uuid.UUID, mentionedUserIDs []uuid.UUID,
	actor domain.OperationActor,
) (CommentCreation, error) {
	return s.createWithExpectedTaskVersion(
		ctx, taskID, &expectedTaskVersion, authorID, body, replyTo, mentionedUserIDs, actor,
	)
}

func (s *CommentStore) createWithExpectedTaskVersion(
	ctx context.Context, taskID uuid.UUID, expectedTaskVersion *int64,
	authorID uuid.UUID, body string, replyTo *uuid.UUID, mentionedUserIDs []uuid.UUID,
	actor domain.OperationActor,
) (CommentCreation, error) {
	if strings.TrimSpace(body) == "" {
		return CommentCreation{}, fmt.Errorf("%w: comment body is required", domain.ErrInvalidInput)
	}
	if err := actor.Validate(); err != nil {
		return CommentCreation{}, err
	}
	if authorID != actor.UserID {
		return CommentCreation{}, fmt.Errorf("%w: comment author must match operation actor", domain.ErrForbidden)
	}
	mentionedUserIDs, err := uniqueUserIDs(mentionedUserIDs)
	if err != nil {
		return CommentCreation{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return CommentCreation{}, fmt.Errorf("begin create comment: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var taskVersion int64
	var projectID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT version, project_id FROM tasks WHERE id=$1 FOR UPDATE`, taskID).Scan(&taskVersion, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentCreation{}, domain.ErrNotFound
	}
	if err != nil {
		return CommentCreation{}, fmt.Errorf("lock task %s for comment: %w", taskID, err)
	}
	if expectedTaskVersion != nil && taskVersion != *expectedTaskVersion {
		return CommentCreation{}, domain.VersionConflictError{CurrentVersion: taskVersion}
	}
	if err := validateMentionedProjectMembers(ctx, tx, projectID, mentionedUserIDs); err != nil {
		return CommentCreation{}, err
	}
	commentID := uuid.New()
	threadRootID := commentID
	var replyAuthorID *uuid.UUID
	if replyTo != nil {
		var rootID uuid.UUID
		var parentAuthor uuid.UUID
		err := tx.QueryRow(ctx, `SELECT thread_root_id, author_id FROM task_comments
			WHERE id=$1 AND task_id=$2`, *replyTo, taskID).Scan(&rootID, &parentAuthor)
		if errors.Is(err, pgx.ErrNoRows) {
			return CommentCreation{}, fmt.Errorf("%w: reply target is not a comment on this task", domain.ErrInvalidInput)
		}
		if err != nil {
			return CommentCreation{}, fmt.Errorf("resolve reply target: %w", err)
		}
		threadRootID = rootID
		replyAuthorID = &parentAuthor
	}
	row := tx.QueryRow(ctx, `INSERT INTO task_comments (
		id, task_id, author_id, body, reply_to_comment_id, thread_root_id
	) VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+commentColumns,
		commentID, taskID, authorID, body, replyTo, threadRootID,
	)
	comment, err := scanComment(row)
	if err != nil {
		return CommentCreation{}, mapPgError(err)
	}
	if err := replaceCommentMentions(ctx, tx, comment.ID, mentionedUserIDs); err != nil {
		return CommentCreation{}, err
	}
	comment.MentionedUserIDs = mentionedUserIDs
	if err := insertCommentEvents(ctx, tx, comment, projectID, authorID, replyAuthorID, mentionedUserIDs, nil); err != nil {
		return CommentCreation{}, err
	}
	if err := tx.QueryRow(ctx, `UPDATE tasks SET version=version+1, updated_at=now()
		WHERE id=$1 AND version=$2 RETURNING version`, taskID, taskVersion).Scan(&taskVersion); err != nil {
		return CommentCreation{}, fmt.Errorf("increment task version for comment: %w", err)
	}
	newValue, _ := json.Marshal(map[string]any{
		"task_id": taskID, "body": comment.Body, "reply_to_comment_id": replyTo,
		"mentioned_user_ids": mentionedUserIDs,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "comment",
		EntityID: comment.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return CommentCreation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommentCreation{}, fmt.Errorf("commit create comment: %w", err)
	}
	slog.Info("comment created", "comment_id", comment.ID, "task_id", taskID,
		"author_id", authorID, "mention_count", len(mentionedUserIDs), "is_reply", replyTo != nil)
	return CommentCreation{Comment: comment, TaskVersion: taskVersion}, nil
}

func (s *CommentStore) Update(ctx context.Context, taskID, id, actorID uuid.UUID, body string) (domain.Comment, error) {
	return s.UpdateWithOperation(ctx, taskID, id, body, domain.SessionOperation(actorID, "internal"))
}

func (s *CommentStore) UpdateWithOperation(
	ctx context.Context, taskID, id uuid.UUID, body string, actor domain.OperationActor,
) (domain.Comment, error) {
	return s.updateWithExpectedVersion(ctx, taskID, id, nil, body, nil, actor)
}

func (s *CommentStore) UpdateVersionedWithOperation(
	ctx context.Context, taskID, id uuid.UUID, expectedVersion int64,
	body string, actor domain.OperationActor,
) (domain.Comment, error) {
	return s.updateWithExpectedVersion(ctx, taskID, id, &expectedVersion, body, nil, actor)
}

func (s *CommentStore) UpdateVersionedMentionedWithOperation(
	ctx context.Context, taskID, id uuid.UUID, expectedVersion int64,
	body string, mentionedUserIDs []uuid.UUID, actor domain.OperationActor,
) (domain.Comment, error) {
	return s.updateWithExpectedVersion(ctx, taskID, id, &expectedVersion, body, mentionedUserIDs, actor)
}

func (s *CommentStore) updateWithExpectedVersion(
	ctx context.Context, taskID, id uuid.UUID, expectedVersion *int64,
	body string, mentionedUserIDs []uuid.UUID, actor domain.OperationActor,
) (domain.Comment, error) {
	if strings.TrimSpace(body) == "" {
		return domain.Comment{}, fmt.Errorf("%w: comment body is required", domain.ErrInvalidInput)
	}
	if err := actor.Validate(); err != nil {
		return domain.Comment{}, err
	}
	mentionedUserIDs, err := uniqueUserIDs(mentionedUserIDs)
	if err != nil {
		return domain.Comment{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.Comment{}, fmt.Errorf("begin update comment: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var author, projectID uuid.UUID
	var oldBody string
	var oldVersion int64
	err = tx.QueryRow(ctx, `SELECT c.author_id, c.body, c.version, t.project_id
		FROM task_comments c JOIN tasks t ON t.id=c.task_id
		WHERE c.id=$1 AND c.task_id=$2 AND c.deleted_at IS NULL FOR UPDATE OF c`, id, taskID).
		Scan(&author, &oldBody, &oldVersion, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Comment{}, fmt.Errorf("lock comment %s: %w", id, err)
	}
	if author != actor.UserID {
		return domain.Comment{}, domain.ErrForbidden
	}
	if expectedVersion != nil && oldVersion != *expectedVersion {
		return domain.Comment{}, domain.VersionConflictError{CurrentVersion: oldVersion}
	}
	if err := validateMentionedProjectMembers(ctx, tx, projectID, mentionedUserIDs); err != nil {
		return domain.Comment{}, err
	}
	oldMentions, err := listCommentMentions(ctx, tx, id)
	if err != nil {
		return domain.Comment{}, err
	}
	row := tx.QueryRow(ctx, `UPDATE task_comments SET body=$2, version=version+1, updated_at=now()
		WHERE id=$1 AND version=$3 RETURNING `+commentColumns, id, body, oldVersion)
	comment, err := scanComment(row)
	if err != nil {
		return domain.Comment{}, mapPgError(err)
	}
	if err := replaceCommentMentions(ctx, tx, comment.ID, mentionedUserIDs); err != nil {
		return domain.Comment{}, err
	}
	comment.MentionedUserIDs = mentionedUserIDs
	addedMentions := difference(mentionedUserIDs, oldMentions)
	if err := insertCommentEvents(ctx, tx, comment, projectID, author, nil, addedMentions, oldMentions); err != nil {
		return domain.Comment{}, err
	}
	oldValue, _ := json.Marshal(map[string]any{"body": oldBody, "mentioned_user_ids": oldMentions})
	newValue, _ := json.Marshal(map[string]any{"body": comment.Body, "mentioned_user_ids": mentionedUserIDs})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "comment",
		EntityID: comment.ID, Action: "updated", OldValue: oldValue, NewValue: newValue,
	}); err != nil {
		return domain.Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, fmt.Errorf("commit update comment: %w", err)
	}
	slog.Info("comment updated", "comment_id", id, "task_id", taskID,
		"actor_id", actor.UserID, "new_mention_count", len(addedMentions))
	return comment, nil
}

func (s *CommentStore) Delete(ctx context.Context, taskID, id, actorID uuid.UUID) error {
	return s.DeleteWithOperation(ctx, taskID, id, domain.SessionOperation(actorID, "internal"))
}

func (s *CommentStore) DeleteWithOperation(
	ctx context.Context, taskID, id uuid.UUID, actor domain.OperationActor,
) error {
	return s.deleteWithExpectedVersion(ctx, taskID, id, nil, actor)
}

func (s *CommentStore) DeleteVersionedWithOperation(
	ctx context.Context, taskID, id uuid.UUID, expectedVersion int64, actor domain.OperationActor,
) error {
	return s.deleteWithExpectedVersion(ctx, taskID, id, &expectedVersion, actor)
}

func (s *CommentStore) deleteWithExpectedVersion(
	ctx context.Context, taskID, id uuid.UUID, expectedVersion *int64, actor domain.OperationActor,
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
	var oldVersion int64
	err = tx.QueryRow(ctx, `SELECT author_id, body, version FROM task_comments
		WHERE id=$1 AND task_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, taskID).
		Scan(&author, &body, &oldVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock comment %s: %w", id, err)
	}
	if author != actor.UserID {
		return domain.ErrForbidden
	}
	if expectedVersion != nil && oldVersion != *expectedVersion {
		return domain.VersionConflictError{CurrentVersion: oldVersion}
	}
	if _, err := tx.Exec(ctx, `UPDATE task_comments
		SET deleted_at=now(), version=version+1, updated_at=now()
		WHERE id=$1 AND version=$2`, id, oldVersion); err != nil {
		return fmt.Errorf("soft-delete comment %s: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM comment_mentions WHERE comment_id=$1`, id); err != nil {
		return fmt.Errorf("delete comment mentions: %w", err)
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
	slog.Info("comment soft-deleted", "comment_id", id, "task_id", taskID, "actor_id", actor.UserID)
	return nil
}

func validateMentionedProjectMembers(
	ctx context.Context, tx pgx.Tx, projectID uuid.UUID, userIDs []uuid.UUID,
) error {
	if len(userIDs) == 0 {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_memberships pm
		JOIN users u ON u.id=pm.user_id
		WHERE pm.project_id=$1 AND pm.user_id=ANY($2) AND u.active`, projectID, userIDs).Scan(&count); err != nil {
		return fmt.Errorf("validate mentioned project members: %w", err)
	}
	if count != len(userIDs) {
		return fmt.Errorf("%w: every mentioned user must be an active Project member", domain.ErrInvalidInput)
	}
	return nil
}

func replaceCommentMentions(ctx context.Context, tx pgx.Tx, commentID uuid.UUID, userIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM comment_mentions WHERE comment_id=$1`, commentID); err != nil {
		return fmt.Errorf("replace comment mentions: %w", err)
	}
	for _, userID := range userIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO comment_mentions (comment_id, user_id) VALUES ($1,$2)`, commentID, userID); err != nil {
			return fmt.Errorf("insert comment mention: %w", err)
		}
	}
	return nil
}

func listCommentMentions(ctx context.Context, tx pgx.Tx, commentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT user_id FROM comment_mentions WHERE comment_id=$1 ORDER BY user_id`, commentID)
	if err != nil {
		return nil, fmt.Errorf("list comment mentions: %w", err)
	}
	defer rows.Close()
	userIDs := []uuid.UUID{}
	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, rows.Err()
}

func insertCommentEvents(
	ctx context.Context, tx pgx.Tx, comment domain.Comment, projectID, authorID uuid.UUID,
	replyAuthorID *uuid.UUID, explicitMentions, _ []uuid.UUID,
) error {
	mentioned := make(map[uuid.UUID]struct{}, len(explicitMentions))
	for _, recipientID := range explicitMentions {
		mentioned[recipientID] = struct{}{}
		if recipientID == authorID {
			continue
		}
		if err := insertCommentEvent(ctx, tx, comment, projectID, recipientID, events.CommentMentioned); err != nil {
			return err
		}
	}
	if replyAuthorID != nil && *replyAuthorID != authorID {
		if _, explicitlyMentioned := mentioned[*replyAuthorID]; !explicitlyMentioned {
			if err := insertCommentEvent(ctx, tx, comment, projectID, *replyAuthorID, events.CommentReplied); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertCommentEvent(
	ctx context.Context, tx pgx.Tx, comment domain.Comment, projectID, recipientID uuid.UUID, eventType string,
) error {
	occurredAt := time.Now().UTC()
	event, err := events.New(events.NewEvent{
		AggregateType: "comment", AggregateID: comment.ID,
		Type: eventType, RecipientID: recipientID,
		Payload: events.CommentPayload{
			ProjectID: projectID, TaskID: comment.TaskID, CommentID: comment.ID,
			CommentAuthorID: comment.AuthorID, ReplyToCommentID: comment.ReplyToCommentID,
			OccurredAt: occurredAt,
		},
		DedupeKey: fmt.Sprintf("%s:%s:%d:%s", eventType, comment.ID, comment.Version, recipientID),
		CreatedAt: occurredAt,
	})
	if err != nil {
		return fmt.Errorf("build comment event: %w", err)
	}
	return insertEvent(ctx, tx, event)
}

func uniqueUserIDs(userIDs []uuid.UUID) ([]uuid.UUID, error) {
	result := append([]uuid.UUID{}, userIDs...)
	slices.SortFunc(result, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for index, userID := range result {
		if userID == uuid.Nil {
			return nil, fmt.Errorf("%w: mentioned user ID is required", domain.ErrInvalidInput)
		}
		if index > 0 && result[index-1] == userID {
			return nil, fmt.Errorf("%w: mentioned users must not contain duplicates", domain.ErrInvalidInput)
		}
	}
	return result, nil
}

func difference(current, previous []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(previous))
	for _, userID := range previous {
		seen[userID] = struct{}{}
	}
	added := make([]uuid.UUID, 0, len(current))
	for _, userID := range current {
		if _, exists := seen[userID]; !exists {
			added = append(added, userID)
		}
	}
	return added
}

func scanComment(scanner scanner) (domain.Comment, error) {
	var comment domain.Comment
	if err := scanner.Scan(
		&comment.ID, &comment.TaskID, &comment.AuthorID, &comment.Body,
		&comment.ReplyToCommentID, &comment.ThreadRootID, &comment.DeletedAt,
		&comment.Version, &comment.CreatedAt, &comment.UpdatedAt,
	); err != nil {
		return domain.Comment{}, fmt.Errorf("scan comment: %w", err)
	}
	if comment.DeletedAt != nil {
		comment.Body = ""
	}
	return comment, nil
}

func scanCommentWithMentions(scanner scanner) (domain.Comment, error) {
	var comment domain.Comment
	if err := scanner.Scan(
		&comment.ID, &comment.TaskID, &comment.AuthorID, &comment.Body,
		&comment.ReplyToCommentID, &comment.ThreadRootID, &comment.DeletedAt,
		&comment.Version, &comment.CreatedAt, &comment.UpdatedAt,
		&comment.MentionedUserIDs,
	); err != nil {
		return domain.Comment{}, fmt.Errorf("scan comment with mentions: %w", err)
	}
	if comment.DeletedAt != nil {
		comment.Body = ""
		comment.MentionedUserIDs = nil
	}
	return comment, nil
}
