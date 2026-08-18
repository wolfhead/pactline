# Pactline Fleet Service Architecture

**Date:** 2026-08-17

**Status:** Accepted design; M5.1 through M5.4 implemented; M6 hardening pending

## Purpose

Pactline Fleet Service turns the existing finite, Harness-neutral Fleet
orchestrator into a resident local service. One service connects to one
Pactline instance, manages multiple logical Fleets, continuously discovers
eligible work, and makes its operating state observable through a local Web UI.

This design extends, rather than replaces, the Harness-neutral boundaries in
[Pactline Fleet Architecture](pactline-fleet-architecture.md). Pactline remains
the workflow authority. Fleet remains a standalone coordinator. Codex,
DeepSeek Harness, and future Harnesses remain interchangeable Adapters.

## Design decisions

1. One Fleet Service connects to exactly one Pactline instance.
2. One logical Fleet belongs to exactly one Pactline Project.
3. Within one Fleet Service, a Project may have at most one enabled Fleet.
4. Across different computers and Fleet Services, multiple Fleets may watch
   and compete for work in the same Project.
5. Pactline Claim creation, not Fleet coordination, provides global Task
   exclusivity.
6. Fleet configuration is declarative YAML and supports validated atomic
   reload.
7. SQLite is the local coordination and recovery registry. It is never the
   source of truth for Pactline workflow state.
8. The local Web UI is an observation surface, not a second Pactline client or
   a mutation control plane.
9. The Web UI is served by Fleet Service on loopback and has no separate
   deployment or login in the first release.
10. Finite diagnostic commands remain available after the resident service is
    introduced.

## Terminology and cardinality

### Fleet Service

The resident operating-system process. It owns one configuration, one local
registry, one global concurrency budget, one scheduler, one reconciler, and
one local observation endpoint.

### Fleet

A local scheduling and policy definition for one Pactline Project. It owns
Project-specific routing, workspace, repository policy, credential references,
concurrency, and health. It is not a new Pactline domain entity.

### Run

One Fleet attempt to coordinate one Task through one Claim stage. Admission
freezes the Task, Project, Adapter, model, reasoning, prompt policy,
verification policy, repository revision, and configuration revision. The
Claim and Adapter Session identities become immutable when they are later
observed.

### Adapter Session

The Codex Thread, DeepSeek Session, or equivalent Harness-native execution
identity associated with a Run.

### Cardinality

~~~text
Fleet Service 1 ----- 1 Pactline instance
Fleet Service 1 ----- N Fleets
Fleet         1 ----- 1 Project
Fleet         1 ----- N Runs
Run           1 ----- 0..1 active Worktree
Run           1 ----- 0..1 Adapter Session
~~~

The one-Fleet-per-Project constraint is local to a Fleet Service configuration.
It is not a global lease. Different users may run independent Fleet Services
against the same Project and compete for the same eligible Tasks.

## System topology

~~~mermaid
flowchart LR
    P["Pactline instance"]

    subgraph S["Fleet Service on one computer"]
        C["Configuration manager"]
        D["Discovery and fair scheduler"]
        R["Run coordinator"]
        X["Startup reconciler"]
        DB["SQLite registry"]
        O["Read-only observation API"]
        UI["Local Web UI"]

        F1["Fleet: Project 5"]
        F2["Fleet: Project 12"]

        A1["Codex Adapter"]
        A2["DeepSeek Adapter"]
    end

    P <-->|"Pactline CLI protocol"| D
    P <-->|"Claim and settlement"| R
    C --> D
    C --> R
    D --> F1
    D --> F2
    F1 --> R
    F2 --> R
    R --> A1
    R --> A2
    R <--> DB
    X <--> DB
    X <--> P
    DB --> O
    O --> UI
~~~

Another computer may run another Fleet Service against the same Pactline
instance and Project. The services do not share a registry, exchange heartbeats,
or elect a leader.

## Authority boundaries

### Pactline is authoritative for

