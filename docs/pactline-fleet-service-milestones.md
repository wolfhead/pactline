# Pactline Fleet Service Milestones

**Date:** 2026-08-15

**Status:** M5.0 through M5.3 complete; M5.4 pending

## Objective

Deliver a recoverable resident Fleet Service that manages multiple
Project-bound Fleets, safely competes with other distributed Fleet Services,
and exposes a local read-only Web UI for operational observation.

The normative design is
[Pactline Fleet Service Architecture](pactline-fleet-service-architecture.md).
This plan begins after the completed Harness-neutral M0 through M4 work. It
divides the existing M5 resident-service goal into four independently
verifiable gates.

~~~text
M5.0 accepted design
  -> M5.1 service foundation
  -> M5.2 durable scheduling and recovery
  -> M5.3 local Web UI and observability
  -> M5.4 distributed and restart acceptance
  -> M6 provider, sandbox, and production hardening
~~~

## Milestone status

| Milestone | Status | Result |
|---|---|---|
| M5.0 Design freeze | complete | Architecture and implementation gates documented |
| M5.1 Service foundation | complete | Resident process, config, registry, local health |
| M5.2 Durable scheduler and recovery | complete | Fair scheduling, durable checkpoints, recovery, work plugins |
| M5.3 Local Web UI and observability | complete | Read-only Operations Console and observation API |
| M5.4 Distributed and restart acceptance | pending | Production-shaped local release candidate |

## M5.0 — Design freeze

### Objective

Freeze the resident-service topology, Project-to-Fleet cardinality,
distributed Claim competition, configuration authority, persistence boundary,
and local UI role before implementation.

### Decisions

- one Fleet Service connects to one Pactline instance;
- one local Fleet maps to one Project;
- one Project appears at most once among enabled Fleets in one service;
- other services may independently watch and compete for the same Project;
- Pactline Claim semantics provide global Task exclusivity;
- YAML is configuration authority;
- SQLite is local coordination and recovery state;
- the local Web UI is embedded, loopback-only, and read-only;
- the first UI has no separate login or remote mode.

### Exit criteria

- the architecture identifies every authority boundary;
- same-principal cross-service ambiguity has an explicit non-adoption rule;
- UI audience, information architecture, deployment, and mutation boundary are
  fixed;
- M5.1 through M5.4 have independent acceptance gates.

## M5.1 — Resident service foundation

### Objective

Create a finite, diagnosable resident process with validated configuration,
local persistence, single-instance protection, lifecycle management, and a
minimal machine-readable health surface.

### Scope

- add the serve command while retaining doctor, version, and finite runners;
- define and validate the versioned YAML configuration schema;
- enforce one Pactline instance and one enabled Fleet per local Project;
- implement credential references without storing secret values;
- create the private state-directory layout;
- implement the SQLite registry interface and schema migration mechanism;
- assign stable service, Fleet, configuration-revision, and Run identities;
- implement an operating-system advisory lock for the local state directory;
- implement startup phases and signal-driven graceful shutdown;
- implement atomic configuration reload with frozen active-Run policy;
- serve livez, readyz, healthz, and a minimal service-status JSON projection;
- add structured, secret-safe lifecycle logging.

### Proposed component boundaries

- fleet/src/service/
- fleet/src/config/
- fleet/src/registry/
- fleet/src/health/
- fleet/src/http/
- fleet/src/commands/serve.ts
- fleet/tests/service/
- fleet/tests/config/
- fleet/tests/registry/

Exact filenames are implementation-plan decisions; the module boundaries are
part of this design.

### Verification

- schema tests for valid and invalid Fleet configurations;
- duplicate Project rejection;
- non-loopback listener rejection;
- config reload rollback after invalid input;
- active Run configuration snapshot remains unchanged after reload;
- SQLite migration and transaction tests;
- permission and symlink checks for state paths;
- second local process fails fast while the first owns the lock;
- SIGTERM and SIGINT stop discovery and close cleanly;
- finite commands do not acquire the resident lock or start HTTP;
- livez, readyz, and healthz reflect startup, ready, degraded, and draining.

### Exit criteria

- a bounded idle child-process smoke exits without leaked subprocesses or
  residual state ownership; the long-duration soak remains the M5.4 gate;
- a two-Fleet configuration loads and exposes both Project mappings;
- no discovery or Claim mutation occurs before readiness;
- invalid reload never partially changes active configuration;
- SQLite contains no credential value;
- the service starts, reports health, drains, and restarts cleanly.

## M5.2 — Durable multi-Fleet scheduler and recovery

### Objective

Continuously discover and process work across multiple Project-bound Fleets
while preserving Pactline authority and converging safely after every crash
boundary.

### Scope

