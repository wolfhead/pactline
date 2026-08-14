-- Make Project repository membership and Task code-change references independent
-- of provider credentials while preserving existing provider evidence.

ALTER TABLE project_repositories
    ADD COLUMN provider text,
    ADD COLUMN origin text,
    ADD COLUMN path_with_namespace text,
    ADD COLUMN path_lookup_key text,
    ADD COLUMN canonical_web_url text;

UPDATE project_repositories AS repository
SET provider = connection.provider,
    origin = connection.origin,
    path_with_namespace = connection.path_with_namespace,
    path_lookup_key = connection.path_lookup_key,
    canonical_web_url = connection.canonical_web_url
FROM repository_connections AS connection
WHERE connection.id = repository.connection_id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM project_repositories
        WHERE provider IS NULL
           OR btrim(origin) = ''
           OR btrim(path_with_namespace) = ''
           OR btrim(path_lookup_key) = ''
           OR btrim(canonical_web_url) = ''
    ) THEN
        RAISE EXCEPTION 'cannot detach Project repository with incomplete repository identity';
    END IF;
END $$;

ALTER TABLE project_repositories
    ALTER COLUMN provider SET NOT NULL,
    ALTER COLUMN origin SET NOT NULL,
    ALTER COLUMN path_with_namespace SET NOT NULL,
    ALTER COLUMN path_lookup_key SET NOT NULL,
    ALTER COLUMN canonical_web_url SET NOT NULL,
    ADD CONSTRAINT project_repositories_provider_check
        CHECK (provider IN ('gitlab','github')),
    ADD CONSTRAINT project_repositories_origin_check CHECK (btrim(origin) <> ''),
    ADD CONSTRAINT project_repositories_path_check CHECK (btrim(path_with_namespace) <> ''),
    ADD CONSTRAINT project_repositories_path_lookup_key_check CHECK (btrim(path_lookup_key) <> ''),
    ADD CONSTRAINT project_repositories_canonical_web_url_check CHECK (btrim(canonical_web_url) <> '');

DROP INDEX project_repositories_active_binding;

CREATE UNIQUE INDEX project_repositories_active_identity
    ON project_repositories (project_id, provider, origin, path_lookup_key)
    WHERE unbound_at IS NULL;

ALTER TABLE task_code_changes
    ADD COLUMN evidence_connection_id uuid REFERENCES repository_connections(id),
    ADD COLUMN evidence_provider_repository_id text,
    ADD COLUMN evidence_observed_at timestamptz;

UPDATE task_code_changes AS code_change
SET evidence_connection_id = repository.connection_id,
    evidence_provider_repository_id = connection.provider_repository_id,
    evidence_observed_at = code_change.observed_at
FROM project_repositories AS repository
JOIN repository_connections AS connection ON connection.id = repository.connection_id
WHERE repository.id = code_change.project_repository_id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM task_code_changes
        WHERE evidence_connection_id IS NULL
           OR btrim(evidence_provider_repository_id) = ''
           OR evidence_observed_at IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot migrate Task code change with incomplete provider evidence';
    END IF;
END $$;

ALTER TABLE task_code_changes
    DROP CONSTRAINT IF EXISTS task_code_changes_observation_status_check,
    DROP CONSTRAINT IF EXISTS task_merge_requests_observation_status_check;

ALTER TABLE task_code_changes
    RENAME COLUMN observation_status TO verification_status;

ALTER TABLE task_code_changes
    RENAME COLUMN observed_at TO verification_attempted_at;

UPDATE task_code_changes
SET verification_status = CASE verification_status
    WHEN 'confirmed' THEN 'verified'
    ELSE verification_status
END;

