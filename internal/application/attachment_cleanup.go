package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wolfhead/pactline/internal/blob"
	"github.com/wolfhead/pactline/internal/store"
)

type AttachmentCleanup struct {
	Attachments *store.AttachmentStore
	Objects     blob.Store
	Interval    time.Duration
}

func (cleanup AttachmentCleanup) Run(ctx context.Context) {
	interval := cleanup.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := cleanup.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("clean private attachment objects", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (cleanup AttachmentCleanup) RunOnce(ctx context.Context) error {
	attachments, err := cleanup.Attachments.ClaimCleanupBatch(ctx, 50)
	if err != nil {
		return err
	}
	for _, attachment := range attachments {
		if attachment.Provider != cleanup.Objects.Provider() {
			err := fmt.Errorf("configured provider %s cannot clean historical %s object", cleanup.Objects.Provider(), attachment.Provider)
			if markErr := cleanup.Attachments.MarkCleanupFailed(ctx, attachment.ID, err); markErr != nil {
				return markErr
			}
			continue
		}
		if err := cleanup.Objects.Delete(ctx, attachment.ObjectKey); err != nil {
			if markErr := cleanup.Attachments.MarkCleanupFailed(ctx, attachment.ID, err); markErr != nil {
				return markErr
			}
			continue
		}
		if err := cleanup.Attachments.MarkCleaned(ctx, attachment.ID); err != nil {
			return err
		}
		slog.Info("attachment object cleaned", "attachment_id", attachment.ID, "provider", attachment.Provider)
	}
	expired, err := cleanup.Attachments.ClaimExpiredUploadSessions(ctx, 50)
	if err != nil {
		return err
	}
	for _, session := range expired {
		if session.Provider != cleanup.Objects.Provider() {
			continue
		}
		if err := cleanup.Objects.Delete(ctx, session.ObjectKey); err != nil {
			slog.Warn("delete expired attachment upload object", "upload_session_id", session.ID, "error", err)
		}
	}
	return nil
}
