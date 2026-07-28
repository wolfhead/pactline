ALTER TABLE users
    ALTER COLUMN email DROP NOT NULL,
    ADD COLUMN avatar_url text,
    ADD COLUMN platform_role text NOT NULL DEFAULT 'MEMBER'
        CHECK (platform_role IN ('ADMIN', 'MEMBER')),
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

CREATE UNIQUE INDEX users_single_admin
    ON users (platform_role) WHERE platform_role = 'ADMIN';

CREATE TABLE external_identities (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL UNIQUE REFERENCES users(id),
    provider text NOT NULL,
    tenant_id text NOT NULL,
    subject_id text NOT NULL,
    provider_profile jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, tenant_id, subject_id)
);

CREATE TABLE oauth_credentials (
    external_identity_id uuid PRIMARY KEY REFERENCES external_identities(id) ON DELETE CASCADE,
    access_token_ciphertext bytea NOT NULL,
    refresh_token_ciphertext bytea NOT NULL,
    access_token_expires_at timestamptz NOT NULL,
    refresh_token_expires_at timestamptz NOT NULL,
    encryption_key_id text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE invitations (
    id uuid PRIMARY KEY,
    provider text NOT NULL,
    tenant_id text NOT NULL,
    target_subject_id text NOT NULL,
    target_snapshot jsonb NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    created_by_user_id uuid NOT NULL REFERENCES users(id),
    expires_at timestamptz NOT NULL,
    accepted_by_user_id uuid REFERENCES users(id),
    accepted_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invitations_one_pending_subject
    ON invitations (provider, tenant_id, target_subject_id)
    WHERE status = 'pending';

CREATE TABLE invitation_deliveries (
    id uuid PRIMARY KEY,
    invitation_id uuid NOT NULL REFERENCES invitations(id) ON DELETE CASCADE,
    channel text NOT NULL CHECK (channel IN ('provider_dm', 'copied_link')),
    status text NOT NULL CHECK (status IN ('delivered', 'failed')),
    provider_reference text,
    error_category text,
    attempted_at timestamptz NOT NULL
);

CREATE INDEX invitation_deliveries_invitation
    ON invitation_deliveries (invitation_id, attempted_at DESC);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    secret_hash bytea NOT NULL,
    csrf_secret_hash bytea NOT NULL,
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    last_provider_verified_at timestamptz,
    provider_failure_since timestamptz,
    revoked_at timestamptz,
    revoke_reason text,
    CHECK (idle_expires_at <= absolute_expires_at)
);

CREATE INDEX sessions_active_user
    ON sessions (user_id, absolute_expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE authorization_transactions (
    id uuid PRIMARY KEY,
    purpose text NOT NULL CHECK (purpose IN ('login', 'invitation')),
    state_hash bytea NOT NULL UNIQUE,
    invitation_id uuid REFERENCES invitations(id),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (purpose = 'login' AND invitation_id IS NULL)
        OR (purpose = 'invitation' AND invitation_id IS NOT NULL)
    )
);

CREATE TABLE impersonations (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES sessions(id),
    actor_user_id uuid NOT NULL REFERENCES users(id),
    subject_user_id uuid NOT NULL REFERENCES users(id),
    started_at timestamptz NOT NULL,
    ended_at timestamptz,
    CHECK (actor_user_id <> subject_user_id)
);

CREATE UNIQUE INDEX impersonations_one_active_session
    ON impersonations (session_id) WHERE ended_at IS NULL;

CREATE TABLE identity_audit_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    actor_user_id uuid REFERENCES users(id),
    subject_user_id uuid REFERENCES users(id),
    invitation_id uuid REFERENCES invitations(id),
    session_id uuid REFERENCES sessions(id),
    request_id text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL
);

CREATE INDEX identity_audit_occurred
    ON identity_audit_events (occurred_at DESC, id DESC);

-- All six original users are test identities. Before consolidating them,
-- retain exactly the most authoritative credit that would occupy each
-- post-mapping uniqueness key.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY bounty_id,
                   CASE WHEN user_id = ANY(ARRAY[
                       '00000000-0000-0000-0000-000000000001'::uuid,
                       '00000000-0000-0000-0000-000000000002'::uuid,
                       '00000000-0000-0000-0000-000000000003'::uuid,
                       '00000000-0000-0000-0000-000000000004'::uuid,
                       '00000000-0000-0000-0000-000000000005'::uuid,
                       '00000000-0000-0000-0000-000000000006'::uuid
                   ]) THEN '00000000-0000-0000-0000-000000000001'::uuid
                   ELSE user_id END,
                   role
               ORDER BY
                   CASE status
                       WHEN 'CONFIRMED' THEN 0
                       WHEN 'PENDING' THEN 1
                       ELSE 2
                   END,
                   confirmed_at NULLS LAST,
                   created_at,
                   id
           ) AS row_rank
    FROM credits
)
DELETE FROM credits
USING ranked
WHERE credits.id = ranked.id AND ranked.row_rank > 1;

DO $$
DECLARE
    primary_seed_id uuid := '00000000-0000-0000-0000-000000000001';
    secondary_seed_ids uuid[] := ARRAY[
        '00000000-0000-0000-0000-000000000002'::uuid,
        '00000000-0000-0000-0000-000000000003'::uuid,
        '00000000-0000-0000-0000-000000000004'::uuid,
        '00000000-0000-0000-0000-000000000005'::uuid,
        '00000000-0000-0000-0000-000000000006'::uuid
    ];
BEGIN
    UPDATE bounties SET directed_to = primary_seed_id WHERE directed_to = ANY(secondary_seed_ids);
    UPDATE bounties SET sponsor_id = primary_seed_id WHERE sponsor_id = ANY(secondary_seed_ids);
    UPDATE bounties SET claimed_by = primary_seed_id WHERE claimed_by = ANY(secondary_seed_ids);
    UPDATE credits SET user_id = primary_seed_id WHERE user_id = ANY(secondary_seed_ids);
    UPDATE credits SET nominated_by = primary_seed_id WHERE nominated_by = ANY(secondary_seed_ids);
    UPDATE calibrations SET created_by = primary_seed_id WHERE created_by = ANY(secondary_seed_ids);
    UPDATE tasks SET assignee_id = primary_seed_id WHERE assignee_id = ANY(secondary_seed_ids);
    UPDATE tasks SET creator_id = primary_seed_id WHERE creator_id = ANY(secondary_seed_ids);
    UPDATE task_comments SET author_id = primary_seed_id WHERE author_id = ANY(secondary_seed_ids);
    UPDATE task_activity SET actor_id = primary_seed_id WHERE actor_id = ANY(secondary_seed_ids);
    UPDATE projects SET owner_id = primary_seed_id WHERE owner_id = ANY(secondary_seed_ids);
    UPDATE projects SET creator_id = primary_seed_id WHERE creator_id = ANY(secondary_seed_ids);
    UPDATE acceptance_checks SET checked_by_user_id = primary_seed_id
        WHERE checked_by_user_id = ANY(secondary_seed_ids);
    UPDATE project_activity SET actor_id = primary_seed_id WHERE actor_id = ANY(secondary_seed_ids);
    UPDATE users SET active = false, updated_at = now()
        WHERE id = ANY(secondary_seed_ids);
END $$;