- Project and Task identity;
- lifecycle phase, activity, and review cycle;
- Claim exclusivity, ownership, expiry, stage, and outcome;
- Task and Claim versions;
- Acceptance Criteria and Checks;
- Main and Issue Threads;
- execution submissions and frozen review delivery;
- linked code changes;
- authentication, authorization, idempotency, and business audit.

### Repository providers are authoritative for

- remote repositories and references;
- pushed commits and branches;
- Pull Request or Merge Request identity and state;
- provider-side delivery evidence.

### Fleet Service owns

- local Fleet definitions;
- bounded Project discovery and admission;
- global and per-Fleet scheduling budgets;
- deterministic Harness routing;
- disposable Worktree allocation;
- coordinator verification and delivery;
- local Run identity and configuration snapshots;
- external-effect checkpoints;
- startup reconciliation decisions;
- operational health, metrics, and bounded local history.

### Harness Adapters own

- capability probing;
- Harness process and Session lifecycle;
- Harness-native tools and sandbox configuration;
- event and usage translation;
- conversion of a terminal Harness result into a Fleet proposal.

An Adapter never claims Pactline work, settles a Claim, pushes a branch, creates
a PR or MR, or receives Pactline and repository-provider write credentials.

## Distributed work competition

Multiple Fleet Services discovering the same Task is expected behavior.

Pactline serializes Claim creation by locking the Task row, checking the
expected Task version, and enforcing one active Claim per Task in PostgreSQL.
Exactly one contender succeeds. Other contenders receive a version or active
Claim conflict.

Fleet treats those conflicts as normal scheduling outcomes:

1. remove the stale candidate from the current local queue;
2. record a bounded debug or informational event;
3. refresh authoritative Task state;
4. continue with the next candidate;
5. apply bounded random jitter before the next discovery cycle.

Fleet must not add a distributed Project lock. Such a lock would incorrectly
prevent independent users from competing for work that Pactline intentionally
makes claimable.

### Same-principal ambiguity

Two Fleet Services may use credentials for the same Pactline logical principal.
Pactline permits that principal to continue its active Claims, while Client
Session ID remains audit provenance rather than ownership.

Therefore a Fleet Service:

- automatically resumes only Claims recorded in its own local registry;
- never adopts an unfamiliar active Claim merely because the principal matches;
- quarantines recovery when its registry was lost and ownership is ambiguous;
- may support an explicit operator adoption command in a later mutation
  control plane, but never performs implicit adoption.

## Configuration model

The YAML document is the configuration authority. SQLite stores the exact
configuration revision frozen into each Run, but it does not become a mutable
configuration database.

~~~yaml
version: 1

service:
  pactline:
    server: http://localhost:8080
    tokenEnv: PACTLINE_TOKEN
  stateDirectory: /var/lib/pactline-fleet
  pollInterval: 10s
  maxConcurrentRuns: 1
  shutdownDeadline: 30s
  http:
    address: 127.0.0.1
    port: 7331

fleets:
  pactline-development:
    project: 5
    enabled: true
    maxConcurrentRuns: 1
    workspaceRoot: /var/lib/pactline-fleet/fleets/project-5

    workPlugin:
      executable: /opt/pactline-fleet/plugins/pactline-development
      args: []
      timeout: 2m

    routing:
      execution:
        adapter: codex
        model: gpt-5.6-sol
        reasoning: high
      review:
        adapter: codex
        model: gpt-5.6-sol
        reasoning: medium
      correction:
        adapter: codex
        model: gpt-5.6-sol
        reasoning: high
      resolutionAnalysis:
        adapter: codex
        model: gpt-5.6-sol
        reasoning: medium

    credentials:
      git: pactline-development-git
~~~

Secrets never appear in this document. Credential fields are references
resolved from the service environment or a later credential provider.

`workPlugin` is the explicit project-policy boundary. Pactline work packets do
not carry repository base selection, allowed paths, verification commands, or
provider write policy, and Fleet never infers them from Task prose. Before a
Claim, the plugin resolves those facts into a complete credential-free work
definition which is frozen in the Run registry. After coordinator verification,
the same frozen plugin may publish a provider-specific delivery and must return
the observed PR/MR URL, branch, and revision. Fleets without this boundary are
health-visible but scheduling-inactive.