ALTER TABLE task_code_changes
    ALTER COLUMN provider_change_id DROP NOT NULL,
    ALTER COLUMN verification_status DROP NOT NULL,
    ALTER COLUMN verification_attempted_at DROP NOT NULL,
    ALTER COLUMN title DROP NOT NULL,
    ALTER COLUMN state DROP NOT NULL,
    ALTER COLUMN draft DROP NOT NULL,
    ALTER COLUMN source_branch DROP NOT NULL,
    ALTER COLUMN target_branch DROP NOT NULL,
    ALTER COLUMN head_sha DROP NOT NULL,
    ALTER COLUMN provider_updated_at DROP NOT NULL,
    ADD CONSTRAINT task_code_changes_verification_status_check CHECK (
        verification_status IS NULL OR verification_status IN (
            'verified','missing','unauthorized','unreachable','disconnected'
        )
    ),
    ADD CONSTRAINT task_code_changes_verification_group_check CHECK (
        (verification_status IS NULL AND verification_attempted_at IS NULL)
        OR
        (verification_status IS NOT NULL AND verification_attempted_at IS NOT NULL)
    ),
    ADD CONSTRAINT task_code_changes_evidence_verification_check CHECK (
        (evidence_connection_id IS NULL AND verification_status <> 'verified')
        OR
        (evidence_connection_id IS NOT NULL AND verification_status IS NOT NULL)
    ),
    ADD CONSTRAINT task_code_changes_evidence_provider_repository_id_check CHECK (
        evidence_provider_repository_id IS NULL OR btrim(evidence_provider_repository_id) <> ''
    ),
    ADD CONSTRAINT task_code_changes_evidence_group_check CHECK (
        (
            evidence_connection_id IS NULL
            AND evidence_provider_repository_id IS NULL
            AND provider_change_id IS NULL
            AND evidence_observed_at IS NULL
            AND title IS NULL
            AND state IS NULL
            AND draft IS NULL
            AND source_branch IS NULL
            AND target_branch IS NULL
            AND head_sha IS NULL
            AND merge_commit_sha IS NULL
            AND merged_at IS NULL
            AND provider_updated_at IS NULL
        )
        OR
        (
            evidence_connection_id IS NOT NULL
            AND evidence_provider_repository_id IS NOT NULL
            AND provider_change_id IS NOT NULL
            AND btrim(provider_change_id) <> ''
            AND evidence_observed_at IS NOT NULL
            AND title IS NOT NULL
            AND btrim(title) <> ''
            AND state IN ('opened','closed','merged','locked')
            AND draft IS NOT NULL
            AND source_branch IS NOT NULL
            AND btrim(source_branch) <> ''
            AND target_branch IS NOT NULL
            AND btrim(target_branch) <> ''
            AND head_sha IS NOT NULL
            AND btrim(head_sha) <> ''
            AND provider_updated_at IS NOT NULL
        )
    );

UPDATE task_thread_items AS item
SET typed_payload = jsonb_set(
    item.typed_payload,
    '{code_changes}',
    COALESCE(
        (
            SELECT jsonb_agg(
                (
                    entry.value
                    - 'connection_id'
                    - 'provider_repository_id'
                    - 'provider_change_id'
                    - 'title'
                    - 'state'
                    - 'draft'
                    - 'source_branch'
                    - 'target_branch'
                    - 'head_sha'
                    - 'merge_commit_sha'
                    - 'merged_at'
                    - 'observation_status'
                    - 'observed_at'
                ) || jsonb_build_object(
                    'provider_evidence', jsonb_strip_nulls(jsonb_build_object(
                        'connection_id', entry.value->'connection_id',
                        'provider_repository_id', entry.value->'provider_repository_id',
                        'provider_change_id', entry.value->'provider_change_id',
                        'title', entry.value->'title',
                        'state', entry.value->'state',
                        'draft', entry.value->'draft',
                        'source_branch', entry.value->'source_branch',
                        'target_branch', entry.value->'target_branch',
                        'head_sha', entry.value->'head_sha',
                        'merge_commit_sha', entry.value->'merge_commit_sha',
                        'merged_at', entry.value->'merged_at',
                        'provider_updated_at', entry.value->'observed_at',
                        'observed_at', entry.value->'observed_at'
                    ))
                )
                ORDER BY entry.ordinality
            )
            FROM jsonb_array_elements(
                COALESCE(item.typed_payload->'code_changes', '[]'::jsonb)
            ) WITH ORDINALITY AS entry(value, ordinality)
        ),
        '[]'::jsonb
    ),
    true
)
WHERE item.kind = 'execution_completed';

ALTER TABLE project_repositories
    DROP COLUMN connection_id;
