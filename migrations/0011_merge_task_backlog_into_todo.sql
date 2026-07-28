UPDATE tasks
SET status = 'todo',
    updated_at = now()
WHERE status = 'backlog';

UPDATE task_activity
SET old_value = 'todo'
WHERE field = 'status'
  AND old_value = 'backlog';

UPDATE task_activity
SET new_value = 'todo'
WHERE field IN ('created', 'status')
  AND new_value = 'backlog';

ALTER TABLE tasks
    ALTER COLUMN status SET DEFAULT 'todo';

ALTER TABLE tasks
    ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('todo', 'in_progress', 'in_review', 'done', 'cancelled'));
