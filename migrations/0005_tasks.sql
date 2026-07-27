-- Phase 1 of the new task-management product: the ordinary substrate a team
-- opens every day. See internal/legacy/README.md for why /api/legacy/* and
-- the mechanism tables (bounties, credits, ...) are untouched by this file.

CREATE TABLE tasks (
    id           uuid PRIMARY KEY,
    number       bigserial NOT NULL,
    title        text NOT NULL,
    description  text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'backlog',
    priority     text NOT NULL DEFAULT 'none',
    assignee_id  uuid REFERENCES users(id),
    creator_id   uuid NOT NULL REFERENCES users(id),
    due_date     date,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    archived_at  timestamptz
);

-- number is the short, sequential, human-facing identifier ("look at 142").
-- It is backed by a bigserial (its own sequence), so it is assigned
-- atomically under concurrent inserts and never steps backward; tasks are
-- never hard-deleted (only archived via archived_at), so a number is never
-- freed for reuse either.
CREATE UNIQUE INDEX idx_tasks_number ON tasks (number);

CREATE INDEX idx_tasks_status ON tasks (status);
CREATE INDEX idx_tasks_priority ON tasks (priority);
CREATE INDEX idx_tasks_assignee ON tasks (assignee_id);
CREATE INDEX idx_tasks_archived_at ON tasks (archived_at);
CREATE INDEX idx_tasks_creator ON tasks (creator_id);
CREATE INDEX idx_tasks_created_at ON tasks (created_at DESC, number DESC);
CREATE INDEX idx_tasks_updated_at ON tasks (updated_at DESC, number DESC);
CREATE INDEX idx_tasks_due_date ON tasks (due_date, number);

CREATE TABLE labels (
    id         uuid PRIMARY KEY,
    name       text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE task_labels (
    task_id  uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    label_id uuid NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, label_id)
);

CREATE INDEX idx_task_labels_label ON task_labels (label_id);

CREATE TABLE task_comments (
    id         uuid PRIMARY KEY,
    task_id    uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id  uuid NOT NULL REFERENCES users(id),
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_comments_task ON task_comments (task_id, created_at);

CREATE TABLE task_activity (
    id         uuid PRIMARY KEY,
    task_id    uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_id   uuid NOT NULL REFERENCES users(id),
    field      text NOT NULL,
    old_value  text,
    new_value  text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_activity_task ON task_activity (task_id, created_at);
