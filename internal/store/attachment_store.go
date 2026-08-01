package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AttachmentStore struct{ db *DB }

func NewAttachmentStore(db *DB) *AttachmentStore { return &AttachmentStore{db: db} }

const attachmentColumns = `id, task_id, uploader_id, filename, media_type, size_bytes,
provider, object_key, version, deleted_at, created_at, updated_at`
const attachmentReturningColumns = `attachment.id, attachment.task_id, attachment.uploader_id,
attachment.filename, attachment.media_type, attachment.size_bytes, attachment.provider,
attachment.object_key, attachment.version, attachment.deleted_at, attachment.created_at,
attachment.updated_at`

const uploadSessionColumns = `id, task_id, uploader_id, provider, object_key, filename,
media_type, expected_size, expires_at, created_at`
const uploadSessionReturningColumns = `upload.id, upload.task_id, upload.uploader_id,
upload.provider, upload.object_key, upload.filename, upload.media_type,
upload.expected_size, upload.expires_at, upload.created_at`

func (s *AttachmentStore) CreateUploadSession(
	ctx context.Context,
	session domain.AttachmentUploadSession,
) (domain.AttachmentUploadSession, error) {
	if err := domain.ValidateAttachmentMetadata(session.Filename, session.MediaType, session.ExpectedSize); err != nil {
		return domain.AttachmentUploadSession{}, err
	}
	if session.ID == uuid.Nil {
		session.ID = uuid.New()
	}
	if session.TaskID == uuid.Nil || session.UploaderID == uuid.Nil || session.ObjectKey == "" {
		return domain.AttachmentUploadSession{}, fmt.Errorf("%w: attachment upload session is incomplete", domain.ErrInvalidInput)
	}
	if session.ExpiresAt.IsZero() || !session.ExpiresAt.After(time.Now()) {
		return domain.AttachmentUploadSession{}, fmt.Errorf("%w: upload session expiry must be in the future", domain.ErrInvalidInput)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return domain.AttachmentUploadSession{}, fmt.Errorf("begin attachment upload session: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM task_attachments WHERE task_id=$1 AND deleted_at IS NULL`, session.TaskID).Scan(&activeCount); err != nil {
		return domain.AttachmentUploadSession{}, fmt.Errorf("count task attachments: %w", err)
	}
	if activeCount >= domain.MaxActiveTaskAttachments {
		return domain.AttachmentUploadSession{}, fmt.Errorf("%w: task already has 100 active attachments", domain.ErrConflict)
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO attachment_upload_sessions (
			id, task_id, uploader_id, provider, object_key, filename,
			media_type, expected_size, status, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9)
		RETURNING `+uploadSessionColumns,
		session.ID, session.TaskID, session.UploaderID, session.Provider,
		session.ObjectKey, session.Filename, session.MediaType,
		session.ExpectedSize, session.ExpiresAt,
	)
	created, err := scanUploadSession(row)
	if err != nil {
		return domain.AttachmentUploadSession{}, mapPgError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AttachmentUploadSession{}, fmt.Errorf("commit attachment upload session: %w", err)
	}
	return created, nil
}

func (s *AttachmentStore) GetUploadSession(
	ctx context.Context,
	id uuid.UUID,
) (domain.AttachmentUploadSession, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+uploadSessionColumns+`
		FROM attachment_upload_sessions WHERE id=$1 AND status='pending'`, id)
	session, err := scanUploadSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AttachmentUploadSession{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AttachmentUploadSession{}, fmt.Errorf("get attachment upload session: %w", err)
	}
	return session, nil
}

type AttachmentCompletion struct {
	Attachment  domain.TaskAttachment
	TaskVersion int64
}

func (s *AttachmentStore) CompleteUploadSession(
	ctx context.Context,
	id, uploaderID uuid.UUID,
	actualSize int64,
	actualMediaType string,
	expectedTaskVersion int64,
	actor domain.OperationActor,
) (AttachmentCompletion, error) {
	if err := actor.Validate(); err != nil {
		return AttachmentCompletion{}, err
	}
	if actor.UserID != uploaderID {
		return AttachmentCompletion{}, domain.ErrForbidden
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return AttachmentCompletion{}, fmt.Errorf("begin complete attachment upload: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	row := tx.QueryRow(ctx, `SELECT `+uploadSessionColumns+`
		FROM attachment_upload_sessions WHERE id=$1 AND status='pending' FOR UPDATE`, id)
	session, err := scanUploadSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttachmentCompletion{}, domain.ErrNotFound
	}
	if err != nil {
		return AttachmentCompletion{}, fmt.Errorf("lock attachment upload session: %w", err)
	}
	if session.UploaderID != uploaderID {
		return AttachmentCompletion{}, domain.ErrForbidden
	}
	if time.Now().After(session.ExpiresAt) {
		if _, err := tx.Exec(ctx, `UPDATE attachment_upload_sessions SET status='expired' WHERE id=$1`, id); err != nil {
			return AttachmentCompletion{}, fmt.Errorf("expire attachment upload session: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return AttachmentCompletion{}, err
		}
		return AttachmentCompletion{}, fmt.Errorf("%w: attachment upload session expired", domain.ErrConflict)
	}
	actualMediaType = domain.NormalizeAttachmentMediaType(session.Filename, actualMediaType)
	if actualMediaType == "application/octet-stream" {
		actualMediaType = session.MediaType
	}
	if actualSize != session.ExpectedSize {
		return AttachmentCompletion{}, fmt.Errorf("%w: uploaded attachment size does not match the upload session", domain.ErrInvalidInput)
	}
	if err := domain.ValidateAttachmentMetadata(session.Filename, actualMediaType, actualSize); err != nil {
		return AttachmentCompletion{}, err
	}
	var taskVersion int64
	if err := tx.QueryRow(ctx, `SELECT version FROM tasks WHERE id=$1 FOR UPDATE`, session.TaskID).Scan(&taskVersion); err != nil {
		return AttachmentCompletion{}, mapPgError(err)
	}
	if taskVersion != expectedTaskVersion {
		return AttachmentCompletion{}, domain.VersionConflictError{CurrentVersion: taskVersion}
	}
	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM task_attachments WHERE task_id=$1 AND deleted_at IS NULL`, session.TaskID).Scan(&activeCount); err != nil {
		return AttachmentCompletion{}, fmt.Errorf("count task attachments: %w", err)
	}
	if activeCount >= domain.MaxActiveTaskAttachments {
		return AttachmentCompletion{}, fmt.Errorf("%w: task already has 100 active attachments", domain.ErrConflict)
	}
	attachmentID := uuid.New()
	attachmentRow := tx.QueryRow(ctx, `
		INSERT INTO task_attachments (
			id, task_id, uploader_id, filename, media_type, size_bytes,
			provider, object_key
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+attachmentColumns,
		attachmentID, session.TaskID, session.UploaderID, session.Filename,
		actualMediaType, actualSize, session.Provider, session.ObjectKey,
	)
	attachment, err := scanAttachment(attachmentRow)
	if err != nil {
		return AttachmentCompletion{}, mapPgError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE attachment_upload_sessions
		SET status='completed', attachment_id=$2, completed_at=now() WHERE id=$1`, id, attachment.ID); err != nil {
		return AttachmentCompletion{}, fmt.Errorf("complete attachment upload session: %w", err)
	}
	if err := tx.QueryRow(ctx, `UPDATE tasks SET version=version+1, updated_at=now()
		WHERE id=$1 AND version=$2 RETURNING version`, session.TaskID, taskVersion).Scan(&taskVersion); err != nil {
		return AttachmentCompletion{}, fmt.Errorf("increment task version for attachment: %w", err)
	}
	newValue, _ := json.Marshal(map[string]any{
		"task_id": attachment.TaskID, "filename": attachment.Filename,
		"media_type": attachment.MediaType, "size_bytes": attachment.SizeBytes,
		"provider": attachment.Provider,
	})
	if err := InsertBusinessAudit(ctx, tx, domain.BusinessAuditEvent{
		OccurredAt: time.Now().UTC(), Actor: actor, EntityType: "attachment",
		EntityID: attachment.ID, Action: "created", NewValue: newValue,
	}); err != nil {
		return AttachmentCompletion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AttachmentCompletion{}, fmt.Errorf("commit attachment upload: %w", err)
	}
	return AttachmentCompletion{Attachment: attachment, TaskVersion: taskVersion}, nil
}

func (s *AttachmentStore) List(ctx context.Context, taskID uuid.UUID) ([]domain.TaskAttachment, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+attachmentColumns+`
		FROM task_attachments WHERE task_id=$1 AND deleted_at IS NULL ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task attachments: %w", err)
	}
	defer rows.Close()
	attachments := []domain.TaskAttachment{}
	for rows.Next() {
		attachment, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func (s *AttachmentStore) Get(ctx context.Context, taskID, id uuid.UUID) (domain.TaskAttachment, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+attachmentColumns+`
		FROM task_attachments WHERE task_id=$1 AND id=$2 AND deleted_at IS NULL`, taskID, id)
	attachment, err := scanAttachment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskAttachment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskAttachment{}, fmt.Errorf("get task attachment: %w", err)
	}
	return attachment, nil
}

func (s *AttachmentStore) SoftDelete(
	ctx context.Context,
	taskID, id uuid.UUID,
	expectedVersion int64,
) (domain.TaskAttachment, error) {
	row := s.db.Pool.QueryRow(ctx, `UPDATE task_attachments
		SET deleted_at=now(), version=version+1, updated_at=now()
		WHERE task_id=$1 AND id=$2 AND deleted_at IS NULL AND version=$3
		RETURNING `+attachmentColumns, taskID, id, expectedVersion)
	attachment, err := scanAttachment(row)
	if !errors.Is(err, pgx.ErrNoRows) {
		return attachment, mapPgError(err)
	}
	var current int64
	err = s.db.Pool.QueryRow(ctx, `SELECT version FROM task_attachments WHERE task_id=$1 AND id=$2`, taskID, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskAttachment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskAttachment{}, fmt.Errorf("read attachment version: %w", err)
	}
	return domain.TaskAttachment{}, domain.VersionConflictError{CurrentVersion: current}
}

func (s *AttachmentStore) ClaimCleanupBatch(ctx context.Context, limit int) ([]domain.TaskAttachment, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin attachment cleanup claim: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `WITH candidates AS (
		SELECT id FROM task_attachments
		WHERE deleted_at IS NOT NULL AND cleaned_at IS NULL
		  AND (cleanup_attempted_at IS NULL OR cleanup_attempted_at < now()-interval '5 minutes')
		ORDER BY deleted_at, id FOR UPDATE SKIP LOCKED LIMIT $1
	)
	UPDATE task_attachments attachment SET cleanup_attempted_at=now(), cleanup_error=NULL
	FROM candidates WHERE attachment.id=candidates.id RETURNING `+attachmentReturningColumns, limit)
	if err != nil {
		return nil, fmt.Errorf("claim attachments for cleanup: %w", err)
	}
	defer rows.Close()
	attachments := []domain.TaskAttachment{}
	for rows.Next() {
		attachment, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit attachment cleanup claim: %w", err)
	}
	return attachments, nil
}

func (s *AttachmentStore) MarkCleaned(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE task_attachments
		SET cleaned_at=now(), cleanup_error=NULL WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("mark attachment cleaned: %w", err)
	}
	return nil
}

func (s *AttachmentStore) MarkCleanupFailed(ctx context.Context, id uuid.UUID, cleanupError error) error {
	message := cleanupError.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err := s.db.Pool.Exec(ctx, `UPDATE task_attachments SET cleanup_error=$2 WHERE id=$1`, id, message)
	if err != nil {
		return fmt.Errorf("mark attachment cleanup failed: %w", err)
	}
	return nil
}

func (s *AttachmentStore) ClaimExpiredUploadSessions(
	ctx context.Context, limit int,
) ([]domain.AttachmentUploadSession, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Pool.Query(ctx, `WITH candidates AS (
		SELECT id FROM attachment_upload_sessions
		WHERE status='pending' AND expires_at <= now()
		ORDER BY expires_at, id FOR UPDATE SKIP LOCKED LIMIT $1
	)
	UPDATE attachment_upload_sessions upload SET status='expired'
	FROM candidates WHERE upload.id=candidates.id RETURNING `+uploadSessionReturningColumns, limit)
	if err != nil {
		return nil, fmt.Errorf("claim expired attachment upload sessions: %w", err)
	}
	defer rows.Close()
	sessions := []domain.AttachmentUploadSession{}
	for rows.Next() {
		session, err := scanUploadSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func scanAttachment(row pgx.Row) (domain.TaskAttachment, error) {
	var attachment domain.TaskAttachment
	err := row.Scan(
		&attachment.ID, &attachment.TaskID, &attachment.UploaderID,
		&attachment.Filename, &attachment.MediaType, &attachment.SizeBytes,
		&attachment.Provider, &attachment.ObjectKey, &attachment.Version,
		&attachment.DeletedAt, &attachment.CreatedAt, &attachment.UpdatedAt,
	)
	return attachment, err
}

func scanUploadSession(row pgx.Row) (domain.AttachmentUploadSession, error) {
	var session domain.AttachmentUploadSession
	err := row.Scan(
		&session.ID, &session.TaskID, &session.UploaderID, &session.Provider,
		&session.ObjectKey, &session.Filename, &session.MediaType,
		&session.ExpectedSize, &session.ExpiresAt, &session.CreatedAt,
	)
	return session, err
}
