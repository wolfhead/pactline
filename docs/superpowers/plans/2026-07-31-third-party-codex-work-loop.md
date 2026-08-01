# Third-Party Codex Work Loop Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-31-third-party-codex-work-loop.md`
**Date:** 2026-07-31

This plan describes intended work. Checkboxes are not evidence of completion;
verify source, migrations, generated code, tests, and the installed skill.

## Phase 1: Domain and migration

- [x] Add `TaskExecutionMode` and `TaskClaim`, Claim status, Agent message kind,
  and transition validation to `internal/domain`.
- [x] Extend `Task` and `TaskPatch` with `execution_mode`.
- [x] Add focused domain tests for eligibility, ownership, transitions,
  terminal behavior, seven-day active expiry, 24-hour waiting expiry, and
  human-status preservation.
- [x] Add migration `0017_third_party_agent_work.sql`:
  - `tasks.execution_mode`, defaulting existing data to `human_only`;
  - `task_claims` with one unfinished Claim per Task and per client session;
  - immutable `task_claim_messages`;
  - constraints and indexes for candidates, current-session lookup, waiting
    expiry, and active expiry;
  - executor Token scope support.

## Phase 2: Persistence and application workflow

- [x] Add TaskStore support for `execution_mode` and candidate filtering.
- [x] Add a dedicated Claim store whose transaction locks Task and Claim rows.
- [x] Add application orchestration for claim, extend, ask, answer, release,
  expiry, and submit.
- [x] Record business audit and Task activity for every Claim-owned status
  change.
- [x] Add integration tests for concurrent claim races, per-session
  exclusivity, release rollback, human status preservation, expiry, immutable
  messages, and submit readiness.
- [x] Extend maintenance to expire due Claims safely.

## Phase 3: least-privilege executor access

- [x] Add a personal Token executor scope to access-domain normalization,
  persistence constraints, API responses, account UI, and audit UI.
- [x] Make executor scope imply read access but not general work-write access.
- [x] Preserve existing `work:write` clients and first-party
  `agent_delegate` behavior.
- [x] Add scope tests proving executor operations are allowed and unrelated
  mutations are denied.

## Phase 4: OpenAPI contract

- [x] Extend Task schemas and list filters with `execution_mode`.
- [x] Add Claim and Agent conversation schemas.
- [x] Add operations for current Claim, claim, extend, release, ask, answer,
  progress, submit, conversation reads, and unified Task timeline.
- [x] Require idempotency keys for executor mutations and ETags where a mutable
  resource is exposed.
- [x] Map expected failures to stable RFC 9457 codes.
- [x] Generate ogen transport and update handler/server operation policies.
- [x] Run `make openapi-generate` and verify a second generation produces the
  same generated-output hash.

## Phase 5: Task UI

- [x] Add an Agent Ready control to Task creation and Task detail.
- [x] Show the current Claim owner summary and status without exposing
  credentials.
- [x] Add a separate Agent interaction presentation with immutable messages and
  explicit reply-and-resume.
- [x] Add a browser control for manual release.
- [x] Merge ordinary comments and Agent messages chronologically while
  retaining their separate write paths.
- [x] Add accessible loading, empty, conflict, and error states.
- [x] Add focused Testing Library coverage.

## Phase 6: Personal Codex skill

- [x] Initialize `pactline-work` with the system skill-creator script under
  `~/.codex/skills`.
- [x] Add a deterministic client script for configuration validation, safe
  Bearer requests, idempotency, ETags, and Claim operations.
- [x] Keep `SKILL.md` concise and place API, workflow, configuration, and
  verification detail in one-level references.
- [x] Read optional `~/.pactline/projects.md` and `pactline.md`.
- [x] Load `~/.pactline/.env` only after verifying mode `0600`; never print the
  Token.
- [x] Use `CODEX_THREAD_ID` as the opaque client session identifier.
- [x] Add same-chat scheduled-task guidance with a ten-minute interval,
  duplicate detection, quiet polling, and explicit stop behavior.
- [x] Generate `agents/openai.yaml`, run `quick_validate.py`, and test the
  deterministic script without a real credential.

## Phase 7: end-to-end verification

- [x] Verify clear claim, progress, question, answer, pre-acceptance, and submit.
- [x] Verify two-session race and wrong-session rejection.
- [x] Verify release and both expiry paths return only Agent-owned
  `in_progress` Tasks to `todo`.
- [x] Verify browser human status changes are never overwritten.
- [x] Verify executor tokens cannot redefine or finish Tasks.
- [x] Verify comments and Agent conversation remain separate while the timeline
  orders both correctly.
- [x] Re-run the PostgreSQL-backed store/API regression after restoring the
  local container runtime.
- [x] Run focused Go unit packages, frontend tests, TypeScript checks, frontend
  build, generator, vet, and skill validation.
- [x] Review final `git diff` and `git status`; preserve unrelated user work.

## Stop conditions

Stop and request user input only when implementation reveals:

- a breaking change beyond the approved executor boundary;
- a destructive action or production mutation;
- a required credential or environment that cannot be replaced with a fake;
  or
- overlapping concurrent edits that cannot be reconciled safely.
