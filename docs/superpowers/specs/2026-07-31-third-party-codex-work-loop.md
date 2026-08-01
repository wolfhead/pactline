# Third-Party Codex Work Loop

**Status:** Accepted
**Date:** 2026-07-31

## Summary

Pactline will let a developer delegate explicitly selected Tasks to a real
Codex session. The Codex session authenticates with the developer's personal
Bearer Token, discovers eligible work, claims one Task atomically, performs the
work in the developer's local environment, asks questions through a dedicated
Agent conversation, records pre-acceptance evidence, and submits the Task for
human review.

This is not Pactline's built-in Lark Agent. It does not use
`internal/agent.Run`, delegated first-party credentials, a server-side model,
or a Pactline Agent profile. The external Codex chat is the execution context.
Its opaque `CODEX_THREAD_ID` binds the Claim to the chat that owns the working
context. Pactline stores that binding but never returns it in Claim responses
or audit payloads, so another session cannot discover and replay it.

## Goals

- Let users explicitly opt assigned Tasks into an Agent-ready pool.
- Let one Codex chat discover, assess, and atomically claim one eligible Task.
- Prevent concurrent Codex chats from working on the same Task.
- Preserve the owning chat's context rather than transferring an active Claim.
- Keep ordinary Task comments separate from Agent interaction.
- Let a human answer an Agent question without treating unrelated comments as
  an answer.
- Record immutable Agent progress, handoff, and pre-acceptance messages.
- Require human confirmation for final Task completion.
- Support repeated work from one Codex chat, with at most one unfinished Claim
  at a time.
- Package the workflow as a personal Codex skill usable in new chats.
- Let a same-chat scheduled task poll Pactline every ten minutes while
  preserving the chat context.

## Non-goals

- Creating a Pactline Agent profile for each external Codex session.
- Reusing the first-party Agent Run or checkpoint model.
- Moving an active Claim from one Codex chat to another.
- Reconstructing lost model context from Task history.
- Letting an Agent redefine a Task or its acceptance contract.
- Letting an Agent mark a Task `done`, cancel it, or archive it.
- Managing a developer's local repository layout inside Pactline.
- Automatically cloning repositories, managing branches, or deploying.
- Waking Codex directly from Pactline.
- Storing a real API Token in the repository or skill bundle.

## Task eligibility

Task gains an `execution_mode`:

- `human_only` is the default and means no external worker may claim the Task.
- `agent_allowed` means the assigned user may delegate the Task to an external
  Agent.

An external Codex chat may claim a Task only when all of these are true:

1. the Task is not archived;
2. the Task status is `todo`;
3. `execution_mode` is `agent_allowed`;
4. the Task assignee is the authenticated Token owner;
5. no unfinished Claim exists for the Task; and
6. the same client session has no other unfinished Claim.

Existing Tasks migrate to `human_only`. `execution_mode` is a first-class,
audited field rather than a reserved Label. A browser user controls it. An
external executor cannot change it.

## Claim

A Claim is the exclusive relationship between one Task and one external client
session. It records:

- Task and assigned user;
- API Token provenance;
- `client_kind`, initially `codex`;
- opaque `client_session_id`, supplied from `CODEX_THREAD_ID`;
- status;
- timestamps and expiry;
- release or terminal reason; and
- the Task version and status transitions caused by the Claim.

Claim statuses are:

- `active`
- `waiting_human`
- `submitted`
- `released`
- `expired`

`submitted`, `released`, and `expired` are terminal.

### Transitions

- Claim: atomically create an `active` Claim and move Task `todo` to
  `in_progress`.
- Ask: atomically append a `question` message and move `active` to
  `waiting_human`.
- Answer: atomically append an `answer` message and move `waiting_human` to
  `active`.
- Submit: atomically append a `submission` message, move Task `in_progress` to
  `in_review`, and move Claim to `submitted`.
- Release: append a `handoff` message when supplied, move Claim to `released`,
  and move Task back to `todo` if it is still `in_progress`.
- Expire: move Claim to `expired` and move Task back to `todo` if it is still
  `in_progress`.

The Claim operation holds the Task and relevant Claim rows in one PostgreSQL
transaction. Concurrent claims for one Task have one winner.

An `active` Claim expires seven days after it is created or explicitly
extended. Only the owning user and `client_session_id` may extend it.
`waiting_human` expires after 24 hours and is not transferable. There is no
heartbeat or short lease.

If a browser user has changed the Task to another status, Claim release or
expiry closes the Claim without overwriting that human-selected status.

Another Codex chat cannot resume or take over an unfinished Claim. After release
or expiry, another chat starts a new Claim from the returned `todo` Task and
uses only durable Task and timeline information.

## Agent conversation

Ordinary Task comments and Agent interaction are separate models.

Every Claim owns one immutable Agent conversation. Message kinds are:

- `progress`
- `question`
- `answer`
- `handoff`
- `submission`

Messages record their author type (`agent`, `user`, or `system`), the effective
user, API Token provenance when applicable, optional reply relationship, body,
request ID, and creation time. They are append-only. Corrections are new
messages.

Messages contain useful plans, conclusions, questions, results, and evidence.
They never request or store private chain-of-thought.

A human answers through the Agent conversation's explicit reply-and-resume
operation. An ordinary Task comment never resumes a waiting Claim.

## Communication timeline

