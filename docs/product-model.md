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
- records one immutable creator and has one or more Project administrators;
- grants Project-scoped access only to explicit `admin` or `member` memberships;
- may contain multiple active Milestones;
- may be archived and restored by a Project administrator or the platform
  Administrator; and
- may be archived only when it has no active or planned Milestones and no
  unfinished Tasks.

Archiving controls visibility. It does not delete the Project or its history.
The platform Administrator has an operational override. Other users receive a
not-found response for Projects where they have no membership. Project members
may read and change ordinary work; membership management and Project lifecycle
operations require a Project administrator. Archived Projects remain readable
but reject ordinary writes.

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

A Task may own up to 100 active private attachments, each no larger than 100
MiB. Attachment authorization always follows the Task's Project membership;
storage object addresses are not public authority. Local disk, private Aliyun
OSS, and private Tencent COS implement one upload-session contract. Completing
an upload revalidates access, expiry, size, media safety, Task version, and the
active attachment limit. Deletion is soft first and physical object cleanup is
asynchronous. Images and safe Markdown are viewable inline, HTML runs only in
a sandboxed standalone page, and spreadsheet/CSV files are download-only.

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

- Any active principal from the configured Lark tenant may authenticate.
- A new Member starts with `PENDING` access and receives only a restricted
  session for viewing the approval result and logging out.
- The Administrator may approve or reject a pending Member. A rejected Member
  may later be approved; an approved Member is suspended through the separate
  active flag rather than by rewriting approval history.
- Production identity comes only from international Lark OAuth.
- Lark principals are periodically revalidated.
- An explicitly invalid principal is deactivated and its sessions are revoked.
- The Administrator may impersonate an active Member for read-only diagnosis.

During impersonation, the Administrator remains the actor and the Member is the
effective subject. All writes other than ending impersonation or logging out
are rejected.

## Human and Agent access

Browser users authenticate with server-owned sessions and CSRF protection.
Pending and rejected sessions cannot access work, administration, personal
tokens, OpenAPI documents, or first-party Agent execution.
External Agents authenticate with user-created personal scoped Bearer Tokens.
The first-party Pactline Agent uses short-lived internal delegation credentials
that represent the initiating user and are bound to one durable Agent Run.

Each external group in which the first-party Agent is addressed has one
provider-neutral `AgentConversation` configuration. It may enable or disable
new Runs, bind one active default Project, and carry up to 4,000 characters of
user-authored Markdown business context. An explicit Project in the triggering
request overrides the group default; an archived default Project is ineffective.
Every accepted Run snapshots an immutable configuration revision. Business
context is untrusted input and cannot change system policy, tool boundaries, or
authorization. Project members may view linked group configuration, while
Project administrators may change it through the web UI or model-selected
Agent tools. The OpenAPI layer derives the current conversation from the Agent
Run and enforces every authorization and version precondition; the model cannot
select another conversation. A disabled conversation does not start an Agent
Run and can be re-enabled only through the web UI.

Conversation artifacts remain provider-owned, opaque references until the
first-party Agent selects them. When creating a Task, the Agent may preserve up
to five directly relevant images or files as private Task attachments. The
server resolves and copies each selected artifact within the current Run's
conversation and time boundary; provider resource keys are never exposed to
the model. Artifact inspection and attachment preservation are independent: an
artifact may be retained as source evidence even when surrounding text made
inspection unnecessary. Decorative images, reactions, memes, avatars,
duplicates, and unrelated history are excluded by default. One failed copy
does not roll back the Task or other successful attachments and is reported in
the verified creation receipt.

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

Ordinary Task comments form one visually flattened thread. Each reply retains
its exact reply target and canonical root so context is not lost. Deleting a
comment preserves a placeholder whenever replies may still refer to it.
Mentions are structured user identifiers, not names parsed from comment text,
and may target only active members of the Task's Project. A reply notifies its
target author; an explicit mention of the same person wins over the implicit
reply notification. Self-notifications are omitted, and edits notify only
newly added mentions.

Application events use one typed contract and are committed atomically with
the state change through a PostgreSQL outbox. A confirmed publisher relays
them to durable RabbitMQ topic/retry/dead-letter topology, where consumers bind
by event type and remain idempotent by event ID. Comment mention and reply
events currently use a no-op consumer. Access requests send the Administrator
a fixed Lark DM card linking to the approval page; approval sends the applicant
a fixed Lark DM card linking to Pactline. A future inbox is another consumer of
the same events rather than a second event-production path.

The Administrator test-tool surface may emit a `notification.test` event for
an approved, active user with a bound Lark identity. It follows the same
outbox, RabbitMQ, retry, and Lark DM path as real notifications and always uses
a fixed diagnostic card. Enqueueing confirms only that the event entered the
delivery pipeline; receipt in Lark remains the end-to-end proof.

## Legacy context

The bounty and credits mechanism under `internal/legacy` is a separate bounded
context. Its attribution, settlement, and lifecycle rules must not leak into
the Pactline Project, Milestone, or Task model.
