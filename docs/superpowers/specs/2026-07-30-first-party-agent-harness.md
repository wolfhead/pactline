# First-Party Agent Harness

**Status:** Accepted
**Date:** 2026-07-30

## Summary

Pactline will expose a first-party Agent through Lark. A user explicitly
mentions the Pactline bot and asks it to create one task, either from a direct
description or from the preceding discussion. The Agent may retrieve additional
history and ask the initiating user for clarification. A single command creates
at most one Task.

The Agent runs inside the existing Go server as a built-in PostgreSQL-backed
worker. CloudWeGo Eino provides the single-agent tool loop and checkpoint/resume
mechanism. DeepSeek V4 Pro is the initial model. Pactline continues to own
channel integration, durable Run state, authorization, idempotency, audit,
business tools, and user-visible replies.

All task-product reads and mutations made by the Agent go through the generated
`/api/v1` OpenAPI client. The first-party Agent uses a short-lived internal
delegation credential that represents the initiating user. External Agents
continue to use user-created personal API tokens.

## Goals

- Create one Task from an explicit Lark bot mention.
- Let the Agent retrieve bounded additional conversation context when needed.
- Ask the initiating user a focused clarification question when the requested
  Task is ambiguous.
- Execute with the initiating user's current Pactline authority.
- Preserve the `/api/v1` boundary, idempotency behavior, and business audit.
- Survive event redelivery, worker crashes, model retries, and delayed
  clarification replies without creating duplicate Tasks.
- Keep the channel boundary reusable for future WeCom or DingTalk adapters.
- Keep the model boundary replaceable even though only DeepSeek is implemented
  initially.

## Non-goals

- Creating multiple Tasks from one command.
- Autonomous operation without an explicit bot mention.
- Long-lived conversational sessions scoped to a group.
- Multi-agent delegation, supervisors, or sub-agents.
- General access to the entire OpenAPI document as model-visible tools.
- Shell, filesystem, code execution, arbitrary HTTP, or MCP tools.
- Acceptance Criterion generation in the first release.
- User-configured first-party Agent tokens.
- Replacing personal API tokens for Codex, Claude Code, or other external
  Agents.

## Product Invariants

1. One explicit command creates one durable Agent Run.
2. One Agent Run creates zero or one Task.
3. Only the original initiating user may clarify or resume a waiting Run.
4. If the discussion contains multiple plausible Tasks, the Agent creates
   nothing and asks the user to state one specific Task.
5. A clear explicit create command executes without a second confirmation.
6. Missing optional values remain absent; the Agent does not invent an
   assignee or due date.
7. Missing or ambiguous project or Task intent requires clarification.
8. A permission failure is reported and never bypassed or retried with elevated
   authority.
9. Every Agent task-product operation uses `/api/v1`; the Harness never calls a
   task application service or task store directly.
10. A Run that already created a Task returns that Task on replay and cannot
    create another one.

## Architecture

```text
Lark SDK WebSocket connection
    -> typed event normalization
    -> Channel ingress
    -> durable Agent Run
    -> PostgreSQL work queue
    -> built-in Agent worker
    -> bounded conversation context
    -> Eino ChatModelAgent
       -> DeepSeek V4 Pro
       -> read-only channel and work tools
       -> ask_user interrupt
       -> create_task mutation tool
    -> /api/v1 generated OpenAPI client
    -> message outbox
    -> fixed-format Lark reply
```

### Ownership boundaries

Eino owns:

- the single-agent ReAct/tool loop;
- model message and tool-call representation;
- maximum model steps;
- interrupt/resume primitives; and
- serializable execution checkpoints.

Pactline owns:

- channel authentication and event normalization;
- explicit-trigger enforcement;
- Run state and queue leases;
- prompt and tool policy;
- conversation context limits;
- first-party delegation credentials;
- OpenAPI client calls;
- exactly-once Tool Call records;
- audit, logging, retention, and encryption;
- clarification correlation; and
- final message rendering and delivery.

Eino is an implementation dependency inside the Agent worker, not a product
boundary or source of business truth.

## Runtime and Model

