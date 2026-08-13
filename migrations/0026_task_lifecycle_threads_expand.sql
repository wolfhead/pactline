-- Add the target Task lifecycle and collaboration model without interpreting
-- existing rows. Historical Tasks retain NULL target columns until the
-- separately approved data migration classifies them.

ALTER TABLE tasks
    ADD COLUMN phase text,
    ADD COLUMN activity_state text,
    ADD COLUMN review_cycle bigint,
    ADD COLUMN active_issue_thread_id uuid,
    ADD CONSTRAINT tasks_target_lifecycle_shape CHECK (
        (phase IS NULL AND activity_state IS NULL AND review_cycle IS NULL)
        OR
        (
            phase IN (
                'backlog','ready','in_progress','in_review','done','cancelled'
            )
            AND review_cycle >= 0
            AND (
                (
                    phase IN ('backlog','ready')
                    AND activity_state IS NULL
                )
                OR
                (
                    phase IN ('in_progress','in_review')
                    AND activity_state IN (
                        'available','working','needs_resolution'
                    )
                )
                OR
                (
                    phase IN ('done','cancelled')
                    AND activity_state IS NULL
                )
            )
        )
    );

-- Defaults affect only rows inserted after this migration. PostgreSQL does
-- not rewrite existing NULL rows when a default is set separately.
ALTER TABLE tasks
    ALTER COLUMN phase SET DEFAULT 'backlog',
    ALTER COLUMN review_cycle SET DEFAULT 0;

CREATE INDEX idx_tasks_target_lifecycle
    ON tasks (project_id, phase, activity_state, priority, due_date, number)
    WHERE phase IS NOT NULL AND archived_at IS NULL;

CREATE TABLE task_threads (
    id                    uuid PRIMARY KEY,
    task_id               uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    role                  text NOT NULL CHECK (role IN ('main','issue')),
    issue_type            text CHECK (
                              issue_type IN (
                                  'decision_required','dependency_required'
                              )
                          ),
    issue_status          text CHECK (issue_status IN ('open','resolved')),
    opened_from_phase     text CHECK (
                              opened_from_phase IN ('in_progress','in_review')
                          ),
    opened_by_type        text CHECK (
                              opened_by_type IN ('user','agent','system')
                          ),
    opened_by_user_id     uuid REFERENCES users(id),
    opened_by_ref         text,
    resolved_by_type      text CHECK (
                              resolved_by_type IN ('user','agent','system')
                          ),
    resolved_by_user_id   uuid REFERENCES users(id),
    resolved_by_ref       text,
    version               bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    resolved_at           timestamptz,
    CHECK (
        (
            role = 'main'
            AND issue_type IS NULL
            AND issue_status IS NULL
            AND opened_from_phase IS NULL
            AND opened_by_type IS NULL
            AND opened_by_user_id IS NULL
            AND opened_by_ref IS NULL
            AND resolved_by_type IS NULL
            AND resolved_by_user_id IS NULL
            AND resolved_by_ref IS NULL
            AND resolved_at IS NULL
        )
        OR
        (
            role = 'issue'
            AND issue_type IS NOT NULL
            AND issue_status IS NOT NULL
            AND opened_from_phase IS NOT NULL
            AND (
                (opened_by_type = 'user'
                    AND opened_by_user_id IS NOT NULL
                    AND opened_by_ref IS NULL)
                OR
                (opened_by_type IN ('agent','system')
                    AND opened_by_user_id IS NULL
                    AND btrim(opened_by_ref) <> '')
            )
            AND (
                (issue_status = 'open'
                    AND resolved_by_type IS NULL
                    AND resolved_by_user_id IS NULL
                    AND resolved_by_ref IS NULL
                    AND resolved_at IS NULL)
                OR
                (issue_status = 'resolved'
                    AND resolved_at IS NOT NULL
                    AND (
                        (resolved_by_type = 'user'
                            AND resolved_by_user_id IS NOT NULL
                            AND resolved_by_ref IS NULL)
                        OR
                        (resolved_by_type IN ('agent','system')
                            AND resolved_by_user_id IS NULL
                            AND btrim(resolved_by_ref) <> '')
                    ))
            )
        )
    )
);

CREATE UNIQUE INDEX task_threads_one_main_per_task
    ON task_threads (task_id) WHERE role = 'main';

CREATE UNIQUE INDEX task_threads_one_open_issue_per_task
    ON task_threads (task_id) WHERE role = 'issue' AND issue_status = 'open';

