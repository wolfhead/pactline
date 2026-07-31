# Pactline Product Model

## Purpose

Pactline coordinates project work shared by people and AI agents. Its domain
model keeps project context, delivery windows, work state, acceptance evidence,
and actor provenance explicit enough for either kind of participant to inspect
and update safely.

## Project

A Project is a long-lived workspace for a durable business or product area. It
is not a delivery lifecycle and therefore has no status, target date, outcome,
or acceptance criteria.

A Project:

- owns its Backlog, Milestones, and Tasks;
- has one active Member as owner and one creator;
- may contain multiple active Milestones;
- may be archived and restored only by the Administrator; and
- may be archived only when it has no active or planned Milestones and no
  unfinished Tasks.

Archiving controls visibility. It does not delete the Project or its history.

## Milestone

A Milestone is an owned delivery window within one Project. It groups work that
should reach a coherent outcome without becoming the Project's lifecycle.

Milestone states are:

- `planned`
- `active`
- `completed`
- `cancelled`

Multiple Milestones may be active in one Project. A Milestone may own
revisioned acceptance criteria and immutable acceptance checks.

## Task

A Task is the smallest independently managed unit of work.

Every Task:

- belongs to exactly one Project;
- may belong to one Milestone in that Project;
- is in the Project Backlog when it has no Milestone;
- may define an optional start date, due date, or coherent inclusive date range;
- has a stable, human-facing sequential number;
- records a creator and an optional assignee;
- declares `human_only` or `agent_allowed` execution mode;
- requires context and an expected result when created; and
- is archived or restored rather than hard-deleted.

Task status and priority are labels, not scheduling constraints. Any valid
status may move to any other valid status. Moving to `done` is the only
acceptance gate: a Task with active acceptance criteria may complete only when
every current criterion revision is passed or waived by a person. After an
Agent submission, only human checks recorded at or after that submission
satisfy this gate; Agent checks are self-verification evidence for the reviewer,
not human acceptance.

`agent_allowed` is an explicit delegation signal, not a second task lifecycle.
An external Codex session may claim only an assigned, unarchived `todo` Task
with that mode. Claiming moves it to `in_progress`; verified submission moves
it to `in_review`. The Agent cannot mark the Task done.

Task relationships add coordination without creating a general-purpose graph:

- Parent-child relationships support exactly one level. Parent and child share
  one Project and either the same Milestone or the same Project Backlog.
- Moving a parent to another Project or Milestone moves its children
  atomically. Moving a child independently across those boundaries requires
  detaching it in the same operation.
- A parent cannot complete while a child remains unfinished.
- Dependencies are directed, cycle-free, and confined to one Project. They may
  cross Milestones in that Project.
- An unfinished dependency does not prevent work from starting or change
  status automatically; it only prevents the dependent Task from completing.
- A direct parent-child pair cannot also be a dependency pair.

List and Gantt are two renderers of the same filtered Task collection. Gantt
uses the optional schedule fields, renders due-only work as a marker, and keeps
entirely unscheduled work available for explicit placement.

## Acceptance

Milestones and Tasks share two acceptance entities:

- `AcceptanceCriterion` is a revisioned statement of something that can be
  checked independently.
- `AcceptanceCheck` is an immutable result and evidence record against one
  criterion revision.

A criterion has exactly one owner scope. Revision history is retained so an
Agent or person can identify exactly what was checked.

## Identity and access

Pactline serves one Lark tenant and has one Administrator.

- Membership is invitation-only.
- Production identity comes only from international Lark OAuth.
- Lark principals are periodically revalidated.
- An explicitly invalid principal is deactivated and its sessions are revoked.
- The Administrator may impersonate an active Member for read-only diagnosis.

During impersonation, the Administrator remains the actor and the Member is the
effective subject. All writes other than ending impersonation or logging out
are rejected.

## Human and Agent access

Browser users authenticate with server-owned sessions and CSRF protection.
External Agents authenticate with user-created personal scoped Bearer Tokens.
The first-party Pactline Agent uses short-lived internal delegation credentials
that represent the initiating user and are bound to one durable Agent Run.

Both use the same `/api/v1` work contract. Agent mutations additionally use
idempotency keys and ETag preconditions where documented. Audit records preserve
the real actor, effective subject, personal-token or Agent-Run provenance, and
request identifier without recording credentials or unnecessary personal data.

External Codex execution uses the least-privilege `work:execute` scope. It
implies read access and permits only Claim-owned lifecycle operations, Agent
conversation messages, and acceptance checks for the claimed Task. It does not
permit editing Task definitions, criteria, Projects, or ordinary comments.

A `TaskClaim` binds one unfinished Task to one real client session. A session
may hold at most one unfinished Claim. Claims are never transferred between
sessions. The opaque client-session binding is stored for authorization but is
not returned in Claim responses or audit payloads:

- active Claims expire after seven days and may be explicitly extended by the
  original session;
- a question atomically changes the Claim to `waiting_human` for at most 24
  hours;
- only an explicit human answer resumes the same Claim;
- submission terminates the Claim and moves an unchanged `in_progress` Task
  to `in_review`, where the latest submission remains visible as current Agent
  work until review finishes or the Task leaves `in_review`; and
- release or expiry returns the Task to `todo` only if it is still
  `in_progress`, preserving any intervening human status change.

Agent progress, questions, answers, handoff, and submission are immutable
conversation messages separate from ordinary comments. The task UI presents
both streams together in chronological order. The latest submitted message is
also the entry point for human, criterion-by-criterion review and explicit Task
completion.

## Legacy context

The bounty and credits mechanism under `internal/legacy` is a separate bounded
context. Its attribution, settlement, and lifecycle rules must not leak into
the Pactline Project, Milestone, or Task model.