Pin the initial dependency line rather than following `latest`:

- `github.com/cloudwego/eino v0.9.13`
- `github.com/cloudwego/eino-ext/components/model/deepseek v0.1.7`

Use `ChatModelAgent` with the ordinary DeepSeek
`ToolCallingChatModel`. Do not use DeepAgent, Supervisor,
AgentTransfer, or `agenticdeepseek` in the first release.

Initial runtime limits:

- model: `deepseek-v4-pro`;
- thinking: enabled;
- reasoning effort: high;
- maximum model steps: 8;
- execution timeout excluding user wait: 5 minutes;
- clarification rounds: 3;
- clarification lifetime: 24 hours.

The model adapter must preserve `reasoning_content` for every assistant tool
call because DeepSeek V4 requires it on subsequent requests. The compatibility
suite must exercise non-streaming and streaming conversion, multiple sequential
tools, checkpoint serialization, and resume.

The system prompt is versioned. A Run records the model and prompt version so a
failure can be reproduced against the same contract. Conversation history is
untrusted content and cannot override the system prompt, tool policy, or
authorization context.

## Channel Boundary

The channel abstraction exposes product-shaped operations rather than raw Lark
SDK types:

```go
type ChannelAdapter interface {
	FetchContext(ctx context.Context, request ContextRequest) ([]ChannelMessage, error)
	Reply(ctx context.Context, request ReplyRequest) (ProviderMessageID, error)
}
```

`IncomingMessage` includes provider, tenant, conversation, message, thread/root,
reply parent, sender identity, message type, normalized text, mentions, and
event ID. A provider transport normalizes its SDK event into this type before
calling the provider-neutral ingress service.

The Lark implementation:

- uses the official Go SDK WebSocket client authenticated by application ID and
  application secret;
- discovers the bot Open ID from Lark during Agent initialization rather than
  requiring operator configuration;
- validates the application and configured tenant on every normalized event;
- subscribes to `im.message.receive_v1`;
- accepts group commands only when the Pactline bot is explicitly mentioned;
- uses the existing Lark external identity mapping to resolve the sender to an
  active Pactline user;
- uses application credentials for Lark reads and replies; and
- keeps Lark application authority separate from Pactline task-product
  delegated authority.

Required Lark capabilities include receiving group mentions, sending as the
bot, and reading messages in associated groups. Enabling history retrieval may
require approval of Lark's sensitive group-message scope and publication of a
new application version.

WebSocket long-connection delivery is the production transport. It does not
require a public event URL, Verification Token, or Encrypt Key. The SDK handler
validates and durably records the event before acknowledging it, while all
model and OpenAPI work is asynchronous.

### Lark connection lifecycle

When `AGENT_ENABLED=true`, startup performs these steps before accepting Agent
events:

1. validate the Lark application credentials through the existing tenant-token
   flow;
2. fetch and cache the application's bot Open ID;
3. register the typed `im.message.receive_v1` dispatcher;
4. start the official SDK WebSocket client; and
5. start the built-in Agent and Outbox worker.

The connection is supervised as `initializing`, `connecting`, `ready`,
`reconnecting`, `degraded`, or `stopped`. Structured transition logs and an
administrator-only status endpoint expose the current state, last successful
connection time, sanitized last-error category, and reconnect count. Invalid
credentials or an unavailable bot fail Agent initialization. After a
successful startup, transient disconnects do not take down the Pactline HTTP
application; the SDK reconnects while Agent status is degraded.

The handler returns within Lark's delivery deadline after normalization,
identity resolution, deduplication, encrypted command persistence, and queueing.
It never calls DeepSeek or the task OpenAPI inline.

## Conversation Context

The initial context contains:

- the normalized command;
- the directly replied-to or quoted message, when present;
- the thread root, when present; and
- up to 20 relevant messages preceding the trigger.

The `get_conversation_context` tool may retrieve older pages:

- at most 20 messages per call;
- at most 100 messages per Run;
- no more than 7 days before the trigger;
- only from the trigger conversation; and
- never after the trigger message.

If the bounded history still does not identify one Task, the Agent calls
`ask_user`.

