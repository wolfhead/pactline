-- Replace GitLab-specific persistence names with provider-neutral repository
-- and code-change identities while preserving every existing GitLab record and
-- frozen execution snapshot.

ALTER TABLE gitlab_connections RENAME TO repository_connections;
ALTER TABLE gitlab_connection_events RENAME TO repository_connection_events;
ALTER TABLE task_merge_requests RENAME TO task_code_changes;

DROP INDEX gitlab_connections_active_project;
DROP INDEX gitlab_connections_active_path;
DROP INDEX gitlab_connections_list;

ALTER TABLE repository_connections
	DROP CONSTRAINT gitlab_connections_gitlab_project_id_check,
	ADD COLUMN provider text NOT NULL DEFAULT 'gitlab',
	ADD CONSTRAINT repository_connections_provider_check
		CHECK (provider IN ('gitlab','github'));

ALTER TABLE repository_connections
	RENAME COLUMN gitlab_project_id TO provider_repository_id;

ALTER TABLE repository_connections
    ALTER COLUMN provider DROP DEFAULT,
    ALTER COLUMN provider_repository_id TYPE text USING provider_repository_id::text,
    ADD CONSTRAINT repository_connections_provider_repository_id_check
        CHECK (btrim(provider_repository_id) <> '');

CREATE UNIQUE INDEX repository_connections_active_identity
    ON repository_connections (provider, origin, provider_repository_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX repository_connections_active_path
    ON repository_connections (provider, origin, path_lookup_key)
    WHERE status = 'active';

CREATE INDEX repository_connections_list
    ON repository_connections (status, provider, label, created_at, id);

ALTER TABLE repository_connection_events
	DROP CONSTRAINT gitlab_connection_events_gitlab_project_id_check,
	ADD COLUMN provider text NOT NULL DEFAULT 'gitlab',
	ADD CONSTRAINT repository_connection_events_provider_check
		CHECK (provider IN ('gitlab','github'));

ALTER TABLE repository_connection_events
	RENAME COLUMN gitlab_project_id TO provider_repository_id;

ALTER TABLE repository_connection_events
    ALTER COLUMN provider DROP DEFAULT,
    ALTER COLUMN provider_repository_id TYPE text USING provider_repository_id::text,
    ADD CONSTRAINT repository_connection_events_provider_repository_id_check
        CHECK (provider_repository_id IS NULL OR btrim(provider_repository_id) <> '');

ALTER INDEX gitlab_connection_events_timeline
    RENAME TO repository_connection_events_timeline;

DROP INDEX task_merge_requests_active_reference;
DROP INDEX task_merge_requests_active_delivery;
DROP INDEX task_merge_requests_history;

ALTER TABLE task_code_changes
	DROP CONSTRAINT task_merge_requests_merge_request_iid_check,
	DROP CONSTRAINT task_merge_requests_gitlab_merge_request_id_check,
	ADD COLUMN kind text NOT NULL DEFAULT 'merge_request',
	ADD CONSTRAINT task_code_changes_kind_check
		CHECK (kind IN ('merge_request','pull_request'));

ALTER TABLE task_code_changes
	RENAME COLUMN merge_request_iid TO change_number;

ALTER TABLE task_code_changes
    RENAME COLUMN gitlab_merge_request_id TO provider_change_id;

ALTER TABLE task_code_changes
    ALTER COLUMN kind DROP DEFAULT,
    ALTER COLUMN provider_change_id TYPE text USING provider_change_id::text,
    ADD CONSTRAINT task_code_changes_change_number_check CHECK (change_number > 0),
    ADD CONSTRAINT task_code_changes_provider_change_id_check
        CHECK (btrim(provider_change_id) <> '');

CREATE UNIQUE INDEX task_code_changes_active_reference
    ON task_code_changes (task_id, project_repository_id, kind, change_number)
    WHERE unlinked_at IS NULL;

CREATE INDEX task_code_changes_active_delivery
    ON task_code_changes (task_id, project_repository_id, kind, change_number, id)
    WHERE unlinked_at IS NULL;

CREATE INDEX task_code_changes_history
    ON task_code_changes (task_id, linked_at, id);

UPDATE task_thread_items AS item
SET typed_payload =
    (item.typed_payload - 'merge_requests') ||
    jsonb_build_object(
        'code_changes',
        COALESCE(
            (
                SELECT jsonb_agg(
                    (entry.value
                        - 'task_merge_request_id'
                        - 'gitlab_project_id'
                        - 'merge_request_iid') ||
                    jsonb_build_object(
                        'task_code_change_id', entry.value->'task_merge_request_id',
                        'provider', 'gitlab',
                        'provider_repository_id', entry.value->>'gitlab_project_id',
                        'kind', 'merge_request',
                        'change_number', entry.value->'merge_request_iid',
                        'provider_change_id', change.provider_change_id
                    )
                    ORDER BY entry.ordinality
                )
                FROM jsonb_array_elements(
                    COALESCE(item.typed_payload->'merge_requests', '[]'::jsonb)
                ) WITH ORDINALITY AS entry(value, ordinality)
                JOIN task_code_changes AS change
                  ON change.id = (entry.value->>'task_merge_request_id')::uuid
            ),
            '[]'::jsonb
        )
    )
WHERE item.kind = 'execution_completed';

DO $$
DECLARE
    constraint_record record;
BEGIN
    FOR constraint_record IN
        SELECT conrelid::regclass AS table_name, conname AS old_name,
            CASE
                WHEN conname LIKE 'gitlab_connections_%'
                    THEN regexp_replace(conname, '^gitlab_connections_', 'repository_connections_')
                WHEN conname LIKE 'gitlab_connection_events_%'
                    THEN regexp_replace(conname, '^gitlab_connection_events_', 'repository_connection_events_')
                WHEN conname LIKE 'task_merge_requests_%'
                    THEN regexp_replace(conname, '^task_merge_requests_', 'task_code_changes_')
            END AS new_name
        FROM pg_constraint
        WHERE conrelid IN (
            'repository_connections'::regclass,
            'repository_connection_events'::regclass,
            'task_code_changes'::regclass
        )
          AND (
            conname LIKE 'gitlab_connections_%'
            OR conname LIKE 'gitlab_connection_events_%'
            OR conname LIKE 'task_merge_requests_%'
          )
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RENAME CONSTRAINT %I TO %I',
            constraint_record.table_name,
            constraint_record.old_name,
            constraint_record.new_name
        );
    END LOOP;
END $$;
