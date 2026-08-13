# Coding Standards

## Purpose

These standards define how this repository should evolve. They favor a
domain-driven design with explicit business meaning and strong boundaries,
without reproducing textbook layers or patterns that do not solve a current
problem.

The rules are intentionally small enough to enforce in ordinary review. When a
rule becomes repeatedly unhelpful, change this document rather than quietly
working around it.

## Design Priorities

In descending order:

1. Correct domain behavior and preserved business invariants.
2. Clear ownership of decisions, state, and side effects.
3. Diagnosable failures and safe data changes.
4. Tests at the cheapest level that proves the behavior.
5. Simple code that can be changed without reconstructing hidden context.
6. Consistency with existing code where that code still reflects the desired
   design.

Do not preserve a local pattern merely because it already exists if it obscures
the domain or contradicts an accepted design. Explain deliberate departures in
the change.

## Pragmatic Domain-Driven Design

### Language and bounded contexts

- Use product language in types, methods, tests, API concepts, and
  documentation. Prefer `Task`, `Assignee`, `Label`, and `Archive` over generic
  names such as `Item`, `OwnerData`, `TagRecord`, or `SetFlag`.
- A term must have one meaning inside a bounded context. If two concepts share
  a name but have different rules, model and name them separately.
- The task product and `internal/legacy` are separate contexts. Do not import
  legacy bounty/credit rules into the task model or make new task behavior
  depend on legacy implementation details.
- Translate explicitly at context or transport boundaries rather than leaking a
  persistence or wire representation into the domain.

### Domain model

- Put business invariants, state meaning, and domain validation in
  `internal/domain` when they can be expressed without I/O.
- Prefer behavior-bearing domain methods when a change has business meaning.
  Plain data structures remain appropriate for records without meaningful
  behavior.
- Introduce a value object when it protects a real invariant, gives a concept a
  useful vocabulary, or prevents invalid states. Do not wrap every string,
  integer, UUID, or date for visual symmetry.
- Define aggregate boundaries around consistency requirements. Changes that
  must succeed or fail together belong to one transaction.
- Do not expose mutable internals across an aggregate boundary merely to make a
  handler or SQL statement convenient.
- Model domain errors so callers can distinguish expected outcomes with
  `errors.Is`. Include useful context when wrapping them.

### Application orchestration

- HTTP handlers translate transport input/output and invoke use cases; they
  must not become the home of business policy.
- Stores handle persistence, queries, transactions, and database error
  translation; they must not define product behavior solely because the rule is
  convenient to express in SQL.
- The existing direct handler-to-store flow is acceptable for a simple CRUD
  use case.
- Add an application service when a use case coordinates multiple aggregates
  or stores, contains workflow policy, controls a non-trivial transaction, or
  integrates external systems. Do not add one merely to forward arguments.
- Add repository interfaces at a boundary with multiple implementations or a
  concrete testing/decoupling need. Do not create one interface per store by
  default.
- Use domain events when independently handled consequences must be decoupled
  or auditable. Do not introduce an event bus for ordinary synchronous method
  calls.

## Go

- Format changed Go files with `gofmt`.
- Keep packages aligned to responsibility: domain, application orchestration
  when needed, persistence, transport, and composition.
- Accept `context.Context` as the first parameter of operations that perform
  I/O or may be cancelled. Pass the request context through; do not replace it
  with `context.Background()` inside request handling.
- Wrap errors with an operation and relevant non-sensitive identifiers. Never
  discard an error unless the fallback is intentional and visible.
- Use `errors.Is`/`errors.As` for classification. Do not branch on error text.
- Keep database operations that preserve one invariant in the same transaction.
  Always handle begin, rollback, and commit paths explicitly.
- Avoid package globals for mutable runtime state.
- Prefer small named helpers when they express a domain or boundary concept;
  avoid fragmentation into one-use helpers that make control flow harder to
  follow.
- Comments explain business meaning, invariants, surprising constraints, or why
  an obvious alternative is wrong. Do not narrate straightforward syntax.

## HTTP API

- Keep request DTOs and response views at the API boundary. Domain entities do
  not acquire JSON concerns solely for handler convenience.
- Validate transport shape at the boundary and domain meaning in the domain
  model or use case.
- Use the shared JSON error envelope and domain-to-status mapping.
- Preserve PATCH presence semantics: an absent nullable field means unchanged;
  explicit `null` means clear.
- Do not expose Task phase, activity, active Claim, active Issue, review cycle,
  or Project reassignment as generic PATCH fields. Use an explicit command that
  preserves the lifecycle aggregate in one transaction.
- Human browser sessions, personal tokens, and delegated Agents use the same
  Task workflow commands. Authentication provenance may constrain ownership
  checks but must not create parallel human-only and Agent-only lifecycles.
- Task checks are Claim-owned. Derive acceptance-check purpose from Claim stage;
  never trust a caller-supplied purpose or review cycle.
