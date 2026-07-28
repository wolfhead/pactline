# Repository Guide for Coding Agents

## Product Direction

This repository is now primarily a general-purpose task-management product.
The original bounty/credits mechanism is retained as a backend compatibility
layer under `internal/legacy` and `/api/legacy/*`; it is not the model for new
task-product behavior.

Use this order when sources disagree:

1. Current code, migrations, and executable tests.
2. The newest accepted design in `docs/superpowers/specs/`.
3. The matching implementation plan in `docs/superpowers/plans/`.
4. `README.md`, `docs/mechanism-design.md`, and the original bounty-board
   specification for legacy context only.

Implementation-plan checkboxes are instructions, not reliable live progress
tracking. Confirm the working tree and recent commits before deciding what is
already implemented.

The normative implementation and review conventions are in
`docs/coding-standards.md`. Follow them for all code changes. In particular,
the project favors pragmatic, full domain-driven design and requires explicit
user approval before any breaking change is implemented.

## Concurrent Work Safety

Another agent or the user may be editing this checkout at the same time.

- Run `git status --short --branch` before editing and again before reporting.
- Treat every pre-existing modification and untracked file as owned by someone
  else. Do not rewrite, stage, delete, or format it unless the task explicitly
  includes it.
- Re-read a file immediately before patching it. If it changed since inspection,
  reconcile with the new content instead of restoring an earlier version.
- Keep patches narrow. Avoid repository-wide formatting and generated-file
  churn.
- Never use destructive Git cleanup commands to make the tree look clean.
- Shared integration tests use one PostgreSQL database. Do not run multiple
  `make test` processes concurrently.

If concurrent work overlaps the requested files, stop and report the exact
overlap rather than guessing which version should win.

## Architecture

### Backend

- Go 1.24 module: `bountyboard`.
- `cmd/server/main.go` wires dependencies, runs migrations, and starts the HTTP
  server.
- `internal/domain` contains task-product entities and validation without I/O.
- `internal/store` contains PostgreSQL persistence using `pgx`.
- `internal/api` contains the top-level `net/http` API, JSON/error mapping, and
  identity middleware.
- `internal/legacy/{domain,store,scoring,api}` contains the preserved
  bounty/credits mechanism. Do not change legacy behavior as a side effect of
  task-product work.
- SQL migrations are ordered files in `migrations/` and are embedded by
  `migrations.go`. Add a new migration; do not edit an applied migration unless
  the task explicitly concerns unreleased migration history.

The task API uses human-facing sequential task numbers in URLs, not UUIDs.
Protected routes use server-owned application sessions. Production identity
comes only from invite-bound international Lark OAuth; Development auth is
startup-rejected in production. Mutations require the `bb_csrf` cookie value
in `X-CSRF-Token` and a same-origin request. There is no production
`X-User-Id` fallback.

### Frontend

- React 18 + TypeScript + Vite live in `web/`.
- API transport is centralized in `web/src/api/client.ts`; task endpoints are
  wrapped by `web/src/api/tasks.ts`.
- Task pages and task-specific components live under
  `web/src/pages/tasks/` and `web/src/components/tasks/`.
- Styling is being migrated to Tailwind v4 and shadcn/ui primitives backed by
  the unified `radix-ui` package. Follow the latest UI rewrite specification
  and existing semantic tokens rather than reviving legacy hand-written style
  patterns.
- Component tests use Vitest + Testing Library. Browser workflows use
  Playwright from `web/e2e/`.

## Current Task-Product Invariants

- Task statuses are labels, not a gated state graph. Any valid status may move
  to any other valid status. Entering `done` is the sole readiness gate: a task
  with no active acceptance criteria completes directly; otherwise every
  active criterion's current revision must be `passed` or human-`waived`.
- Projects, milestones, and tasks share the same revisioned
  `AcceptanceCriterion` and immutable `AcceptanceCheck` entities. A criterion
  has exactly one owner scope.
- Priorities are labels, not scheduling constraints.
- Tasks are archived/restored, never hard-deleted. Their sequential numbers are
  stable and never reused.
- An unassigned task and a missing due date are valid first-class states.
- PATCH handling must distinguish an absent nullable field ("leave unchanged")
  from explicit JSON `null` ("clear it"). Preserve the `*Set` semantics in
  `domain.TaskPatch`.
- Task responses embed creator, optional assignee, and labels. Avoid introducing
  frontend N+1 fetches for data the task view already supplies.
