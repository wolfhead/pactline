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
- is structurally in the Project Backlog when it has no Milestone, independently
  of its lifecycle phase;
- may define an optional start date, due date, or coherent inclusive date range;
- has a stable, human-facing sequential number;
- records a creator and an optional assignee;
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

Task lifecycle is a command-driven state machine shared by people and Agents.
Its phase records delivery progress:

- `backlog`: defined but not ready to start;
- `ready`: immediately claimable for execution;
- `in_progress`: execution of the work;
- `in_review`: delivery review, including code review, technical verification,
  required confirmation, merge, and final Task acceptance;
- `done`: accepted and complete; and
- `cancelled`: intentionally stopped.

`in_progress` and `in_review` additionally have one activity state:

- `available`: the phase can be claimed;
- `working`: exactly one active Claim owns the phase; and
- `needs_resolution`: work is blocked by exactly one open typed Issue Thread.

Priority remains a label, not a scheduling constraint. Phase and activity are
not generic PATCH fields. Commands atomically update the Task, Claim, Thread
Items, audit history, and acceptance context. A Task's Project is immutable;
moving work across Projects requires creating another Task rather than
rewriting its authorization and history boundary.

Task relationships add coordination without creating a general-purpose graph:

- Parent-child relationships support exactly one level. Parent and child share
  one Project and either the same Milestone or the same Project Backlog.
- Moving a parent to another Project or Milestone moves its children
  atomically. Moving a child independently across those boundaries requires
  detaching it in the same operation.
- A parent cannot complete while a child remains unfinished.
- Dependencies are directed, cycle-free, and confined to one Project. They may
  cross Milestones in that Project.
- An unfinished dependency does not prevent work from starting or change phase
  automatically; it only prevents the dependent Task from completing.
- A direct parent-child pair cannot also be a dependency pair.

List and Gantt are two renderers of the same filtered Task collection. Gantt
uses the optional schedule fields, renders due-only work as a marker, and keeps
entirely unscheduled work available for explicit placement.

## Repository delivery evidence

A GitLab Connection is a repository-scoped machine identity created and
managed by the platform Administrator. One active Connection identifies
exactly one GitLab origin and numeric project ID. Its read credential is
encrypted at rest and never returned by an API. Pactline uses it only for
read-only project and Merge Request requests; it does not write to GitLab,
merge code, trigger CI, or treat provider state as Task workflow authority.

A Project administrator authorizes a repository for one Project by pasting its
GitLab repository URL. Pactline matches an existing active Connection and
performs live authentication before atomically creating the Project binding.
Connections may be shared by several Projects, and one Project may authorize
several repositories. The binding authorizes evidence only; it does not define
a local checkout location or permit a Task to cross its immutable Project
boundary.

During an owned execution Claim, a person or Agent may link or unlink several
GitLab Merge Requests from the Task's authorized repositories. These commands
preserve Claim ownership and Claim version while incrementing Task version.
Work submission remains repeatable and does not freeze the set. A review Claim
cannot edit delivery links.

Completing execution refreshes the active links and freezes their exact,
ordered identities and observations in the immutable completion Thread Item
for the new review cycle. The review read model presents that frozen snapshot
beside current GitLab observations and classifies each MR as unchanged, moved,
merged, missing, unauthorized, unreachable, or disconnected. Provider failure
does not by itself block completion because each link required a successful
initial resolution; the frozen snapshot retains last-known metadata and the
failure status for the reviewer.

GitLab refresh is bounded and on demand through the dedicated Task delivery
read. Ordinary Task lists never contact GitLab, and Pactline has no scheduled
polling or hidden refresh loop. A merged MR is evidence only: Code Review is
review work, and current-cycle Acceptance Checks remain the sole authority for
accepting the Task.

## Acceptance

Milestones and Tasks share two acceptance entities:

- `AcceptanceCriterion` is a revisioned statement of something that can be
  checked independently.
- `AcceptanceCheck` is an immutable result and evidence record against one
  criterion revision.

