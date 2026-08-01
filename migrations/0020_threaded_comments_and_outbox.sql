ALTER TABLE task_comments
    ADD COLUMN reply_to_comment_id uuid,
    ADD COLUMN thread_root_id uuid,
    ADD COLUMN deleted_at timestamptz;

UPDATE task_comments SET thread_root_id = id;

ALTER TABLE task_comments
    ALTER COLUMN thread_root_id SET NOT NULL,
    ADD CONSTRAINT task_comments_reply_to_fk
        FOREIGN KEY (reply_to_comment_id) REFERENCES task_comments(id),
    ADD CONSTRAINT task_comments_thread_root_fk
        FOREIGN KEY (thread_root_id) REFERENCES task_comments(id);

CREATE INDEX idx_task_comments_thread
    ON task_comments (task_id, thread_root_id, created_at, id);

CREATE TABLE comment_mentions (
    comment_id  uuid NOT NULL REFERENCES task_comments(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id)
);

CREATE INDEX idx_comment_mentions_user
    ON comment_mentions (user_id, created_at, comment_id);

CREATE TABLE outbox_events (
    id              uuid PRIMARY KEY,
    aggregate_type  text NOT NULL CHECK (btrim(aggregate_type) <> ''),
    aggregate_id    uuid NOT NULL,
    event_type      text NOT NULL CHECK (btrim(event_type) <> ''),
    recipient_id    uuid NOT NULL REFERENCES users(id),
    payload         jsonb NOT NULL,
    dedupe_key      text NOT NULL UNIQUE CHECK (btrim(dedupe_key) <> ''),
    status          text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'publishing', 'published')),
    attempt_count   integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    published_at    timestamptz
);

CREATE INDEX idx_outbox_events_pending
    ON outbox_events (next_attempt_at, created_at, id)
    WHERE status IN ('pending', 'publishing');

CREATE TABLE message_consumptions (
    consumer_name text NOT NULL CHECK (btrim(consumer_name) <> ''),
    event_id      uuid NOT NULL,
    processed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer_name, event_id)
);