## Tools

The first release exposes five model-visible tools.

### `get_conversation_context`

Retrieves another bounded page of messages from the trigger conversation.
Provider identifiers are opaque to the model except as cursors and source
references.

### `search_projects`

Calls `/api/v1` and returns a small set of active project candidates with
numbers and names. A single exact candidate may be used. Multiple plausible
candidates require clarification.

### `search_users`

Calls `/api/v1` and resolves an optional assignee. No match leaves the Task
unassigned when that is consistent with the user's command. Multiple plausible
matches require clarification.

### `ask_user`

Produces an Eino interrupt. It:

1. saves an encrypted checkpoint;
2. moves the Run to `waiting_user`;
3. enqueues a fixed-format clarification reply; and
4. binds the resulting provider message ID to the waiting Run.

The question may list concise candidate directions but must ask the user to
state one specific Task.

### `create_task`

This is the only mutation tool. It receives resolved Pactline fields:

```json
{
  "title": "string",
  "context": "string",
  "expected_result": "string",
  "project_number": 12,
  "milestone_id": null,
  "assignee_id": null,
  "due_date": "2026-08-10",
  "priority": "medium"
}
```

`milestone_id: null` represents the Project Backlog. The tool validates the
Run invariant before calling `/api/v1/tasks`. It uses:

```text
Idempotency-Key: agent-run:{run_id}:create-task:v1
```

The Run and Tool Call ledger provide the first idempotency layer; the OpenAPI
idempotency record provides the second.

Relative dates use an explicitly configured Pactline tenant timezone and a
clock injected into the Agent application service. Dates not supplied or
implied unambiguously remain null.

## First-Party Delegated Authentication

Add `agent_delegate` as an authentication method distinct from `session` and
`api_token`.

Before each OpenAPI request, the worker mints a short-lived signed Bearer
credential containing:

- issuer `pactline-agent`;
- audience `pactline-openapi`;
- initiating user ID as subject;
- Agent Run ID;
- `work:read` and `work:write` scopes;
- issued-at, expiry, and unique credential ID; and
- a signing key identifier.

The default lifetime is 5 minutes. A resumed Run receives a new credential.
Raw credentials are never stored.

The `/api/v1` authentication boundary validates the signature and claims,
loads the Run, confirms that the Run belongs to the subject, and reuses the
existing active-user and Lark identity verification policy. The credential is
accepted only on `/api/v1`.

Authorization remains the initiating user's authorization. The Agent never
falls back to an administrator, application identity, or hidden personal API
token.

Audit provenance includes:

- actor user ID;
- `agent_delegate` authentication method;
- Agent Run ID;
- request ID; and
- business entity and action.

Personal API token behavior remains unchanged for external Agents.

## Persistence

### `agent_runs`

Stores channel source identity, initiating user, status, command kind, pinned
model and prompt version, queue lease, attempt count, clarification binding,
terminal error, and optional created Task identity.

Unique constraints deduplicate provider events and trigger messages. A
non-null created Task is immutable and uniquely associated with one Run.

### `agent_run_checkpoints`

Stores one current encrypted Eino checkpoint per non-terminal Run with a format
version, Eino version, model, encryption key ID, and ciphertext.

### `agent_tool_calls`

Stores `(run_id, tool_call_id)`, tool name, argument hash, sanitized argument
summary, state, result, error classification, and timing. Repeating a completed
call with the same hash replays its result. Reusing an ID with different
arguments is a terminal protocol failure.

### `agent_message_outbox`

Stores durable clarification, success, failure, and expiry messages. Delivery
is retried independently from Agent execution. A successfully created Task is
never rolled back because a channel reply failed.

### Audit and idempotency changes

Extend request identity, operation actor, API access audit, business audit,
rate-limit identity, and idempotency credential identity to support an Agent
Run in addition to a personal API token. Existing session and personal-token
records retain their current meaning.

## State Machine

Supported states:

- `queued`
- `running`
- `waiting_user`
- `succeeded`
- `failed`
- `cancelled`

Transitions:

