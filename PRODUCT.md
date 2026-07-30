# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Pactline is primarily for small product and engineering teams where project
managers, task creators, developers, and AI agents manage delivery together.

People need to understand, assign, schedule, execute, and verify work without
reconstructing its intent from chat history. Agents need the same context and
an explicit contract they can inspect and update safely.

## Product Purpose

Pactline coordinates long-running project work shared by people and AI agents.
Projects preserve durable context, Milestones define coherent delivery windows,
and Tasks describe independently managed work with an expected result and
optional, independently checkable acceptance criteria.

Success means either a person or an Agent can determine why work exists, what
result is expected, where it belongs, who is responsible, and whether it is
complete from Pactline's recorded state.

## Positioning

Pactline is an Agent-operable system of record, not a chat-shaped task capture
tool. It deliberately requires structured context and an expected result rather
than encouraging one-line requests.

People and Agents operate the same contract-first work model. Browser sessions
and scoped personal Agent tokens use the same versioned API, while audit
provenance, idempotency, and optimistic concurrency make automated changes
inspectable and safe to retry.

## Operating Context

- A Project is a long-lived workspace, not a delivery lifecycle.
- A Project can contain multiple concurrently active Milestones.
- Every Task belongs to one Project. A Task without a Milestone belongs to that
  Project's Backlog.
- Teams work through project, milestone, backlog, and personal-work views.
- Desktop is the primary high-density operating environment. Mobile web
  supports the core viewing, creation, and editing workflows.
- Production membership is invitation-only and currently uses international
  Lark OAuth. The external office identity and notification provider must
  remain behind an explicit integration boundary so another provider can be
  adopted without rewriting the work domain.

## Capabilities and Constraints

- Task creation requires context and an expected result. Acceptance criteria
  are optional, revisioned, and independently checkable.
- Milestones and Tasks share the same acceptance-criterion and acceptance-check
  concepts.
- Projects are archived rather than deleted. Tasks are archived and restored,
  and their human-facing sequential numbers remain stable.
- Human browser users and Agents use the same supported `/api/v1` work
  contract. Agent access uses scoped personal tokens.
- The product serves one enterprise tenant and one Administrator. It does not
  currently support multiple tenants, Administrator promotion, or multiple
  Administrators.
- Administrator impersonation is read-only and preserves the Administrator as
  the audited actor.
- Complexity should enter the product only when a real workflow requires it.
  Domain concepts should be explicit, but not forced into textbook structure.
- Backward compatibility is not a standing requirement, but every breaking
  change requires explicit user approval before implementation.
- The source repository is public. Product artifacts, examples, fixtures, and
  documentation must not expose company information, personal information,
  credentials, production data, or private infrastructure details.
- Code, identifiers, comments, logs, tests, commit messages, and
  developer-facing documentation use English. Other languages appear only as
  intentional end-user-facing copy or localization.

## Brand Commitments

The product name is **Pactline**. Its existing mark depicts two paths converging
at a milestone and continuing forward, expressing coordination between people
and Agents.

The current product identity is fixed-light Glacier Blue with a restrained teal
accent. There is no runtime light/dark theme preference.

Canonical brand assets:

- `web/public/pactline-mark.svg`
- `web/public/pactline-logo.svg`
- `web/public/favicon.svg`

## Evidence on Hand

- `docs/product-model.md` defines the durable Project, Milestone, Task,
  acceptance, and identity semantics.
- `api/openapi.yaml` is the canonical supported work API contract.
- `README.md` records the public product model and supported operating paths.
- The repository contains automated domain, API, component, and end-to-end
  tests for implemented behavior.
- The existing SVG mark and wordmark are usable product assets.

No public testimonials, customer logos, adoption metrics, benchmarks, or case
studies are available. Future product or marketing work must not fabricate
them.

## Product Principles

1. **Project context before isolated tasks.** Work should retain the durable
   context and delivery window that give it meaning.
2. **Structured intent before effortless capture.** Creating work should make
   the problem and expected result explicit without turning every task into a
   heavyweight specification.
3. **One truthful system for people and Agents.** Both participants should
   inspect and update the same domain model rather than maintain parallel
   automation state.
4. **Safe automation is visible automation.** Actor provenance, retry safety,
   concurrency conflicts, and acceptance evidence should remain inspectable.
5. **Model experienced complexity, not hypothetical complexity.** Preserve
   strong domain language and invariants while postponing mechanisms that do
   not yet solve an observed need.

## Accessibility & Inclusion

Pactline targets WCAG 2.2 AA. Desktop interactions should retain useful
information density for daily mouse use, while coarse-pointer devices receive
appropriately sized targets. Core workflows must remain keyboard accessible
and usable on mobile web without requiring desktop-equivalent layout.
