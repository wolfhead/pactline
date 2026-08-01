# Project Collaboration Foundation Implementation Plan

Date: 2026-08-01
Design: `docs/superpowers/specs/2026-08-01-project-collaboration-foundation.md`

## Phase 1: Project ACL vertical slice

1. Add a migration for `project_memberships`, backfill existing visibility, promote
   creator/owner memberships, and remove `projects.owner_id`.
2. Add `ProjectRole`, `ProjectMembership`, access decisions, and last-admin domain
   rules with focused unit tests.
3. Add a membership store that performs locked add/change/remove operations and
   query-level Project/Task visibility filters.
4. Refactor `ProjectStore`, Project creation, Project responses, and audit fields to
   creator plus memberships.
5. Add an application-level Project access service and require it from every
   Project-, Milestone-, Task-, acceptance-, comment-, activity-, and Claim-owned
   handler path.
6. Add membership API endpoints and update the canonical OpenAPI contract, generated
   server/client, web API wrappers, and Project pages.
7. Update all fixtures/callers that still send or assert `owner_id`.
8. Verify migration, last-admin races, private list pagination, member/admin
   permissions, platform override, token scope plus membership, archived behavior,
   and existing Agent Claim flows.

Primary files/components:

- `migrations/0018_project_memberships.sql`
- `internal/domain/project.go`, new `internal/domain/project_membership.go`
- new `internal/application/project_access.go`
- `internal/application/{project_service,task_service}.go`
- `internal/store/{project_store,task_store,milestone_store,acceptance_store,comment_store}.go`
- new `internal/store/project_membership_store.go`
- `internal/api/v1/{handler,projects,tasks,task_claims,security}.go`
- `api/openapi.yaml` and generated `internal/api/v1generated/*`
- `web/src/api/projects.ts`, Project and Task pages/components
- affected Go/Vitest/Playwright tests

Acceptance:

- A non-member cannot discover a Project or any owned resource.
- A member can do normal work but cannot manage Project/membership lifecycle.
- Multiple admins work and the last active admin cannot be removed/demoted.
- Existing active users retain access after migration.
- Project create no longer accepts owner and creator is immediately an admin.

## Phase 2: Private attachments vertical slice

1. Add upload-session, attachment, and cleanup-state tables and domain validation.
2. Define a provider-neutral blob port and implement the Local adapter first with
   safe paths, streaming, size limits, `HEAD`, and idempotent deletion.
3. Implement OSS SDK v2 and Tencent COS adapters for signed PUT/GET, `HEAD`, and
   delete without exposing provider metadata.
4. Add configuration validation and dependency wiring for provider selection,
   limits, expiry, local root, cloud endpoints/buckets, and credential files.
5. Implement upload-session/create, local PUT, complete, list, preview, download,
   soft-delete, and background cleanup application services.
6. Add OpenAPI paths/schemas and generated transport code, including binary/HTML
   response handling and security headers.
7. Add frontend upload progress, attachment list, image/Markdown preview, standalone
   HTML preview, and download-only handling for spreadsheets/CSV.
8. Add Local storage volumes and optional OSS/COS configuration documentation.

Primary files/components:

- `migrations/0019_task_attachments.sql`
- new `internal/domain/attachment.go`
- new `internal/blob/{store,local,oss,cos}.go`
- new `internal/application/attachment_service.go`
- new `internal/store/attachment_store.go`
- new `internal/api/v1/attachments.go`
- `cmd/server/{config,main}.go`
- `api/openapi.yaml`, generated code, `web/src/api/attachments.ts`
- new Task attachment components and viewer page/routes
- Compose, secrets example, and operations docs

Acceptance:

- Local upload and complete work end-to-end under 100 MiB.
- Count, size, type, expiry, and archived-Project rejection are race safe.
- Every preview/download is authorization checked; object details remain private.
- Image and safe Markdown previews work; standalone HTML is CSP-sandboxed; CSV/XLSX
  are download only.
