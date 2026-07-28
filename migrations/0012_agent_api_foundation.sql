-- Foundations for the human-and-agent work API. Raw bearer secrets are never
-- stored; API access logs intentionally exclude request and response bodies.

ALTER TABLE tasks
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE task_comments
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE projects
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE milestones
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE acceptance_criteria
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE labels
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

CREATE TABLE api_tokens (
    id                 uuid PRIMARY KEY,
    user_id            uuid NOT NULL REFERENCES users(id),
    name               text NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 100),
    secret_hash        bytea NOT NULL UNIQUE CHECK (octet_length(secret_hash) = 32),
    display_prefix     text NOT NULL CHECK (btrim(display_prefix) <> ''),
    scopes             text[] NOT NULL,
    expires_at         timestamptz NOT NULL,
    last_used_at       timestamptz,
    revoked_at         timestamptz,
    revoked_by_user_id uuid REFERENCES users(id),
    created_at         timestamptz NOT NULL,
    CHECK (scopes <@ ARRAY['work:read','work:write']::text[]),
    CHECK (cardinality(scopes) > 0),
    CHECK (expires_at > created_at),
    CHECK (
        (revoked_at IS NULL AND revoked_by_user_id IS NULL)
        OR
        (revoked_at IS NOT NULL AND revoked_by_user_id IS NOT NULL)
    )
);

CREATE INDEX api_tokens_active_user
    ON api_tokens (user_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE api_request_audit_events (
    id                   uuid PRIMARY KEY,
    occurred_at          timestamptz NOT NULL,
    request_id           text NOT NULL,
    auth_method          text NOT NULL
                         CHECK (auth_method IN ('session','api_token','unknown')),
    auth_outcome         text NOT NULL
                         CHECK (auth_outcome IN ('authenticated','rejected')),
    user_id              uuid REFERENCES users(id),
    token_id             uuid REFERENCES api_tokens(id),
    token_name           text,
    method               text NOT NULL,
    route_pattern        text NOT NULL,
    status_code          integer NOT NULL CHECK (status_code BETWEEN 100 AND 599),
    problem_code         text,
    duration_ms          bigint NOT NULL CHECK (duration_ms >= 0),
    response_bytes       bigint NOT NULL CHECK (response_bytes >= 0),
    idempotency_replayed boolean NOT NULL DEFAULT false,
    user_agent           text NOT NULL DEFAULT '',
    network_address      inet
);

CREATE INDEX api_request_audit_recent
    ON api_request_audit_events (occurred_at DESC, id DESC);

CREATE INDEX api_request_audit_user
    ON api_request_audit_events (user_id, occurred_at DESC, id DESC);

CREATE INDEX api_request_audit_token
    ON api_request_audit_events (token_id, occurred_at DESC, id DESC);

CREATE TABLE business_audit_events (
    id            uuid PRIMARY KEY,
    occurred_at   timestamptz NOT NULL,
    request_id    text NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES users(id),
    auth_method   text NOT NULL CHECK (auth_method IN ('session','api_token')),
    token_id      uuid REFERENCES api_tokens(id),
    token_name    text,
    entity_type   text NOT NULL,
    entity_id     uuid NOT NULL,
    entity_number bigint,
    action        text NOT NULL,
    old_value     jsonb,
    new_value     jsonb,
    CHECK (
        (auth_method = 'session' AND token_id IS NULL)
        OR
        (auth_method = 'api_token' AND token_id IS NOT NULL)
    )
);

CREATE INDEX business_audit_entity
    ON business_audit_events (entity_type, entity_id, occurred_at, id);

CREATE INDEX business_audit_actor
    ON business_audit_events (actor_user_id, occurred_at DESC, id DESC);

CREATE TABLE idempotency_records (
    user_id          uuid NOT NULL REFERENCES users(id),
    token_id         uuid NOT NULL REFERENCES api_tokens(id),
    method           text NOT NULL,
    route_pattern    text NOT NULL,
    idempotency_key  text NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 128
    ),
    request_hash     bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    state            text NOT NULL CHECK (state IN ('processing','completed')),
    status_code      integer CHECK (status_code BETWEEN 100 AND 599),
    response_headers jsonb,
    response_body    bytea,
    created_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    PRIMARY KEY (user_id, token_id, method, route_pattern, idempotency_key),
    CHECK (expires_at > created_at),
    CHECK (
        (state = 'processing' AND status_code IS NULL
                              AND response_headers IS NULL
                              AND response_body IS NULL)
        OR
        (state = 'completed' AND status_code IS NOT NULL
                             AND response_headers IS NOT NULL
                             AND response_body IS NOT NULL)
    )
);

CREATE INDEX idempotency_expiry ON idempotency_records (expires_at);

ALTER TABLE task_activity
    ADD COLUMN request_id text,
    ADD COLUMN auth_method text CHECK (auth_method IN ('session','api_token')),
    ADD COLUMN api_token_id uuid REFERENCES api_tokens(id),
    ADD COLUMN token_name_snapshot text,
    ADD CONSTRAINT task_activity_auth_shape CHECK (
        (auth_method IS NULL AND api_token_id IS NULL)
        OR
        (auth_method = 'session' AND api_token_id IS NULL)
        OR
        (auth_method = 'api_token' AND api_token_id IS NOT NULL)
    );

ALTER TABLE project_activity
    ADD COLUMN request_id text,
    ADD COLUMN auth_method text CHECK (auth_method IN ('session','api_token')),
    ADD COLUMN api_token_id uuid REFERENCES api_tokens(id),
    ADD COLUMN token_name_snapshot text,
    ADD CONSTRAINT project_activity_auth_shape CHECK (
        (auth_method IS NULL AND api_token_id IS NULL)
        OR
        (auth_method = 'session' AND api_token_id IS NULL)
        OR
        (auth_method = 'api_token' AND api_token_id IS NOT NULL)
    );
