package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TaskThreadStore struct{ db *DB }

func NewTaskThreadStore(db *DB) *TaskThreadStore { return &TaskThreadStore{db: db} }

const taskThreadColumns = `thread.id, thread.task_id, thread.role,
	thread.issue_type, thread.issue_status, thread.opened_from_phase,
	thread.opened_by_type, thread.opened_by_user_id, thread.opened_by_ref,
	thread.resolved_by_type, thread.resolved_by_user_id, thread.resolved_by_ref,
	thread.version, thread.created_at, thread.updated_at, thread.resolved_at`

func scanTaskThread(row scanner) (domain.Thread, error) {
	var (
		thread                                  domain.Thread
		issueType, issueStatus, openedFromPhase *string
		openedByType, openedByRef               *string
		resolvedByType, resolvedByRef           *string
		openedByUserID, resolvedByUserID        *uuid.UUID
	)
	if err := row.Scan(
		&thread.ID, &thread.TaskID, &thread.Role,
		&issueType, &issueStatus, &openedFromPhase,
		&openedByType, &openedByUserID, &openedByRef,
		&resolvedByType, &resolvedByUserID, &resolvedByRef,
		&thread.Version, &thread.CreatedAt, &thread.UpdatedAt, &thread.ResolvedAt,
	); err != nil {
		return domain.Thread{}, err
	}
	if issueType != nil {
		thread.IssueType = domain.IssueThreadType(*issueType)
	}
	if issueStatus != nil {
		thread.IssueStatus = domain.IssueThreadStatus(*issueStatus)
	}
	if openedFromPhase != nil {
		thread.OpenedFromPhase = domain.TaskPhase(*openedFromPhase)
	}
	if openedByType != nil {
		thread.OpenedBy = actorFromColumns(*openedByType, openedByUserID, openedByRef)
	}
	if resolvedByType != nil {
		resolvedBy := actorFromColumns(*resolvedByType, resolvedByUserID, resolvedByRef)
		thread.ResolvedBy = &resolvedBy
	}
	return thread, nil
}