- poll execution and review candidates per configured Project;
- implement deterministic in-Fleet ordering and cross-Fleet round-robin;
- implement global and per-Fleet concurrency budgets;
- add random poll jitter and bounded backoff;
- treat Claim conflicts as normal distributed contention;
- persist Run and Claim identity before Harness dispatch;
- freeze Adapter, model, reasoning, prompt, repository, and verification policy;
- integrate disposable Worktree allocation with registry ownership;
- persist external-effect intent and observed-fact checkpoints;
- resume supported Adapter Sessions;
- release or quarantine unsupported and ambiguous recovery states;
- refuse implicit adoption of unfamiliar same-principal Claims;
- reconcile Pactline, Git, provider, workspace, and Adapter facts at startup;
- retain a finite once path for deterministic diagnosis;
- add fault injection around every durable and external-effect boundary.

### Required crash checkpoints

1. before Claim creation;
2. after Claim creation and before Run persistence;
3. after Run persistence and before Harness Session creation;
4. after Session identity and before the first Agent effect;
5. after a workspace effect and before result capture;
6. after structured result and before commit;
7. after commit and before push;
8. after push and before PR or MR creation;
9. after PR or MR creation and before Pactline linking;
10. after linking and before execution completion;
11. after settlement and before local terminal persistence;
12. before and after typed Issue resolution;
13. before a post-resolution Claim dispatch;
14. during graceful-shutdown deadline expiry.

### Distributed competition tests

- two service fixtures discover the same Task and only one Claim succeeds;
- the losing service refreshes and continues without degraded health;
- repeated conflicts apply jitter and do not spin;
- services using different principals preserve the winning principal;
- two services using the same principal do not adopt each other's registered
  Claims;
- missing local registry plus an unfamiliar same-principal Claim quarantines
  rather than continues or releases.

### Verification

- deterministic scheduler tests with fake time;
- registry transition and crash-reopen tests;
- Replay Adapter recovery tests;
- Codex resume and cancellation tests;
- DeepSeek non-resumable release or quarantine tests;
- provider and Pactline uncertain-response reconciliation;
- Worktree ownership and exact cleanup tests;
- serialized local Docker integration against real Pactline;
- bounded queue drain with at least two Fleets and global concurrency one.

### Exit criteria

- every injected checkpoint converges to completed, released, or quarantined;
- no duplicate Claim mutation, submission, check, branch, PR/MR, or Thread Item
  occurs;
- no recovery changes Adapter automatically;
- one Fleet cannot starve another;
- Claim contention is not reported as a service failure;
- graceful shutdown leaves no unrecorded local effect;
- restart finishes reconciliation before admitting new work.

## M5.3 — Local Web UI and observability

### Objective

Give the local operator a clear, read-only view of service readiness, Fleet
health, active work, recent outcomes, and recovery evidence.

### Scope

- create fleet/web as an independent React and TypeScript Vite application;
- extend Pactline's Glacier Blue and Confluence Teal visual identity without
  importing the main Pactline frontend;
- implement the versioned read-only local observation API;
- implement Server-Sent Events with polling fallback;
- build Overview, Fleet detail, Runs, Run detail, and System routes;
- expose Adapter capability and health projections;
- expose bounded Run timelines and recovery decisions;
- expose Prometheus metrics with bounded labels;
- package static UI assets with Fleet Service;
- add a command that prints and optionally opens the local UI URL;
- implement desktop, medium-width, and phone observation layouts;
- document which diagnostics are intentionally absent or redacted.

### Surface acceptance

#### Overview

- readiness and operating mode are visible in the first viewport;
- Attention appears before summary data when intervention is needed;
- every Fleet row maps unambiguously to one Project;
- active Runs show stage, age, Adapter, and latest safe checkpoint;
- healthy idle does not render excessive success decoration.

#### Fleet detail

- Project identity, discovery state, route policy, repository readiness,
  active Runs, and recent health are visible;
- a disabled, draining, retired, or degraded Fleet has an explicit explanation;
- failure of one Fleet does not visually imply global service failure.

#### Run detail

- Task, Claim, stage, versions, Adapter Session, Worktree, and revisions are
  traceable;
- the current state and last safe checkpoint lead the page;
- timeline entries explain recovery decisions in causal order;
- raw prompts, reasoning, credentials, and unbounded output are absent.

#### System

- Pactline, registry, configuration, disk, Adapter, version, and uptime status
  are available;
- rejected configuration reloads are visible without exposing file contents or
  secrets.

### Interaction acceptance

- there is no mutation HTTP route;
- UI controls cannot Claim, retry, release, adopt, settle, pause, or edit
  configuration;
- Pactline Task and PR/MR links open the authoritative external surface;
- identifiers and a bounded diagnostic summary can be copied;
- loading, empty, degraded, disconnected, overflow, and stale-data states are
  designed and tested;
- keyboard navigation and contrast meet WCAG 2.2 AA.

### Verification