- Comment edit/delete ownership is enforced; ordinary task status changes are
  not permission gates.
- API errors use the shared `{"error":"..."}` envelope. Expected rejections are
  mapped to a domain error and logged; unexpected errors become logged 500s.
- External and boundary failures must remain diagnosable without logging
  secrets or sensitive personal data.
- The product serves one Lark tenant with one Administrator. Members are
  invitation-only; there is no Administrator promotion or multi-tenant path.
- During impersonation, the real Administrator remains the actor and an active
  Member becomes the effective subject. The backend rejects all writes except
  ending impersonation and logout, and denies other Administrator routes.
- Active Lark principals are request-revalidated after 15 minutes. Explicitly
  invalid principals are deactivated and have all sessions revoked; transient
  provider failures receive a one-hour grace window.

For the accepted task UI rewrite:

- Desktop is navigation, task collection, and detail in three persistent
  columns; mobile uses a dedicated compact list and full-page detail flow.
- Mouse-visible controls are the primary interaction. Do not add application
  keyboard shortcuts. `Escape` behavior supplied by accessible Radix overlays
  remains valid.
- Board view remains a peer view; bulk-selection UI is out of scope.
- Preserve theme behavior exactly: `system | light | dark`, storage key
  `bountyboard.theme.v2`, `data-theme`, and light as the default. Do not write a
  default preference to storage as if the user selected it.
- Preserve identity bootstrap ordering: protected pages must wait for
  `/api/me`; requests use same-origin cookies and never synthesize a user ID.

## Legacy Boundaries

The legacy mechanism has different rules from the task product. In particular,
credit confirmation and bounty lifecycle rules belong only to
`internal/legacy`. Its important invariants remain:

- Unconfirmed credits do not count as attribution.
- Abandoned work remains visible with a retrospective.
- Legacy endpoints stay under `/api/legacy/*`.

Read `internal/legacy/README.md` and the original mechanism documents before
making an explicitly requested legacy change. Some old documentation still
mentions frontend legacy routes that the newer task UI rewrite removed; current
code and the newer accepted design take precedence.

## Working Method

1. Inspect the relevant code, tests, latest design, and Git state.
2. For a non-trivial change, state a short plan and identify affected
   contracts before editing.
3. For bugs, prefer a failing focused regression test or concrete automated
   reproduction before the fix. Test-first work is preferred for new behavior
   but is not a ceremonial requirement.
4. Implement the smallest coherent change using existing package and UI
   patterns.
5. Add structured logs at meaningful integration decisions and error paths.
6. Run focused verification first, then broader checks when shared contracts or
   user workflows changed.
7. Review `git diff` and `git status`; report verified results separately from
   assumptions or checks that were not run.

Use English for all source code, identifiers, filenames, code comments, test
names, logs, errors, commit messages, and developer-facing documentation.
Chinese may appear only when it is intentionally required in end-user-facing
product copy or localization resources. Do not mix Chinese into implementation
details for convenience.

## Commands

```bash
make up            # PostgreSQL 16 on localhost:5433
make run           # migrate and run Go API on localhost:8080
make web-install   # install frontend dependencies
make web-dev       # Vite on localhost:5173

make test          # all Go tests; starts PostgreSQL; serialized with -p 1
make web-test      # Vitest component tests
make e2e           # Playwright; starts/reuses DB, API, and Vite
```

Useful narrower checks:

```bash
go test ./internal/domain
go test ./internal/api -run TestName -count=1
go test ./internal/store -run TestName -count=1 -p 1

cd web
npm test -- path/to/file.test.tsx
npx tsc -b --noEmit
npm run build
npx playwright test e2e/example.spec.ts
```

Store and API integration tests require the database:

```bash
make up
DATABASE_URL="postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable" \
  go test ./internal/store -run TestName -count=1 -p 1
```

Playwright owns isolated test data and can reuse already-running local
services. Before diagnosing a port conflict, check whether the active agent is
intentionally running the Go API or Vite server.

## Documentation

- Keep implementation conventions in `docs/coding-standards.md`; update it when
  an agreed rule changes or repeated review feedback reveals a missing rule.
- Put accepted product designs in `docs/superpowers/specs/`.
- Put stepwise execution plans in `docs/superpowers/plans/`.
- Date new design/plan filenames as `YYYY-MM-DD-topic.md`.
- Record the rationale for irreversible or surprising decisions, not transient
  work status.
- Update `README.md` when setup commands or the high-level product direction
  change. Keep this file focused on instructions that coding agents need on
  every task.
