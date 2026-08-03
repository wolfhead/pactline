CREATE TABLE agent_conversations (
    id                   uuid PRIMARY KEY,
    provider             text NOT NULL CHECK (provider IN ('lark')),
    tenant_id            text NOT NULL CHECK (btrim(tenant_id) <> ''),
    external_id          text NOT NULL CHECK (btrim(external_id) <> ''),
    name                 text NOT NULL DEFAULT '',
    enabled              boolean NOT NULL DEFAULT true,
    binding_active       boolean NOT NULL DEFAULT false,
    default_project_id   uuid REFERENCES projects(id),
    business_context     text NOT NULL DEFAULT ''
                         CHECK (length(business_context) <= 4000),
    version              bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by           uuid NOT NULL REFERENCES users(id),
    updated_by           uuid NOT NULL REFERENCES users(id),
    last_seen_at         timestamptz NOT NULL,
    created_at           timestamptz NOT NULL,
    updated_at           timestamptz NOT NULL,
    UNIQUE (provider, tenant_id, external_id),
    CHECK (NOT binding_active OR default_project_id IS NOT NULL)
);

CREATE INDEX agent_conversations_project
    ON agent_conversations (default_project_id, binding_active, updated_at DESC);

CREATE TABLE agent_conversation_revisions (
    id                   uuid PRIMARY KEY,
    conversation_id      uuid NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    version              bigint NOT NULL CHECK (version > 0),
    enabled              boolean NOT NULL,
    binding_active       boolean NOT NULL,
    default_project_id   uuid REFERENCES projects(id),
    business_context     text NOT NULL DEFAULT ''
                         CHECK (length(business_context) <= 4000),
    updated_by           uuid NOT NULL REFERENCES users(id),
    created_at           timestamptz NOT NULL,
    UNIQUE (conversation_id, version),
    CHECK (NOT binding_active OR default_project_id IS NOT NULL)
);

ALTER TABLE agent_runs
    ADD COLUMN conversation_revision_id uuid
        REFERENCES agent_conversation_revisions(id);

ALTER TABLE agent_runs
    DROP CONSTRAINT agent_runs_command_kind_check,
    ADD CONSTRAINT agent_runs_command_kind_check
        CHECK (command_kind IN ('direct', 'discussion', 'configuration'));

CREATE INDEX agent_runs_conversation_revision
    ON agent_runs (conversation_revision_id)
    WHERE conversation_revision_id IS NOT NULL;
