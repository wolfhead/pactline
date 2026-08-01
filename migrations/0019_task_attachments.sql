CREATE TABLE attachment_upload_sessions (
    id              uuid PRIMARY KEY,
    task_id         uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    uploader_id     uuid NOT NULL REFERENCES users(id),
    provider        text NOT NULL CHECK (provider IN ('local', 'oss', 'cos')),
    object_key      text NOT NULL CHECK (btrim(object_key) <> ''),
    filename        text NOT NULL CHECK (btrim(filename) <> ''),
    media_type      text NOT NULL CHECK (btrim(media_type) <> ''),
    expected_size   bigint NOT NULL CHECK (expected_size > 0 AND expected_size <= 104857600),
    status          text NOT NULL CHECK (status IN ('pending', 'completed', 'expired')),
    expires_at      timestamptz NOT NULL,
    attachment_id   uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    CHECK (
        (status = 'pending' AND attachment_id IS NULL AND completed_at IS NULL)
        OR
        (status = 'completed' AND attachment_id IS NOT NULL AND completed_at IS NOT NULL)
        OR
        (status = 'expired' AND attachment_id IS NULL AND completed_at IS NULL)
    )
);

CREATE TABLE task_attachments (
    id                  uuid PRIMARY KEY,
    task_id             uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    uploader_id         uuid NOT NULL REFERENCES users(id),
    filename            text NOT NULL CHECK (btrim(filename) <> ''),
    media_type          text NOT NULL CHECK (btrim(media_type) <> ''),
    size_bytes          bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 104857600),
    provider            text NOT NULL CHECK (provider IN ('local', 'oss', 'cos')),
    object_key          text NOT NULL UNIQUE CHECK (btrim(object_key) <> ''),
    version             bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    deleted_at          timestamptz,
    cleanup_attempted_at timestamptz,
    cleanup_error       text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE attachment_upload_sessions
    ADD CONSTRAINT attachment_upload_sessions_attachment_fk
    FOREIGN KEY (attachment_id) REFERENCES task_attachments(id);

CREATE INDEX idx_task_attachments_active
    ON task_attachments (task_id, created_at, id)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_task_attachments_cleanup
    ON task_attachments (deleted_at, cleanup_attempted_at, id)
    WHERE deleted_at IS NOT NULL;
CREATE INDEX idx_attachment_upload_sessions_expiry
    ON attachment_upload_sessions (expires_at, id)
    WHERE status = 'pending';
CREATE INDEX idx_attachment_upload_sessions_task
    ON attachment_upload_sessions (task_id, created_at, id);
