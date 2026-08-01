# Project Collaboration Foundation

Date: 2026-08-01
Status: Approved for implementation

## 1. Outcome

Pactline gains the collaboration foundation required for private company work:

- project-scoped `admin` and `member` access;
- private Task attachments with pluggable storage;
- one-level comment threads and structured project-member mentions;
- durable comment notification events through a PostgreSQL outbox and RabbitMQ.

Actual inbox and IM delivery are explicitly out of scope. A durable no-op consumer
proves the event path without committing to a notification channel.

## 2. Non-goals

- Multi-tenant organization or workspace roles.
- Public projects or public object URLs.
- Comment attachments.
- Nested comment presentation beyond one visual reply level.
- Rich-text comments or parsing plain `@name` text into mentions.
- Spreadsheet/CSV preview.
- HTML sanitization or rewriting.
- Actual inbox, email, or IM notification delivery.
- Multipart/resumable cloud upload.

## 3. Project access model

### 3.1 Roles and authority

`ProjectMembership` is the sole project-local authority and has one of two roles:

- `admin`: normal member capabilities plus project settings, membership, and
  archive/restore;
- `member`: normal Project, Milestone, Task, comment, acceptance, attachment, and
  Agent work operations.

The platform Administrator has an explicit override for every Project. API token
and Agent delegate requests require both the documented scope and access for the
effective subject.

Projects are private. Non-members see `404 Not Found`, not `403`, for project-owned
resources so the API does not disclose their existence. A known member without the
required project role receives `403 Forbidden`.

### 3.2 Project creator and owner removal

`Project.creator_id` remains immutable history. The single `owner_id` field and API
property are removed. Creating a Project atomically adds the creator as its first
project admin.

Migration behavior:

1. Create `project_memberships`.
2. Add every active user to every existing Project as `member`, preserving current
   visibility.
3. Upsert each existing creator and owner as `admin`.
4. Remove `projects.owner_id` only after membership data exists.

This is an approved breaking API change. Rollback requires restoring `owner_id`
from one deterministic active admin per Project; the deployment runbook must call
out that this loses multi-admin semantics.

### 3.3 Membership invariants

- Membership is unique by `(project_id, user_id)`.
- Only active users may be newly added or mentioned.
- Every Project must retain at least one active admin.
- Removing or demoting the last active admin is rejected under a locked Project
  transaction.
- A platform Administrator may recover a Project that has no active admin due to
  external account deactivation.
- Archiving a Project retains membership and read/download access.
- Archived Projects reject normal mutations, new comments, and new attachments.
  Restore remains available to a project admin or platform Administrator.

### 3.4 Enforcement boundary

Application services receive an `AccessSubject` containing the effective user ID
and platform role. Stores own query-level filtering needed for correct pagination;
services own intent checks (`read`, `write`, `admin`). Handlers must not duplicate
role rules.

Every Project-owned read/write resolves the Project before accessing the resource.
Task list and Project list queries join membership directly unless the subject is a
platform Administrator. Claim-backed Agent operations additionally retain their
existing Claim ownership checks.

## 4. Attachment model

### 4.1 Aggregate and lifecycle

Attachments belong to a Task and inherit its Project ACL. They use two records:

- `attachment_upload_sessions`: short-lived authorization to upload one object;
- `task_attachments`: completed immutable file metadata visible to users.

Upload session states are `pending | completed | expired`. An attachment is active
until `deleted_at` is set. Deletion is soft in PostgreSQL and queues object cleanup.

Limits are enforced transactionally when completing an upload:

- 100 MiB maximum expected and actual size per file;
- 100 active attachments per Task;
- dangerous/executable media types and filename extensions are denied.

### 4.2 Unified storage port

The application depends on a `BlobStore` port rather than provider SDK types. The
port supports:

- create a short-lived direct-upload instruction when supported;
- accept a streamed local upload;
- `HEAD` metadata verification;
- open a private object stream or create a short-lived private GET URL;
- delete an object idempotently.

Adapters:

- `local`: real filesystem storage, upload streamed through Pactline;
- `oss`: private Alibaba Cloud OSS using the official Go SDK v2;
- `cos`: private Tencent COS using the official Go SDK.

Object keys are server-generated opaque values. Bucket, root path, provider key,
and object key are never returned in attachment API responses.

### 4.3 Upload protocol

1. `POST /tasks/{number}/attachment-upload-sessions` validates Task write access,
   active Project state, filename, media type, declared size, and current count.
2. Local returns an API `PUT` URL. OSS/COS returns a short-lived signed `PUT` URL
   plus the exact signed headers.
3. The client uploads exactly one object.
4. `POST .../attachment-upload-sessions/{id}/complete` reauthorizes the user,
   locks the Task, checks session expiry, calls `HEAD`, compares size/media type,
   enforces the final count, creates the attachment, and marks the session complete.
5. Repeating complete is idempotent and returns the same attachment.

Pending sessions expire after 30 minutes. A background cleanup loop marks expired
sessions and removes their objects. Soft-deleted attachment objects are retried by
the same cleanup loop until deletion succeeds.

### 4.4 Read, preview, and download

Every read endpoint reauthorizes the current subject against the owning Project.
Short-lived cloud GET URLs are minted only after this check.

- Images: inline preview with the recorded content type and `nosniff`.
- Markdown: server-rendered safe GFM; raw HTML disabled; remote images disabled;
  links open in a new tab with `noopener noreferrer`.
