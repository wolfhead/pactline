# First-Party Agent Harness Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-30-first-party-agent-harness.md`
**Date:** 2026-07-30

This plan describes intended work. Checkboxes are not evidence that a step has
been implemented; verify code, migrations, and tests directly.

## Phase 1: DeepSeek and Eino Compatibility Gate

- [ ] Pin Eino `v0.9.13` and the Eino DeepSeek component `v0.1.7` in
  `go.mod`/`go.sum`.
- [ ] Add a small model adapter package under `internal/integrations/deepseek`
  that owns configuration and exposes only the Eino model interface required by
  the Harness.
- [ ] Add offline contract tests for:
  - reasoning-content conversion;
  - one tool call;
  - multiple sequential tool calls;
  - malformed and unknown tool calls;
  - streaming message concatenation; and
  - checkpoint serialization and restoration.
- [ ] Add opt-in live tests gated by `DEEPSEEK_API_KEY` for V4 Flash
  non-thinking mode, multiple tools, and a resumed tool-call conversation.
- [ ] Record latency, token-usage, and provider request identifiers without
  recording prompts or reasoning.

**Gate:** do not proceed to Lark production wiring until the live V4 Flash
non-thinking tool/resume test succeeds against the configured DeepSeek
endpoint.

## Phase 2: Agent Domain and Persistence

- [ ] Add migration `migrations/0016_first_party_agent_harness.sql`.
- [ ] Create `agent_runs`, `agent_run_checkpoints`, `agent_tool_calls`, and
  `agent_message_outbox` with the accepted uniqueness, foreign-key, state, and
  retention constraints.
- [ ] Add the `agent_delegate` authentication method and Agent Run provenance to
  request/business audit storage without changing existing session or
  personal-token semantics.
- [ ] Generalize idempotency credential identity so both a personal API token
  and an Agent Run can own an idempotency key.
- [ ] Add Agent domain types and state-transition rules under `internal/agent`.
- [ ] Add focused domain tests for all valid and invalid transitions, the
  one-Task invariant, original-user resume, three-round clarification limit,
  and 24-hour expiry.
- [ ] Add `internal/store/agent_store.go` and integration tests for event
  deduplication, work claiming, leases, checkpoint replacement, Tool Call
  replay, Task attachment, and Outbox delivery claims.
- [ ] Extend maintenance pruning for 90-day Agent metadata while preserving
  permanent business audit.

**Verification:**

```bash
go test ./internal/agent
make up
DATABASE_URL="postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable" \
  go test ./internal/store -run 'TestAgent|TestIdempotency' -count=1 -p 1
```

## Phase 3: First-Party Delegated Authentication

- [ ] Add a dedicated delegation signing configuration and key identifier to
  `cmd/server/config.go`.
- [ ] Implement short-lived signed credential issuance and validation in
  `internal/access` with issuer, audience, subject, Agent Run, scope, expiry,
  unique credential ID, and key ID checks.
- [ ] Extend `identity.RequestIdentity` and `domain.OperationActor` with Agent
  Run provenance.
- [ ] Extend `/api/v1` Bearer authentication, scope checks, rate-limit identity,
  API access audit, and business audit for `agent_delegate`.
- [ ] Validate the Run-subject relationship and current user/Lark identity on
  every delegated request.
- [ ] Reject delegated credentials outside `/api/v1`.
- [ ] Update the OpenAPI-visible authentication method enums and generated
  transport if they expose provenance.
- [ ] Add tests for valid delegation, bad signature, wrong audience, expired
  credential, missing Run, mismatched subject, inactive user, Lark verification
  failure, internal-route isolation, idempotency replay, and audit provenance.
- [ ] Confirm all existing session and personal API token tests remain green.

**Verification:**

```bash
go test ./internal/access ./internal/domain
go test ./internal/api -run 'Test.*Delegat|TestBearer|TestScope|Test.*Audit' -count=1
```

## Phase 4: Lark Bot Channel Adapter

- [ ] Add the official Lark Go SDK and its WebSocket event transport.
- [ ] Extend `internal/integrations/lark` with typed message normalization,
  bot Open ID discovery, history retrieval, reply sending, processing
  reactions, provider error translation, and structured diagnostics.
- [ ] Normalize both Lark `text` and `post` payloads, including rich-text
  history returned in the OpenAPI `body.content` envelope.
- [ ] Define provider-neutral channel types and the `ChannelAdapter` boundary
  under `internal/agent/channel`.