### Validation

Configuration loading rejects:

- more than one Pactline instance;
- duplicate Fleet IDs;
- more than one enabled Fleet for the same Project;
- relative or overlapping workspace roots;
- unknown Adapters, stages, models, or reasoning values;
- a per-Fleet concurrency greater than the global budget;
- missing credential references for an enabled delivery policy;
- a non-loopback HTTP address in the first release.

### Atomic reload

Fleet watches the configuration file or receives an explicit reload signal.
It parses and validates the complete candidate document before swapping the
active configuration. An invalid candidate leaves the previous configuration
running and produces a visible health event.

Active Runs retain their frozen configuration. Disabling or removing a Fleet
stops new discovery immediately and lets active Runs reach a safe terminal or
recovery state. Historical Runs remain queryable after their Fleet definition
is retired.

Pactline endpoint, state-directory, and HTTP-listener changes are
process-bound and require restart. A hot reload that changes one of these
values is rejected without partially applying the candidate configuration.

## Process model

Fleet Service is one Node.js process with owned Adapter subprocesses.

The first release uses:

- one scheduler and reconciler;
- one SQLite writer;
- configurable global concurrency with a default of one;
- configurable per-Fleet concurrency with a default of one;
- one Adapter process per active Run when the Adapter requires a process;
- an operating-system advisory lock on the state directory.

The advisory lock prevents accidental duplicate startup against the same local
registry. It does not attempt to exclude services on other computers.

## Startup and shutdown

### Startup

~~~text
load and validate configuration
  -> validate private state-directory permissions
  -> open and migrate SQLite
  -> acquire the local service lock
  -> check Pactline CLI protocol and authentication
  -> probe every Adapter required by an enabled Fleet
  -> reconcile every non-terminal Run
  -> start the observation endpoint
  -> declare readiness
  -> begin discovery
~~~

Discovery must not begin before reconciliation completes.

### Graceful shutdown

~~~text
enter draining
  -> stop new discovery and Claim attempts
  -> signal active Runs
  -> wait until the configured deadline
  -> persist the latest safe checkpoint
  -> cancel or terminate owned Adapter processes
  -> close the observation endpoint
  -> close SQLite
  -> release the local service lock
~~~

Shutdown never invents a Pactline terminal outcome. A Claim left active is
reconciled on the next start.

## Scheduling

Each enabled Fleet polls only its configured Project through the Pactline CLI
machine contract.

Execution discovery follows Pactline assignment visibility. Review discovery
follows Project visibility. Fleet does not broaden either rule.

The scheduler uses two levels of budget:

- a global maximum for the service;
- a per-Fleet maximum.

Eligible Fleets are visited round-robin. Candidate ordering inside one Fleet is
deterministic. A Project with a large queue must not indefinitely starve other
Projects.

Before Claim creation, Fleet verifies:

- the Fleet and Project mapping is unchanged;
- the Task is still eligible for the requested stage;
- the current Task version matches discovery;
- the configured Adapter satisfies the stage capability policy;
- the repository and base revision are allowed;
- local and global concurrency are available;
- the service is not draining.

No Harness process starts before a Claim is successfully created and durably
recorded.

## Registry and persistence

SQLite is justified at the resident-service boundary because the service must
transactionally coordinate multiple Fleets, active Runs, external effects,
health history, and observation queries. It introduces no external database
service and fits the single-process writer model.

The registry is a local operational ledger, not a Task database.

### Minimum records

- service identity and schema version;
- accepted configuration revisions;
- Fleet identity and Project mapping history;
- Run identity and frozen policy;
- Task, Claim, stage, and version facts;
- Adapter and Session identity;
- workspace and exact Git revision;
- protected idempotency material;
- external-effect checkpoints;
- Run events and health observations;
- terminal disposition and cleanup status.

### Run state

~~~text
admitted
  -> claiming
  -> claimed
  -> preparing_workspace
  -> starting_harness
  -> running_harness
  -> validating
  -> delivering
  -> settling
  -> completed

Any non-terminal state
  -> releasing
  -> released