- Deleted/expired objects are eventually removed with diagnosable retries.

## Phase 3: Threaded comments and structured mentions

1. Add thread/deletion fields and `comment_mentions`, preserving existing comments
   as roots.
2. Extend comment domain/store operations with reply validation, deleted placeholder
   semantics, transactional Task versioning, project-member mention validation, and
   mention replacement on edits.
3. Extend the OpenAPI comment schemas and transport mapping to include author,
   relations, deletion state, and structured mentions.
4. Update the Task comment timeline to group one visual level while preserving
   chronological Agent messages, support reply actions, render deleted roots, and
   provide a project-member mention picker.

Primary files/components:

- `migrations/0020_comment_threads_mentions.sql`
- `internal/domain/comment.go`
- `internal/store/comment_store.go`
- `internal/application/task_service.go`
- `internal/api/v1/tasks.go`, `api/openapi.yaml`, generated code
- `web/src/components/tasks/CommentSection.tsx` and tests

Acceptance:

- Replies retain true target and root, but render at only one nested level.
- Deleted roots with replies remain visible as placeholders.
- Only active Project members can be mentioned.
- Comment edits emit only newly added mention intents.

## Phase 4: Outbox and RabbitMQ

1. Add transactional outbox and consumer-inbox tables.
2. Insert deduplicated `comment.mentioned` and `comment.replied` outbox rows inside
   the comment transaction.
3. Implement a database relay repository with `SKIP LOCKED`, retry scheduling, and
   publisher-confirm state transitions.
4. Add RabbitMQ topology, reconnecting publisher, retry/DLX behavior, and durable
   no-op idempotent consumer.
5. Add configuration, startup/shutdown, liveness/readiness behavior, Compose broker,
   persistent volume, internal networking, secrets, and operations guidance.
6. Test commit atomicity, relay duplicates, confirmations, broker outage/recovery,
   retry/DLX, consumer idempotency, self-notification suppression, and recipient
   deduplication.

Primary files/components:

- `migrations/0021_event_outbox.sql`
- new `internal/events/{event,outbox}.go`
- new `internal/store/{outbox_store,event_consumption_store}.go`
- new `internal/messaging/rabbitmq/*`
- `internal/store/comment_store.go`
- `cmd/server/{config,main}.go`
- Compose and operations docs

Acceptance:

- A committed comment never lacks its intended outbox rows.
- Rabbit outage does not fail comment creation and pending events drain on recovery.
- Rows are marked published only after confirmation.
- The no-op consumer safely ACKs duplicates once and poison messages reach DLQ.

## Phase 5: Stable documentation and full verification

1. Distill durable semantics into `docs/product-model.md` and implementation rules
   into `docs/coding-standards.md` only where reusable.
2. Update README, deployment, test-environment, secret, backup, and recovery docs.
3. Validate migration rollback notes and ensure backup includes local blob and
   RabbitMQ persistence expectations.
4. Run focused tests after every phase, then:

```bash
make openapi-check
go test ./... -count=1 -p 1
go vet ./...
make web-test
cd web && npx tsc -b --noEmit && npm run build
npx playwright test <targeted attachment/comment specs>
docker compose -f compose.yaml config --quiet
docker compose -f compose.production.yaml config --quiet
git diff --check
```

5. Inspect the final diff and status, separating pre-existing conversation-evaluation
   changes from this goal's files. Do not deploy, stage, commit, or publish unless
   separately requested.

Final user-visible acceptance:

- Private Project access is enforced consistently across browser, token, and Agent
  paths.
- Project admins can manage members without a single-owner bottleneck.
- Users can securely upload, view, download, and delete common Task artifacts.
- Users can reply and mention Project members without flattening conversation intent.
- Intended notification events survive process/broker failure and are observable in
  RabbitMQ even though delivery channels are intentionally not implemented.
