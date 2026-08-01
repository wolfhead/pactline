package application

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/blob"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/google/uuid"
)

const attachmentUploadLifetime = 30 * time.Minute

type AttachmentService struct {
	Attachments *store.AttachmentStore
	Objects     blob.Store
	Now         func() time.Time
}

type AttachmentUpload struct {
	Session     domain.AttachmentUploadSession
	Instruction blob.UploadInstruction
}

func (s *AttachmentService) StartUpload(
	ctx context.Context,
	taskID, uploaderID uuid.UUID,
	filename, mediaType string,
	size int64,
) (AttachmentUpload, error) {
	mediaType = domain.NormalizeAttachmentMediaType(filename, mediaType)
	if err := domain.ValidateAttachmentMetadata(filename, mediaType, size); err != nil {
		return AttachmentUpload{}, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	sessionID := uuid.New()
	expiresAt := now.Add(attachmentUploadLifetime)
	session := domain.AttachmentUploadSession{
		ID: sessionID, TaskID: taskID, UploaderID: uploaderID,
		Provider: s.Objects.Provider(), ObjectKey: path.Join("tasks", taskID.String(), sessionID.String()),
		Filename: filename, MediaType: mediaType, ExpectedSize: size, ExpiresAt: expiresAt,
	}
	created, err := s.Attachments.CreateUploadSession(ctx, session)
	if err != nil {
		return AttachmentUpload{}, err
	}
	instruction, err := s.Objects.CreateUpload(ctx, created.ObjectKey, mediaType, size, expiresAt)
	if err != nil {
		slog.Error("create attachment upload instruction",
			"provider", s.Objects.Provider(), "upload_session_id", created.ID, "error", err)
		return AttachmentUpload{}, fmt.Errorf("create attachment upload instruction: %w", err)
	}
	return AttachmentUpload{Session: created, Instruction: instruction}, nil
}

func (s *AttachmentService) UploadLocal(
	ctx context.Context,
	session domain.AttachmentUploadSession,
	uploaderID uuid.UUID,
	body io.Reader,
	contentLength int64,
) error {
	if session.UploaderID != uploaderID {
		return domain.ErrForbidden
	}
	if s.Objects.Provider() != domain.AttachmentProviderLocal || session.Provider != domain.AttachmentProviderLocal {
		return fmt.Errorf("%w: direct cloud uploads do not accept proxied content", domain.ErrConflict)
	}
	if time.Now().After(session.ExpiresAt) {
		return fmt.Errorf("%w: attachment upload session expired", domain.ErrConflict)
	}
	if contentLength != session.ExpectedSize {
		return fmt.Errorf("%w: Content-Length does not match the upload session", domain.ErrInvalidInput)
	}
	if err := s.Objects.Put(ctx, session.ObjectKey, body, contentLength, session.MediaType); err != nil {
		slog.Error("store local attachment content",
			"upload_session_id", session.ID, "size_bytes", contentLength, "error", err)
		return err
	}
	slog.Info("local attachment content stored", "upload_session_id", session.ID, "size_bytes", contentLength)
	return nil
}

func (s *AttachmentService) CompleteUpload(
	ctx context.Context,
	sessionID, uploaderID uuid.UUID,
	expectedTaskVersion int64,
	actor domain.OperationActor,
) (store.AttachmentCompletion, error) {
	session, err := s.Attachments.GetUploadSession(ctx, sessionID)
	if err != nil {
		return store.AttachmentCompletion{}, err
	}
	if session.UploaderID != uploaderID {
		return store.AttachmentCompletion{}, domain.ErrForbidden
	}
	info, err := s.Objects.Stat(ctx, session.ObjectKey)
	if err != nil {
		slog.Warn("attachment upload completion could not find object",
			"provider", session.Provider, "upload_session_id", session.ID, "error", err)
		return store.AttachmentCompletion{}, fmt.Errorf("verify uploaded attachment: %w", err)
	}
	content, _, err := s.Objects.Open(ctx, session.ObjectKey)
	if err != nil {
		return store.AttachmentCompletion{}, fmt.Errorf("inspect uploaded attachment: %w", err)
	}
	header := make([]byte, 512)
	read, readErr := io.ReadFull(content, header)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		content.Close() //nolint:errcheck
		return store.AttachmentCompletion{}, fmt.Errorf("inspect uploaded attachment: %w", readErr)
	}
	if closeErr := content.Close(); closeErr != nil {
		return store.AttachmentCompletion{}, fmt.Errorf("close attachment inspection stream: %w", closeErr)
	}
	if err := domain.ValidateAttachmentContentSignature(bytes.TrimSpace(header[:read])); err != nil {
		if deleteErr := s.Objects.Delete(ctx, session.ObjectKey); deleteErr != nil {
			slog.Error("delete rejected attachment object", "upload_session_id", session.ID, "error", deleteErr)
		}
		return store.AttachmentCompletion{}, err
	}
	detectedMediaType := domain.NormalizeAttachmentMediaType(
		session.Filename, http.DetectContentType(header[:read]),
	)
	if detectedMediaType == "text/html" || strings.HasPrefix(detectedMediaType, "image/") {
		info.MediaType = detectedMediaType
	}
	completed, err := s.Attachments.CompleteUploadSession(
		ctx, sessionID, uploaderID, info.Size, info.MediaType, expectedTaskVersion, actor,
	)
	if err != nil {
		return store.AttachmentCompletion{}, err
	}
	slog.Info("attachment upload completed", "attachment_id", completed.Attachment.ID,
		"task_id", completed.Attachment.TaskID, "provider", completed.Attachment.Provider,
		"size_bytes", completed.Attachment.SizeBytes)
	return completed, nil
}

func (s *AttachmentService) Open(
	ctx context.Context,
	attachment domain.TaskAttachment,
) (io.ReadCloser, error) {
	body, info, err := s.Objects.Open(ctx, attachment.ObjectKey)
	if err != nil {
		slog.Error("open attachment content", "attachment_id", attachment.ID,
			"provider", attachment.Provider, "error", err)
		return nil, err
	}
	if info.Size >= 0 && info.Size != attachment.SizeBytes {
		body.Close() //nolint:errcheck
		return nil, fmt.Errorf("attachment object size changed after completion")
	}
	return body, nil
}
