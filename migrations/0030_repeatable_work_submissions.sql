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

CREATE TEMP TABLE migration_execution_completion_candidates ON COMMIT DROP AS
SELECT
    item.id AS item_id,
    claim.id AS claim_id,
    claim.task_id,
    claim.completed_at,
    claim.created_at AS claim_created_at
FROM task_thread_items item
JOIN task_threads thread ON thread.id = item.thread_id
JOIN task_stage_claims claim
  ON claim.task_id = thread.task_id
 AND claim.stage = 'execution'
 AND claim.status = 'completed'
 AND claim.outcome = 'work_submitted'
LEFT JOIN task_claim_messages legacy_message
  ON legacy_message.id = item.id
 AND legacy_message.claim_id = claim.id
WHERE item.kind = 'work_submission'
  AND (
      legacy_message.id IS NOT NULL
      OR (
          NOT EXISTS (
              SELECT 1
              FROM task_claim_messages exact_legacy_message
              WHERE exact_legacy_message.id = item.id
          )
          AND claim.completed_at = item.created_at
      )
  );

DO $$
BEGIN
    IF EXISTS (
        SELECT item.id
        FROM task_thread_items item
        LEFT JOIN migration_execution_completion_candidates candidate
          ON candidate.item_id = item.id
        WHERE item.kind = 'work_submission'
        GROUP BY item.id
        HAVING count(candidate.claim_id) <> 1
    ) THEN
        RAISE EXCEPTION 'each historical work submission must match exactly one execution Claim';
    END IF;

    IF EXISTS (
        SELECT claim.id
        FROM task_stage_claims claim
        LEFT JOIN migration_execution_completion_candidates candidate
          ON candidate.claim_id = claim.id
        WHERE claim.stage = 'execution'
          AND claim.status = 'completed'
          AND claim.outcome = 'work_submitted'
        GROUP BY claim.id
        HAVING count(candidate.item_id) <> 1
    ) THEN
        RAISE EXCEPTION 'each historical submitted execution Claim must match exactly one work submission';
    END IF;
END $$;

CREATE TEMP TABLE migration_execution_completions ON COMMIT DROP AS
SELECT
    candidate.item_id,
    candidate.claim_id,
    candidate.task_id,
    row_number() OVER (
        PARTITION BY candidate.task_id
        ORDER BY candidate.completed_at, candidate.claim_created_at, candidate.claim_id
    )::bigint AS review_cycle
FROM migration_execution_completion_candidates candidate;

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
