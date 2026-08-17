# Pactline Fleet Master Runbook

**Date:** 2026-08-15

**Status:** M0 through M5 complete; M6 pending

**Purpose:** Canonical recovery and execution document for the complete
Harness-neutral Pactline Fleet initiative. Read this file first after context
compaction.

## Mission

Deliver a standalone `pactline-fleet` application that coordinates Pactline
work through internal Harness Adapters. Codex is the default quality-first
Adapter. DeepSeek Harness is a supported sibling Adapter and retained baseline,
not the Fleet host. The system must pass deterministic lifecycle, repository,
credential, recovery, and live capability gates before controlled real work.

## Current checkpoint

The standalone application under `fleet/` has completed M4. M1 supplied the
private `@pactline/fleet` package, typed Pactline CLI boundary,
capability-gated static Runtime Router, Claim-stage coordinator, deterministic
settlement, disposable workspaces, fixed verification, delivery evidence, and
Replay lifecycle coverage. M2 added `DeepSeekHarnessAdapter` as the first real
Adapter without moving DSH or Cordis types into Core and without automatic
fallback.

M5.1 added the resident, deliberately non-scheduling service foundation:
versioned YAML configuration, one local Fleet per Project, atomic safe reload,
private SQLite coordination state, a heartbeat-backed local process lock,
periodic Pactline and Adapter health probes, loopback-only read-only health,
and graceful signal shutdown.

M5.2 adds the durable multi-Fleet scheduler and recovery plane: deterministic
in-Fleet ordering, cross-Fleet round-robin, global/per-Fleet concurrency with
slot replenishment, jittered backoff, Pactline contention handling, registry
schema v3, exact Claim/Session/workspace/effect checkpoints, startup
reconciliation, Codex-style Session resume, DeepSeek-style non-resumable
release, unfamiliar same-principal Claim quarantine, finite `serve --once`,
and a trusted executable work-plugin boundary for repository and verification
policy. A real DeepSeek Pro/max isolated service demo and real local Pactline
two-Project discovery both pass.

M5.3 adds the independent read-only Operations Console and its versioned local
observation plane. Overview, Fleet, Runs, Run detail, and System routes are
served from the Fleet Service loopback listener. Bounded SSE revision events
fall back to polling; safe projections exclude raw prompts, reasoning, secrets,
unbounded output, and raw registry rows. Prometheus metrics use bounded labels,
and the packed artifact contains its production-built static assets. Component,
real Chromium responsive, HTTP boundary, and package smoke gates pass.

M5.4 completed the bounded usability gate. Replay proved deterministic
delivery, request-changes/correction/final review, and one non-resumable
post-Session restart. DeepSeek Pro/max execution with Codex high review and
Codex high execution/review both reached `done` through real local Pactline,
local Git delivery, and resident scheduling. The real Operations Console
accurately exposed the Fleet, Adapter routes, Sessions, checkpoints, and
settlement. Bounded local trials are accepted; production provider, sandbox,
and operational hardening remains M6.

The DeepSeek Adapter owns a separate pinned DSH `0.1.0-rc.6` runtime closure,
Cordis profile, terminal result plugin, credential resolution, JSON-RPC
transport, event/usage translation, and one process per Run. The fixed
qualification route is `deepseek-v4-pro` with reasoning `max`. Keyless doctor,
real replay contracts, cancellation/reap, conditional dependency loading,
local Pactline L0, and a live read-only L1 gate all pass.

M3 is complete. Its finite spike selected the pinned `@openai/codex` 0.147.0 CLI
as the Adapter runtime boundary rather than importing the TypeScript SDK
wrapper. Both surfaces drive the same Codex Agent; direct CLI ownership also
permits mandatory user-config/rules isolation, private result modes, bounded
wire parsing, and process-tree reap. The real read-only spike passed with
`gpt-5.6-sol` / `high`, captured Thread ID and usage, returned structured JSON,
kept Git clean, and exposed no Pactline/GitHub sentinel environment name to
model commands. M4 then completed six Codex/Codex lifecycle cases and two
bounded mixed-Adapter comparisons. Codex is the accepted default; DeepSeek is
retained as an explicit opt-in sibling Adapter. M5 is complete.

The former DeepSeek-specific Bundle was retired with owner approval after M5.4
proved the standalone Core, both Adapters, resident scheduling, representative
recovery, and bounded usability. DeepSeek support remains under
`fleet/runtime/deepseek/`; the standalone application never imported the old
source.

### Verified baseline snapshot

As of 2026-08-15:

- local Pactline Docker stack was healthy at `http://localhost:5173`;
- Pactline CLI protocol 2 exposed the 16 required Fleet features;
- the old DeepSeek Bundle passed typecheck, build, 15 test files, and 70 tests;
- the old real L2 preflight admitted 6 Tasks and 9 remote refs;
- DeepSeek L0 and L1 completed;
- L2 cases L2-01 through L2-06 completed at Pro/max;
- Pactline Tasks #6 through #11 are `done`;
- Task #9 completed two review cycles; the others completed one;
- typed resolution completed before repository mutation for Task #11;
- GitHub Draft Pull Requests #31 through #37 remain open and unmerged as
  retained evaluation evidence;
