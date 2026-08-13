-- Deleted editable messages remain as timeline tombstones without retaining
-- their former body or mentions. Immutable Items always retain content.

DO $$
DECLARE
    content_constraint text;
BEGIN
    SELECT conname INTO content_constraint
    FROM pg_constraint
    WHERE conrelid = 'task_thread_items'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%body IS NOT NULL%typed_payload IS NOT NULL%';

    IF content_constraint IS NOT NULL THEN
        EXECUTE format(
            'ALTER TABLE task_thread_items DROP CONSTRAINT %I',
            content_constraint
        );
    END IF;
END;
$$;

ALTER TABLE task_thread_items
    ADD CONSTRAINT task_thread_items_content_shape CHECK (
        body IS NOT NULL
        OR typed_payload IS NOT NULL
        OR (kind = 'message' AND deleted_at IS NOT NULL)
    );