CREATE INDEX task_threads_task_history
    ON task_threads (task_id, created_at, id);

ALTER TABLE tasks
    ADD CONSTRAINT tasks_active_issue_thread_fk
        FOREIGN KEY (active_issue_thread_id)
        REFERENCES task_threads(id)
        DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT tasks_active_issue_shape CHECK (
        (phase IS NULL AND active_issue_thread_id IS NULL)
        OR
        (
            phase IN ('backlog','ready')
            AND activity_state IS NULL
            AND active_issue_thread_id IS NULL
        )
        OR
        (
            phase IN ('in_progress','in_review')
            AND (
                (activity_state = 'needs_resolution'
                    AND active_issue_thread_id IS NOT NULL)
                OR
                (activity_state IN ('available','working')
                    AND active_issue_thread_id IS NULL)
            )
        )
        OR
        (
            phase IN ('done','cancelled')
            AND active_issue_thread_id IS NULL
        )
    );

-- Every newly created Task gets its durable Main Thread in the same INSERT
-- statement. Existing Tasks are deliberately not backfilled here.
CREATE FUNCTION create_task_main_thread() RETURNS trigger AS $$
BEGIN
    IF NEW.phase IS NOT NULL THEN
        INSERT INTO task_threads (
            id, task_id, role, version, created_at, updated_at
        ) VALUES (
            gen_random_uuid(), NEW.id, 'main', 1, NEW.created_at, NEW.created_at
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tasks_create_main_thread
    AFTER INSERT ON tasks
    FOR EACH ROW EXECUTE FUNCTION create_task_main_thread();

CREATE TABLE task_thread_items (
    id                    uuid PRIMARY KEY,
    thread_id             uuid NOT NULL REFERENCES task_threads(id) ON DELETE CASCADE,
    kind                  text NOT NULL CHECK (
                              kind IN (
                                  'message','progress','handoff','work_submission',
                                  'review_outcome','resolution_request','resolution',
                                  'issue_resolution','system_event'
                              )
                          ),
    author_type           text NOT NULL CHECK (
                              author_type IN ('user','agent','system')
                          ),
    author_user_id        uuid REFERENCES users(id),
    author_ref            text,
    body                  text,
    typed_payload         jsonb,
    reply_to_item_id      uuid REFERENCES task_thread_items(id),
    request_id            text NOT NULL CHECK (btrim(request_id) <> ''),
    version               bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    deleted_at            timestamptz,
    CHECK (
        (author_type = 'user'
            AND author_user_id IS NOT NULL
            AND author_ref IS NULL)
        OR
        (author_type IN ('agent','system')
            AND author_user_id IS NULL
            AND btrim(author_ref) <> '')
    ),
    CHECK (body IS NOT NULL OR typed_payload IS NOT NULL),
    CHECK (deleted_at IS NULL OR kind = 'message'),
    CHECK (
        (kind = 'issue_resolution' AND typed_payload IS NOT NULL)
        OR kind <> 'issue_resolution'
    )
);

CREATE INDEX task_thread_items_timeline
    ON task_thread_items (thread_id, created_at, id);

CREATE UNIQUE INDEX task_thread_items_one_issue_request
    ON task_thread_items (thread_id)
    WHERE kind = 'resolution_request';

CREATE UNIQUE INDEX task_thread_items_one_issue_resolution
    ON task_thread_items (thread_id)
    WHERE kind = 'resolution';

CREATE TABLE task_thread_item_mentions (
    item_id     uuid NOT NULL REFERENCES task_thread_items(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, user_id)
);

CREATE INDEX task_thread_item_mentions_user
    ON task_thread_item_mentions (user_id, created_at, item_id);

-- Actor-neutral execution and review Claims coexist with the legacy
-- Agent-only task_claims table until the contract migration is approved.
CREATE TABLE task_stage_claims (
    id                    uuid PRIMARY KEY,
    task_id               uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_number           bigint NOT NULL,
    stage                 text NOT NULL CHECK (stage IN ('execution','review')),
    claimed_by_type       text NOT NULL CHECK (claimed_by_type IN ('user','agent')),
    claimed_by_user_id    uuid REFERENCES users(id),
    claimed_by_ref        text,
    subject_user_id       uuid NOT NULL REFERENCES users(id),
    auth_method           text NOT NULL CHECK (
                              auth_method IN (
                                  'session','api_token','agent_delegate'
                              )
                          ),
    api_token_id          uuid REFERENCES api_tokens(id),
    token_name_snapshot   text,
    agent_run_id          uuid REFERENCES agent_runs(id),
    client_kind           text NOT NULL DEFAULT '',
    client_session_id     text NOT NULL DEFAULT '',
    status                text NOT NULL CHECK (
                              status IN (
                                  'active','completed','released','expired','cancelled'
                              )
                          ),
    outcome               text CHECK (
                              outcome IN (
                                  'work_submitted','task_accepted',
                                  'changes_requested','needs_resolution',
                                  'voluntarily_released','deadline_elapsed',
                                  'task_cancelled'
                              )
                          ),
    version               bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    expires_at            timestamptz NOT NULL,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    completed_at          timestamptz,
    CHECK (expires_at > created_at),
    CHECK (
        (claimed_by_type = 'user'
            AND claimed_by_user_id = subject_user_id
            AND claimed_by_ref IS NULL)
        OR
        (claimed_by_type = 'agent'
            AND claimed_by_user_id IS NULL
            AND btrim(claimed_by_ref) <> '')
    ),
    CHECK (
        (auth_method = 'session'
            AND claimed_by_type = 'user'
            AND api_token_id IS NULL
            AND token_name_snapshot IS NULL
            AND agent_run_id IS NULL)
        OR
        (auth_method = 'api_token'
            AND claimed_by_type = 'agent'
            AND api_token_id IS NOT NULL
            AND btrim(token_name_snapshot) <> ''
            AND agent_run_id IS NULL)
        OR
        (auth_method = 'agent_delegate'
            AND claimed_by_type = 'agent'
            AND api_token_id IS NULL
            AND token_name_snapshot IS NULL
            AND agent_run_id IS NOT NULL)
    ),
    CHECK (
        (status = 'active' AND outcome IS NULL AND completed_at IS NULL)
        OR
        (status <> 'active' AND outcome IS NOT NULL AND completed_at IS NOT NULL)
    ),
    CHECK (
        (status = 'completed'
            AND outcome IN (
                'work_submitted','task_accepted','changes_requested'
            ))
        OR
        (status = 'released'
            AND outcome IN ('needs_resolution','voluntarily_released'))
        OR
        (status = 'expired' AND outcome = 'deadline_elapsed')
        OR
        (status = 'cancelled' AND outcome = 'task_cancelled')
        OR
        status = 'active'
    ),
    CHECK (
        (stage = 'execution' AND outcome NOT IN (
            'task_accepted','changes_requested'
        ))
        OR
        (stage = 'review' AND outcome <> 'work_submitted')
        OR
        outcome IS NULL
    )
);

CREATE UNIQUE INDEX task_stage_claims_one_active_per_task
    ON task_stage_claims (task_id) WHERE status = 'active';

CREATE INDEX task_stage_claims_task_history
    ON task_stage_claims (task_id, created_at DESC, id);

CREATE INDEX task_stage_claims_expiry
    ON task_stage_claims (expires_at, id) WHERE status = 'active';

-- Existing Acceptance Checks are left semantically untouched. New Task checks
-- use all three columns together to distinguish execution self-verification
-- from acceptance in the current review cycle.
ALTER TABLE acceptance_checks
    ADD COLUMN purpose text,
    ADD COLUMN task_stage_claim_id uuid REFERENCES task_stage_claims(id),
    ADD COLUMN task_review_cycle bigint,
    ADD CONSTRAINT acceptance_checks_task_context_shape CHECK (
        (purpose IS NULL
            AND task_stage_claim_id IS NULL
            AND task_review_cycle IS NULL)
        OR
        (purpose IN ('execution_verification','acceptance')
            AND task_stage_claim_id IS NOT NULL
            AND task_review_cycle >= 0)
    );

CREATE INDEX acceptance_checks_task_review
    ON acceptance_checks (
        criterion_id, criterion_revision, task_review_cycle, purpose,
        checked_at DESC, id DESC
    )
    WHERE purpose IS NOT NULL;

-- Waiver authority is a permission decision, not an actor-type invariant.
DO $$
DECLARE
    waiver_constraint text;
BEGIN
    SELECT conname INTO waiver_constraint
    FROM pg_constraint
    WHERE conrelid = 'acceptance_checks'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%waived%checker_type%';

    IF waiver_constraint IS NOT NULL THEN
        EXECUTE format(
            'ALTER TABLE acceptance_checks DROP CONSTRAINT %I',
            waiver_constraint
        );
    END IF;
END;
$$;