- a new valid trigger enters `queued`;
- a worker lease moves it to `running`;
- `ask_user` moves it to `waiting_user`;
- a valid original-user reply moves it back to `queued`;
- a successful Task creation moves it to `succeeded`;
- a non-retryable failure moves it to `failed`;
- a transient failure returns it to `queued` with bounded backoff; and
- clarification expiry or explicit cancellation moves it to `cancelled`.

Only the initiating user may resume a Run. A reply from another member does
not modify the waiting Run. If an incoming message cannot uniquely identify a
waiting Run, the bot asks the user to reply directly to the clarification
message rather than guessing.

## Reliability

- Claim queued work with PostgreSQL `FOR UPDATE SKIP LOCKED` and an expiring
  lease.
- Renew the lease during active model work.
- Use bounded exponential backoff with jitter for model, network, Lark, and
  explicitly retryable OpenAPI failures.
- Do not retry validation, permission, identity, or ambiguous-input failures.
- Persist each Tool Call before execution and its result before returning it to
  the model.
- Recover a Run with the encrypted checkpoint and Tool Call ledger.
- Deliver messages through the Outbox so external delivery does not share a
  transaction with Task creation.

## Security and Privacy

- Keep DeepSeek API keys, delegation signing keys, checkpoint encryption keys,
  Lark credentials, and Bearer values out of logs and audit records.
- Use a dedicated checkpoint encryption key and a separately rotatable
  delegation signing key.
- Never log or display model reasoning.
- Treat fetched chat history and tool results as untrusted model input.
- Restrict tools by code; prompts are not an authorization boundary.
- Restrict conversation retrieval by tenant, conversation, trigger position,
  age, and count.
- Store raw command/history only inside the encrypted live checkpoint.
- Delete checkpoints immediately on terminal transition and after 24 hours of
  waiting.
- Retain sanitized Run and access metadata for 90 days.
- Retain task business audit indefinitely.

## Observability

Structured logs and metrics include non-sensitive identifiers and:

- Run status transitions and lease changes;
- provider event deduplication and Lark connection state transitions;
- model name, prompt version, latency, token usage, and error category;
- tool name, latency, replay state, and sanitized failure category;
- OpenAPI request ID and idempotency replay state;
- clarification rounds and expiry; and
- Outbox attempts and provider error codes.

Logs exclude message bodies, tool secrets, raw model requests and responses,
reasoning content, and authorization values.

## User-Visible Replies

Replies use fixed renderers, not free-form final model output.

Success includes the Task number, title, Project, Backlog or Milestone,
assignee, due date, status, URL, and short Run reference.

Clarification explains that no Task was created, may list concise candidate
directions, and asks the initiating user to reply with one specific Task.

Permission failure states that no Task was created and identifies the denied
operation without suggesting privilege escalation.

Transient failure mentions automatic retry only when the Run has actually been
requeued. Terminal failures state that no Task was created.

## Retention

- clarification lifetime and encrypted checkpoint: at most 24 hours;
- clarification rounds: at most 3;
- sanitized Run, Tool Call, Outbox, and API access metadata: 90 days;
- OpenAPI idempotency responses: existing 24-hour policy; and
- business audit: indefinite.

## Acceptance Criteria

1. A clear direct command creates one Task with the initiating user's
   authorization and returns a fixed-format Lark reply.
2. A clear discussion command may retrieve bounded older context and creates
   one Task.
3. Multiple plausible Tasks produce a clarification and no mutation.
4. Only the original initiating user can resume a waiting Run.
5. A resumed Run survives a process restart and preserves the DeepSeek tool
   message protocol.
6. Duplicate Lark deliveries, repeated model Tool Calls, worker crashes, and Outbox
   retries do not create duplicate Tasks.
7. A permission or inactive-user failure creates no Task and cannot elevate
   authority.
8. Every task-product operation appears through `/api/v1` request and business
   audit with `agent_delegate` and the Agent Run ID.
9. No raw token, raw checkpoint, message body, or reasoning content appears in
   logs or audit tables.
10. Existing browser sessions and external personal API tokens behave exactly
    as before.