- Treat work submission and execution completion as separate commands. A work
  submission does not mutate Task or Claim lifecycle versions; completion
  derives and freezes the next review-cycle snapshot while both are locked.
- Return enough related data for a screen or use case without creating
  client-side N+1 requests.
- Log rejected and failed requests with the operation, route, status, useful
  identifiers, and error. Do not log secrets, credentials, raw authorization
  material, or unnecessary personal data.

## Database and Migrations

- PostgreSQL is the source of persisted truth. Express essential integrity
  constraints in the database as well as in code when practical.
- Add ordered migration files; do not rewrite migration history that may have
  been applied.
- Migrations must be deterministic and safe to run once through the repository
  migration runner.
- Data migrations must state assumptions about existing rows and define how
  invalid or ambiguous data is handled.
- Avoid query-per-row access patterns. Fetch relations in a bounded query when a
  list or detail response always needs them.
- Tests that share the integration database must run serially and clean up the
  data they create.
- Preserve the Task workflow lock order: Task, active Claim, active Issue
  Thread, then acceptance rows and new Thread Items. State transitions that
  cross these records must commit or roll back together.
- Claim expiry is lazy workflow behavior. Do not add periodic expiry polling or
  heartbeat extension without a separately approved product change.

## Breaking Changes

Backward compatibility is not a standing requirement. A clean replacement is
preferred over maintaining dual paths when a breaking change has been approved.

However, do not implement a breaking change without explicit user approval.
Before requesting approval, state:

- what API, stored data, user workflow, or external consumer breaks;
- whether existing data needs migration, reset, or manual repair;
- which in-repository callers, tests, and documentation will change; and
- the proposed cutover and rollback approach.

Approval of a feature is not automatically approval of an undisclosed breaking
change discovered during implementation. Stop, present the impact, and obtain
confirmation.

After approval, update all in-repository producers and consumers in one coherent
change. Do not retain compatibility shims unless they have a named consumer or
the user requests a transition period.

## React and TypeScript

- Keep TypeScript strict. Do not weaken compiler options to make a change pass.
- Avoid `any`; narrow `unknown` at boundaries and give API data explicit types.
- Keep HTTP transport in `web/src/api/client.ts` and endpoint-specific wrappers
  in `web/src/api/`. Components should not duplicate headers, error parsing, or
  URL construction.
- Components own presentation and local interaction. Move reusable domain/UI
  behavior into focused hooks or modules when it has more than one consumer or
  needs independent tests.
- Do not mirror server state into multiple competing client states. When using
  optimistic updates, define the rollback and visible error path.
- Effects that start asynchronous work must prevent stale results from
  overwriting current identity, route, or component state.
- Use accessible names and semantic elements. Prefer Testing Library queries
  that reflect how a user finds a control.
- Reuse semantic design tokens and existing shadcn/Radix primitives. Do not add
  one-off colors or recreate overlay focus, dismissal, and collision behavior.
- File or component size alone is not a reason to split. Split when a unit has
  multiple reasons to change, hides distinct responsibilities, is difficult to
  test, or contains meaningful duplication.

## Testing

### Development approach

- A bug fix starts with a focused failing regression test or a concrete
  automated reproduction whenever practical.
- Test-first development is preferred for new behavior, especially domain
  rules and boundary cases, but is not a ceremonial requirement.
- If exploratory implementation is the fastest way to understand the solution,
  convert the discovered behavior into tests before declaring the work done.
- Test observable contracts and invariants. Avoid tests that merely mirror
  private implementation structure.

### Verification by change type

- Domain-only Go change: focused domain tests.
- Store or migration change: focused integration tests with PostgreSQL,
  followed by the affected package tests.
- HTTP contract change: handler/API tests plus affected domain/store tests.
- React behavior change: focused Vitest/Testing Library tests and TypeScript
  checking.
- User-visible workflow, routing, responsive layout, or overlay interaction:
  relevant component tests plus targeted Playwright coverage.
- Shared contract or cross-cutting change: run the broader Go or frontend suite
  after focused checks pass.

Do not claim success from unrelated green tests. Report exactly what was run,
what passed, and what was not run.

## Review Standard

Review in this order:

1. Incorrect behavior, invariant violations, and data-loss risk.
2. Security, authorization, privacy, and unsafe external effects.
3. Concurrency, transaction, lifecycle, and stale-state failures.
4. API/domain contract regressions and missing error handling.
5. Missing or misleading tests and diagnostics.
6. Maintainability problems that materially raise change risk.
7. Style issues only when they conflict with an agreed rule or reduce clarity.

Do not request rewrites for personal taste when the code is correct, clear,
tested, and consistent with these standards.

## Language

Use English for all source code, identifiers, filenames, code comments, test
names and descriptions, logs, errors, commit messages, and developer-facing
documentation.

Chinese may appear only when it is intentionally required in end-user-facing
product copy or localization resources. Keep localized copy at the presentation
boundary where practical; do not use Chinese for internal constants, fixtures,
debug messages, implementation notes, or developer convenience.