A criterion has exactly one owner scope. Revision history is retained so an
Agent or person can identify exactly what was checked.

Task checks are always recorded through an active Claim. An execution Claim
creates `execution_verification` evidence. A review Claim creates `acceptance`
evidence for the Task's current review cycle. Completing execution freezes the
current delivery, execution checks, and active criterion revisions into an
immutable completion record. Completing the Task requires each
active criterion's current revision to have a passing current-cycle acceptance
check. A changed criterion or a new submission cannot silently reuse stale
acceptance. Milestone checks remain acceptance checks without a Task Claim or
Task review cycle.

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
implies read access and permits Claim-owned lifecycle operations, unified
Thread interaction, Issue resolution, and acceptance checks for the claimed
Task. It does not permit editing Task definitions, criteria, or Projects.

People and Agents have the same Task execution and acceptance capabilities.
Authentication method and actor type are provenance, not workflow policy. A
`TaskStageClaim` represents exclusive, temporary ownership of either execution
or review. Claim stage is inferred from Task phase and cannot be chosen by the
caller. One Task has at most one active Claim; the Claim is bound to its actor,
effective user, and authentication provenance. It is never transferred or
implicitly resumed. Client kind and Client Session ID are audit provenance,
not ownership: another process using the exact same personal Token or delegated
Agent Run may continue an explicitly identified Claim from another Session ID.

- Claiming `ready` or `in_progress.available` creates an execution Claim and
  produces `in_progress.working`.
- Claiming `in_review.available` creates a review Claim and produces
  `in_review.working`.
- Releasing or lazily expiring a Claim keeps the phase and returns it to
  `available`.
- Recording a work submission appends an immutable delivery record and keeps
  the execution Claim and `in_progress.working` state unchanged. It is
  repeatable and targets the next review cycle.
- Completing execution ends the Claim, freezes the delivery snapshot,
  increments the review cycle, and produces `in_review.available`.
- Requesting changes ends a review Claim and produces
  `in_progress.available`.
- Accepting through a review Claim ends it and produces `done`.

Claim creation and history are Task-scoped because a Claim does not exist
before it is created. Every later Claim-owned command targets the globally
unique Claim ID. The server derives its immutable Task association and current
Claim version; callers do not send a Task number or Claim version. Task
`If-Match` remains explicit for lifecycle decisions because the server cannot
infer which Task definition version the caller evaluated.

Claims expire after seven days. Expiry is evaluated while the workflow is
accessed; there is no periodic Claim poll, heartbeat, or extension command.

Every Task owns one durable Main Thread across its entire lifetime. Messages,
progress, handoffs, work submissions, execution completions, review outcomes,
lifecycle events, and merged Issue conclusions are Thread Items shared by
people and Agents. Editable user
or Agent messages retain structured replies and mentions; immutable workflow
Items preserve state-changing context.

When work needs a decision or dependency resolution, the active Claim uses the
atomic `request_resolution` command. It ends the Claim, creates one Issue Thread
typed `decision_required` or `dependency_required`, and changes the Task to
`needs_resolution`. Any authorized person or Agent may discuss and resolve the
Issue. Resolution records the conclusion, merges the original request and final
resolution into one structured Main Thread Item, and returns the same phase to
`available`. The old Claim is not restored.

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

Every outbound Lark HTTP call produces a separate transient audit record. It
captures a stable operation and route template, result classification, HTTP
and provider codes, provider request ID, duration, byte counts, credential
kind, and available Pactline request, user, Agent Run, or application-event
correlation. It never stores credentials, request or response bodies, message
content, search queries, provider identity keys, concrete resource URLs, or
attachment names. Audit persistence failure is diagnosed but does not turn a
completed provider operation into a retryable failure. Lark API audit records
are visible only to the Administrator and expire after 90 days.

## Legacy context

The bounty and credits mechanism under `internal/legacy` is a separate bounded
context. Its attribution, settlement, and lifecycle rules must not leak into
the Pactline Project, Milestone, or Task model.