- observation API schema and redaction tests;
- SSE reconnect and polling fallback tests;
- component tests for all material states;
- Playwright flows for Overview, Fleet detail, Run detail, and System;
- responsive screenshots at phone, medium, and desktop widths;
- package test proving the release artifact serves the UI without Vite;
- CSP, Host, Origin, CORS, loopback, and absence-of-mutation tests;
- one manual visual-quality pass against the Fleet surface brief and Pactline
  design system.

### Exit criteria

- a fresh installation exposes one local URL with no separate UI process;
- the UI reflects registry and live service changes without manual refresh;
- an SSE outage falls back without losing the last-known-state indication;
- every degraded condition identifies its scope and last successful check;
- no secret or raw model reasoning is visible in HTML, API responses, browser
  storage, logs, or packaged fixtures;
- all primary information remains usable on desktop and phone.

### Completion evidence

- the versioned read-only observation API projects service, Fleet, Run,
  Adapter, discovery, timeline, and external-effect facts with bounded fields;
- SSE revision notifications and polling fallback are covered independently;
- the production package serves the independent React application from the
  same loopback listener and includes its hashed static assets;
- Overview, Fleet detail, Run detail, System, phone, and medium-width flows pass
  real Chromium tests without horizontal overflow;
- Host, Origin, method, CSP, SPA fallback, and package-delivery tests pass;
- `make pactline-fleet-check` passes 144 service tests, 5 component tests, 5
  browser tests, type checking, production build, process smoke, and package
  smoke with zero npm audit findings.

## M5.4 — Distributed and restart acceptance

### Objective

Prove the complete resident service under realistic local operation before
provider and sandbox hardening or real-work promotion.

### Test topology

- one local Pactline instance;
- at least two Pactline Projects;
- one Fleet Service managing both Projects;
- a second isolated Fleet Service competing for one of those Projects;
- separate state directories and registries;
- both Codex and DeepSeek Adapters available where their configured stage
  requires them;
- a controlled local repository and provider fixture;
- Web UI observation throughout the run.

### Scenarios

- both local Fleets discover and drain bounded work fairly;
- the second service loses and wins different Claim races;
- one Adapter becomes unavailable while another Fleet remains healthy;
- Pactline becomes temporarily unreachable;
- configuration reload disables one Fleet during an active Run;
- service termination occurs at every required crash checkpoint;
- the service restarts with resumable and non-resumable Sessions;
- an unfamiliar same-principal Claim is presented;
- SSE disconnects while work continues;
- SQLite and disk health degrade in controlled fixtures;
- old terminal history is retained and exact disposable artifacts are cleaned.

### Evidence

- Pactline Task, Claim, Thread, Check, and code-change state;
- local Run registry transitions;
- exact Git revisions and provider evidence;
- Adapter Session identity and bounded usage;
- structured service logs;
- health endpoint snapshots;
- Web UI screenshots for healthy, active, degraded, reconciling, and
  quarantined states;
- resource and subprocess leak checks;
- a secret-shape scan over retained evidence and packaged UI assets.

### Exit criteria

- all admitted Tasks have one correct terminal or quarantined disposition;
- no duplicate external effect occurs across crashes or service competition;
- no unfamiliar Claim is adopted implicitly;
- one Fleet failure remains isolated;
- the UI matches authoritative and local coordination state throughout;
- service restart requires no database or file repair by the operator;
- a 24-hour bounded soak completes without unbounded memory, database, event,
  file-descriptor, or subprocess growth;
- the owner receives a release-candidate acceptance report.

## M6 boundary after resident-service delivery

M6 remains the provider, sandbox, and production-operations hardening gate. It
does not need to recreate the local health model or Web UI.

M6 adds:

- complete GitHub and GitLab provider contract qualification;
- stronger Harness filesystem, network, process, CPU, and memory isolation;
- credential proxying where required;
- production retention and cleanup policy;
- external metrics collection, dashboards, and alerts;
- incident-response and backup documentation;
- remote observation only if separately designed and approved;
- public-repository and secret-scanning release gates.

## Cross-Milestone invariants

- Pactline remains the workflow authority.
- Fleet configuration never becomes Pactline Task state.
- One local Fleet maps to one Project.
- Distributed Fleet Services compete through Claim creation.
- Claim conflicts are expected scheduling outcomes.
- A Run never changes Adapter automatically.
- Active Runs use frozen policy.
- Models never receive Pactline or repository-provider write authority.
- SQLite stores coordination facts, not credentials or workflow truth.
- Recovery re-reads every external authority before retrying an uncertain
  effect.
- The first Web UI is loopback-only and read-only.
- Finite diagnosis remains possible without starting the resident scheduler.

## Implementation planning rule

Before starting each pending Milestone:

1. inspect the current source and repository status;
2. write a concrete file-level implementation plan;
3. identify schema and compatibility impact;
4. define focused tests and live fixtures;
5. confirm any new credential, external write, or destructive cleanup boundary;
6. implement only that Milestone;
7. produce an acceptance report before activating the next gate.
