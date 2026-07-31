-- External coding Agents may execute only Tasks that their assigned human
-- explicitly makes available. Existing Tasks remain human-only.

ALTER TABLE tasks
    ADD COLUMN execution_mode text NOT NULL DEFAULT 'human_only',
    ADD CONSTRAINT tasks_execution_mode_check
        CHECK (execution_mode IN ('human_only', 'agent_allowed'));

CREATE INDEX idx_tasks_agent_candidates
    ON tasks (assignee_id, project_id, priority, due_date, number)
    WHERE execution_mode = 'agent_allowed'
      AND status = 'todo'
      AND archived_at IS NULL;

-- A Claim binds the Task to the external client's real conversation context.
-- Active work is deliberately not transferable between client sessions.
CREATE TABLE task_claims (
    id                  uuid PRIMARY KEY,
    task_id             uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    claimed_by_user_id  uuid NOT NULL REFERENCES users(id),
    claimed_via_token_id uuid NOT NULL REFERENCES api_tokens(id),
    token_name_snapshot text NOT NULL,
    client_kind         text NOT NULL CHECK (btrim(client_kind) <> ''),
    client_session_id   text NOT NULL CHECK (btrim(client_session_id) <> ''),
    status              text NOT NULL CHECK (
                            status IN (
                                'active','waiting_human','submitted',
                                'released','expired'
                            )
                        ),
    version             bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    expires_at          timestamptz NOT NULL,
    terminal_reason     text,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    completed_at        timestamptz,
    CHECK (
        (status IN ('submitted','released','expired') AND completed_at IS NOT NULL)
        OR
        (status IN ('active','waiting_human') AND completed_at IS NULL)
    ),
    CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX task_claims_one_unfinished_per_task
    ON task_claims (task_id)
    WHERE status IN ('active','waiting_human');

CREATE UNIQUE INDEX task_claims_one_unfinished_per_session
    ON task_claims (claimed_by_user_id, client_kind, client_session_id)
    WHERE status IN ('active','waiting_human');

CREATE INDEX task_claims_session_history
    ON task_claims (
        claimed_by_user_id, client_kind, client_session_id, created_at DESC, id
    );

CREATE INDEX task_claims_expiry
    ON task_claims (expires_at, id)
    WHERE status IN ('active','waiting_human');

-- Agent interaction is immutable and remains separate from editable Task
-- comments. The Claim itself is the conversation root.
CREATE TABLE task_claim_messages (
    id                  uuid PRIMARY KEY,
    claim_id            uuid NOT NULL REFERENCES task_claims(id) ON DELETE CASCADE,
    author_type         text NOT NULL CHECK (
                            author_type IN ('user','agent','system')
                        ),
    author_user_id      uuid REFERENCES users(id),
    kind                text NOT NULL CHECK (
                            kind IN (
                                'progress','question','answer',
                                'handoff','submission'
                            )
                        ),
    body                text NOT NULL CHECK (btrim(body) <> ''),
    reply_to_message_id uuid REFERENCES task_claim_messages(id),
    request_id          text NOT NULL CHECK (btrim(request_id) <> ''),
    api_token_id        uuid REFERENCES api_tokens(id),
    token_name_snapshot text,
    created_at          timestamptz NOT NULL,
    CHECK (
        (author_type = 'agent'
            AND author_user_id IS NOT NULL
            AND api_token_id IS NOT NULL
            AND token_name_snapshot IS NOT NULL
            AND kind IN ('progress','question','handoff','submission'))
        OR
        (author_type = 'user'
            AND author_user_id IS NOT NULL
            AND api_token_id IS NULL
            AND token_name_snapshot IS NULL
            AND kind = 'answer')
        OR
        (author_type = 'system'
            AND author_user_id IS NULL
            AND api_token_id IS NULL
            AND token_name_snapshot IS NULL)
    )
);

CREATE INDEX task_claim_messages_claim
    ON task_claim_messages (claim_id, created_at, id);

-- Executor Tokens can read work and invoke only the Claim-oriented mutation
-- surface. Existing work:write Tokens retain their current behavior.
ALTER TABLE api_tokens
    DROP CONSTRAINT api_tokens_scopes_check,
    ADD CONSTRAINT api_tokens_scopes_check
        CHECK (
            scopes <@ ARRAY['work:read','work:execute','work:write']::text[]
        );
