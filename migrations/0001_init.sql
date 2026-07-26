CREATE TABLE users (
    id             uuid PRIMARY KEY,
    name           text NOT NULL,
    email          text NOT NULL UNIQUE,
    roles          text[] NOT NULL DEFAULT '{}',
    feishu_open_id text,
    active         boolean NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE bounties (
    id                  uuid PRIMARY KEY,
    type                text NOT NULL,
    parent_id           uuid REFERENCES bounties(id),
    title               text NOT NULL,
    goal                text NOT NULL DEFAULT '',
    acceptance_criteria text NOT NULL DEFAULT '',
    visibility          text NOT NULL DEFAULT 'PUBLIC',
    restriction         text,
    directed_to         uuid REFERENCES users(id),
    business_lines      jsonb NOT NULL DEFAULT '[]'::jsonb,
    value_level         text,
    difficulty          text,
    commitment          text NOT NULL DEFAULT 'COMMITTED',
    completion          text,
    status              text NOT NULL DEFAULT 'DRAFT',
    sponsor_id          uuid NOT NULL REFERENCES users(id),
    claimed_by          uuid REFERENCES users(id),
    claimed_at          timestamptz,
    due_date            date,
    person_days         numeric,
    baseline_system_id  uuid,
    retrospective       text,
    settled_score       numeric,
    settled_at          timestamptz,
    completed_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_bounties_status ON bounties (status);
CREATE INDEX idx_bounties_completed_at ON bounties (completed_at DESC);

CREATE TABLE credits (
    id           uuid PRIMARY KEY,
    bounty_id    uuid NOT NULL REFERENCES bounties(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id),
    role         text NOT NULL,
    nominated_by uuid REFERENCES users(id),
    evidence     text,
    status       text NOT NULL DEFAULT 'PENDING',
    confirmed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bounty_id, user_id, role)
);

CREATE INDEX idx_credits_user_status ON credits (user_id, status);
CREATE INDEX idx_credits_bounty ON credits (bounty_id);
