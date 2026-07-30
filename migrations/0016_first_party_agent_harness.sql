-- Durable first-party Agent execution, delegation provenance, and reliable
-- channel delivery. Raw model prompts, chat history, and bearer credentials
-- are intentionally absent from these tables.

CREATE TABLE agent_runs (
    id                         uuid PRIMARY KEY,
    provider                   text NOT NULL CHECK (provider IN ('lark')),
    tenant_id                  text NOT NULL CHECK (btrim(tenant_id) <> ''),
    conversation_id            text NOT NULL CHECK (btrim(conversation_id) <> ''),
    trigger_message_id         text NOT NULL CHECK (btrim(trigger_message_id) <> ''),
    provider_event_id          text NOT NULL CHECK (btrim(provider_event_id) <> ''),
    thread_root_message_id     text,
    reply_parent_message_id    text,
    trigger_occurred_at        timestamptz NOT NULL,
    initiating_user_id         uuid NOT NULL REFERENCES users(id),
    initiating_subject_id      text NOT NULL CHECK (btrim(initiating_subject_id) <> ''),
    status                     text NOT NULL CHECK (
                                   status IN (
                                       'queued','running','waiting_user',
                                       'succeeded','failed','cancelled'
                                   )
                               ),
    command_kind               text NOT NULL CHECK (
                                   command_kind IN ('direct','discussion')
                               ),
    model                      text NOT NULL CHECK (btrim(model) <> ''),
    prompt_version             text NOT NULL CHECK (btrim(prompt_version) <> ''),
    attempt_count              integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    clarification_rounds       integer NOT NULL DEFAULT 0
                                   CHECK (clarification_rounds BETWEEN 0 AND 3),
    clarification_message_id   text,
    clarification_interrupt_id text,
    clarification_expires_at   timestamptz,
    context_messages_used      integer NOT NULL DEFAULT 0
                                   CHECK (context_messages_used BETWEEN 0 AND 100),
    lease_owner                text,
    lease_expires_at           timestamptz,
    available_at               timestamptz NOT NULL,
    created_task_id            uuid UNIQUE REFERENCES tasks(id),
    created_task_number        bigint UNIQUE,
    terminal_error_category    text,
    terminal_error_detail      text,
    created_at                 timestamptz NOT NULL,
    updated_at                 timestamptz NOT NULL,
    completed_at               timestamptz,
    UNIQUE (provider, tenant_id, provider_event_id),
    UNIQUE (provider, tenant_id, trigger_message_id),
    CHECK (
        (status = 'running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (status <> 'running' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK (
        (status = 'waiting_user'
            AND clarification_message_id IS NOT NULL
            AND clarification_interrupt_id IS NOT NULL
            AND clarification_expires_at IS NOT NULL)
        OR
        (status <> 'waiting_user')
    ),
    CHECK (
        (created_task_id IS NULL AND created_task_number IS NULL)
        OR
        (created_task_id IS NOT NULL AND created_task_number IS NOT NULL)
    ),
    CHECK (
        (status IN ('succeeded','failed','cancelled') AND completed_at IS NOT NULL)
        OR
        (status NOT IN ('succeeded','failed','cancelled') AND completed_at IS NULL)
    )
);

CREATE INDEX agent_runs_work_queue
    ON agent_runs (available_at, created_at, id)
    WHERE status = 'queued';

CREATE INDEX agent_runs_expired_leases
    ON agent_runs (lease_expires_at, id)
    WHERE status = 'running';

CREATE INDEX agent_runs_waiting_reply
    ON agent_runs (
        provider, tenant_id, conversation_id, clarification_message_id
    )
    WHERE status = 'waiting_user';

CREATE INDEX agent_runs_waiting_expiry
    ON agent_runs (clarification_expires_at, id)
    WHERE status = 'waiting_user';

-- The command and pending clarification answer are required to survive the
-- asynchronous webhook/worker boundary. They are encrypted independently
-- from the Eino checkpoint so a queued clarification answer cannot overwrite
-- the checkpoint it must resume.
CREATE TABLE agent_run_inputs (
    run_id                      uuid PRIMARY KEY REFERENCES agent_runs(id) ON DELETE CASCADE,
    encryption_key_id           text NOT NULL CHECK (btrim(encryption_key_id) <> ''),
    command_ciphertext          bytea NOT NULL CHECK (octet_length(command_ciphertext) > 0),
    pending_resume_ciphertext   bytea,
    updated_at                  timestamptz NOT NULL
);

CREATE TABLE agent_run_checkpoints (
    run_id             uuid PRIMARY KEY REFERENCES agent_runs(id) ON DELETE CASCADE,
    format_version     integer NOT NULL CHECK (format_version > 0),
    eino_version       text NOT NULL CHECK (btrim(eino_version) <> ''),
    model              text NOT NULL CHECK (btrim(model) <> ''),
    encryption_key_id  text NOT NULL CHECK (btrim(encryption_key_id) <> ''),
    ciphertext         bytea NOT NULL CHECK (octet_length(ciphertext) > 0),
    updated_at         timestamptz NOT NULL
);

CREATE TABLE agent_tool_calls (
    run_id              uuid NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    tool_call_id        text NOT NULL CHECK (btrim(tool_call_id) <> ''),
    tool_name           text NOT NULL CHECK (btrim(tool_name) <> ''),
    argument_hash       bytea NOT NULL CHECK (octet_length(argument_hash) = 32),
    argument_summary    jsonb NOT NULL DEFAULT '{}'::jsonb,
    state               text NOT NULL CHECK (
                            state IN ('running','completed','failed')
                        ),
    result               jsonb,
    error_category       text,
    started_at           timestamptz NOT NULL,
    completed_at         timestamptz,
    PRIMARY KEY (run_id, tool_call_id),
    CHECK (
        (state = 'running' AND completed_at IS NULL)
        OR
        (state IN ('completed','failed') AND completed_at IS NOT NULL)
    ),
    CHECK (
        (state = 'completed' AND result IS NOT NULL AND error_category IS NULL)
        OR
        (state = 'failed' AND error_category IS NOT NULL)
        OR
        (state = 'running' AND result IS NULL AND error_category IS NULL)
    )
);

CREATE TABLE agent_message_outbox (
    id                    uuid PRIMARY KEY,
    run_id                uuid NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    deduplication_key     text NOT NULL UNIQUE CHECK (btrim(deduplication_key) <> ''),
    kind                  text NOT NULL CHECK (
                              kind IN (
                                  'clarification','success','permission_failure',
                                  'terminal_failure','retrying','expired'
                              )
                          ),
    target_message_id     text NOT NULL CHECK (btrim(target_message_id) <> ''),
    body                  text NOT NULL CHECK (btrim(body) <> ''),
    state                 text NOT NULL CHECK (
                              state IN ('pending','delivering','delivered','failed')
                          ),
    attempt_count         integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    available_at          timestamptz NOT NULL,
    lease_owner           text,
    lease_expires_at      timestamptz,
    provider_message_id   text,
    provider_error_code   text,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    delivered_at          timestamptz,
    CHECK (
        (state = 'delivering' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (state <> 'delivering' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK (
        (state = 'delivered'
            AND provider_message_id IS NOT NULL
            AND delivered_at IS NOT NULL)
        OR
        (state <> 'delivered' AND delivered_at IS NULL)
    )
);

CREATE INDEX agent_message_outbox_queue
    ON agent_message_outbox (available_at, created_at, id)
    WHERE state IN ('pending','failed');

ALTER TABLE api_request_audit_events
    DROP CONSTRAINT api_request_audit_events_auth_method_check,
    ADD COLUMN agent_run_id uuid,
    ADD CONSTRAINT api_request_audit_events_auth_method_check
        CHECK (auth_method IN ('session','api_token','agent_delegate','unknown'));

CREATE INDEX api_request_audit_agent_run
    ON api_request_audit_events (agent_run_id, occurred_at DESC, id DESC);

ALTER TABLE business_audit_events
    DROP CONSTRAINT business_audit_events_auth_method_check,
    DROP CONSTRAINT business_audit_events_check,
    ADD COLUMN agent_run_id uuid,
    ADD CONSTRAINT business_audit_events_auth_method_check
        CHECK (auth_method IN ('session','api_token','agent_delegate')),
    ADD CONSTRAINT business_audit_events_auth_shape CHECK (
        (auth_method = 'session' AND token_id IS NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'api_token' AND token_id IS NOT NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'agent_delegate' AND token_id IS NULL AND agent_run_id IS NOT NULL)
    );

CREATE INDEX business_audit_agent_run
    ON business_audit_events (agent_run_id, occurred_at, id);

ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_pkey,
    ALTER COLUMN token_id DROP NOT NULL,
    ADD COLUMN credential_kind text,
    ADD COLUMN credential_id uuid,
    ADD COLUMN agent_run_id uuid REFERENCES agent_runs(id) ON DELETE CASCADE;

UPDATE idempotency_records
SET credential_kind = 'api_token',
    credential_id = token_id;

ALTER TABLE idempotency_records
    ALTER COLUMN credential_kind SET NOT NULL,
    ALTER COLUMN credential_id SET NOT NULL,
    ADD CONSTRAINT idempotency_records_credential_kind_check
        CHECK (credential_kind IN ('api_token','agent_delegate')),
    ADD CONSTRAINT idempotency_records_credential_shape CHECK (
        (credential_kind = 'api_token'
            AND token_id IS NOT NULL
            AND agent_run_id IS NULL
            AND credential_id = token_id)
        OR
        (credential_kind = 'agent_delegate'
            AND token_id IS NULL
            AND agent_run_id IS NOT NULL
            AND credential_id = agent_run_id)
    ),
    ADD PRIMARY KEY (
        user_id, credential_kind, credential_id,
        method, route_pattern, idempotency_key
    );

ALTER TABLE task_activity
    DROP CONSTRAINT task_activity_auth_method_check,
    DROP CONSTRAINT task_activity_auth_shape,
    ADD COLUMN agent_run_id uuid,
    ADD CONSTRAINT task_activity_auth_method_check
        CHECK (auth_method IN ('session','api_token','agent_delegate')),
    ADD CONSTRAINT task_activity_auth_shape CHECK (
        (auth_method IS NULL AND api_token_id IS NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'session' AND api_token_id IS NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'api_token' AND api_token_id IS NOT NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'agent_delegate' AND api_token_id IS NULL AND agent_run_id IS NOT NULL)
    );

ALTER TABLE project_activity
    DROP CONSTRAINT project_activity_auth_method_check,
    DROP CONSTRAINT project_activity_auth_shape,
    ADD COLUMN agent_run_id uuid,
    ADD CONSTRAINT project_activity_auth_method_check
        CHECK (auth_method IN ('session','api_token','agent_delegate')),
    ADD CONSTRAINT project_activity_auth_shape CHECK (
        (auth_method IS NULL AND api_token_id IS NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'session' AND api_token_id IS NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'api_token' AND api_token_id IS NOT NULL AND agent_run_id IS NULL)
        OR
        (auth_method = 'agent_delegate' AND api_token_id IS NULL AND agent_run_id IS NOT NULL)
    );
