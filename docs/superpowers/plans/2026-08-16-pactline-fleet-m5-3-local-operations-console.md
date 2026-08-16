# Pactline Fleet M5.3 Local Operations Console Implementation Plan

**Status:** complete

## Objective

Ship a read-only local Operations Console from the resident Fleet Service so
an operator can understand service readiness, Fleet scope, active and recent
Runs, recovery decisions, and dependency health from one loopback URL.

The console extends Pactline's fixed-light Glacier Blue and Confluence Teal
identity as an independent application under `fleet/web`. It is an Operate
surface: attention and causal evidence lead, while summary counts remain
supporting information.

## Frozen interaction boundary

- the HTTP surface remains loopback-only and read-only;
- the UI may filter, copy identifiers, and open authoritative Pactline or
  provider links;
- the UI cannot Claim, retry, release, adopt, settle, pause, drain, or edit
  configuration;
- raw prompts, model reasoning, credentials, unbounded output, and raw SQLite
  rows are never projected;
- browser storage contains display preferences only, never operational data or
  credentials.

## Phase 1 — observation contract and safe projections

Files:

- `fleet/src/observation/model.ts`
- `fleet/src/observation/projection.ts`
- `fleet/src/registry/fleet-registry.ts`
- `fleet/src/http/health-server.ts`
- `fleet/src/service/fleet-service.ts`
- `fleet/tests/observation/`
- `fleet/tests/http/health-server.test.ts`

Changes:

- define versioned service, Fleet, Run summary, Run detail, Adapter, event,
  pagination, and error envelopes;
- add bounded registry queries for Runs, events, and external-effect evidence;
- sanitize projected diagnostics and recursively remove sensitive keys;
- implement `/api/v1/fleets`, `/api/v1/runs`, `/api/v1/adapters`, and bounded
  detail routes;
- implement an SSE change feed with heartbeat and polling-compatible revision
  identifiers;
- expose Prometheus text metrics with only finite or configuration-bounded
  labels;
- preserve Host, Origin, CSP, no-store, and method rejection guarantees.

## Phase 2 — independent Operations Console

Files:

- `fleet/web/package.json`
- `fleet/web/src/app/`
- `fleet/web/src/components/`
- `fleet/web/src/data/`
- `fleet/web/src/styles.css`
- `fleet/web/tests/`

Changes:

- create a React 18, TypeScript, and Vite application without importing source
  from Pactline's main frontend;
- reproduce the committed Pactline semantic tokens locally;
- build Overview, Fleet detail, Runs, Run detail, and System routes;
- lead Overview and Run detail with current state, scope, and last safe
  checkpoint instead of ornamental metric cards;
- implement desktop persistent navigation, medium-width compact navigation,
  and phone observation cards;
- implement loading, empty, stale, disconnected, degraded, long-history, and
  partial-evidence states;
- connect through SSE and fall back to bounded polling while retaining a clear
  last-known-state indicator.

## Phase 3 — static delivery and UI command

Files:

- `fleet/package.json`
- `fleet/src/http/static-assets.ts`
- `fleet/src/commands/ui.ts`
- `fleet/src/cli.ts`
- `fleet/tests/commands.test.ts`
- `fleet/tests/http/static-assets.test.ts`

Changes:

- build the web application as part of the Fleet package build;
- serve hashed assets and client routes from the same loopback listener;
- apply a UI-specific CSP without weakening the JSON API policy;
- include generated assets in the packed Fleet artifact without committing
  build output;
- add `pactline-fleet ui --config <path> [--open]` to print, validate, and
  optionally open the configured local URL;
- prove an installed package serves the console without Vite.

## Phase 4 — acceptance and visual finish

- component tests for every material operational state;
- API schema, pagination, redaction, method, Host, Origin, and CSP tests;
- SSE reconnect and polling-fallback tests;
- Playwright flows for Overview, Fleet detail, Run detail, and System;
- screenshots at phone, medium, and desktop widths;
- keyboard, focus, contrast, reduced-motion, overflow, and stale-data review;
- one package smoke against the production-built static assets;
- `npm run typecheck`, package tests, build, audit, and repository Fleet check.

## Acceptance criteria

- the service exposes one local URL and requires no separate UI process;
- readiness and operating mode are visible in the first viewport;
- attention conditions identify their Fleet, dependency, and last check;
- every Fleet maps unambiguously to one Pactline Project;
- active Runs expose stage, age, Adapter, state, and latest safe checkpoint;
- Run detail preserves Task, Claim, Session, workspace, revision, effect, and
  recovery causality without sensitive content;
- an SSE outage changes the UI to stale/polling mode without discarding the
  last valid snapshot;
- a degraded Fleet does not visually imply a global outage;
- all primary information is usable on desktop and phone;
- the observation surface contains no mutation route or mutation control.

## Completion evidence

- 144 Fleet Service tests and 5 UI component tests pass;
- 5 real Chromium flows cover Overview-to-Fleet navigation, Run evidence,
  System diagnostics, phone layout, and medium-width layout;
- TypeScript checks and the Vite production build pass;
- the packed package contains and serves the built console without Vite;
- SSE, polling fallback, loopback Host/Origin restrictions, read-only methods,
  API and UI CSPs, bounded projections, and safe external-effect fields are
  covered by focused tests;
- npm audit reports zero vulnerabilities;
- a manual desktop, medium, and phone pass found no console errors or
  horizontal overflow after responsive corrections.