Any ambiguous recovery state
  -> quarantined
~~~

Transitions are monotonic and transactional. A state name alone is never
enough to decide recovery; each transition records the authoritative facts and
external-effect checkpoint that justify it.

### External-effect checkpoints

Fleet records intent before, and observed fact after:

- Claim creation;
- workspace allocation;
- Adapter Session creation;
- Harness result capture;
- commit creation;
- branch push;
- PR or MR creation;
- Claim settlement;
- recovery Claim release.

Run state and the matching Effect fact are committed atomically at authority
boundaries. Recovery re-reads Pactline, the retained workspace, local Git, and
the remote branch before continuing. An already observed effect is never
repeated. An unobserved effect is repeated only when the available authority
can prove it did not happen; otherwise the Run is quarantined. In particular,
the current Work Plugin cannot read PR/MR identity, so an unobserved
code-change-creation intent is quarantined rather than blindly replayed.

`validating` and `delivering` recovery revalidate the persisted Harness result,
current Claim, frozen route, workspace, allowed paths, and fixed verification
without calling Harness `run` or `resume`. `settling` recovery re-reads the
Claim and replays only the exact persisted settlement action and original
idempotency-key prefix.

## Health model

Health is hierarchical and distinguishes liveness from readiness.

### Service health

- process and scheduler heartbeat;
- local lock ownership;
- SQLite readability and writability;
- state-directory space and permissions;
- configuration validity;
- graceful-shutdown state.

### Pactline health

- CLI protocol compatibility;
- authentication validity;
- endpoint reachability;
- rate-limit state;
- last successful request and discovery cycle.

### Fleet health

- enabled, disabled, draining, or retired state;
- Project visibility;
- last successful poll;
- candidate and active Run counts;
- stalled or quarantined Runs;
- Adapter route availability;
- repository policy and credential readiness;
- recent success and failure summary.

### Adapter health

- installed and configured version;
- declared capabilities;
- latest successful probe and Run;
- recent timeout, cancellation, protocol, and tool-error counts;
- latency and usage summaries when available.

### Readiness semantics

The process is live when its event loop and observation endpoint respond.

The service is ready when reconciliation is complete and at least one enabled
Fleet can safely schedule. A broken Fleet may make aggregate health degraded
without making every other Fleet unavailable. A global Pactline outage or an
unwritable registry makes the service not ready.

## Local observation API

Fleet Service serves a read-only HTTP surface on loopback:

- GET /livez
- GET /readyz
- GET /healthz
- GET /metrics
- GET /api/v1/service
- GET /api/v1/fleets
- GET /api/v1/fleets/:fleetId
- GET /api/v1/runs
- GET /api/v1/runs/:runId
- GET /api/v1/adapters
- GET /api/v1/events

The events endpoint uses Server-Sent Events for bounded live updates. The UI
falls back to polling after a connection loss. WebSocket bidirectional
complexity is unnecessary for a read-only surface.

The API is internal and versioned so the separately built static UI does not
depend on in-process objects. It exposes sanitized operational projections,
not raw SQLite rows, prompts, reasoning, credentials, or unbounded model output.

## Web UI design

### Job and audience

The UI is a local Operations Console for the person running Fleet Service. Its
job is to answer, in order:

1. Is the service healthy and scheduling?
2. Which Fleet or Run needs attention?
3. What is running now, and what authoritative facts support its state?
4. Did recent work complete, recover, release, or quarantine correctly?

The interaction mode is Operate. It favors scanability, precise state, and
evidence over decorative dashboard presentation.

### Visual direction

The UI extends Pactline's fixed-light Glacier Blue and Confluence Teal visual
identity. It uses the same semantic colors, typography, compact spacing, and
quiet-at-rest interaction grammar, but it is built independently under
fleet/web and imports no source from Pactline's main web application.

The page is a layered workbench, not a grid of ornamental metric cards.
Attention conditions lead. Summary counts support diagnosis rather than
compete for visual priority.

### Information architecture

~~~text
Overview
  - service readiness and current mode
  - conditions requiring attention
  - Fleet table
  - active Runs
  - recent terminal Runs