- remote `wolfhead/pactline` `main` remained at
  `abc3599c863fbc2041e0cd463776d3d8ca8c7fb1` during final verification;
- the evaluation Project had zero Repository Connections;
- the private L2 corpus manifest was mode 0600;
- no recovery-authority file or `pactline-fleet-l2-*` temporary workspace
  remained after final settlement.

These facts are a historical baseline. Re-read live state before relying on it
for a later mutation.

### Working-tree caution

At the planning checkpoint the repository was already dirty and contained
untracked user/agent work, including the complete old Bundle, CLI auth work,
and local design documents. Before every implementation interval:

```bash
git status --short --branch
```

Treat every pre-existing modification as separately owned. Never clean,
format, stage, delete, or rewrite unrelated files.

## Accepted architecture decisions

1. `fleet/` is a standalone application, not a DeepSeek Harness Bundle.
2. The package is `@pactline/fleet`; the executable is `pactline-fleet`.
3. Fleet Core is Harness-neutral and communicates with Pactline only through
   the installed CLI machine contract.
4. Codex and DeepSeek Harness are sibling Adapters.
5. Codex is the default for execution, review, correction, and resolution
   analysis during capability-first qualification.
6. Fleet owns the outer deterministic workflow; each Harness owns its inner
   Agent Loop.
7. Harnesses never receive Pactline or repository-provider write credentials.
8. Harnesses propose results; Fleet validates and settles them.
9. There is no automatic cross-Adapter fallback.
10. The Adapter interface remains internal until Codex and DeepSeek both prove
    it; no public third-party plugin API is frozen in the first version.
11. Finite runners may retain private atomic JSON evidence. The resident Fleet
    Service uses SQLite for local coordination, recovery, and observation.
12. The former reference Bundle was never a runtime dependency and was retired
    after M5.4; no active command or test may restore that dependency.
13. No compatibility alias remains for `@pactline/dsh-fleet` after cutover.
14. New qualification uses new Tasks and branches; completed Tasks #6 through
    #11 are never reused.
15. Game Design remains outside the qualification and requires a separate
    post-pilot decision.
16. One Fleet Service connects to one Pactline instance and manages multiple
    logical Fleets, each mapped to exactly one Project.
17. Independent Fleet Services may compete for the same Project; Pactline
    Claim semantics provide global Task exclusivity.
18. The first Fleet Web UI is embedded, loopback-only, and read-only.

## Canonical documents

Read in this order:

1. this Master Runbook;
2. `docs/pactline-fleet-architecture.md`;
3. `docs/pactline-fleet-service-architecture.md`;
4. `docs/pactline-fleet-service-milestones.md`;
5. `docs/superpowers/specs/2026-08-15-pactline-fleet-harness-neutral-architecture.md`;
6. `docs/superpowers/plans/2026-08-15-pactline-fleet-harness-adapter-migration.md`;
7. `docs/coding-standards.md`;
8. old DeepSeek baseline only when a Milestone explicitly names it:
   - `docs/superpowers/plans/2026-08-14-pactline-deepseek-fleet-l1-runbook.md`;
   - `docs/superpowers/plans/2026-08-15-pactline-deepseek-fleet-l2-corpus.md`.

The old DeepSeek documents describe historical implementation and evidence;
they do not override the new Harness-neutral architecture.

## Milestone map

```text
M0 Architecture freeze and standalone scaffold
  -> M1 Harness-neutral Core with Fake Adapter
  -> M2 DeepSeek Harness Adapter parity
  -> M3 Codex Adapter and read-only live gate
  -> M4 Codex L2 v2 and comparative qualification
  -> M5.1 Resident service foundation
  -> M5.2 Durable multi-Fleet scheduler and recovery
  -> M5.3 Local Web UI and observability
  -> M5.4 Bounded usability acceptance
  -> M6 Provider, sandbox, and operations hardening
  -> M7 Final cutover and residual cleanup
  -> M8 Controlled real-work pilot and Game Design decision input
```

Milestones are sequential release gates. Work inside a Milestone may be
parallelized only when it does not create concurrent writes to the shared
PostgreSQL test database or overlapping files.

## M0 — Architecture freeze and standalone scaffold

**Status:** complete

### Objective

Create the independent application boundary and freeze reproducible evidence
for the old implementation before porting behavior.

### Scope

- create `fleet/` with independent private package metadata;
- create the `pactline-fleet` CLI shell with finite `doctor` and `version`
  commands only;
- add initial `core`, `adapters`, `pactline`, `repository`, `recovery`,
  `evaluation`, and `commands` modules;
- define internal Harness Adapter types and a Fake Adapter;
- create an architecture dependency test that rejects Harness imports from
  Core;
- add new root Make targets with `pactline-fleet-*` names;
- preserve all old `fleet-local-*` targets and old source unchanged;
- record old source tree/hash, package lock, test count, and preflight evidence;
- distill accepted architecture into a tracked document under `docs/` before
  later code depends on local `docs/superpowers` files.

### Planned primary files

