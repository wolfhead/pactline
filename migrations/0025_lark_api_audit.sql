CREATE TABLE lark_api_audit_events (
    id                   uuid PRIMARY KEY,
    occurred_at          timestamptz NOT NULL,
    operation            text NOT NULL CHECK (btrim(operation) <> ''),
    category             text NOT NULL CHECK (btrim(category) <> ''),
    method               text NOT NULL CHECK (btrim(method) <> ''),
    route_pattern        text NOT NULL CHECK (btrim(route_pattern) <> ''),
    credential_kind      text NOT NULL
                         CHECK (credential_kind IN ('none','app','tenant','user')),
    outcome              text NOT NULL
                         CHECK (outcome IN (
                             'succeeded','rejected','rate_limited','unavailable',
                             'cancelled','contract_error'
                         )),
    http_status          integer CHECK (http_status BETWEEN 100 AND 599),
    provider_code        integer,
    provider_request_id  text,
    error_category       text,
    duration_ms          bigint NOT NULL CHECK (duration_ms >= 0),
    request_bytes        bigint NOT NULL CHECK (request_bytes >= 0),
    response_bytes       bigint NOT NULL CHECK (response_bytes >= 0),
    request_id           text,
    actor_user_id        uuid REFERENCES users(id),
    subject_user_id      uuid REFERENCES users(id),
    agent_run_id         uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
    application_event_id uuid
);

CREATE INDEX lark_api_audit_recent
    ON lark_api_audit_events (occurred_at DESC, id DESC);

CREATE INDEX lark_api_audit_operation
    ON lark_api_audit_events (operation, outcome, occurred_at DESC, id DESC);

CREATE INDEX lark_api_audit_provider_request
    ON lark_api_audit_events (provider_request_id)
    WHERE provider_request_id IS NOT NULL;

CREATE INDEX lark_api_audit_agent_run
    ON lark_api_audit_events (agent_run_id, occurred_at DESC, id DESC)
    WHERE agent_run_id IS NOT NULL;

CREATE INDEX lark_api_audit_application_event
    ON lark_api_audit_events (application_event_id, occurred_at DESC, id DESC)
    WHERE application_event_id IS NOT NULL;
