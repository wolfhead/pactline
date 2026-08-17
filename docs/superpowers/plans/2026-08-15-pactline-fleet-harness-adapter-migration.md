# Pactline Fleet Harness-Adapter Migration Plan

**Date:** 2026-08-15

**Status:** M0 through M5 complete; former reference Bundle retired after M5.4

References below to a frozen or future-removed old Bundle record the original
milestone sequence. The current decision is that the untracked reference source
and its active verification commands were retired after M5.4; they are not a
remaining gate.

**Supersedes for new development:** the DeepSeek-specific architecture in
`docs/superpowers/specs/2026-08-14-pactline-deepseek-fleet-bundle.md`

**Preserves as evaluation evidence:** the completed DeepSeek L0, L1, and L2
baseline and its retained artifacts

## Objective

Build `Pactline Fleet` as a Harness-neutral, deterministic orchestration layer
above multiple Agent Harnesses. Pactline remains authoritative for Task,
Claim, Thread, Issue, acceptance, and repository-delivery state. Each Harness
is integrated through an Adapter that owns only its Agent Session lifecycle,
tool/prompt composition, event capture, and result translation.

The first supported Adapters are:

- `CodexHarnessAdapter`, the default execution and review runtime;
- `DeepSeekHarnessAdapter`, a supported evaluation and optional routing
  runtime backed by DeepSeek Harness.

The migration created the standalone `fleet` application subtree. The former
DeepSeek-specific Bundle was never a runtime dependency and was retired with
owner approval after the standalone Fleet passed M5.4 bounded usability.

## Accepted decisions

- Pactline Fleet is Harness-neutral.
- DeepSeek Harness is one compatible Harness, not the Fleet host architecture.
- Codex is the default Adapter for execution, review, correction, and
  resolution analysis during the capability-first phase.
- Fleet owns the outer workflow; each Harness owns its inner Agent Loop.
- Fleet invokes Codex and DeepSeek as siblings. DeepSeek does not invoke Codex.
- Pactline workflow and repository delivery remain deterministic coordinator
  responsibilities.
- The model receives no Pactline lifecycle authority or repository-provider
  write credential.
- There is no automatic cross-Adapter fallback. A provider failure remains
  attributable to the selected Adapter and is retried or recovered explicitly.
- The standalone implementation was built without importing the old Bundle;
  the reference source and its obsolete commands were retired after M5.4.
- The package and directory are renamed without a compatibility alias after
  cutover:
  - `bundles/deepseek-fleet` -> standalone `fleet` application;
  - `@pactline/dsh-fleet` -> `@pactline/fleet`.
- The first Adapter contract remains internal during Codex and DeepSeek
  qualification. A versioned third-party Adapter API is considered only after
  both implementations prove the contract.
- Persistent recovery begins with private atomic JSON records. SQLite is not a
  prerequisite and may be introduced only when concurrency or query needs
  justify it.

## Non-goals

- Implementing a new Agent Loop inside Pactline Fleet.
- Wrapping Codex inside a DeepSeek Harness plugin.
- Adding Harness or model selection fields to Pactline's Task domain.
- Allowing an Adapter to claim, complete, accept, release, or resolve Pactline
  work.
- Allowing an Adapter to push, create a PR/MR, or merge repository changes.
- Silent fallback from Codex to DeepSeek or from one model to another.
- Multi-host distributed scheduling in the first cutover.
- Moving Game Design work before the new Codex qualification corpus passes.
- Deleting the completed DeepSeek evaluation evidence during source cleanup.

## Architecture

```text
Pactline CLI protocol
        |
        v
Pactline Fleet Core
  +-- work discovery and admission
  +-- Claim-stage state machine
  +-- runtime policy and Adapter routing
  +-- disposable workspace provider
  +-- deterministic verification and Git audit
  +-- branch / push / PR-MR delivery
  +-- Pactline settlement and reconciliation
  +-- Run registry and bounded evidence
        |
        v
Harness Adapter contract
  +-- CodexHarnessAdapter ----> Codex Thread / Agent Loop
  +-- DeepSeekHarnessAdapter -> DSH Session / Agent Loop
  +-- future Adapter ---------> external Harness Agent Loop
```

### Ownership boundary