- [ ] Add a provider-neutral ingress service that resolves the sender, enforces
  explicit bot mention, persists a Run, schedules one best-effort processing
  reaction, and returns without invoking the model inline.
- [ ] Add a managed Lark connection lifecycle with initialization, graceful
  stop, official SDK reconnect callbacks, structured transition logs, and an
  administrator-only status endpoint.
- [ ] Deduplicate using both Lark event ID and trigger message ID.
- [ ] Reject unsupported message types visibly without creating a Run.
- [ ] Add bounded context retrieval that cannot cross tenant, conversation,
  trigger position, seven-day age, or 100-message Run limits.
- [ ] Implement clarification reply correlation using the provider reply/root
  identifiers.
- [ ] Enforce that only the original initiating user resumes a Run.
- [ ] Wire bot discovery, the WebSocket dispatcher, ingress, and lifecycle
  supervision in `cmd/server/main.go`.
- [ ] Remove the HTTP event route, Verification Token, Encrypt Key, and
  configured Bot Open ID.
- [ ] Add unit tests using recorded sanitized Lark fixtures for bot discovery,
  typed mentions, threads, replies, duplicates, unknown users, inactive users,
  lifecycle transitions, reconnects, and context boundaries.

**Operational prerequisite:** enable the Lark bot, subscribe to
`im.message.receive_v1`, approve receiving group mentions, sending as the bot,
adding message reactions, and reading associated group history, then publish
the new Lark application version.

## Phase 5: OpenAPI Business Tools

- [ ] Add an internal generated `/api/v1` client factory that authenticates with
  a fresh delegated credential for each operation.
- [ ] Implement the five accepted tools under `internal/agent/tools`:
  `get_conversation_context`, `search_projects`, `search_users`, `ask_user`, and
  `create_task`.
- [ ] Keep all task-product reads and writes behind the generated OpenAPI
  client; add an architecture test or dependency check preventing Agent tool
  packages from importing task stores or task application services.
- [ ] Validate all tool arguments locally before external effects.
- [ ] Return small typed results rather than raw OpenAPI response bodies.
- [ ] Allow `search_projects` to list active Projects when the command omits a
  Project and identify the only active Project without overriding an explicitly
  named non-match.
- [ ] Persist Tool Calls before execution and persist results before returning
  them to Eino.
- [ ] Implement the stable create idempotency key
  `agent-run:{run_id}:create-task:v1`.
- [ ] Atomically attach the created Task identity to the Run after a successful
  or replayed OpenAPI response.
- [ ] Reject every subsequent Task creation attempt for that Run by returning
  the existing Task.
- [ ] Add tests for project ambiguity, user ambiguity, optional assignee and due
  date, Backlog placement, OpenAPI validation, permission denial, inactive
  identity, retryable failure, and duplicate Tool Calls.

## Phase 6: Agent Worker and Resume

- [ ] Implement the built-in worker under `internal/agent` with PostgreSQL
  claiming, leases, renewal, bounded concurrency, graceful shutdown, retry
  classification, and jittered backoff.
- [ ] Build the versioned system prompt and inject the current date, tenant
  timezone, Run limits, and strict one-Task policy.
- [ ] Construct an Eino `ChatModelAgent` with only the five accepted tools,
  eight maximum model steps, and the five-minute execution timeout.
- [ ] Implement encrypted Eino checkpoint storage with a dedicated key and
  format/version metadata.
- [ ] Implement `ask_user` as an interrupt and valid clarification replies as a
  resume of the same Run.
- [ ] Register every clarification interrupt info and state type used behind
  Eino interface fields and cover checkpoint persistence through the production
  Tool Call middleware.
- [ ] Delete the checkpoint on every terminal transition and on clarification
  expiry.
- [ ] Ensure model output cannot directly become a user-visible final response;
  only fixed renderers may enqueue Outbox messages.
- [ ] Add structured logs and metrics for state transitions, model usage, tools,
  OpenAPI correlation, retries, expiry, and Outbox delivery without content or
  reasoning.
- [ ] Wire worker start and graceful stop in `cmd/server/main.go`.

## Phase 7: Reliable Lark Replies

- [ ] Implement fixed success, clarification, permission, terminal failure,
  retrying, and expiry renderers.
- [ ] Include the short Run reference in every reply.
- [ ] Add an Outbox delivery loop with bounded retry, provider error
  classification, and delivery idempotency.
- [ ] Reply to the trigger for terminal outcomes and to the relevant message for
  clarification/resume flows.
- [ ] Ensure a failed reply never changes a successful Run or causes another
  Task mutation.