Fleet detail
  - Project identity and Pactline link
  - discovery and scheduling state
  - routing policy snapshot
  - repository and workspace readiness
  - active and recent Runs
  - Fleet-scoped health timeline

Runs
  - filterable active and historical collection
  - stage, Task, Fleet, Adapter, state, age, and outcome

Run detail
  - frozen identities and policy
  - ordered state timeline
  - Claim and Pactline versions
  - Worktree and Git revisions
  - verification and delivery evidence
  - bounded Adapter events and usage
  - recovery decisions and cleanup disposition

System
  - Pactline connection
  - registry and disk
  - configuration revision and reload result
  - Adapter inventory and capability probes
  - service version and uptime
~~~

### Overview hierarchy

The first viewport contains:

1. a compact service header with ready, degraded, reconciling, or draining
   state;
2. an Attention section shown only when action is needed;
3. a dense Fleet table with one row per Project;
4. active Runs with stage, elapsed time, Adapter, and latest checkpoint;
5. recent outcomes below the fold.

Healthy zero states should feel calm. The interface must not render a wall of
green badges when nothing needs attention.

~~~text
┌ Fleet Service ───────────────────────────────── Ready · updated 3s ago ┐
│ Pactline local · 2 Fleets · 1 active Run                 System health │
├─────────────────────────────────────────────────────────────────────────┤
│ Attention                                                               │
│ Project 12 cannot probe DeepSeek Adapter              Adapter unavailable│
├─────────────────────────────────────────────────────────────────────────┤
│ Fleets                                                                  │
│ Project       Discovery       Active       Queue       Health            │
│ Pactline      4s ago          1 review     3           Healthy           │
│ Game Design   7s ago          —            2           Degraded          │
├─────────────────────────────────────────────────────────────────────────┤
│ Active Runs                                                             │
│ #41  Review  Codex  03:18  validating        last checkpoint 2s ago     │
├─────────────────────────────────────────────────────────────────────────┤
│ Recent outcomes                                                         │
│ #39 completed · #40 released · #38 recovered                            │
└─────────────────────────────────────────────────────────────────────────┘
~~~

### Run detail hierarchy

Run detail leads with the current state and latest safe checkpoint. It then
shows Task, Claim, Fleet, Adapter, Session, repository, and revision identities.
The timeline explains transitions and recovery decisions in causal order.

Raw model reasoning is never displayed. Bounded normalized Adapter events may
show tool category, duration, result class, and sanitized summary.

~~~text
┌ Run #41 · validating ────────────────────────── last checkpoint 2s ago ┐
│ Task #18 · review · Project 5 · Codex / gpt-5.6-sol / medium           │
│ Claim 8d… · Session 01… · delivery 7f92…                              │
├──────────────────────────────┬──────────────────────────────────────────┤
│ Timeline                     │ Evidence                                 │
│ 10:12 claimed                │ Worktree     /…/runs/41                  │
│ 10:12 workspace prepared     │ Base         13ad…                       │
│ 10:13 Harness started        │ Delivery     7f92…                       │
│ 10:16 result received        │ Verification 4/4 passed                  │
│ 10:16 validating ← current   │ PR/MR        not required yet            │
└──────────────────────────────┴──────────────────────────────────────────┘
~~~

### Interaction boundaries

The first release is read-only:

- links may open the corresponding Pactline Task or provider PR/MR;
- identifiers and diagnostic bundles may be copied;
- filters and display preferences may be local browser state;
- no UI action claims, releases, settles, adopts, retries, pauses, drains, or
  edits configuration.

Mutating operator actions remain explicit CLI commands until a separate local
control-plane authorization design is approved.

### Responsive behavior

Desktop is the primary surface. It uses persistent navigation, a dense
collection, and an optional detail region.

At medium widths, navigation collapses and detail becomes a full-width route.
On phones, Fleet and Run rows become compact two-line cards. The mobile surface
retains observation and links but does not attempt desktop-equivalent density.

All core information is keyboard reachable and meets WCAG 2.2 AA. Status is
communicated through text and iconography as well as color.

### Required states