func (s *TaskThreadStore) GetMainByTaskNumber(
	ctx context.Context,
	taskNumber int64,
) (domain.Thread, error) {
	thread, err := scanTaskThread(s.db.Pool.QueryRow(ctx, `
		SELECT `+taskThreadColumns+`
		FROM task_threads thread
		JOIN tasks task ON task.id=thread.task_id
		WHERE task.number=$1 AND thread.role='main'`, taskNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Thread{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Thread{}, fmt.Errorf("get Main Thread for Task %d: %w", taskNumber, err)
	}
	return thread, nil
}

func (s *TaskThreadStore) GetForTaskNumber(
	ctx context.Context,
	taskNumber int64,
	threadID uuid.UUID,
) (domain.Thread, error) {
	thread, err := scanTaskThread(s.db.Pool.QueryRow(ctx, `
		SELECT `+taskThreadColumns+`
		FROM task_threads thread
		JOIN tasks task ON task.id=thread.task_id
		WHERE task.number=$1 AND thread.id=$2`, taskNumber, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Thread{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Thread{}, fmt.Errorf("get Thread %s for Task %d: %w", threadID, taskNumber, err)
	}
	return thread, nil
}

func (s *TaskThreadStore) Get(ctx context.Context, threadID uuid.UUID) (domain.Thread, error) {
	thread, err := scanTaskThread(s.db.Pool.QueryRow(ctx, `
		SELECT `+taskThreadColumns+`
		FROM task_threads thread
		WHERE thread.id=$1`, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Thread{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Thread{}, fmt.Errorf("get Thread %s: %w", threadID, err)
	}
	return thread, nil
}

func (s *TaskThreadStore) GetThreadForItem(
	ctx context.Context,
	itemID uuid.UUID,
) (domain.Thread, error) {
	thread, err := scanTaskThread(s.db.Pool.QueryRow(ctx, `
		SELECT `+taskThreadColumns+`
		FROM task_threads thread
		JOIN task_thread_items item ON item.thread_id=thread.id
		WHERE item.id=$1`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Thread{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Thread{}, fmt.Errorf("get Thread for Item %s: %w", itemID, err)
	}
	return thread, nil
}

func (s *TaskThreadStore) ListForTaskNumber(
	ctx context.Context,
	taskNumber int64,
) ([]domain.Thread, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+taskThreadColumns+`
		FROM task_threads thread
		JOIN tasks task ON task.id=thread.task_id
		WHERE task.number=$1
		ORDER BY CASE WHEN thread.role='main' THEN 0 ELSE 1 END,
			thread.created_at, thread.id`, taskNumber)
	if err != nil {
		return nil, fmt.Errorf("list Threads for Task %d: %w", taskNumber, err)
	}
	defer rows.Close()
	threads := []domain.Thread{}
	for rows.Next() {
		thread, scanErr := scanTaskThread(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Thread for Task %d: %w", taskNumber, scanErr)
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Threads for Task %d: %w", taskNumber, err)
	}
	return threads, nil
}

func (s *TaskThreadStore) ListItems(
	ctx context.Context,
	threadID uuid.UUID,
) ([]domain.ThreadItem, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT item.id,item.thread_id,item.kind,
			item.author_type,item.author_user_id,item.author_ref,
			item.body,item.typed_payload,item.reply_to_item_id,
			COALESCE(ARRAY(
				SELECT mention.user_id
				FROM task_thread_item_mentions mention
				WHERE mention.item_id=item.id
				ORDER BY mention.user_id
			), ARRAY[]::uuid[]),
			item.version,item.created_at,item.updated_at,item.deleted_at
		FROM task_thread_items item
		WHERE item.thread_id=$1
		ORDER BY item.created_at,item.id`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list Thread Items for Thread %s: %w", threadID, err)
	}
	defer rows.Close()
	items := []domain.ThreadItem{}
	for rows.Next() {
		item, scanErr := scanTaskThreadItem(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan Thread Item for Thread %s: %w", threadID, scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Thread Items for Thread %s: %w", threadID, err)
	}
	return items, nil
}

func scanTaskThreadItem(row scanner) (domain.ThreadItem, error) {
	var (
		item         domain.ThreadItem
		authorType   string
		authorUserID *uuid.UUID
		authorRef    *string
		body         *string
		typedPayload []byte
	)
	if err := row.Scan(
		&item.ID, &item.ThreadID, &item.Kind,
		&authorType, &authorUserID, &authorRef,
		&body, &typedPayload, &item.ReplyToItemID, &item.MentionedUserIDs,
		&item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
	); err != nil {
		return domain.ThreadItem{}, err
	}
	item.Author = actorFromColumns(authorType, authorUserID, authorRef)
	if body != nil {
		item.Body = *body
	}
	if len(typedPayload) > 0 {
		var payload domain.IssueResolutionPayload
		if err := json.Unmarshal(typedPayload, &payload); err != nil {
			return domain.ThreadItem{}, fmt.Errorf("decode Issue resolution payload: %w", err)
		}
		item.IssueResolution = &payload
	}
	return item, nil
}

func actorFromColumns(actorType string, userID *uuid.UUID, ref *string) domain.Actor {
	actor := domain.Actor{Type: domain.ActorType(actorType), UserID: userID}
	if ref != nil {
		actor.Ref = *ref
	}
	return actor
}

func actorColumns(actor domain.Actor) (any, any) {
	if actor.Type == domain.ActorTypeUser {
		return actor.UserID, nil
	}
	return nil, nullIfEmpty(actor.Ref)
}

func (s *TaskThreadStore) AddItem(
	ctx context.Context,
	threadID uuid.UUID,
	kind domain.ThreadItemKind,
	body string,
	replyToItemID *uuid.UUID,
	mentionedUserIDs []uuid.UUID,
	author domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (domain.ThreadItem, error) {
	if err := validateThreadOperationActor(author, operation); err != nil {
		return domain.ThreadItem{}, err
	}
	if kind != domain.ThreadItemKindMessage && kind != domain.ThreadItemKindProgress {
		return domain.ThreadItem{}, fmt.Errorf(
			"%w: callers may only add message or progress Items",
			domain.ErrInvalidInput,
		)
	}
	if kind == domain.ThreadItemKindProgress && replyToItemID != nil {
		return domain.ThreadItem{}, fmt.Errorf(
			"%w: progress Items cannot be replies",
			domain.ErrInvalidInput,
		)
	}
	item := domain.ThreadItem{
		ID: uuid.New(), ThreadID: threadID, Kind: kind,
		Author: author, Body: body, ReplyToItemID: replyToItemID,
		MentionedUserIDs: append([]uuid.UUID(nil), mentionedUserIDs...),
		Version:          1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	if err := item.Validate(); err != nil {
		return domain.ThreadItem{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.ThreadItem{}, fmt.Errorf("begin add Thread message: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var taskID uuid.UUID
	var taskNumber int64
	var threadRole string
	var issueStatus *string
	if err := tx.QueryRow(ctx, `
		SELECT thread.task_id,task.number,thread.role,thread.issue_status
		FROM task_threads thread
		JOIN tasks task ON task.id=thread.task_id
		WHERE thread.id=$1
		FOR UPDATE OF thread`, threadID).Scan(&taskID, &taskNumber, &threadRole, &issueStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ThreadItem{}, domain.ErrNotFound
		}
		return domain.ThreadItem{}, fmt.Errorf("lock Thread %s: %w", threadID, err)
	}
	if issueStatus != nil && *issueStatus == string(domain.IssueThreadStatusResolved) {
		return domain.ThreadItem{}, fmt.Errorf("%w: resolved Issue Thread is immutable", domain.ErrConflict)
	}
	if kind == domain.ThreadItemKindProgress && threadRole != string(domain.ThreadRoleMain) {
		return domain.ThreadItem{}, fmt.Errorf(
			"%w: progress Items belong only in the Main Thread",
			domain.ErrInvalidInput,
		)
	}
	if replyToItemID != nil {
		var replyBelongs bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM task_thread_items
				WHERE id=$1 AND thread_id=$2
			)`, *replyToItemID, threadID).Scan(&replyBelongs); err != nil {
			return domain.ThreadItem{}, fmt.Errorf("validate Thread reply: %w", err)
		}
		if !replyBelongs {
			return domain.ThreadItem{}, fmt.Errorf("%w: reply Item is not in this Thread", domain.ErrInvalidInput)
		}
	}
	if err := insertTaskThreadItem(ctx, tx, item, operation.RequestID); err != nil {
		return domain.ThreadItem{}, err
	}
	if err := replaceThreadItemMentions(ctx, tx, item.ID, mentionedUserIDs); err != nil {
		return domain.ThreadItem{}, err
	}
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now.UTC(), Actor: operation, EntityType: "task_thread_item",
		EntityID: item.ID, EntityNumber: &taskNumber, Action: string(kind) + "_added",
	}); err != nil {
		return domain.ThreadItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ThreadItem{}, fmt.Errorf("commit add Thread message: %w", err)
	}
	return item, nil
}

func (s *TaskThreadStore) EditMessage(
	ctx context.Context,
	itemID uuid.UUID,
	expectedVersion int64,
	body string,
	mentionedUserIDs []uuid.UUID,
	author domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (domain.ThreadItem, error) {
	return s.mutateMessage(
		ctx, itemID, expectedVersion, author, operation, now,
		func(item *domain.ThreadItem) error {
			return item.Edit(body, mentionedUserIDs, now)
		},
		"message_edited",
	)
}

func (s *TaskThreadStore) DeleteMessage(
	ctx context.Context,
	itemID uuid.UUID,
	expectedVersion int64,
	author domain.Actor,
	operation domain.OperationActor,
	now time.Time,
) (domain.ThreadItem, error) {
	return s.mutateMessage(
		ctx, itemID, expectedVersion, author, operation, now,
		func(item *domain.ThreadItem) error { return item.Delete(now) },
		"message_deleted",
	)
}

type messageMutation func(*domain.ThreadItem) error

func (s *TaskThreadStore) mutateMessage(
	ctx context.Context,
	itemID uuid.UUID,
	expectedVersion int64,
	author domain.Actor,
	operation domain.OperationActor,
	now time.Time,
	mutate messageMutation,
	action string,
) (domain.ThreadItem, error) {
	if err := validateThreadOperationActor(author, operation); err != nil {
		return domain.ThreadItem{}, err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.ThreadItem{}, fmt.Errorf("begin mutate Thread message: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	item, taskNumber, issueStatus, err := lockTaskThreadItem(ctx, tx, itemID)
	if err != nil {
		return domain.ThreadItem{}, err
	}
	if item.Version != expectedVersion {
		return domain.ThreadItem{}, domain.VersionConflictError{CurrentVersion: item.Version}
	}
	if issueStatus != nil && *issueStatus == string(domain.IssueThreadStatusResolved) {
		return domain.ThreadItem{}, fmt.Errorf("%w: resolved Issue Thread is immutable", domain.ErrConflict)
	}
	if item.Author.Type != author.Type || item.Author.Ref != author.Ref ||
		!sameOptionalUUID(item.Author.UserID, author.UserID) {
		return domain.ThreadItem{}, fmt.Errorf("%w: only the message author may modify it", domain.ErrForbidden)
	}
	if err := mutate(&item); err != nil {
		return domain.ThreadItem{}, err
	}
	var bodyValue any
	if item.DeletedAt == nil {
		bodyValue = item.Body
	}
	commandTag, err := tx.Exec(ctx, `
		UPDATE task_thread_items
		SET body=$1,version=$2,updated_at=$3,deleted_at=$4
		WHERE id=$5 AND version=$6`,
		bodyValue, item.Version, item.UpdatedAt, item.DeletedAt,
		item.ID, expectedVersion,
	)
	if err != nil {
		return domain.ThreadItem{}, mapPgError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return domain.ThreadItem{}, domain.ErrVersionConflict
	}
	if err := replaceThreadItemMentions(ctx, tx, item.ID, item.MentionedUserIDs); err != nil {
		return domain.ThreadItem{}, err
	}
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: now.UTC(), Actor: operation, EntityType: "task_thread_item",
		EntityID: item.ID, EntityNumber: &taskNumber, Action: action,
	}); err != nil {
		return domain.ThreadItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ThreadItem{}, fmt.Errorf("commit mutate Thread message: %w", err)
	}
	return item, nil
}

func lockTaskThreadItem(
	ctx context.Context,
	tx pgx.Tx,
	itemID uuid.UUID,
) (domain.ThreadItem, int64, *string, error) {
	var taskNumber int64
	var issueStatus *string
	row := tx.QueryRow(ctx, `
		SELECT item.id,item.thread_id,item.kind,
			item.author_type,item.author_user_id,item.author_ref,
			item.body,item.typed_payload,item.reply_to_item_id,
			COALESCE(ARRAY(
				SELECT mention.user_id
				FROM task_thread_item_mentions mention
				WHERE mention.item_id=item.id
				ORDER BY mention.user_id
			), ARRAY[]::uuid[]),
			item.version,item.created_at,item.updated_at,item.deleted_at
		FROM task_thread_items item
		WHERE item.id=$1
		FOR UPDATE`, itemID)
	item, err := scanTaskThreadItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ThreadItem{}, 0, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.ThreadItem{}, 0, nil, fmt.Errorf("lock Thread Item: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT task.number,thread.issue_status
		FROM task_thread_items item
		JOIN task_threads thread ON thread.id=item.thread_id
		JOIN tasks task ON task.id=thread.task_id
		WHERE item.id=$1`, itemID).Scan(&taskNumber, &issueStatus); err != nil {
		return domain.ThreadItem{}, 0, nil, fmt.Errorf("load Thread Item context: %w", err)
	}
	return item, taskNumber, issueStatus, nil
}

func validateThreadOperationActor(actor domain.Actor, operation domain.OperationActor) error {
	if err := operation.Validate(); err != nil {
		return err
	}
	switch actor.Type {
	case domain.ActorTypeUser:
		if operation.AuthMethod != domain.AuthenticationMethodSession ||
			actor.UserID == nil || *actor.UserID != operation.UserID {
			return fmt.Errorf("%w: Thread actor does not match session subject", domain.ErrForbidden)
		}
	case domain.ActorTypeAgent:
		if operation.AuthMethod == domain.AuthenticationMethodSession ||
			strings.TrimSpace(actor.Ref) == "" {
			return fmt.Errorf("%w: Thread Agent provenance does not match authentication", domain.ErrForbidden)
		}
	default:
		return fmt.Errorf("%w: callers cannot impersonate a system actor", domain.ErrForbidden)
	}
	return nil
}

func insertTaskThreadItem(
	ctx context.Context,
	tx pgx.Tx,
	item domain.ThreadItem,
	requestID string,
) error {
	if err := item.Validate(); err != nil {
		return err
	}
	authorUserID, authorRef := actorColumns(item.Author)
	var payload any
	if item.IssueResolution != nil {
		encoded, err := json.Marshal(item.IssueResolution)
		if err != nil {
			return fmt.Errorf("encode Issue resolution payload: %w", err)
		}
		payload = encoded
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO task_thread_items (
			id,thread_id,kind,author_type,author_user_id,author_ref,
			body,typed_payload,reply_to_item_id,request_id,version,
			created_at,updated_at,deleted_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		item.ID, item.ThreadID, item.Kind, item.Author.Type, authorUserID, authorRef,
		nullIfEmpty(item.Body), payload, item.ReplyToItemID, requestID, item.Version,
		item.CreatedAt, item.UpdatedAt, item.DeletedAt,
	)
	if err != nil {
		return mapPgError(err)
	}
	return nil
}

func replaceThreadItemMentions(
	ctx context.Context,
	tx pgx.Tx,
	itemID uuid.UUID,
	userIDs []uuid.UUID,
) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM task_thread_item_mentions WHERE item_id=$1`, itemID); err != nil {
		return fmt.Errorf("clear Thread Item mentions: %w", err)
	}
	seen := make(map[uuid.UUID]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == uuid.Nil {
			return fmt.Errorf("%w: mentioned user is required", domain.ErrInvalidInput)
		}
		if _, duplicate := seen[userID]; duplicate {
			continue
		}
		seen[userID] = struct{}{}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_thread_item_mentions (item_id,user_id)
			VALUES ($1,$2)`, itemID, userID); err != nil {
			return mapPgError(err)
		}
	}
	return nil
}
