-- Separate repeatable work submissions from the execution-stage completion
-- boundary. Historical "work submitted" records represented that boundary,
-- so they become execution-completion records rather than ordinary updates.

ALTER TABLE task_thread_items
    ADD COLUMN task_stage_claim_id uuid REFERENCES task_stage_claims(id),
    ADD COLUMN task_review_cycle bigint;

DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'task_thread_items'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%work_submission%review_outcome%';
    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE task_thread_items DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

ALTER TABLE task_thread_items
    ADD CONSTRAINT task_thread_items_kind_check CHECK (
        kind IN (
            'message','progress','handoff','work_submission',
            'execution_completed','review_outcome','resolution_request',
            'resolution','issue_resolution','system_event'
        )
    );

CREATE TEMP TABLE migration_execution_completions ON COMMIT DROP AS
SELECT
    item.id AS item_id,
    claim.id AS claim_id,
    claim.task_id,
    row_number() OVER (
        PARTITION BY claim.task_id
        ORDER BY claim.completed_at, claim.created_at, claim.id
    )::bigint AS review_cycle
FROM task_thread_items item
JOIN task_threads thread ON thread.id = item.thread_id
JOIN LATERAL (
    SELECT candidate.*
    FROM task_stage_claims candidate
    LEFT JOIN task_claim_messages legacy_message
        ON legacy_message.id = item.id
       AND legacy_message.claim_id = candidate.id
    WHERE candidate.task_id = thread.task_id
      AND candidate.stage = 'execution'
      AND candidate.status = 'completed'
      AND candidate.outcome = 'work_submitted'
      AND (
          legacy_message.id IS NOT NULL
          OR candidate.completed_at = item.created_at
      )
    ORDER BY
        CASE WHEN legacy_message.id IS NOT NULL THEN 0 ELSE 1 END,
        candidate.completed_at,
        candidate.id
    LIMIT 1
) claim ON true
WHERE item.kind = 'work_submission';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM task_thread_items item
        WHERE item.kind = 'work_submission'
          AND NOT EXISTS (
              SELECT 1
              FROM migration_execution_completions completion
              WHERE completion.item_id = item.id
          )
    ) THEN
        RAISE EXCEPTION 'cannot deterministically link historical work submission to execution Claim';
    END IF;
END $$;

UPDATE task_thread_items item
SET
    kind = 'execution_completed',
    task_stage_claim_id = completion.claim_id,
    task_review_cycle = completion.review_cycle,
    typed_payload = jsonb_build_object(
        'review_cycle', completion.review_cycle,
        'submission_item_ids', '[]'::jsonb,
        'execution_check_ids', coalesce((
            SELECT jsonb_agg(check_result.id ORDER BY check_result.checked_at, check_result.id)
            FROM acceptance_checks check_result
            WHERE check_result.task_stage_claim_id = completion.claim_id
              AND check_result.purpose = 'execution_verification'
        ), '[]'::jsonb),
        'criterion_revisions', coalesce((
            SELECT jsonb_agg(
                jsonb_build_object(
                    'criterion_id', criterion.id,
                    'revision', criterion.revision
                )
                ORDER BY criterion.position, criterion.id
            )
            FROM acceptance_criteria criterion
            WHERE criterion.task_id = completion.task_id
              AND criterion.archived_at IS NULL
        ), '[]'::jsonb)
    )
FROM migration_execution_completions completion
WHERE item.id = completion.item_id;

DO $$
DECLARE
    constraint_name text;
BEGIN
    FOR constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'task_stage_claims'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%work_submitted%'
    LOOP
        EXECUTE format('ALTER TABLE task_stage_claims DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

UPDATE task_stage_claims
SET outcome = 'execution_completed'
WHERE outcome = 'work_submitted';

ALTER TABLE task_stage_claims
    ADD CONSTRAINT task_stage_claims_outcome_check CHECK (
        outcome IN (
            'execution_completed','task_accepted','changes_requested',
            'needs_resolution','voluntarily_released','deadline_elapsed',
            'task_cancelled'
        )
    ),
    ADD CONSTRAINT task_stage_claims_status_outcome_check CHECK (
        (status = 'completed'
            AND outcome IN (
                'execution_completed','task_accepted','changes_requested'
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
    ADD CONSTRAINT task_stage_claims_stage_outcome_check CHECK (
        (stage = 'execution' AND outcome NOT IN (
            'task_accepted','changes_requested'
        ))
        OR
        (stage = 'review' AND outcome <> 'execution_completed')
        OR
        outcome IS NULL
    );

ALTER TABLE task_thread_items
    ADD CONSTRAINT task_thread_items_delivery_context_check CHECK (
        (kind IN ('work_submission','execution_completed')
            AND task_stage_claim_id IS NOT NULL
            AND task_review_cycle >= 1)
        OR
        (kind NOT IN ('work_submission','execution_completed')
            AND task_stage_claim_id IS NULL
            AND task_review_cycle IS NULL)
    ),
    ADD CONSTRAINT task_thread_items_execution_payload_check CHECK (
        (kind = 'execution_completed' AND typed_payload IS NOT NULL)
        OR kind <> 'execution_completed'
    );

CREATE INDEX task_thread_items_delivery_cycle
    ON task_thread_items (thread_id, task_review_cycle, created_at, id)
    WHERE kind IN ('work_submission','execution_completed');
