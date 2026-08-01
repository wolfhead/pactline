ALTER TABLE task_attachments ADD COLUMN cleaned_at timestamptz;

DROP INDEX idx_task_attachments_cleanup;
CREATE INDEX idx_task_attachments_cleanup
    ON task_attachments (deleted_at, cleanup_attempted_at, id)
    WHERE deleted_at IS NOT NULL AND cleaned_at IS NULL;