- [ ] Add renderer snapshots and Outbox crash/retry tests.

## Phase 8: End-to-End Verification

- [ ] Add an end-to-end fake channel and fake DeepSeek model test covering a
  clear direct Task command.
- [ ] Cover discussion-context expansion before Task creation.
- [ ] Cover multiple plausible Tasks producing clarification and no mutation.
- [ ] Cover a valid original-user clarification after process restart.
- [ ] Cover another group member attempting to resume the Run.
- [ ] Cover three clarification rounds and 24-hour expiry.
- [ ] Cover duplicate Lark delivery.
- [ ] Cover a crash immediately before and immediately after the OpenAPI Task
  response.
- [ ] Cover a duplicate DeepSeek Tool Call with the same and different argument
  hashes.
- [ ] Cover Lark reply failure after successful Task creation.
- [ ] Cover permission, inactive-user, rate-limit, and DeepSeek transient
  failures.
- [ ] Assert that Agent operations are present in API and business audit with
  `agent_delegate` and the Run ID.
- [ ] Assert that logs and persisted audit rows contain no raw credentials,
  message bodies, checkpoints, or reasoning content.

**Broader verification:**

```bash
make test
cd web && npx tsc -b --noEmit && npm run build
```

The frontend checks are regression checks; no new Agent UI is required in the
first release.

## Phase 9: Operations and Rollout

- [ ] Add DeepSeek, delegation-signing, checkpoint-encryption, worker,
  concurrency, timezone, retry, and Lark bot configuration to
  `docs/operations/deployment.md`.
- [ ] Add Lark bot scopes, WebSocket event subscription, app publication,
  connection-state inspection, and troubleshooting to
  `docs/operations/lark-identity.md`.
- [ ] Extend `docs/operations/agent-api.md` with first-party delegation audit
  semantics while preserving external personal-token instructions.
- [ ] Add dashboards or queries for queued/running/waiting Runs, lease expiry,
  model error rates, Task-create replays, and Outbox failures.
- [ ] Roll out first to a dedicated test group with a small worker concurrency
  limit.
- [ ] Verify the live DeepSeek compatibility suite and the full Lark flow in the
  deployed environment.
- [ ] Enable selected production groups only after duplicate protection,
  permission attribution, and checkpoint deletion are verified.

## Phase 10: Status Queries and Structured Responses

- [ ] Add bounded `search_tasks` and `get_task` tools backed only by the
  generated `/api/v1` client.
- [ ] Add deterministic `get_project_overview` and
  `get_milestone_overview` aggregation tools with compact status counts,
  overdue counts, Backlog counts, and bounded attention items.
- [ ] Add a mandatory terminal `respond` tool with `task_created`,
  `task_detail`, `project_status`, `milestone_status`, `error`,
  `ask_user_question`, and `general_response` contracts.
- [ ] Require compatible successful tool evidence for structured business
  responses and prevent `general_response` from claiming a write-tool result.
- [ ] Move clarification interruption behind `respond` while preserving
  checkpoint/resume, original-user correlation, three-round limits, and
  restart safety.
- [ ] Generalize worker success from "Task was created" to "a valid terminal
  response was selected", while continuing to ignore ordinary model prose.
- [ ] Add platform renderers for Task detail, Project status, Milestone status,
  semantic errors, and bounded general responses.
- [ ] Update the prompt contract so the model composes read tools, uses
  deterministic aggregates rather than counting, and always terminates through
  `respond`.
- [ ] Add focused tests for evidence compatibility, missing/ambiguous entities,
  overdue calculations in the tenant timezone, bounded results, unrestricted
  general response text, mutation-response enforcement, and durable
  clarification resume.
- [ ] Verify real Lark flows for one Task detail, one Project report, and one
  Milestone report before rollout.

## User-Visible Acceptance

- A user can explicitly mention Pactline with one clear Task request and receive
  a link to exactly one created Task.
- A user can ask Pactline to turn the preceding discussion into one Backlog
  Task.
- Ambiguous multi-topic history results in one focused question and no Task.
- Replying to that question resumes the same request, including across a server
  restart.
- Another group member cannot answer on behalf of the initiating user.
- Permission and identity rules match the initiating user's ordinary Pactline
  access.
- Repeated events and retries never create a second Task.
- A user can request one Task's status and receive a verified `task_detail`
  template.
- A user can request a Project or Milestone report and receive deterministic
  counts plus a separately labeled Agent summary.
- A general conversational request may use `general_response`, while an
  already-successful mutation always receives its verified structured receipt.