The Task detail communication timeline merges ordinary comments and Agent
conversation messages in stable chronological order. Claim status is shown as
a separate execution summary, while Task field activity and acceptance checks
retain their existing dedicated sections.

The underlying comment and Agent records remain separate; the merged view is
not a new mutable aggregate.

## External executor authority

The Codex worker uses a least-privilege personal Token capability. The intended
executor mutation surface is:

- create, inspect, extend, release, or submit its own Claim;
- append Agent progress or question messages;
- read Agent messages;
- create Acceptance Checks with concrete evidence; and
- cause only the Claim-owned `todo -> in_progress -> in_review` Task changes.

It cannot:

- modify Task title, context, expected result, description, priority,
  assignee, schedule, Labels, Project, Milestone, parent, or dependencies;
- change `execution_mode`;
- create, edit, reorder, archive, or delete Acceptance Criteria;
- set Task status directly;
- mark a Task `done` or `cancelled`; or
- archive or restore a Task.

The implementation enforces this as a least-privilege token scope rather than
unnecessarily removing existing `work:write` behavior from unrelated external
clients. The personal Codex skill recommends the executor scope and the API
rejects a read-only Token. Existing `work:write` Tokens imply executor access
for compatibility.

The first-party `agent_delegate` flow remains separate and unchanged.

## Pre-acceptance

The Agent cannot create or change Acceptance Criteria.

When active criteria exist, the Agent executes each current revision's
verification instructions and records `passed`, `failed`, or `unable` with
concrete evidence. It cannot waive a criterion.

Submit succeeds only when every active current criterion is satisfied. Task
relationship readiness remains part of the later human `done` transition, not
the Agent's move to review. When no criteria exist, Submit requires a non-empty
verification report tied to the Task's expected result. Submit always moves
the Task to `in_review`, never `done`.

The browser user performs final review and moves the Task to `done` through the
existing acceptance gate.

## Local Codex configuration

The skill is installed personally at:

```text
~/.codex/skills/pactline-work/
```

It optionally reads:

```text
~/.pactline/
├── projects.md
├── pactline.md
└── .env
```

`projects.md` and `pactline.md` are user-owned natural-language context. Their
structure, repository mapping, paths, commands, and workspace policy are not
part of Pactline's domain model.

`.env` contains `PACTLINE_BASE_URL` and `PACTLINE_TOKEN`, has mode `0600`, and
is never logged, printed, committed, or copied into a Task. The skill refuses
to use an over-permissive secret file.

Repository-local `AGENTS.md` and project documentation continue to govern work
inside each repository.

## Skill work loop

One invocation:

1. load and validate local configuration without exposing credentials;
2. identify the current Codex chat from `CODEX_THREAD_ID`;
3. retrieve that chat's unfinished Claim;
4. resume it when present;
5. otherwise discover eligible Tasks assigned to the Token owner;
6. inspect Task detail, dependencies, acceptance criteria, repository guidance,
   and local Git state without modifying files;
7. choose work consistent with the user's local instructions and the Agent's
   current capabilities;
8. atomically claim one Task;
9. execute, report progress, ask a focused question, release with a handoff, or
   submit pre-acceptance;
10. after a terminal Claim, optionally claim another Task; and
11. stop when waiting for a human, no suitable work remains, permissions or
    environment are insufficient, or continuing would be unsafe.

One chat may process multiple Tasks sequentially but may hold at most one
unfinished Claim.

## Monitor

The skill may prompt the user to create one scheduled task inside the current
Codex chat. It must not silently create duplicates.

The monitor runs every ten minutes and invokes the same skill entry point. A
scheduled task inside the existing chat preserves context, which is required
for Claim ownership. A standalone scheduled task that creates a new chat is not
used for continuation.

Every scheduled run checks the current Claim before discovering new work. When
nothing changed and no suitable Task exists, it exits quietly.

Pactline persists answers but does not wake Codex. The owning chat observes the
answer on its next manual or scheduled run.

Scheduled execution uses the user's Codex environment and permissions. The
skill never bypasses sandbox, approval, repository, or user constraints.

## Observability and audit

Claim and message mutations produce business audit records with request ID,
effective user, authentication method, Token ID, and Token-name snapshot.
Structured logs record transition names, Task number, Claim ID, status, and
error category without message bodies, credentials, or local paths.

Claim lifecycle and Agent messages appear in the Task timeline. Permanent
business history remains after expired operational records are pruned.

## User-visible acceptance

- A user can mark an assigned `todo` Task Agent Ready.
- Two Codex chats racing for that Task produce exactly one Claim.
- A different chat cannot resume or mutate the active Claim.
- Reopening the owning chat resumes its Claim using the same context.
- Agent questions appear outside ordinary comments and require explicit
  reply-and-resume.
- A waiting Claim expires after 24 hours and returns an unchanged
  `in_progress` Task to `todo`.
- An active Claim expires after seven days unless extended.
- Release returns an Agent-owned `in_progress` Task to `todo`.
- Pre-acceptance evidence is attributable to an Agent and immutable.
- Submit moves the Task to `in_review`; only a human can move it to `done`.
- A task communication timeline shows comments and Agent interaction together,
  while an active or waiting Claim remains visible even before its first
  message.
- A personal Codex skill can perform the workflow from a new chat and can
  configure a same-chat ten-minute monitor.
