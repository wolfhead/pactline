CREATE TABLE projects (
    id            uuid PRIMARY KEY,
    number        bigserial NOT NULL,
    name          text NOT NULL,
    outcome       text NOT NULL,
    description   text NOT NULL DEFAULT '',
    owner_id      uuid NOT NULL REFERENCES users(id),
    creator_id    uuid NOT NULL REFERENCES users(id),
    status        text NOT NULL DEFAULT 'planned'
                  CHECK (status IN ('planned', 'active', 'paused', 'completed', 'cancelled')),
    target_date   date,
    completed_at  timestamptz,
    cancelled_at  timestamptz,
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (btrim(name) <> ''),
    CHECK (btrim(outcome) <> '')
);

CREATE UNIQUE INDEX idx_projects_number ON projects (number);
CREATE INDEX idx_projects_owner ON projects (owner_id);
CREATE INDEX idx_projects_status ON projects (status, target_date, number);
CREATE INDEX idx_projects_archived ON projects (archived_at);

CREATE TABLE milestones (
    id            uuid PRIMARY KEY,
    project_id    uuid NOT NULL REFERENCES projects(id),
    name          text NOT NULL,
    outcome       text NOT NULL,
    description   text NOT NULL DEFAULT '',
    status        text NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open', 'completed', 'cancelled')),
    target_date   date,
    position      integer NOT NULL CHECK (position >= 0),
    completed_at  timestamptz,
    cancelled_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (btrim(name) <> ''),
    CHECK (btrim(outcome) <> ''),
    UNIQUE (project_id, id)
);

CREATE INDEX idx_milestones_project ON milestones (project_id, position, created_at);

CREATE TABLE acceptance_criteria (
    id                         uuid PRIMARY KEY,
    project_id                 uuid REFERENCES projects(id),
    milestone_id               uuid REFERENCES milestones(id),
    criterion                  text NOT NULL,
    verification_instructions  text NOT NULL,
    revision                   integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    position                   integer NOT NULL CHECK (position >= 0),
    archived_at                timestamptz,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    CHECK ((project_id IS NOT NULL)::integer + (milestone_id IS NOT NULL)::integer = 1),
    CHECK (btrim(criterion) <> ''),
    CHECK (btrim(verification_instructions) <> '')
);

CREATE INDEX idx_acceptance_criteria_project
    ON acceptance_criteria (project_id, position) WHERE project_id IS NOT NULL;
CREATE INDEX idx_acceptance_criteria_milestone
    ON acceptance_criteria (milestone_id, position) WHERE milestone_id IS NOT NULL;

CREATE TABLE acceptance_checks (
    id                   uuid PRIMARY KEY,
    criterion_id         uuid NOT NULL REFERENCES acceptance_criteria(id),
    criterion_revision   integer NOT NULL CHECK (criterion_revision > 0),
    outcome              text NOT NULL CHECK (outcome IN ('passed', 'failed', 'unable', 'waived')),
    evidence             text NOT NULL CHECK (btrim(evidence) <> ''),
    checker_type         text NOT NULL CHECK (checker_type IN ('user', 'agent', 'system')),
    checked_by_user_id   uuid REFERENCES users(id),
    checker_ref          text,
    checked_at           timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (checker_type = 'user' AND checked_by_user_id IS NOT NULL)
        OR
        (checker_type IN ('agent', 'system') AND checked_by_user_id IS NULL AND btrim(checker_ref) <> '')
    ),
    CHECK (outcome <> 'waived' OR checker_type = 'user')
);

CREATE INDEX idx_acceptance_checks_current
    ON acceptance_checks (criterion_id, criterion_revision, checked_at DESC, id DESC);

CREATE TABLE project_activity (
    id            uuid PRIMARY KEY,
    project_id    uuid NOT NULL REFERENCES projects(id),
    milestone_id  uuid REFERENCES milestones(id),
    actor_id      uuid NOT NULL REFERENCES users(id),
    action        text NOT NULL,
    reason        text,
    old_value     text,
    new_value     text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_activity_project ON project_activity (project_id, created_at, id);

ALTER TABLE tasks
    ADD COLUMN project_id uuid REFERENCES projects(id),
    ADD COLUMN milestone_id uuid,
    ADD CONSTRAINT tasks_milestone_requires_project
        CHECK (milestone_id IS NULL OR project_id IS NOT NULL),
    ADD CONSTRAINT tasks_project_milestone_fk
        FOREIGN KEY (project_id, milestone_id)
        REFERENCES milestones(project_id, id);

CREATE INDEX idx_tasks_project ON tasks (project_id, status);
CREATE INDEX idx_tasks_milestone ON tasks (milestone_id, status);