| Concern | Fleet Core | Harness Adapter | Pactline |
|---|---:|---:|---:|
| Discover eligible work | yes | no | source of truth |
| Create and continue Claims | yes | no | source of truth |
| Select Adapter and policy | yes | reports capabilities | no |
| Create Agent Session/Thread | no | yes | no |
| Model/tool loop | no | yes | no |
| Modify disposable execution workspace | audits | yes | no |
| Verify fixed commands and Git state | yes | may report | no |
| Push and create PR/MR | yes | no | records link |
| Propose execution/review outcome | validates | yes | no |
| Apply lifecycle transition | yes | no | source of truth |
| Persist Task/Claim/Issue state | no | no | yes |
| Persist runtime Session reference | registry | supplies it | no |

An Adapter proposal never becomes a Pactline mutation until Fleet Core has
validated the structured result, workspace, verification, delivery, Claim,
Task version, review cycle, and criterion revisions.

## Harness Adapter contract

The contract is defined around one Pactline Claim stage, not around any
provider's model API.

```ts
export type HarnessStage =
  | 'execution'
  | 'review'
  | 'correction'
  | 'resolution_analysis'

export interface HarnessAdapter {
  readonly id: string
  readonly version: string

  probe(request: HarnessProbeRequest): Promise<HarnessCapabilities>

  run(
    request: HarnessRunRequest,
    observer: HarnessRunObserver,
    signal: AbortSignal,
  ): Promise<HarnessRunResult>

  resume?(
    runtimeSessionId: string,
    request: HarnessRunRequest,
    observer: HarnessRunObserver,
    signal: AbortSignal,
  ): Promise<HarnessRunResult>

  cancel?(runtimeSessionId: string, reason: string): Promise<void>
}
```

### Capabilities

```ts
export interface HarnessCapabilities {
  nativeTools: boolean
  structuredResult: boolean
  eventStream: boolean
  cancellation: boolean
  sessionResume: boolean
  sandboxModes: readonly ('read_only' | 'workspace_write')[]
  supportedStages: readonly HarnessStage[]
}
```

Fleet checks required capabilities before creating a Claim. It fails admission
rather than weakening a sandbox, result, cancellation, or stage requirement
after work has been claimed.

### Runtime-neutral request

```ts
export interface HarnessRunRequest {
  runId: string
  claimId: string
  stage: HarnessStage
  workspace: string
  repositoryRevision: string
  taskPacket: PactlineWorkPacket
  allowedPaths: readonly string[]
  verificationCommands: readonly string[]
  resultSchema: JsonSchema
  sandbox: 'read_only' | 'workspace_write'
  deadline: string
  policy: HarnessRunPolicy
}
```

The request contains no Pactline Token, API command surface, GitHub/GitLab
write credential, settlement callback, or mutable Fleet registry handle.

### Runtime-neutral result

```ts
export interface HarnessRunResult {
  adapterId: string
  adapterVersion: string
  runtimeSessionId: string
  model: ModelProvenance
  terminalState: 'completed' | 'failed' | 'cancelled' | 'timed_out'
  proposal: ExecutionProposal | ReviewProposal | ResolutionProposal
  usage: TokenUsage
  eventSummary: HarnessEventSummary
}
```

Fleet owns the common execution, review, and resolution schemas. Adapters may
register different native tool mechanisms, but they must return the same
validated semantic result.

The observer receives the runtime Session identity before the first Agent
effect so Fleet can persist an exact recovery mapping. The common
`AbortSignal` propagates timeout, graceful shutdown, and operator cancellation.

## Runtime policy and routing

Initial capability-first configuration:

```yaml
runtime:
  defaults:
    execution: codex
    review: codex
    correction: codex
    resolution_analysis: codex
  automaticFallback: false
```

The selected Adapter, Adapter version, model identifier, reasoning setting,
prompt version, result-contract version, and sandbox mode are frozen per Run
and retained as evidence. Global configuration changes do not alter an active
Run.

Runtime selection remains Fleet configuration. Pactline receives provenance
through existing Client Session and work-evidence surfaces where supported;
it does not acquire provider-specific Task fields.

The initial router is deterministic and configuration-driven. Task-based
routing rules are deferred until two Adapters pass the same corpus and there is
evidence for a useful policy. No model chooses the Adapter.

## Quality-first workflow

### Default path

```text
Codex execution Thread
  -> Fleet verification and Git audit
  -> coordinator-owned Draft PR
  -> independent Codex review Thread
  -> deterministic settlement
```

