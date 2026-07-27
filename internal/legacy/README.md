# Legacy: the bounty/credit mechanism

This tree holds the team-management mechanism this project originally built:
bounties and their status graph, credits (attribution), the work feed,
personal portfolios, scoring, settlement, calibration, the anchor list, and
the steward correction channel. See `../../docs/mechanism-design.md` for the
mechanism's design and `../../docs/superpowers/specs/2026-07-26-bounty-board-design.md`
for the original system spec — both describe this code, unchanged.

## Why it's here

The project's direction changed: a proper task-management platform comes
first, and this mechanism reattaches later as an option on top of it. This
tree is that mechanism moved aside — not deleted, not disabled — so the new
task system can be built in the route and navigation space it used to
occupy. Nothing in here changed behaviour during the move; only import paths,
package paths and route prefixes did. See the move's report at
`../../.superpowers/sdd/legacy-move-report.md` for exactly what moved and
what stayed.

## What's here

- `domain/` — the mechanism's entities and business rules (bounty status
  graph, credit nomination/response, calibration, anchor examples) and their
  own errors. Performs no IO, same as before the move.
- `store/` — PostgreSQL persistence for those entities. Reuses the shared
  connection pool and migration runner from `internal/store` (`DB` here is a
  type alias for `internal/store.DB`) rather than opening its own.
- `scoring/` — the settlement score formula and its tuning constants.
- `api/` — the HTTP handlers, mounted by `internal/api.NewRouter` under the
  `/api/legacy/` prefix, behind the same identity middleware as the rest of
  the API. Reuses `internal/api`'s exported JSON response helpers
  (`WriteJSON`, `DecodeBody`, `ErrorBody`) and `CurrentUser`.

The equivalent frontend pages and components live under `web/src/legacy/`:
the work feed, board, bounty detail, portfolio, mine and steward pages, and
their supporting components. They call the `/api/legacy/...` endpoints this
package serves. They are no longer linked from the app's navigation, but
every route is still mounted in `web/src/App.tsx` and reachable by direct
URL.

## What's shared, not here

Users and identity are not mechanism-specific — a task system needs both —
so they stay in `internal/domain`, `internal/store` and `internal/api`
(`user.go`, `user_store.go`, `identity.go`, `user_handler.go`, `router.go`,
`response.go`), unmoved. `internal/domain.ErrNotFound` in particular is
shared by both the user store and every store in this tree.

## It is still tested, and is expected to reattach

Every test that covered this mechanism before the move still runs, in its
new location, with the same assertions: the Go unit and integration tests
under `domain/`, `store/`, `scoring/` and `api/`, the frontend component
tests under `web/src/legacy/`, and the Playwright end-to-end suite in
`web/e2e/` (updated only to call `/api/legacy/...`). `make test`, `npm test`
and `npx playwright test` all exercise this code exactly as they did before
the move.

That is deliberate, not incidental: this mechanism is expected to reattach
to the new task platform later, as an optional layer, and the only way to
trust it at that point is if it never stopped being exercised in the
meantime. Do not let this tree's tests silently stop running, and do not
change its behaviour "while you're here" — file a report and leave it if you
spot something wrong, the same rule the move itself followed.
