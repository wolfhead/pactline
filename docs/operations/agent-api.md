# Agent Work API Operations

## Scope

This guide covers safe operation of the contract-first `/api/v1` work API:
personal token handling, idempotent mutations, optimistic concurrency, error
recovery, audit inspection, rotation, revocation, and incident response.

The OpenAPI 3.1 contract is the source of truth:

- source: `api/openapi.yaml`;
- authenticated raw document: `/api/openapi.yaml`;
- authenticated browser reference: `/api-docs`;
- generated Go transport: `internal/api/v1generated`.

Run `make openapi-check` after any contract change. Do not edit generated files.

## Authentication boundary

External Agents authenticate only with a user-owned personal Bearer Token:

```http
Authorization: Bearer bb_pat_example_never_use
```

The literal value above is intentionally invalid. Never put a real token in
source code, documentation, tickets, chat, logs, shell history, URLs, query
parameters, or screenshots.

Users create and revoke tokens from **API Token** in the application. The
complete secret is displayed once. The database stores only its digest and a
display-safe prefix.

Available scopes:

- `work:read`: GET and HEAD under `/api/v1`;
- `work:write`: mutations and all `work:read` access.

Tokens expire after 30, 90, or 365 days. Ninety days is the UI default. Token
authority never exceeds the current authority of its owner; deactivating the
owner rejects the token immediately.

Bearer tokens cannot access `/api/auth`, `/api/account`, `/api/admin`, or
`/api/legacy`. Those routes intentionally return 404 to bearer requests.

Pactline's built-in Lark Agent also calls the generated `/api/v1` client, but
uses an internal `agent_delegate` credential issued for the active Run and
initiating user. It is signed in memory, expires within five minutes, is
reissued for each operation, and is never exposed to the user or stored in the
database. It grants only `work:read` and `work:write`, remains subject to the
initiating user's current active status, and is valid only while that Run is
executing. Users must not create a personal token for the built-in Agent.

## Safe client setup

Keep the token in an ephemeral environment variable populated by a secret
manager. The examples use a deliberately invalid placeholder:

```bash
export AGENT_API_BASE='https://tasks.example.com'
export AGENT_API_TOKEN='bb_pat_example_never_use'
```

Clear the variable when finished:

```bash
unset AGENT_API_TOKEN
```

Read the current principal:

```bash
curl --fail-with-body \
  --header "Authorization: Bearer ${AGENT_API_TOKEN}" \
  --header 'Accept: application/json' \
  "${AGENT_API_BASE}/api/v1/me"
```

Every response has an `X-Request-ID`. Bearer responses also include
`RateLimit-Limit`, `RateLimit-Remaining`, and `RateLimit-Reset`.

## Mutation protocol

Every Bearer-authenticated POST, PATCH, and DELETE requires a unique
`Idempotency-Key`. Keys are retained for 24 hours and may contain 1–128 visible
ASCII characters.

Create a task:

```bash
curl --fail-with-body \
  --request POST \
  --header "Authorization: Bearer ${AGENT_API_TOKEN}" \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: task-create-example-001' \
  --data '{
    "title":"Prepare release evidence",
    "context":"The release decision currently lacks durable verification evidence.",
    "expected_result":"Reviewers can verify every release claim from linked evidence.",
    "project_number":42
  }' \
  "${AGENT_API_BASE}/api/v1/tasks"
```

`title`, `context`, `expected_result`, and `project_number` are required.
Acceptance criteria remain optional and may be added when the work needs a
formal, checkable completion contract.

Repeating the same method, canonical route, key, and request returns the stored
response without duplicating the mutation and sets:

```http
Idempotency-Replayed: true
```

Never reuse a key for different content. The server returns
`IDEMPOTENCY_KEY_REUSED` if the fingerprint changes, or
`IDEMPOTENCY_IN_PROGRESS` with `Retry-After: 1` while an identical first request
is still executing.

## ETags and concurrent human/Agent edits

Mutable resources return a quoted integer `ETag`, for example:

```http
ETag: "7"
```

Read the resource immediately before modifying it, then send that exact value
in `If-Match`:

```bash
curl --fail-with-body \
  --request PATCH \
  --header "Authorization: Bearer ${AGENT_API_TOKEN}" \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: task-update-example-001' \
  --header 'If-Match: "7"' \
  --data '{"status":"in_progress"}' \
  "${AGENT_API_BASE}/api/v1/tasks/42"
```

Milestone operations that also change their project require both:

```http
If-Match: "<current milestone version>"
X-Project-If-Match: "<current project version>"
```

A missing precondition returns `428 PRECONDITION_REQUIRED`. A stale version
returns `412 VERSION_CONFLICT` and includes `current_version`.

Recovery procedure:

1. GET the resource again.
2. Re-evaluate the intended change against the new representation.
3. If the change is still valid, retry with the new ETag and a new
   `Idempotency-Key`.