Execution and review never share a Thread. Review uses a credential-free,
detached workspace and a read-only sandbox. Correction and post-resolution
execution use new Threads and fresh Claims.

### High-risk path

After the default path is stable, selected domain, authorization, migration,
concurrency, and security changes may use two independent review Runs:

```text
correctness review ----+
                       +-> deterministic aggregation -> settlement
specialist review -----+
```

Only one Agent may write an execution workspace. Additional Agents are
read-only reviewers. DeepSeek may participate as advisory evidence, but it
does not independently block or accept a Task unless an explicit evaluation
policy assigns it that role.

## Proposed new subtree

```text
fleet/
  package.json
  package-lock.json
  tsconfig.json
  tsconfig.test.json
  vitest.config.ts
  README.md
  src/
    core/
      harness-adapter.ts
      harness-result.ts
      claim-stage.ts
      runtime-router.ts
      prompt-policy.ts
      verification.ts
    adapters/
      codex/
        codex-adapter.ts
        codex-events.ts
        codex-policy.ts
        codex-preflight.ts
      deepseek/
        deepseek-adapter.ts
        deepseek-events.ts
        deepseek-policy.ts
        deepseek-profile.ts
    pactline/
      client.ts
      settlement.ts
    repository/
      workspace.ts
      delivery.ts
    recovery/
      run-record.ts
      atomic-json-registry.ts
      reconciler.ts
    evaluation/
      corpus.ts
      runner.ts
    commands/
      preflight.ts
      once.ts
      daemon.ts
  tests/
    contract/
    core/
    adapters/
    pactline/
    repository/
    recovery/
    evaluation/
```

The first implementation remains one private npm package named
`@pactline/fleet`. Separate npm packages are deferred until an independently
released third-party Adapter exists. Internal module boundaries must still
prevent Fleet Core from importing Codex or DSH modules.

## Existing-code reuse map

The old Bundle is reference input, not a runtime dependency.

| Existing source | New ownership | Migration approach |
|---|---|---|
| `src/client.ts` | `src/pactline/client.ts` | port typed CLI behavior and tests |
| `src/l2-coordinator.ts` | `src/core/claim-stage.ts` | generalize existing `runAgent` seam |
| `src/l2-result.ts` | `src/core/harness-result.ts` | remove L2 and DSH naming |
| `src/l2-settlement.ts` | `src/pactline/settlement.ts` | preserve deterministic lifecycle rules |
| `src/l2-workspace.ts` | `src/repository/workspace.ts` | preserve clone and audit boundary |
| `src/l2-agent-once.ts` | Core plus DeepSeek Adapter | split prompt/result/audit from DSH APIs |
| `src/local-l2.ts` | evaluation runner and delivery | split orchestration from DSH setup |
| `src/startup.ts` | commands plus DeepSeek profile | remove DSH command ownership from Core |
| `cordis.patch.yml` | DeepSeek Adapter asset | never loaded by Codex path |
| L1/L2 fixtures | evaluation baseline | copy only non-secret reusable fixtures |

No new source imports from `bundles/deepseek-fleet`. Reused logic is ported
with attribution in the migration history and protected by behavioral tests.

## Codex Adapter design gate

The Codex Adapter uses the pinned official Codex Agent CLI as its programmatic
surface. The finite spike verified the exact installed CLI/runtime behavior
for:

- explicit working directory;
- `workspace_write` execution and `read_only` review;
- event streaming and bounded evidence;
- cancellation and deadline behavior;
- Thread ID capture and resume;
- model/reasoning configuration;
- structured final result or a Fleet-owned MCP result tool;
- credential isolation from repository commands.

The selected implementation uses the documented `codex exec --json` and
`--output-schema` path. This remains an Adapter-local choice and does not
change Fleet Core.

The current CLI read-only sandbox is not a filesystem read allowlist. The owner
explicitly accepted Codex authentication as a trusted runtime dependency for
the M3 read-only gate. A local credential proxy or equivalent process boundary
remains a possible later requirement before live writable promotion; Pactline
and repository-provider credentials remain strictly excluded now.

## DeepSeek Adapter design

The DeepSeek Adapter ports the proven DSH behavior:

