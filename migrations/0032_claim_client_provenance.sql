ALTER TABLE api_request_audit_events
    ADD COLUMN client_kind text,
    ADD COLUMN client_session_id text,
    ADD CONSTRAINT api_request_audit_events_client_provenance_check CHECK (
        (client_kind IS NULL AND client_session_id IS NULL)
        OR
        (
            client_kind IS NOT NULL
            AND client_session_id IS NOT NULL
            AND
            length(btrim(client_kind)) BETWEEN 1 AND 100
            AND length(btrim(client_session_id)) BETWEEN 1 AND 255
        )
    );

ALTER TABLE task_thread_items
    DROP CONSTRAINT task_thread_items_delivery_context_check,
    ADD CONSTRAINT task_thread_items_delivery_context_check CHECK (
        (kind IN ('work_submission','execution_completed')
            AND task_stage_claim_id IS NOT NULL
            AND task_review_cycle >= 1)
        OR
        (kind = 'progress'
            AND (
                (task_stage_claim_id IS NULL AND task_review_cycle IS NULL)
                OR
                (task_stage_claim_id IS NOT NULL AND task_review_cycle >= 1)
            ))
        OR
        (kind NOT IN ('progress','work_submission','execution_completed')
            AND task_stage_claim_id IS NULL
            AND task_review_cycle IS NULL)
    );