- `fleet/package.json`
- `fleet/package-lock.json`
- `fleet/tsconfig.json`
- `fleet/tsconfig.test.json`
- `fleet/vitest.config.ts`
- `fleet/src/core/harness-adapter.ts`
- `fleet/src/core/harness-result.ts`
- `fleet/src/commands/doctor.ts`
- `fleet/src/commands/version.ts`
- `fleet/tests/core/architecture.test.ts`
- `fleet/tests/contract/fake-adapter.ts`
- `Makefile`
- a new tracked Harness-neutral document under `docs/`

### Exit criteria

- `fleet/` builds and tests without Codex or DSH installed;
- Core has no import, peer dependency, or type reference to either Harness;
- Fake Adapter can return one bounded structured terminal result;
- old Bundle verification still passes unchanged;
- new and old commands are independently addressable;
- no Pactline Task, Claim, Project, repository, or database state is mutated;
- no credentials or private evidence enter tracked files.

### Evidence

- build/typecheck/test output;
- architecture dependency-test output;
- old baseline manifest and checksum summary without credentials;
- `git diff --check` and exact status review.

## M1 — Harness-neutral Core with Fake Adapter

**Status:** complete

### Objective

Reproduce the complete Pactline workflow using no real Harness, proving that
all workflow, workspace, verification, delivery, and settlement behavior belongs
to Fleet Core.

### Scope

- port the typed Pactline CLI client;
- implement capability admission before Claim creation;
- port direct execution/review settlement;
- port candidate import and review-first flow;
- port changes-request/correction flow;
- port typed-resolution and post-resolution waived-criterion rules;
- port disposable repository workspace, fixed verification, Git audit, and
  delivery boundaries;
- implement deterministic static Runtime Router;
- normalize operational events and result schemas;
- reproduce all lifecycle paths with Fake/Replay Adapters;
- preserve provider-neutral GitHub/GitLab delivery types.

### Planned primary files

- `fleet/src/core/claim-stage.ts`
- `fleet/src/core/runtime-router.ts`
- `fleet/src/core/prompt-policy.ts`
- `fleet/src/core/verification.ts`
- `fleet/src/pactline/client.ts`
- `fleet/src/pactline/settlement.ts`
- `fleet/src/repository/workspace.ts`
- `fleet/src/repository/delivery.ts`
- `fleet/src/evaluation/corpus.ts`
- matching `fleet/tests/` suites

### Contract tests

- stale Task version rejects before Claim;
- missing Adapter capability rejects before Claim;
- exact Claim identity is preserved;
- invalid or incomplete Agent proposal cannot settle;
- reported diff and verification must match coordinator observation;
- review workspace mutation blocks settlement;
- request changes creates a later new execution/review path;
- request resolution requires unchanged workspace and HEAD;
- waived criteria are accepted only after explicit typed resolution;
- an uncertain Pactline response is reconciled before mutation retry;
- a provider error never selects a different Adapter automatically.

### Exit criteria

- Fake Adapter drives direct, review-first, correction, and resolution flows;
- all Pactline lifecycle mutations are provably outside Adapter code;
- Core public and persistent types contain no Harness-specific concept;
- local Docker integration tests pass serially;
- current CLI protocol and auth behavior remain unchanged.

### Evidence

- `make pactline-fleet-check` passed after a clean `npm ci`: 12 test files and
  49 tests, typecheck, build, baseline verification, and zero npm audit
  vulnerabilities;
- direct execution/review, candidate import/review-first,
  changes-request/correction/new-review, and typed-resolution/post-resolution
  waiver paths passed through the Replay Adapter and in-memory Pactline
  authority;
- all 11 contract cases are represented in `fleet/tests/core`,
  `fleet/tests/pactline`, and `fleet/tests/repository`;
- the Adapter architecture test rejects Pactline lifecycle, repository
  delivery, credential, vendor SDK, and reverse Core dependency leakage;
- `make pactline-fleet-local-integration` passed serially against the healthy
  Docker stack at `http://localhost:5173`, using protocol 2 and 16 required
  features; its temporary Token was revoked and no work resource was mutated;
- `go test ./internal/cli -count=1` passed, confirming the existing CLI auth
  behavior remains intact;