- isolated DSH profile installation;
- exact provider/model/thinking/reasoning selection;
- one DSH Session per Claim;
- native DSH coding tools;
- Fleet terminal result tool registration;
- Session event normalization;
- timeout, cancellation, and bounded evidence;
- no Pactline or provider-write authority.

DSH-specific packages are installed in the separate Adapter-owned
`fleet/runtime/deepseek` closure and used only by this Adapter. The root Fleet
package has no DSH or Cordis dependency, and Core tests run without the runtime
installation.

M2 confirmed that DeepSeek Harness exposes a newline-delimited JSON-RPC server
with `initialize`, `session/prompt`, shutdown, and Session event/status
notifications. It does not expose prompt cancellation, Session close, or
resume. The Adapter therefore owns one DSH process per Run and provides strong
cancellation and timeout semantics by terminating and reaping the process. It
reports `sessionResume: false` rather than weakening the common contract.

The Cordis composition is Adapter-owned. It pins the published DSH
`0.1.0-rc.6` package family, fixes `deepseek-v4-pro` with reasoning `max`,
provides native sandboxed Bash/filesystem tools, and registers
`submit_fleet_result` as the only accepted terminal result. Managed tool
variables such as `DSH_HOME` and `DSH_SESSION_ID` are current-Session facts,
not credentials; stale ambient `DSH_*` variables and credential-shaped values
are removed before runtime launch.

### M2 completion evidence

- real keyless DSH boot/shutdown passed through `deepseek-doctor` without a
  model call;
- installed-runtime tests passed native read-only and workspace-write Runs,
  direct completion, clean acceptance, changes request, typed resolution,
  cancellation, deadline, and process reap;
- Fleet typecheck, tests, and build passed with `runtime/deepseek/node_modules`
  absent, proving conditional dependency loading;
- the finite live L1 Pro/max Run recalled all three seeded issues, produced one
  additional finding, passed the fixed four-test suite, and left the admitted
  Git revision and tree unchanged;
- evidence is bounded, sanitized, and private; no Pactline Task, Claim,
  repository remote, or provider state was mutated;
- no fresh six-Task cohort was needed because deterministic Core lifecycle
  coverage plus real DSH replay established lifecycle parity;
