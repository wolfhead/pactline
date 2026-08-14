-- Add GitLab repository authorization and Task delivery evidence. The schema is
-- additive: historical execution-completion payloads omit merge_requests and
-- are interpreted by the application as an empty collection.

CREATE TABLE gitlab_connections (
    id                          uuid PRIMARY KEY,
    version                     bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    label                       text NOT NULL CHECK (btrim(label) <> ''),
    origin                      text NOT NULL CHECK (btrim(origin) <> ''),
    gitlab_project_id           bigint NOT NULL CHECK (gitlab_project_id > 0),
    path_with_namespace         text NOT NULL CHECK (btrim(path_with_namespace) <> ''),
    path_lookup_key             text NOT NULL CHECK (btrim(path_lookup_key) <> ''),
    canonical_web_url           text NOT NULL CHECK (btrim(canonical_web_url) <> ''),
    default_branch              text NOT NULL DEFAULT '',
    credential_ciphertext       bytea NOT NULL CHECK (octet_length(credential_ciphertext) > 0),
    encryption_key_id           text NOT NULL CHECK (btrim(encryption_key_id) <> ''),
    credential_expires_at       timestamptz,
    status                      text NOT NULL CHECK (status IN ('active','disabled')),
    last_validated_at           timestamptz NOT NULL,
    created_by                  uuid NOT NULL REFERENCES users(id),
    disabled_by                 uuid REFERENCES users(id),
    disabled_at                 timestamptz,
    created_at                  timestamptz NOT NULL,
    updated_at                  timestamptz NOT NULL,
    CHECK (
        (status = 'active' AND disabled_by IS NULL AND disabled_at IS NULL)
        OR
        (status = 'disabled' AND disabled_by IS NOT NULL AND disabled_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX gitlab_connections_active_project
    ON gitlab_connections (origin, gitlab_project_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX gitlab_connections_active_path
    ON gitlab_connections (origin, path_lookup_key)
    WHERE status = 'active';

CREATE INDEX gitlab_connections_list
    ON gitlab_connections (status, label, created_at, id);

CREATE TABLE gitlab_connection_events (
    id                  uuid PRIMARY KEY,
    connection_id       uuid REFERENCES gitlab_connections(id),
    actor_user_id       uuid NOT NULL REFERENCES users(id),
    event_type          text NOT NULL CHECK (
                            event_type IN ('created','credential_rotated','validated','disabled')
                        ),
    outcome             text NOT NULL CHECK (outcome IN ('succeeded','failed')),
    origin              text CHECK (origin IS NULL OR btrim(origin) <> ''),
    gitlab_project_id   bigint CHECK (gitlab_project_id IS NULL OR gitlab_project_id > 0),
    error_category      text CHECK (error_category IS NULL OR btrim(error_category) <> ''),
    request_id          text NOT NULL CHECK (btrim(request_id) <> ''),
    created_at          timestamptz NOT NULL
);

CREATE INDEX gitlab_connection_events_timeline
    ON gitlab_connection_events (created_at DESC, id DESC);

CREATE TABLE project_repositories (
    id                  uuid PRIMARY KEY,
    project_id          uuid NOT NULL REFERENCES projects(id),
    connection_id       uuid NOT NULL REFERENCES gitlab_connections(id),
    bound_by            uuid NOT NULL REFERENCES users(id),
    bound_at            timestamptz NOT NULL,
    unbound_by          uuid REFERENCES users(id),
    unbound_at          timestamptz,
    CHECK (
        (unbound_by IS NULL AND unbound_at IS NULL)
        OR
        (unbound_by IS NOT NULL AND unbound_at IS NOT NULL AND unbound_at >= bound_at)
    ),
    UNIQUE (id, project_id)
);

CREATE UNIQUE INDEX project_repositories_active_binding
    ON project_repositories (project_id, connection_id)
    WHERE unbound_at IS NULL;

CREATE INDEX project_repositories_project_history
    ON project_repositories (project_id, bound_at, id);

ALTER TABLE tasks
    ADD CONSTRAINT tasks_id_project_unique UNIQUE (id, project_id);

CREATE TABLE task_merge_requests (
    id                          uuid PRIMARY KEY,
    task_id                     uuid NOT NULL,
    project_id                  uuid NOT NULL REFERENCES projects(id),
    project_repository_id       uuid NOT NULL,
    merge_request_iid           bigint NOT NULL CHECK (merge_request_iid > 0),
    gitlab_merge_request_id     bigint NOT NULL CHECK (gitlab_merge_request_id > 0),
    web_url                     text NOT NULL CHECK (btrim(web_url) <> ''),
    linked_by_type              text NOT NULL CHECK (linked_by_type IN ('user','agent')),
    linked_by_user_id           uuid REFERENCES users(id),
    linked_by_ref               text,
    linked_through_claim_id     uuid NOT NULL REFERENCES task_stage_claims(id),
    linked_at                   timestamptz NOT NULL,
    unlinked_by_type            text CHECK (unlinked_by_type IN ('user','agent')),
    unlinked_by_user_id         uuid REFERENCES users(id),
    unlinked_by_ref             text,
    unlinked_through_claim_id   uuid REFERENCES task_stage_claims(id),
    unlinked_at                 timestamptz,
    observation_status          text NOT NULL CHECK (
                                    observation_status IN (
                                        'confirmed','missing','unauthorized',
                                        'unreachable','disconnected'
                                    )
                                ),
    observed_at                 timestamptz NOT NULL,
    title                       text NOT NULL CHECK (btrim(title) <> ''),
    state                       text NOT NULL CHECK (state IN ('opened','closed','merged','locked')),
    draft                       boolean NOT NULL,
    source_branch               text NOT NULL CHECK (btrim(source_branch) <> ''),
    target_branch               text NOT NULL CHECK (btrim(target_branch) <> ''),
    head_sha                    text NOT NULL CHECK (btrim(head_sha) <> ''),
    merge_commit_sha            text,
    merged_at                   timestamptz,
    provider_updated_at         timestamptz NOT NULL,
    created_at                  timestamptz NOT NULL,
    updated_at                  timestamptz NOT NULL,
    FOREIGN KEY (task_id, project_id) REFERENCES tasks(id, project_id),
    FOREIGN KEY (project_repository_id, project_id)
        REFERENCES project_repositories(id, project_id),
    CHECK (
        (linked_by_type = 'user' AND linked_by_user_id IS NOT NULL AND linked_by_ref IS NULL)
        OR
        (linked_by_type = 'agent' AND linked_by_user_id IS NULL
            AND linked_by_ref IS NOT NULL AND btrim(linked_by_ref) <> '')
    ),
    CHECK (
        (
            unlinked_at IS NULL
            AND unlinked_by_type IS NULL
            AND unlinked_by_user_id IS NULL
            AND unlinked_by_ref IS NULL
            AND unlinked_through_claim_id IS NULL
        )
        OR
        (
            unlinked_at IS NOT NULL
            AND unlinked_through_claim_id IS NOT NULL
            AND (
                (unlinked_by_type = 'user'
                    AND unlinked_by_user_id IS NOT NULL
                    AND unlinked_by_ref IS NULL)
                OR
                (unlinked_by_type = 'agent'
                    AND unlinked_by_user_id IS NULL
                    AND unlinked_by_ref IS NOT NULL
                    AND btrim(unlinked_by_ref) <> '')
            )
        )
    )
);

CREATE UNIQUE INDEX task_merge_requests_active_reference
    ON task_merge_requests (task_id, project_repository_id, merge_request_iid)
    WHERE unlinked_at IS NULL;

CREATE INDEX task_merge_requests_active_delivery
    ON task_merge_requests (task_id, project_repository_id, merge_request_iid, id)
    WHERE unlinked_at IS NULL;

CREATE INDEX task_merge_requests_history
    ON task_merge_requests (task_id, linked_at, id);