- first start with no configured Fleets;
- healthy idle;
- active execution and review Runs;
- configuration reload rejected;
- Pactline unreachable or unauthorized;
- one Adapter unavailable while others remain usable;
- service reconciling after restart;
- Fleet draining or retired;
- Run stalled, released, failed, or quarantined;
- SSE disconnected with polling fallback;
- long histories and bounded pagination;
- partial data where provider enrichment is unavailable.

## Packaging and deployment

The Web UI is a standalone React and TypeScript application under fleet/web,
built with Vite into versioned static assets included in the Fleet package.
Fleet Service serves those assets and the read-only API from the same loopback
listener.

The UI does not:

- import the Pactline web application's runtime or API clients;
- require Pactline cookies or Lark authentication;
- access SQLite directly;
- call Pactline from the browser;
- require a second Node.js process in production.

Development may use a separate Vite server proxied to Fleet Service, but the
release artifact is one Fleet Service installation.

## Local HTTP safety

The first release:

- binds only to an explicit loopback address;
- refuses 0.0.0.0 and non-loopback addresses;
- does not enable CORS;
- validates Host and Origin where relevant;
- serves a restrictive Content Security Policy;
- exposes no secrets, prompts, raw environment, or filesystem browsing;
- provides no mutation route;
- redacts sensitive values before they enter registry events.

Remote access, TLS termination, login, and multi-user UI authorization are
future product decisions rather than hidden configuration switches.

## Observability and retention

Structured logs and UI events share stable event categories but not necessarily
the same storage representation.

Metrics use bounded labels such as Fleet, Project, Adapter, stage, and outcome.
Task IDs, Run IDs, error messages, branch names, and repository URLs must not
be Prometheus labels.

The current M5 implementation removes disposable workspaces on terminal or
safely released paths, but retains terminal Runs, effects, events, and recorded
configuration revisions in the private local registry. It does not yet perform
automatic age- or count-based history pruning. Concrete retention limits,
operator-visible pruning, and proof that cleanup deletes only registry-owned
records and exact recorded artifacts remain M6 work.

Run states and stages come from the Run domain, while the closed External
Effect title and safe-field map is owned by the Observation Model. Recovery
decisions are projected into the causal Run timeline but never become mutation
authority. The broad `fleet/src/index.ts` root exports are retained as a
compatibility surface pending a separate review; new coordination internals
should not be exported there by default.

## Non-goals

- global Fleet registration in Pactline;
- cross-computer Fleet coordination or leader election;
- high availability for Fleet Service;
- automatic Harness fallback;
- a new Task lifecycle or Project field;
- remote Web UI access;
- Web UI mutations;
- automatic PR or MR merge;
- displaying raw prompts, chain of thought, or unbounded logs;
- replacing Pactline's Project or Task UI.

## Architectural acceptance criteria

- one service manages at least two Fleets mapped to different Projects;
- separate services may compete for the same Project without Fleet-level
  coordination;
- concurrent Claim attempts converge through Pactline with one owner;
- a local Project cannot be duplicated across enabled Fleet definitions;
- active Runs retain their frozen configuration after reload;
- startup reconciles all non-terminal Runs before discovery;
- every uncertain external effect is re-read before retry;
- unfamiliar same-principal Claims are not adopted automatically;
- one Fleet failure does not block healthy Fleets unless a global dependency
  is unavailable;
- the local UI accurately represents service, Fleet, Adapter, and Run health;
- the UI remains read-only and loopback-only;
- finite diagnostic commands continue to work without starting the service.

M5.4 verified the complete resident path for one Project-bound Fleet,
deterministic delivery and correction, one representative post-Session
restart, both live Adapter routes, and real-browser observation. Run-domain
hardening subsequently added deterministic close/reopen coverage for persisted
post-result work, Git commit and push reconciliation, settlement replay, and
safe quarantine where provider observation is unavailable. Storage fault
injection, long-duration soak, live provider degradation, and retention policy
remain M6 qualification. The exact M5.4 evidence and limits are recorded in the
[M5.4 Acceptance Report](pactline-fleet-m5-4-report.md).