- the frozen old Bundle remained 56 files at aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`.

## Run registry and recovery

The initial registry is one private mode-0600 atomic JSON record per Run. It
stores only coordination data that Pactline and repository providers do not
model:

```json
{
  "run_id": "...",
  "task_number": 12,
  "claim_id": "...",
  "stage": "execution",
  "adapter_id": "codex",
  "adapter_version": "...",
  "runtime_session_id": "...",
  "workspace": "...",
  "repository_revision": "...",
  "state": "agent_running"
}
```

Run state transitions use write-to-new-file, fsync, atomic rename, and
directory fsync where supported. Startup recovery re-reads Pactline and remote
repository state before it resumes, retries, settles, or quarantines a Run.

The registry never copies Pactline Task state as authority. SQLite remains an
optional future implementation behind the same registry interface.

## Credential and sandbox boundary

- Pactline Token exists only in the Fleet coordinator process boundary.
- GitHub/GitLab write credentials exist only in the delivery boundary.
- Harness Adapters receive neither credential.
- Review workspaces have no remote-write credential and use read-only sandbox.
- Execution workspaces use workspace-scoped writes in disposable clones.
- Harness authentication must not be inherited by repository commands or test
  processes.
- The canonical repository checkout and real user home are not mounted as
  writable Agent roots.
- Logs contain Adapter/session/model identifiers and bounded outcomes, never
  raw credentials, prompts, reasoning, or unbounded command output.

## Implementation sequence

### Phase 0 — Freeze the baseline and scaffold the new tree

Planned changes:

- create the standalone `fleet` application with independent package metadata;
- record the old Bundle's current 70-test/typecheck/build/preflight baseline;
- add architecture tests that fail if Core imports Codex or DSH modules;
- add a fake Harness Adapter used by all Core tests;
- add new Make targets without changing or deleting old targets.

Acceptance:

- the old Bundle remains byte-for-byte unchanged during the phase;
- old and new package commands can run independently;
- no new package imports from the old Bundle path;
- Core compiles and tests without either real Harness installed.

### Phase 1 — Port the Harness-neutral Core

Planned changes:

- port the Pactline CLI adapter;
- port Claim-stage admission and settlement;
- port workspace preparation, verification, Git audit, and delivery;
- define common result and event schemas;
- implement static Runtime Router and capability admission;
- reproduce direct, request-changes, correction, and typed-resolution flows
  with the fake Adapter.

Acceptance:

- every lifecycle mutation remains coordinator-owned;
- stale Task/Claim/revision data blocks settlement;
- fake-Adapter contract tests cover success and all terminal failure classes;
- no Harness-specific type appears in Core public types or retained Run state.

### Phase 2 — Port DeepSeek as the parity Adapter

Planned changes:

- implement `DeepSeekHarnessAdapter` from the frozen Bundle behavior;
- move Cordis patch/profile assets under the Adapter;
- normalize DSH events and token usage;
- run the existing offline and finite live DeepSeek gates from the new Fleet.

Acceptance:

- the new Adapter reproduces the completed DeepSeek L0/L1 behavior;
- the six-case L2 lifecycle semantics are reproduced by real DSH replay, or a
  fresh cohort is run only when parity cannot otherwise be established;
- old and new evidence agree on lifecycle semantics;
- the new Fleet does not load DSH dependencies when DeepSeek is disabled.

### Phase 3 — Implement Codex Adapter

Planned changes:

- complete the finite SDK/CLI contract spike;
- implement Codex probe, run, resume, cancel, events, and result capture;
- enforce execution/review sandbox differences;
- freeze model, reasoning, prompt, and Adapter provenance per Run;
- add credential-isolation and malicious-repository tests;
- add one read-only live Codex capability run.

Acceptance:

- Codex performs one finite read-only Run with a schema-valid result;
- cancellation and timeout leave no Agent process or writable workspace;
- Codex receives no Pactline or repository-provider credential; hard isolation
  of Codex's own login cache is explicitly deferred beyond the read-only M3
  gate;
- retained evidence is bounded and sanitized;
- no Pactline lifecycle mutation occurs during the read-only gate.

### Phase 4 — Run Codex L2 v2 qualification

Planned changes:

- create a new isolated Pactline evaluation Project or a clearly versioned new
  Task cohort;
- create new frozen base/candidate branches and Tasks; never reuse completed
  Tasks #6 through #11;
- run Codex execution plus independent Codex review for all six cases;
- retain a separate comparison cohort for mixed Adapter combinations;
- compare correctness, false blocking, correction success, latency, tokens,
  tool errors, and human intervention with the DeepSeek baseline.

Required default matrix:

| Execution Adapter | Review Adapter | Purpose |
|---|---|---|
| Codex | Codex | default production candidate |
| DeepSeek | Codex | measure Codex review/correction value |
| Codex | DeepSeek | measure DeepSeek review limitations |
| DeepSeek | DeepSeek | retained baseline |

The mixed cohorts may use a statistically smaller copy after the complete
Codex/Codex corpus passes; they never overwrite baseline Tasks.

Acceptance:

- all six Codex/Codex cases converge to expected Pactline outcomes;
- the defective review case is rejected and corrected;
- the clean control receives no fabricated blocking finding;
- the conflicting case requests typed resolution before any mutation;
- all fixed and hidden verification passes at the frozen final revision;
- every PR/MR remains isolated from `main` and unmerged;
- zero unauthorized or credential-bearing Agent action occurs.

### Phase 5 — Durable recovery and continuous single-node scheduling

Planned changes:

- implement atomic JSON Run Registry and reconciliation lock;
- persist Adapter and runtime Session identity before dispatch;
- inject termination at every external-effect boundary;
- implement bounded polling, concurrency one, graceful shutdown, and Claim
  recovery;
- quarantine ambiguous state instead of guessing or switching Adapters.

Acceptance:

- every crash checkpoint converges to resumed, settled, released, or
  quarantined with no duplicate business mutation;
- no cross-Adapter recovery occurs automatically;
- continuous mode can drain a bounded test queue and stop cleanly;
- finite `once` mode remains available for diagnosis and evaluation.

### Phase 6 — Cutover and old-Bundle cleanup

Cutover prerequisites:

- all new Core and Adapter contract tests pass;
- DeepSeek parity and Codex L2 v2 qualification pass;
- recovery gates pass;
- root Make targets and documentation point to the standalone `fleet`
  application;
- a source scan finds no active dependency on `@pactline/dsh-fleet` or the old
  directory;
- private old evaluation evidence has an explicit retain/archive disposition;
- no credential or private evidence is added to tracked files;
- the owner reviews the final cutover report.

Cleanup procedure:

1. inventory old source, private manifests, and retained evidence by exact
   path;
2. preserve the completed DeepSeek baseline in a private, mode-restricted
   archive location outside the source subtree;
3. remove only the old `bundles/deepseek-fleet` source and obsolete Make
   targets/docs after the new targets pass again;
4. run a reference scan, full package verification, Pactline CLI tests, and
   fresh local preflight;
5. report exactly what was removed and where retained evidence remains.

There is no compatibility package or alias after cutover. Rollback before old
source removal is switching Make targets back to the frozen Bundle. Rollback
after removal uses the pre-cutover Git commit; private evaluation evidence is
not part of rollback and remains independently retained.

## Test strategy

### Core tests

- work admission before Claim creation;
- capability rejection;
- exact Claim and Task version ownership;
- structured-result validation;
- independent verification mismatch;
- review workspace immutability;
- request changes and new review cycle;
- typed resolution and waived-criterion rules;
- uncertain settlement reconciliation;
- no automatic Adapter fallback.

### Shared Adapter contract tests

Every real Adapter must pass:

- availability and capability probe;
- one execution Run;
- one read-only review Run;
- schema-valid terminal result;
- invalid and missing result rejection;
- event and output bounds;
- timeout and cancellation;
- credential-shaped data redaction;
- process and workspace cleanup;
- resume behavior, or an explicit unsupported capability rejected before
  Claim creation.

### Integration tests

- Docker Pactline CLI protocol and auth;
- repository clone and exact SHA validation;
- coordinator-owned delivery;
- Pactline execution/review/correction/resolution settlement;
- GitHub without a Pactline Repository Connection;
- GitLab provider path before production promotion;
- crash/restart checkpoints.

### Live qualification

- finite read-only Codex gate;
- complete Codex/Codex L2 v2 corpus;
- DeepSeek parity corpus from the new Adapter;
- bounded mixed-Adapter comparison;
- low-risk real-work pilot only after separate owner approval.

## Observability

Every Run records:

- Fleet Run, Task, Claim, stage, and review-cycle identities;
- selected Adapter, Adapter version, runtime Session ID, and model policy;
- workspace and exact repository revisions;
- bounded normalized event counts and tool outcomes;
- deterministic verification observations;
- proposal validation and settlement decisions;
- Pactline request IDs and external-effect reconciliation;
- timeout, cancellation, provider failure, policy failure, and cleanup status;
- token usage where the Harness supplies it.

Raw Harness event formats remain Adapter-private. Fleet operational logs use a
normalized bounded projection and never require model reasoning to reconstruct
the outer workflow.

## Breaking-change impact

The approved cutover breaks:

- local paths under `bundles/deepseek-fleet`;
- package name `@pactline/dsh-fleet`;
- DSH-specific Make target names if they are replaced rather than retained as
  explicitly DeepSeek-only diagnostics;
- documentation and scripts that assume DSH is the Fleet host;
- any unpublished local consumer importing current Bundle exports.

No Pactline API, database data, Task lifecycle, Project, repository membership,
or existing completed evaluation Task requires migration. New evaluation uses
new Tasks and evidence. The old package is private and currently has no
declared supported external consumer.

## Documentation deliverables

Before Core code becomes authoritative:

- create a tracked Harness-neutral architecture document under `docs/`;
- update the root README with Fleet installation and dependency boundaries;
- document Adapter development and contract testing;
- document Codex and DeepSeek credential provisioning separately;
- document finite, continuous, recovery, and cleanup commands;
- retain the old DeepSeek baseline runbook as historical evaluation evidence;
- remove statements that describe DeepSeek Harness as the Fleet host.

This planning document remains local under `docs/superpowers/`; stable accepted
decisions must be distilled into tracked documentation during Phase 0.

## Alignment checkpoint

The owner approved a standalone Fleet application, sibling Codex and DeepSeek
Harness Adapters, parallel construction beside the frozen old Bundle, eventual
cleanup after acceptance, and an internal-first Adapter contract. The detailed
Milestone decomposition and execution checkpoint are maintained in
`docs/superpowers/plans/2026-08-15-pactline-fleet-master-runbook.md`.

Implementation starts only after the owner reviews that Milestone plan.