4. If the new state changes the business intent, stop and request human
   direction. Do not blindly overwrite the human change.

## Acceptance checks

Milestones and tasks use the same revisioned acceptance criterion and immutable
acceptance check model. Projects are long-lived workspaces and intentionally
do not own acceptance criteria. An Agent should:

1. list or create the criterion under its owning resource;
2. execute `verification_instructions`;
3. POST a check to `/api/v1/criteria/{id}/checks`;
4. send the criterion revision, outcome, and concrete evidence;
5. use the criterion ETag in `If-Match`.

An Agent check is recorded with checker type `agent`, the owning user, and the
token-name snapshot. Do not report `passed` without executing the described
verification.

## Error handling

`/api/v1` errors use RFC 9457 Problem Details:

```json
{
  "type": "about:blank",
  "title": "Version conflict",
  "status": 412,
  "detail": "The resource changed after the supplied ETag was issued.",
  "instance": "/api/v1/tasks/42",
  "code": "VERSION_CONFLICT",
  "request_id": "example-request-id",
  "current_version": 8
}
```

Operational categories:

- authentication: `AUTHENTICATION_REQUIRED`, `TOKEN_INVALID`,
  `TOKEN_EXPIRED`, `TOKEN_REVOKED`, `USER_INACTIVE`;
- authorization: `INSUFFICIENT_SCOPE`, `FORBIDDEN`;
- request: `INVALID_REQUEST`, `VALIDATION_FAILED`, `NOT_FOUND`, `CONFLICT`;
- concurrency: `PRECONDITION_REQUIRED`, `VERSION_CONFLICT`;
- idempotency: `IDEMPOTENCY_KEY_REQUIRED`, `IDEMPOTENCY_KEY_REUSED`,
  `IDEMPOTENCY_IN_PROGRESS`;
- capacity: `RATE_LIMITED`;
- service: `INTERNAL_ERROR`.

Retry only when the response explicitly permits it or the failure is known to
be transient. Preserve the `request_id` in diagnostics.

## Rate limits

Each token has a sustained limit of 120 requests per minute and a burst
capacity of 30. `RATE_LIMITED` returns HTTP 429 and `Retry-After`.

Clients should:

- serialize dependent writes;
- use pagination instead of high-frequency full scans;
- honor `Retry-After`;
- add bounded exponential backoff with jitter for transient retries;
- never retry validation, scope, revoked-token, or stale-version failures
  without changing the request or state.

## Audit and retention

Users see their token metadata and recent API activity on **API Token**.
Administrators use **API 审计** to filter by user, token, method, route, status,
request ID, and time range.

Access audit records every mutation and every rejected or failed request.
Successful GET requests are intentionally not persisted because route-pattern
metadata cannot identify the exact resource read and ordinary UI refreshes
otherwise dominate the audit stream. Recorded events include authentication
outcome, route pattern, status, duration, response size, and idempotency replay
state. They never include request or response bodies. Access events are
retained for 90 days.

Business audit and product activity retain the actor, request ID,
authentication method, token ID, and token-name snapshot transactionally with
the mutation. Business history is retained indefinitely.

For built-in Agent operations, the authentication method is
`agent_delegate`; the Agent Run ID replaces token identity in access audit,
idempotency ownership, business audit, and product activity. This lets an
operator trace a created Task back to the Lark command without persisting the
command body, conversation history, model reasoning, checkpoint plaintext, or
delegation credential.

For mutations and failed requests, use `request_id` to correlate:

- the client response;
- structured application logs;
- API access audit;
- task or project activity;
- permanent business audit.

## Rotation and revocation

Routine rotation:

1. Create a replacement token with the minimum required scope and lifetime.
2. Update the Agent's secret store.
3. Verify `/api/v1/me` and one safe read with the replacement.
4. stop the Agent instance using the old token;
5. revoke the old token in **API Token**;
6. verify the old token returns `401 TOKEN_REVOKED`;
7. inspect recent activity for unexpected continued use.

Administrators may revoke any user's token from **API 审计**, but cannot recover
its secret or create a token on someone else's behalf.

## Incident response

For a suspected token leak or unexpected Agent behavior:

1. Revoke the token immediately.
2. Stop workloads that may still hold it.
3. Filter API audit by token and time range.
4. Preserve relevant request IDs and structured logs.
5. Review business activity for mutations attributed to the token.
6. Reconcile or reverse business changes through normal versioned operations;
   do not edit audit history.
7. Deactivate the user if their account is compromised.
8. Issue a least-privilege replacement only after the cause is contained.

Do not rotate application session secrets, OAuth encryption keys, or unrelated
tokens as a reflex. Expand rotation only when evidence shows broader exposure.

## Verification

Run the focused browser acceptance:

```bash
make agent-api-e2e
```

Run the complete contract and application checks before release:

```bash
make openapi-check
make test
make web-test
make web-build
make e2e
```