- HTML: a standalone authenticated preview response with
  `Content-Type: text/html`, `X-Content-Type-Options: nosniff`, and
  `Content-Security-Policy: sandbox allow-scripts`. It may load remote resources,
  but the opaque sandbox origin cannot access Pactline cookies, storage, or API.
- CSV, XLS/XLSX, and other formats: download only.

`Content-Disposition` uses a sanitized fallback filename plus RFC 5987 `filename*`.
Preview endpoints never trust a browser-supplied content type.

## 5. Comment threads and mentions

### 5.1 Thread shape

Comments add:

- nullable `reply_to_comment_id` for the exact comment being answered;
- nullable `thread_root_id` for grouping;
- nullable `deleted_at` for placeholders;
- embedded author and mention references in API responses.

A top-level comment has both relation fields null. Replying to a root sets both to
the root ID. Replying to a reply records that reply in `reply_to_comment_id` while
retaining the original root in `thread_root_id`. The UI renders only root plus one
indented reply level.

Deleting a root that has replies keeps a body-less deleted placeholder. Other
comments are soft-deleted as well so audit and reply relationships remain stable;
deleted leaf comments may be omitted from normal presentation.

### 5.2 Structured mentions

Comment writes carry:

- `body`;
- `mentioned_user_ids` (deduplicated UUIDs);
- optional `reply_to_comment_id` on create only.

The backend does not infer mentions from text. Every mentioned user must be an
active current member of the Task's Project. `comment_mentions` stores the current
relation plus a display-name snapshot for historical presentation.

On edit, only newly added recipients generate a mention event. Removing a mention
updates the relation without a notification. A deleted comment cannot be edited.

### 5.3 Notification event rules

Creating a reply may produce `comment.replied` for the exact replied-to author.
Adding an explicit mention may produce `comment.mentioned`. Rules:

- never notify the acting user;
- at most one event per recipient per comment mutation;
- explicit mention wins when the same recipient is also the reply target;
- events contain stable IDs and display metadata, never the full comment body.

## 6. Reliable event delivery

### 6.1 Transactional outbox

Comment data, mention relations, and event records commit in one PostgreSQL
transaction. `event_outbox` contains an immutable event ID, type, schema version,
routing key, JSON payload, availability time, attempts, publish state, and diagnostic
error category.

The relay claims rows with `FOR UPDATE SKIP LOCKED`, publishes persistent messages,
waits for a RabbitMQ publisher confirmation, and only then marks the row published.
The crash window after broker confirmation can duplicate delivery, so consumers use
`event_consumptions(consumer_name, event_id)` as an idempotency inbox.

### 6.2 RabbitMQ topology

- durable topic exchange: `pactline.events`;
- durable no-op queue: `pactline.events.noop.v1`;
- durable retry queue with TTL and dead-letter return;
- durable dead-letter exchange and queue;
- routing keys use the event type (`comment.mentioned`, `comment.replied`).

Messages are persistent JSON and include `event_id`, `event_type`,
`schema_version`, `occurred_at`, `recipient_user_id`, `project_id`, `task_id`, and
`comment_id`. The no-op consumer records its inbox claim and ACKs. Invalid payloads
and exhausted deliveries are dead-lettered with structured logs; transient failures
go through the retry queue.

RabbitMQ unavailability never rolls back a completed comment mutation. It leaves an
outbox row pending and readiness reports the degraded dependency while liveness
continues.

## 7. API and UI behavior

### 7.1 New API resources

- Project membership list/add/update/remove endpoints.
- Task attachment upload-session, upload, complete, list, metadata, preview,
  download, and delete endpoints.
- Extended comment write/response schemas and reply relations.

All mutations keep existing idempotency and ETag conventions where the owning Task
or Project version changes. Membership mutations increment Project version;
comments and attachment completion/deletion increment Task version.

### 7.2 UI

- Project detail has an admin-only member management surface.
- Task detail has an attachment area with drag/drop/file picker, per-file progress,
  preview/download, and admin-authorized deletion behavior based on API results.
- Images preview in an accessible overlay; Markdown uses a dedicated safe viewer;
  HTML opens a standalone preview page; CSV/Excel show download actions.
- Comment composer inserts structured mentions from a project-member picker.
- Replies are visually nested one level, show the actual reply target, and preserve
  deleted-root placeholders.

## 8. Security and observability

- Authorization failures log request ID, operation, subject, resource type/number,
  and decision category without object keys, comment bodies, filenames, or secrets.
- Storage calls log provider, operation, opaque attachment/session ID, result, and
  latency; signed URLs and credentials are never logged.
- RabbitMQ logs connection lifecycle, topology setup, publish confirm/retry/DLX,
  and consumer outcomes without message bodies.
- Local object paths are resolved beneath one configured root and reject traversal.
- Cloud credentials use environment/file configuration and least-privilege private
  bucket policies documented in operations guidance.

## 9. Rollout

1. Deploy schema and code together during a maintenance window because Project
   owner removal is a breaking contract.
2. Migration preserves existing visibility before dropping `owner_id`.
3. Start with `local` storage and RabbitMQ in the standard Compose stack.
4. OSS/COS configuration is optional and startup-validated only when selected.
5. Confirm outbox drain, no-op consumer ACKs, local upload/preview, and ACL isolation
   before normal traffic resumes.

No production cloud credentials or deployment are part of implementation.