- package dry-run contained 49 files, doctor passed against `bin/pactline`, and
  the old Bundle remained 56 files at aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`;
- no Codex, DeepSeek, Cordis, GitHub credential, or GitLab credential was loaded
  or used.

### M1 boundary retained for M2

- Replay is evaluation infrastructure, not an automatic fallback;
- the local Docker integration is intentionally read-only for work resources;
  full lifecycle semantics are deterministic Core tests, while real Harness
  work starts only after the corresponding live gate;
- durable Run recovery, continuous scheduling, provider writes, and real
  Harness Session resume remain assigned to later Milestones.

## M2 — DeepSeek Harness Adapter parity

**Status:** complete

### Objective

Prove the Adapter boundary against the already-known DSH implementation before
adding a second Harness.

### Scope

- implement `DeepSeekHarnessAdapter`;
- isolate all Cordis and `@deepseek-ai/*` dependencies under the Adapter;
- create an Adapter-owned DSH plugin shim for prompt/result/event integration;
- port profile installation, model selection, finite shutdown, timeout,
  cancellation, and Session evidence;
- normalize DSH events and token usage;
- run a new finite L0/L1 parity gate;
- create a fresh DeepSeek parity cohort for the six L2 cases only if lifecycle
  parity cannot be established without real Tasks.

### Exit criteria

- selecting DeepSeek loads only the DeepSeek Adapter dependencies;
- selecting Fake or Codex does not require DSH installation;
- DeepSeek read-only and native coding Runs pass the shared Adapter contract;
- DeepSeek direct, changes-request, clean-review, and typed-resolution semantics
  match the frozen baseline;
- no old source import exists;
- old Bundle remains available for comparison.

### Stop conditions

- a DSH requirement forces Cordis types into Core;
- parity requires weakening the common security or result contract;
- old and new lifecycle evidence disagree without an explained root cause.

### Completion evidence

- The root package depends only on `yaml`; DSH and Cordis packages are pinned
  under `fleet/runtime/deepseek` and are not needed for Core/Fake builds or
  tests.
- `deepseek-doctor` completed a real keyless initialize/shutdown handshake and
  reported native tools, structured results, events, cancellation, both
  sandbox modes, all four stages, and no Session resume.
- Real DSH replay passed workspace-write execution, read-only review, direct
  completion, clean acceptance, changes request, typed resolution,
  cancellation, deadline, and actual process reap.
- Local Pactline integration again passed protocol 2 with 16 features and
  mutated no work resource.
- Live L1 Run `m2-l1-0be0b84e-08a1-4bda-8a63-2fb34ea4e7fa` used Pro/max,
  recalled all three seeded issues, produced one additional finding, passed
  the fixed four-test suite, emitted 49 normalized events, and left revision
  `381c9100ee153781d2178419a2d3a525d99d0b0f` and its tree unchanged.
- Live evidence is retained locally under
  `fleet/.fleet/deepseek-l1-results/m2-l1-0be0b84e-08a1-4bda-8a63-2fb34ea4e7fa`
  with directory mode 0700, file mode 0600, bounded content, and no detected
  credential shape.
- A fresh Pactline six-Task cohort was not needed: deterministic Core lifecycle
  tests plus real DSH replay established direct, changes-request,
  clean-review, and typed-resolution parity without Task or remote mutation.
- The old Bundle remains 56 files at aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`.

## M3 — Codex Adapter and read-only live gate

**Status:** complete (2026-08-15)

### Objective

Integrate the full Codex Agent as the default Harness while proving its
programmatic, sandbox, structured-result, lifecycle, and credential boundaries
before writable Pactline work.

### Scope

- perform a finite Codex SDK/CLI contract spike;
- implement `CodexHarnessAdapter` probe, run, resume, cancel, event mapping,
  usage, and cleanup;
- use a new Codex Thread per Claim;
- implement workspace-write execution and read-only review policies;
- use a common Fleet result schema through supported structured output or a
  Fleet-owned local MCP result tool;
- freeze exact Codex model and reasoning policy per Run;
- isolate Pactline and repository-provider credentials from Codex;
- add malicious repository, environment inspection, timeout, cancellation,
  invalid result, and output-bound tests;
- run one live read-only Codex capability task with no Pactline settlement.

### Owner input supplied for live gate

- existing local ChatGPT authentication was approved for the bounded gate;
- `gpt-5.6-sol` with reasoning `high` was selected; Max is unnecessary;
- the owner explicitly deferred hard isolation of Codex's own auth cache.

### Exit criteria

- the official programmatic surface and exact version are recorded;
- one finite read-only Codex Run returns a valid common result;
- Thread identity, model, usage, and bounded events are retained;
- timeout and cancellation terminate the full process tree;
- Pactline and repository-provider credentials are absent from the Codex
  process environment;
- Codex receives no Pactline or provider-write authority;
- no Task lifecycle or remote repository mutation occurs.

### Active M3 implementation decision

- Use current `gpt-5.6-sol` at reasoning `high`; do not use Max. Medium is a
  later measured optimization, never an automatic fallback.
- Pin `@openai/codex` 0.147.0 in an Adapter-owned runtime closure.
- Force `--ignore-user-config`, `--ignore-rules`, approval `never`, no login
  shell, no multi-agent, no web/network, and an empty model-tool environment
  plus explicit non-secret PATH/locale.
- Accept only the private final output-schema document, not intermediate agent
  messages.
- Preserve Thread resume capability while owning timeout/cancel process-tree
  termination.
- Use `docs/superpowers/plans/2026-08-15-pactline-fleet-m3-codex-adapter.md`
  as the concrete implementation and verification plan.

### Completion evidence

- `CodexHarnessAdapter` implements probe, run, resume, cancel, structured
  output, bounded JSONL events, usage, sandbox selection, and process cleanup.
- Adapter-owned `@openai/codex` 0.147.0 passed `codex-doctor`.
- The real shared L1 read-only gate used `gpt-5.6-sol/high`, recalled all three
  seeded defects, reported zero false positives, passed fixed verification,
  and preserved the exact Git tree.
- The final accepted Run recorded Session
  `01a00460-0f63-7053-ab08-0facbf87cbee`, 18 normalized events, and bounded
  private evidence under `.fleet/codex-l1-results/`.
- No Pactline Task, branch, push, PR, or Game Design mutation was performed.

## M4 — Codex L2 v2 and comparative qualification

**Status:** complete

### Objective

Measure Codex's real coding and review quality through the complete Pactline
workflow, then compare Adapter combinations without changing the workflow.

### Scope

- create `Pactline Fleet L2 v2 Evaluation` or an equivalently isolated new
  Project/cohort;
- pin a new exact repository base and case branch namespace;
- create new Tasks and criterion revisions derived from the six proven cases;
- run the complete Codex/Codex corpus first;
- run bounded DeepSeek/Codex and Codex/DeepSeek comparison cohorts;
- retain DeepSeek/DeepSeek baseline without rewriting its existing Tasks;
- collect correctness, evidence accuracy, false acceptance, false blocking,
  correction success, latency, tokens, tool errors, recovery, and human
  intervention metrics.

### Required behavior gates

- direct execution -> independent review -> accept;
- defective candidate -> request changes -> correction -> new review -> accept;
- clean candidate -> accept with no fabricated blocking finding;
- conflicting criteria -> resolution before mutation -> new Claim -> accept;
- exact repository URL, base, branch, PR/MR, and revision evidence;
- no Repository Connection required for behavior;
- no merge and no mutation of `main`.

### Exit criteria

- all six Codex/Codex cases reach the expected authoritative Pactline outcome;
- hidden and fixed verification passes at final frozen revisions;
- every finding references valid evidence;
- the clean control is not falsely blocked;
- the defective control is not falsely accepted;
- typed resolution preserves the no-mutation-before-resolution invariant;
- all credentials, temporary workspaces, and process groups have a verified
  cleanup disposition;
- an owner-readable Codex versus DeepSeek capability report is produced.

### Decision output

The report recommends one of:

- Codex default for all stages;
- Codex execution and another explicit review policy;
- more qualification required;
- stop before real-work promotion.

Cost optimization is not considered until the strongest configuration baseline
is frozen.

### Completion evidence

- Codex/Codex Tasks #14-#19: 6/6 done.
- DeepSeek/Codex Task #20 and Codex/DeepSeek Task #21: 2/2 done.
- Defective and clean controls produced zero false acceptance and zero false
  blocking.
- Typed resolution completed before repository mutation.
- Decision report:
  `docs/superpowers/plans/2026-08-15-pactline-fleet-m4-report.md`.

## M5 — Resident Fleet Service

**Status:** complete

### Objective

Turn finite qualification runners into a recoverable resident Fleet Service
that manages multiple Project-bound Fleets and exposes a local read-only Web
UI without changing Pactline's workflow authority.

### Scope

- implement one logical Fleet per Pactline Project;
- permit independent Fleet Services to compete for the same Project through
  Pactline Claim semantics;
- implement declarative YAML configuration with validated atomic reload;
- implement a private SQLite Run Registry;
- implement protected idempotency and exact external-effect checkpoints;
- implement a local single-process lock without cross-service leader election;
- implement fair multi-Fleet polling with bounded global and per-Fleet
  concurrency;
- implement graceful shutdown and restart;
- implement exact Adapter Session resume when supported;
- release or quarantine unsupported/ambiguous recovery states;
- refuse implicit adoption of unfamiliar same-principal Claims;
- implement a loopback-only read-only observation API and Web UI;
- expose scoped service, Fleet, Adapter, and Run health;
- retain finite `once` commands for diagnosis;
- retain injectable durable and external-effect checkpoints for deferred
  reliability qualification.

### Delivery gates

The detailed plan is
`docs/pactline-fleet-service-milestones.md`:

- M5.0 design freeze;
- M5.1 resident service foundation;
- M5.2 durable multi-Fleet scheduler and recovery;
- M5.3 local Web UI and observability;
- M5.4 bounded usability acceptance.

### Deferred exhaustive crash qualification

1. before Claim creation;
2. after Claim creation and before Run persistence;
3. after Run persistence and before Harness Session creation;
4. after Session creation and before first tool effect;
5. after a workspace effect and before result capture;
6. after structured result and before commit/push;
7. after push and before PR/MR creation;
8. after PR/MR creation and before Pactline link;
9. after Pactline link and before execution completion;
10. after settlement and before local terminal record;
11. before and after typed Issue resolution;
12. before new post-resolution Claim dispatch.

These checkpoints remain implemented and individually testable, but M5.4 uses
one representative post-Session termination to decide bounded usability. The
full matrix is not a prerequisite for local trials.

### Exit criteria

- deterministic and representative live Tasks reach their expected Pactline
  state without manual repair;
- no duplicate Claim mutation, delivery, submission, check, or Thread Item
  occurs in the bounded scenarios;
- no recovery changes Harness automatically;
- graceful stop leaves no unfinished local write;
- one representative injected restart converges safely without database
  repair;
- one local URL exposes accurate read-only service, Fleet, Adapter, and Run
  health without a separate UI process.

## M6 — Provider, sandbox, and operations hardening

**Status:** pending

### Objective

Close the remaining security and operational gaps before any real-work pilot.

### Scope

- complete GitLab MR delivery and reconciliation;
- formalize GitHub/GitLab provider contract tests;
- run Harnesses in an isolated execution boundary with explicit filesystem,
  network, CPU, memory, subprocess, and timeout policy;
- introduce a credential proxy when required to keep Harness master credentials
  out of repository commands;
- implement bounded evidence retention and exact cleanup commands;
- extend failure categories, external metrics collection, dashboards, alerts,
  and operator diagnostics beyond the M5 local health surface;
- document installation, Adapter configuration, authentication, recovery,
  cleanup, and incident handling;
- review public-repository and secret-scanning gates.

### Exit criteria

- Codex and DeepSeek writable Runs meet the same isolation contract;
- GitHub and GitLab delivery paths pass idempotent failure/recovery tests;
- no Harness subprocess receives Pactline or provider-write credentials;
- master Harness credentials are inaccessible to repository code;
- logs alone identify every outer state transition without raw reasoning;
- retention and cleanup act only on exact recorded artifacts;
- operational documentation can be followed from a fresh local environment.

## M7 — Final cutover and residual cleanup

**Status:** reference source cleanup complete; final cutover pending M6

### Objective

Complete the post-hardening cutover with standalone Pactline Fleet as the only
active implementation. The former DeepSeek-specific reference source and its
obsolete commands were retired early with owner approval after M5.4.

### Scope

- confirm root commands and current docs use `fleet/` only;
- scan all active code and docs for retired package/path dependencies;
- rerun full new verification and fresh local preflight;
- publish the final post-M6 cutover report.

### Destructive boundary

The owner explicitly approved retiring the untracked reference Bundle after
M5.4. It was moved to the operating-system Trash rather than irreversibly
deleted. Any later cleanup must still resolve exact targets and preserve active
standalone Fleet state and private acceptance evidence.

### Exit criteria

- `@pactline/fleet` and `pactline-fleet` are the only active names;
- no runtime or test imports the old package;
- no compatibility alias remains;
- new Core, both Adapters, recovery, provider, and live preflight gates pass;
- retained private standalone evidence remains accessible at its documented
  restricted path;
- final cutover and recovery approach are reported explicitly.

## M8 — Controlled real-work pilot and Game Design decision input

**Status:** pending M7 and separate owner approval

### Objective

Validate the production-shaped Fleet on a small set of reversible, low-risk
real Tasks before making any Game Design migration decision.

### Scope

- owner selects an allowlisted non-critical Project and Tasks;
- concurrency remains one;
- Codex is the default executor and independent reviewer;
- every delivery remains a Draft PR/MR and requires owner review;
- no automatic merge;
- collect task success, review quality, recovery, cost, latency, and human
  intervention evidence;
- produce a separate Game Design go/no-go recommendation.

### Exit criteria

- every admitted Task has a correct and auditable terminal disposition;
- no unauthorized repository or Pactline mutation occurs;
- recovery and operator procedures are exercised at least once;
- the owner receives a capability, risk, cost, and operational report;
- Game Design remains unchanged until a new explicit decision.

## Cross-Milestone invariants

- Pactline is the only workflow authority.
- Every mutation uses an explicit Claim ID, current version, and coordinator
  idempotency material.
- Every Agent Run uses a disposable workspace.
- Review uses an independent Session and immutable workspace.
- Models do not receive lifecycle or remote-write authority.
- Verification claims are checked against coordinator observation.
- Adapter-specific types do not cross into Fleet Core or persisted common
  records.
- Failures remain attributable to the selected Adapter.
- No secret enters argv, tracked files, ordinary logs, prompts, or retained
  common evidence.
- No evaluation PR/MR is merged automatically.
- No destructive cleanup targets unresolved paths or broad prefixes.

## Working procedure for every Milestone

1. Read this Runbook and the relevant architecture/migration section.
2. Inspect `git status --short --branch` and active processes.
3. Re-read every file immediately before patching it.
4. State the Milestone contract and affected files before editing.
5. Add focused failing tests or a concrete reproduction for behavior changes.
6. Implement the smallest coherent Milestone slice.
7. Add structured, secret-safe diagnostics at external boundaries.
8. Run focused tests first, then shared and live gates required by the
   Milestone.
9. Review diff and status without touching unrelated work.
10. Append verified facts, failures, decisions, artifact locations, and the
    next exact action to `## Progress log`.
11. Stop at the Milestone gate for owner alignment when the Runbook requires
    new credentials, destructive cleanup, real work, or a new breaking change.

## Recovery procedure after context compaction

1. Read this complete file.
2. Inspect the latest `## Milestone status` and `## Progress log` entries.
3. Call `get_goal`; continue an existing active Goal rather than creating a
   duplicate.
4. Run `git status --short --branch` and preserve unrelated changes.
5. Confirm no active code or command has reintroduced a dependency on the
   retired `bundles/deepseek-fleet` path.
6. Inspect only the files and evidence named by the current Milestone.
7. Check Docker, Fleet, Harness, and test processes before starting new ones.
8. Run the smallest checkpoint verification that proves current state.
9. Continue from the first incomplete acceptance criterion; do not restart the
   initiative or rerun completed live cohorts without a new Task set.
10. Update this file before ending a long interval.

## Milestone status

| Milestone | Status | Next gate |
|---|---|---|
| M0 Architecture/scaffold | complete | all exit criteria passed |
| M1 Harness-neutral Core | complete | all exit criteria passed |
| M2 DeepSeek Adapter parity | complete | all exit criteria passed |
| M3 Codex Adapter | complete | shared L1 read-only gate passed |
| M4 Codex L2 v2 | complete | capability report accepted |
| M5 Resident Service | complete | bounded local trials accepted |
| M6 Provider/security/ops | pending | M5 usability report |
| M7 Final cutover | source cleanup complete; final gate pending | M6 hardening report |
| M8 Real-work pilot | pending separate approval | M7 cutover report |

At most one Milestone is active. A Milestone may contain several coherent
slices, but its exit criteria are evaluated together before advancing.

## Owner inputs by Milestone

- M0-M2: no new credential or product decision expected.
- M3: supplied and consumed; no further owner input required.
- M4: permission to create the isolated v2 Pactline Project/Task cohort and
  `fleet-eval` Draft PR branches in the existing repository.
- M5: no new external credential expected.
- M6: GitLab test repository/authentication only when the live GitLab gate is
  reached; no production credential is required for earlier fixture tests.
- M7: review of the exact cleanup inventory before deletion.
- M8: explicit real Project/Task allowlist and separate pilot approval.

Do not request later-Milestone credentials early.

## Stop conditions

Stop and request owner direction when:

- a new breaking change outside the approved rename/cutover is required;
- Core cannot remain free of Harness-specific types or dependencies;
- a Harness requires exposing control-plane or repository-write credentials;
- Pactline or repository-provider credentials would need to enter a Harness;
- lifecycle or repository evidence disagrees with the deterministic
  coordinator;
- a live run would touch `main`, a production Project, or an unapproved
  repository;
- cleanup targets or evidence-retention disposition are ambiguous;
- a Milestone requires real work or Game Design changes without separate
  approval.

## Progress log

- 2026-08-15: M0 completed. The standalone `fleet/` application now builds as
  private package `@pactline/fleet` and exposes finite `pactline-fleet version`
  and non-authenticated protocol doctor commands. The doctor verified local
  Pactline CLI `0.1.0-dev`, protocol 2, and 16 features without starting a
  Harness or mutating Pactline. New Make targets
  `pactline-fleet-install`, `pactline-fleet-check`, and
  `pactline-fleet-doctor` are independent from the retained old commands.

- 2026-08-15: M0 added the internal runtime-neutral `HarnessAdapter` contract,
  early runtime Session identity observation, common `AbortSignal`, common
  result/provenance/event types, a deterministic Fake Adapter, and architecture
  tests that reject Codex, DSH, Cordis, or implementation-Adapter imports from
  Core. The new package has no runtime, peer, Codex, or DeepSeek dependency.
  Three test files and 14 tests pass with strict typecheck and build; production
  dependency audit reports zero vulnerabilities.

- 2026-08-15: The old DeepSeek Bundle baseline was frozen in
  `fleet/evaluation/baselines/deepseek-fleet-m0.json`. The checked-in verifier
  observed 56 source files and aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`
  before and after M0. The old Bundle again passed strict typecheck, build, 15
  test files, 70 tests, and its live L2 preflight with 6 Tasks and 9 remote
  refs. The old source and commands were not modified by M0.

- 2026-08-15: Stable Harness-neutral decisions were distilled into tracked
  `docs/pactline-fleet-architecture.md`. Secret-shape and trailing-whitespace
  scans found no result in the new source/documentation, external doctor
  diagnostics redact credential-shaped values, and `git diff --check` passed.
  M1 remains unstarted pending owner direction.

- 2026-08-15: The owner confirmed that Pactline Fleet must be a standalone
  application rather than a DSH Bundle. DeepSeek Harness is one Adapter only.
  The planned new path changed from `bundles/pactline-fleet` to repository-root
  `fleet/`; the package remains `@pactline/fleet` and executable
  `pactline-fleet`. Harness dependencies are confined to their Adapters.

- 2026-08-15: The owner approved parallel construction beside the existing
  DeepSeek Bundle and eventual cleanup after complete acceptance. The old
  source stays frozen as reference until M7. The Adapter contract is
  internal-first and becomes a potential public third-party API only after
  Codex and DeepSeek prove it.

- 2026-08-15: The Harness-neutral architecture specification, migration plan,
  and this complete Milestone Runbook were created. No new Fleet source code
  has been implemented. M0 remains blocked only on owner approval of this
  Milestone decomposition.

- 2026-08-15: M1 completed the typed Pactline CLI boundary, capability-gated
  static Runtime Router, common prompt/result/event contracts, Claim-stage
  coordinator, deterministic settlement and uncertain-response
  reconciliation, disposable workspaces, fixed verification and Git audit,
  provider-neutral delivery types, and private evaluation corpus parser. No
  real Harness dependency or automatic fallback was added.

- 2026-08-15: Replay drove direct execution/review, candidate
  import/review-first, changes-request/correction/new-review, and typed
  resolution/post-resolution waiver paths. Explicit Issue resolution now mints
  the only in-memory Core authority accepted for later waived criteria. The 11
  named M1 contract cases are covered across 12 test files and 49 passing
  tests.

- 2026-08-15: Final M1 verification passed `make pactline-fleet-check`,
  `go test ./internal/cli -count=1`, package dry-run, doctor, secret-shape scan,
  and `git diff --check`. Serial local Docker integration authenticated through
  protocol 2 with 16 required features and performed bounded execution/review
  discovery; it revoked its temporary Token and mutated no work resource. The
  old Bundle remained 56 files at aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`.
  M2 was awaiting explicit owner direction.

- 2026-08-15: M2 completed `DeepSeekHarnessAdapter` under
  `fleet/src/adapters/deepseek` with an Adapter-owned DSH JSON-RPC runtime,
  Cordis composition, terminal result plugin, private credential resolver,
  deterministic Pro/max policy, event and usage translation, and process-level
  cancellation. DeepSeek is an optional sibling Adapter; Core has no DSH or
  Cordis dependency and no old-Bundle import.

- 2026-08-15: Installed-runtime tests proved native read-only and
  workspace-write tools, structured direct/clean-review/changes-request/typed-
  resolution results, deadlines, cancellation, and process reap. Core
  typecheck, tests, and build also passed with the DSH installation temporarily
  absent. `deepseek-doctor` passed a real keyless initialize/shutdown handshake.

- 2026-08-15: The final finite live L1 Run
  `m2-l1-0be0b84e-08a1-4bda-8a63-2fb34ea4e7fa` passed at
  `deepseek-v4-pro`/`max`: issue recall 3/3, one additional finding, fixed tests
  4/4, 49 normalized events, clean Git revision/tree, and private sanitized
  evidence. Investigation of earlier rejected attempts corrected two boundary
  defects: coordinator verification no longer starts a login shell, and the
  fixture now distinguishes DSH's documented managed shell facts from actual
  credentials. No fresh Pactline Tasks, Claims, branches, pushes, or PRs were
  created. This was the M2 completion boundary before M3 began.

- 2026-08-15: M3 completed `CodexHarnessAdapter` with the pinned Codex Agent
  CLI 0.147.0, fixed `gpt-5.6-sol/high` policy, structured output, bounded
  JSONL translation, resume, cancellation, and read-only/workspace-write
  sandbox selection. The real shared L1 review recalled all three seeded
  defects with zero false positives, passed fixed verification, preserved the
  Git tree, emitted 18 normalized events, and left no residual Run process.
  No Pactline Task or remote repository mutation occurred; M4 remains pending
  owner direction.

- 2026-08-15: M4 completed the six-case Codex/Codex L2 v2 cohort and bounded
  mixed-Adapter comparisons. Tasks #14 through #21 reached their expected
  authoritative outcomes with zero false acceptance and zero false blocking.
  The accepted capability report retained Codex as the quality-first default.

- 2026-08-15: M5.1 completed the resident, non-scheduling Fleet Service
  foundation. It adds strict YAML configuration, one local Fleet per Project,
  atomic reload, private SQLite registry, stable Service/Fleet/Run identities,
  local single-process locking, periodic dependency health, loopback-only
  read-only HTTP, structured redacted logs, and graceful signal shutdown.
  `make pactline-fleet-check` passed 32 test files and 120 tests; the real
  process smoke rejected a second owner, stopped cleanly, and retained no
  Pactline credential. Local authenticated Pactline integration and real
  keyless Codex doctor also passed.

- 2026-08-16: M5.2 completed durable multi-Fleet scheduling and recovery.
  Registry schema v3 freezes work and route policy plus admission and
  post-Claim Task versions. Fair scheduling, concurrency replenishment,
  contention/backoff, finite once, draining, unfamiliar Claim quarantine,
  exact Codex-style resume, DeepSeek-style release, and terminal settlement
  reconciliation are covered. The work-plugin protocol isolates project
  policy and checkpoints commit, push, and PR/MR creation separately. The
  bounded real DeepSeek Pro/max service demo completed in an isolated local
  repository with a fake Pactline authority; real local Pactline protocol 2
  discovery passed across two Projects with a revoked temporary Token.

- 2026-08-16: M5.3 completed the loopback-only read-only Operations Console,
  versioned observation API, SSE with polling fallback, bounded Run timelines
  and external-effect projections, Adapter and scheduler discovery health,
  Prometheus metrics, static delivery, and the `ui --open` command. The full
  Fleet gate passed 144 service tests, 5 component tests, 5 real Chromium
  flows, type checking, production build, M5.1 process smoke, and packed UI
  smoke. npm audit reported zero vulnerabilities. M5.4 bounded usability
  acceptance is next.

- 2026-08-17: M5.4 accepted bounded local trials. Deterministic Replay Tasks
  completed direct delivery, four-Claim correction, and a post-Session restart
  with safe release and one replacement delivery. Live DeepSeek Pro/max plus
  Codex high and Codex/Codex paths both reached `done`. Browser inspection
  confirmed accurate Overview, Fleet, Run, and System state with no console
  errors. The vertical slices exposed and fixed empty Claim-list encoding,
  rate-limit retry propagation, and two Codex structured-output constraints.
  Exhaustive crashes, distributed competition, storage faults, soak, live
  provider failure, and production sandbox qualification remain M6 work.

- 2026-08-17: With owner approval, the untracked DeepSeek-specific reference
  Bundle was retired after M5.4. Obsolete root Make targets, the standalone
  Fleet hash verifier, its baseline manifest, and active documentation claims
  were removed. DeepSeek support remains in the standalone
  `DeepSeekHarnessAdapter` and `fleet/runtime/deepseek/`; no active build or
  test depends on the retired source.
