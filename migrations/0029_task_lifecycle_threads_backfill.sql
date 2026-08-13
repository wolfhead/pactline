-- Classify pre-lifecycle Tasks and preserve their collaboration history in the
-- actor-neutral Claim and Main Thread model. This migration deliberately
-- aborts on legacy shapes whose business meaning cannot be reconstructed from
-- durable structure; it never infers Issue types from message text.

SELECT pg_advisory_xact_lock(hashtext('pactline:task-lifecycle-backfill'));

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM tasks
        WHERE (phase IS NULL AND (
                activity_state IS NOT NULL OR review_cycle IS NOT NULL
            ))
           OR (phase IS NOT NULL AND review_cycle IS NULL)
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found partially classified rows';
    END IF;

    IF EXISTS (SELECT 1 FROM tasks WHERE phase IS NULL AND project_id IS NULL) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found a Task without a Project';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM tasks child
        JOIN tasks parent ON parent.id = child.parent_task_id
        WHERE child.phase IS NULL
          AND child.project_id IS DISTINCT FROM parent.project_id
    ) OR EXISTS (
        SELECT 1
        FROM task_dependencies dependency_edge
        JOIN tasks dependent ON dependent.id = dependency_edge.task_id
        JOIN tasks dependency ON dependency.id = dependency_edge.depends_on_task_id
        WHERE dependent.phase IS NULL
          AND dependent.project_id IS DISTINCT FROM dependency.project_id
    ) OR EXISTS (
        SELECT 1
        FROM tasks task
        JOIN milestones milestone ON milestone.id = task.milestone_id
        WHERE task.phase IS NULL
          AND task.project_id IS DISTINCT FROM milestone.project_id
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found a cross-Project relationship';
    END IF;

    IF EXISTS (
        SELECT 1 FROM task_claims claim
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL AND claim.status = 'waiting_human'
    ) OR EXISTS (
        SELECT 1 FROM task_claim_messages message
        JOIN task_claims claim ON claim.id = message.claim_id
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL AND message.kind IN ('question', 'answer')
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill requires explicit Issue types for legacy resolution conversations';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM task_claims claim
        JOIN tasks task ON task.id = claim.task_id
        LEFT JOIN api_tokens token ON token.id = claim.claimed_via_token_id
        WHERE task.phase IS NULL
          AND (
              claim.claimed_by_user_id IS NULL
              OR token.id IS NULL
              OR btrim(claim.token_name_snapshot) = ''
              OR btrim(claim.client_kind) = ''
              OR btrim(claim.client_session_id) = ''
              OR claim.expires_at <= claim.created_at
              OR (claim.status IN ('submitted', 'released', 'expired'))
                    <> (claim.completed_at IS NOT NULL)
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found invalid legacy Claim provenance';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM task_claims claim
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL
          AND claim.status IN ('active', 'waiting_human')
          AND task.status IN ('in_review', 'done', 'cancelled')
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found active ownership outside execution';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM task_comments comment
        LEFT JOIN task_comments reply ON reply.id = comment.reply_to_comment_id
        LEFT JOIN task_comments root ON root.id = comment.thread_root_id
        JOIN tasks task ON task.id = comment.task_id
        WHERE task.phase IS NULL
          AND (
              (comment.reply_to_comment_id IS NOT NULL
                  AND (reply.id IS NULL OR reply.task_id <> comment.task_id))
              OR root.id IS NULL
              OR root.task_id <> comment.task_id
          )
    ) OR EXISTS (
        SELECT 1
        FROM task_claim_messages message
        LEFT JOIN task_claim_messages reply ON reply.id = message.reply_to_message_id
        JOIN task_claims claim ON claim.id = message.claim_id
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL
          AND message.reply_to_message_id IS NOT NULL
          AND (reply.id IS NULL OR reply.claim_id <> message.claim_id)
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found a broken conversation relationship';
    END IF;

    IF EXISTS (
        SELECT 1 FROM task_comments comment
        JOIN task_claim_messages message ON message.id = comment.id
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found colliding Comment and Claim message IDs';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM task_claim_messages message
        JOIN task_claims claim ON claim.id = message.claim_id
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL
          AND (
              btrim(message.body) = ''
              OR btrim(message.request_id) = ''
              OR (message.author_type = 'agent' AND (
                    message.api_token_id IS NULL
                    OR btrim(coalesce(message.token_name_snapshot, '')) = ''
              ))
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found invalid Claim message provenance';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM acceptance_checks check_result
        JOIN acceptance_criteria criterion ON criterion.id = check_result.criterion_id
        JOIN tasks task ON task.id = criterion.task_id
        WHERE task.phase IS NULL
          AND (
              check_result.criterion_revision <> criterion.revision
              OR btrim(check_result.evidence) = ''
              OR check_result.checked_at < criterion.created_at
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found invalid Task acceptance evidence';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM task_claims claim
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL AND claim.status = 'submitted'
          AND NOT EXISTS (
              SELECT 1 FROM task_activity activity
              WHERE activity.task_id = task.id
                AND activity.field = 'status'
                AND activity.new_value = 'in_review'
                AND activity.created_at >= claim.completed_at
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found a submission without a review transition';
    END IF;
END;
$$;

CREATE TEMP TABLE migration_review_windows ON COMMIT DROP AS
WITH review_entries AS (
    SELECT
        activity.task_id,
        row_number() OVER (
            PARTITION BY activity.task_id ORDER BY activity.created_at, activity.id
        )::bigint AS review_cycle,
        activity.created_at AS started_at
    FROM task_activity activity
    JOIN tasks task ON task.id = activity.task_id
    WHERE task.phase IS NULL
      AND activity.field = 'status'
      AND activity.new_value = 'in_review'
)
SELECT
    entry.task_id,
    entry.review_cycle,
    entry.started_at,
    review_exit.created_at AS ended_at,
    review_exit.new_value AS exit_status
FROM review_entries entry
LEFT JOIN LATERAL (
    SELECT activity.created_at, activity.new_value
    FROM task_activity activity
    WHERE activity.task_id = entry.task_id
      AND activity.field = 'status'
      AND activity.old_value = 'in_review'
      AND activity.new_value IN ('in_progress', 'done', 'cancelled')
      AND activity.created_at >= entry.started_at
    ORDER BY activity.created_at, activity.id
    LIMIT 1
) review_exit ON true;

CREATE TEMP TABLE migration_task_checks ON COMMIT DROP AS
SELECT
    check_result.id AS check_id,
    task.id AS task_id,
    task.number AS task_number,
    check_result.checker_type,
    check_result.checked_by_user_id,
    CASE
        WHEN review_window.review_cycle IS NOT NULL THEN 'acceptance'
        -- Before Claims existed, humans could record checks at any Task status.
        -- Preserve those as cycle-zero acceptance history so a future review
        -- starts at cycle one and cannot reuse the imported evidence.
        WHEN check_result.checker_type = 'user' THEN 'acceptance'
        ELSE 'execution_verification'
    END AS purpose,
    coalesce(review_window.review_cycle, 0) AS review_cycle,
    coalesce(review_window.started_at, check_result.checked_at) AS review_started_at,
    coalesce(review_window.ended_at, check_result.checked_at) AS review_ended_at,
    coalesce(
        review_window.exit_status,
        CASE WHEN task.status = 'done' THEN 'done' ELSE 'in_progress' END
    ) AS exit_status
FROM acceptance_checks check_result
JOIN acceptance_criteria criterion ON criterion.id = check_result.criterion_id
JOIN tasks task ON task.id = criterion.task_id
LEFT JOIN LATERAL (
    SELECT review_window_source.*
    FROM migration_review_windows review_window_source
    WHERE review_window_source.task_id = task.id
      AND check_result.checked_at >= review_window_source.started_at
      AND (
          review_window_source.ended_at IS NULL
          OR check_result.checked_at <= review_window_source.ended_at
      )
    ORDER BY review_window_source.review_cycle DESC
    LIMIT 1
) review_window ON true
WHERE task.phase IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM migration_task_checks check_context
        WHERE check_context.purpose = 'execution_verification'
          AND (
              check_context.checker_type <> 'agent'
              OR (
                  SELECT count(*)
                  FROM task_claims claim
                  WHERE claim.task_id = check_context.task_id
                    AND claim.created_at <= (
                        SELECT checked_at FROM acceptance_checks
                        WHERE id = check_context.check_id
                    )
                    AND coalesce(claim.completed_at, claim.expires_at) >= (
                        SELECT checked_at FROM acceptance_checks
                        WHERE id = check_context.check_id
                    )
              ) <> 1
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill cannot identify one execution Claim for a check';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM migration_task_checks check_context
        WHERE check_context.purpose = 'acceptance'
          AND (
              check_context.checker_type <> 'user'
              OR check_context.checked_by_user_id IS NULL
              OR check_context.review_ended_at IS NULL
              OR check_context.exit_status NOT IN ('in_progress', 'done')
          )
    ) OR EXISTS (
        SELECT 1
        FROM migration_task_checks
        WHERE purpose = 'acceptance'
        GROUP BY task_id, review_cycle
        HAVING count(DISTINCT checked_by_user_id) <> 1
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill cannot reconstruct one human review owner';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM tasks task
        WHERE task.phase IS NULL
          AND md5('pactline:main-thread:' || task.id::text)::uuid IN (
              SELECT id FROM task_threads
          )
    ) OR EXISTS (
        SELECT 1
        FROM migration_task_checks check_context
        WHERE check_context.purpose = 'acceptance'
          AND md5(
              'pactline:review-claim:' || check_context.task_id::text
              || ':' || check_context.review_cycle::text
          )::uuid IN (SELECT id FROM task_stage_claims)
    ) OR EXISTS (
        SELECT 1 FROM task_claims claim
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL
          AND claim.id IN (SELECT id FROM task_stage_claims)
    ) OR EXISTS (
        SELECT 1 FROM task_comments comment
        JOIN tasks task ON task.id = comment.task_id
        WHERE task.phase IS NULL
          AND comment.id IN (SELECT id FROM task_thread_items)
    ) OR EXISTS (
        SELECT 1 FROM task_claim_messages message
        JOIN task_claims claim ON claim.id = message.claim_id
        JOIN tasks task ON task.id = claim.task_id
        WHERE task.phase IS NULL
          AND message.id IN (SELECT id FROM task_thread_items)
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill found a deterministic ID collision';
    END IF;
END;
$$;

INSERT INTO task_threads (
    id, task_id, role, version, created_at, updated_at
)
SELECT
    md5('pactline:main-thread:' || task.id::text)::uuid,
    task.id,
    'main',
    1,
    task.created_at,
    task.updated_at
FROM tasks task
WHERE task.phase IS NULL;

INSERT INTO task_stage_claims (
    id, task_id, task_number, stage,
    claimed_by_type, claimed_by_user_id, claimed_by_ref,
    subject_user_id, auth_method, api_token_id, token_name_snapshot,
    agent_run_id, client_kind, client_session_id,
    status, outcome, version, expires_at, created_at, updated_at, completed_at
)
SELECT
    claim.id,
    claim.task_id,
    task.number,
    'execution',
    'agent',
    NULL,
    'api-token/' || claim.token_name_snapshot,
    claim.claimed_by_user_id,
    'api_token',
    claim.claimed_via_token_id,
    claim.token_name_snapshot,
    NULL,
    claim.client_kind,
    claim.client_session_id,
    CASE claim.status
        WHEN 'active' THEN 'active'
        WHEN 'submitted' THEN 'completed'
        WHEN 'released' THEN 'released'
        WHEN 'expired' THEN 'expired'
    END,
    CASE claim.status
        WHEN 'submitted' THEN 'work_submitted'
        WHEN 'released' THEN 'voluntarily_released'
        WHEN 'expired' THEN 'deadline_elapsed'
        ELSE NULL
    END,
    claim.version,
    claim.expires_at,
    claim.created_at,
    claim.updated_at,
    claim.completed_at
FROM task_claims claim
JOIN tasks task ON task.id = claim.task_id
WHERE task.phase IS NULL;

INSERT INTO task_stage_claims (
    id, task_id, task_number, stage,
    claimed_by_type, claimed_by_user_id, claimed_by_ref,
    subject_user_id, auth_method, api_token_id, token_name_snapshot,
    agent_run_id, client_kind, client_session_id,
    status, outcome, version, expires_at, created_at, updated_at, completed_at
)
SELECT DISTINCT
    md5(
        'pactline:review-claim:' || check_context.task_id::text
        || ':' || check_context.review_cycle::text
    )::uuid,
    check_context.task_id,
    check_context.task_number,
    'review',
    'user',
    check_context.checked_by_user_id,
    NULL::text,
    check_context.checked_by_user_id,
    'session',
    NULL::uuid,
    NULL::text,
    NULL::uuid,
    'migration',
    'migration/review-cycle/' || check_context.review_cycle::text,
    'completed',
    CASE check_context.exit_status
        WHEN 'done' THEN 'task_accepted'
        ELSE 'changes_requested'
    END,
    2,
    check_context.review_started_at + interval '7 days',
    check_context.review_started_at,
    check_context.review_ended_at,
    check_context.review_ended_at
FROM migration_task_checks check_context
WHERE check_context.purpose = 'acceptance';

INSERT INTO task_thread_items (
    id, thread_id, kind, author_type, author_user_id, author_ref,
    body, typed_payload, reply_to_item_id, request_id, version,
    created_at, updated_at, deleted_at
)
SELECT
    comment.id,
    md5('pactline:main-thread:' || comment.task_id::text)::uuid,
    'message',
    'user',
    comment.author_id,
    NULL,
    CASE WHEN comment.deleted_at IS NULL THEN comment.body ELSE NULL END,
    NULL,
    comment.reply_to_comment_id,
    'migration:comment:' || comment.id::text,
    comment.version,
    comment.created_at,
    comment.updated_at,
    comment.deleted_at
FROM task_comments comment
JOIN tasks task ON task.id = comment.task_id
WHERE task.phase IS NULL;

INSERT INTO task_thread_item_mentions (item_id, user_id, created_at)
SELECT mention.comment_id, mention.user_id, mention.created_at
FROM comment_mentions mention
JOIN task_comments comment ON comment.id = mention.comment_id
JOIN tasks task ON task.id = comment.task_id
WHERE task.phase IS NULL AND comment.deleted_at IS NULL;

INSERT INTO task_thread_items (
    id, thread_id, kind, author_type, author_user_id, author_ref,
    body, typed_payload, reply_to_item_id, request_id, version,
    created_at, updated_at, deleted_at
)
SELECT
    message.id,
    md5('pactline:main-thread:' || claim.task_id::text)::uuid,
    CASE message.kind
        WHEN 'submission' THEN 'work_submission'
        ELSE message.kind
    END,
    message.author_type,
    CASE WHEN message.author_type = 'user' THEN message.author_user_id ELSE NULL END,
    CASE
        WHEN message.author_type = 'agent'
            THEN 'api-token/' || message.token_name_snapshot
        WHEN message.author_type = 'system' THEN 'legacy-task-claim'
        ELSE NULL
    END,
    message.body,
    NULL,
    message.reply_to_message_id,
    message.request_id,
    1,
    message.created_at,
    message.created_at,
    NULL
FROM task_claim_messages message
JOIN task_claims claim ON claim.id = message.claim_id
JOIN tasks task ON task.id = claim.task_id
WHERE task.phase IS NULL;

UPDATE acceptance_checks check_result
SET
    purpose = check_context.purpose,
    task_review_cycle = check_context.review_cycle,
    task_stage_claim_id = CASE
        WHEN check_context.purpose = 'acceptance' THEN md5(
            'pactline:review-claim:' || check_context.task_id::text
            || ':' || check_context.review_cycle::text
        )::uuid
        ELSE (
            SELECT claim.id
            FROM task_claims claim
            WHERE claim.task_id = check_context.task_id
              AND claim.created_at <= check_result.checked_at
              AND coalesce(claim.completed_at, claim.expires_at) >= check_result.checked_at
        )
    END
FROM migration_task_checks check_context
WHERE check_result.id = check_context.check_id;

UPDATE tasks task
SET
    phase = CASE task.status
        WHEN 'todo' THEN 'backlog'
        WHEN 'in_progress' THEN 'in_progress'
        WHEN 'in_review' THEN 'in_review'
        WHEN 'done' THEN 'done'
        WHEN 'cancelled' THEN 'cancelled'
    END,
    activity_state = CASE
        WHEN task.status = 'in_progress' AND EXISTS (
            SELECT 1 FROM task_claims claim
            WHERE claim.task_id = task.id AND claim.status = 'active'
        ) THEN 'working'
        WHEN task.status IN ('in_progress', 'in_review') THEN 'available'
        ELSE NULL
    END,
    review_cycle = (
        SELECT count(*)
        FROM task_activity activity
        WHERE activity.task_id = task.id
          AND activity.field = 'status'
          AND activity.new_value = 'in_review'
    ),
    active_issue_thread_id = NULL
WHERE task.phase IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM tasks
        WHERE phase IS NULL OR review_cycle IS NULL
    ) OR EXISTS (
        SELECT 1 FROM tasks task
        WHERE (SELECT count(*) FROM task_threads thread
               WHERE thread.task_id = task.id AND thread.role = 'main') <> 1
    ) OR EXISTS (
        SELECT 1 FROM tasks task
        WHERE task.activity_state = 'working'
          AND (SELECT count(*) FROM task_stage_claims claim
               WHERE claim.task_id = task.id AND claim.status = 'active') <> 1
    ) OR EXISTS (
        SELECT 1 FROM tasks task
        WHERE task.phase IN ('done', 'cancelled')
          AND (
              task.activity_state IS NOT NULL
              OR task.active_issue_thread_id IS NOT NULL
              OR EXISTS (SELECT 1 FROM task_stage_claims claim
                         WHERE claim.task_id = task.id AND claim.status = 'active')
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill failed lifecycle reconciliation';
    END IF;

    IF (SELECT count(*) FROM task_thread_items item
        JOIN task_comments comment ON comment.id = item.id)
        <> (SELECT count(*) FROM task_comments)
    OR (SELECT count(*) FROM task_thread_items item
        JOIN task_claim_messages message ON message.id = item.id)
        <> (SELECT count(*) FROM task_claim_messages)
    OR EXISTS (
        SELECT 1
        FROM acceptance_checks check_result
        JOIN acceptance_criteria criterion ON criterion.id = check_result.criterion_id
        WHERE criterion.task_id IS NOT NULL
          AND (
              check_result.purpose IS NULL
              OR check_result.task_stage_claim_id IS NULL
              OR check_result.task_review_cycle IS NULL
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill failed collaboration reconciliation';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM tasks task
        JOIN acceptance_criteria criterion
          ON criterion.task_id = task.id AND criterion.archived_at IS NULL
        WHERE task.phase = 'done'
          AND NOT EXISTS (
              SELECT 1
              FROM acceptance_checks check_result
              WHERE check_result.criterion_id = criterion.id
                AND check_result.criterion_revision = criterion.revision
                AND check_result.purpose = 'acceptance'
                AND check_result.task_review_cycle = task.review_cycle
                AND check_result.outcome IN ('passed', 'waived')
          )
    ) THEN
        RAISE EXCEPTION 'Task lifecycle backfill would leave a done Task without acceptance';
    END IF;
END;
$$;
