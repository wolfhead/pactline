# Pactline Fleet Harness-Neutral Architecture

**Date:** 2026-08-15

**Status:** Implemented through M5; former reference Bundle retired after M5.4

## Product statement

Pactline Fleet is a standalone, Harness-neutral orchestration application. It
coordinates Pactline work through interchangeable Agent Harness Adapters while
keeping workflow transitions, repository delivery, verification, recovery,
and audit deterministic.

DeepSeek Harness is one supported Harness. It is not the host process,
composition framework, or architectural foundation of Pactline Fleet. Codex is
the default Harness during the capability-first rollout. Future Harnesses can
be added without changing Pactline workflow semantics.

## System model

```text
Pactline
  Task / Claim / Thread / Issue / Criteria / Code Change
                         |
                         | installed Pactline CLI machine contract
                         v
Standalone Pactline Fleet process
  +-- scheduler and admission
  +-- Claim-stage orchestration
  +-- Harness routing
  +-- workspace and repository delivery
  +-- deterministic verification
  +-- settlement and reconciliation
  +-- Run registry and evidence
                         |
                         v
Harness Adapter boundary
  +-- Codex Adapter ---------> Codex Agent Loop
  +-- DeepSeek Adapter ------> DeepSeek Harness Agent Loop
  +-- future Adapter --------> another Harness Agent Loop
```

There are two layers of control:

1. Fleet's outer loop is deterministic application code. It discovers work,
   creates or continues a Claim, dispatches one Harness Run, validates all
   effects, and applies one legal Pactline settlement.
2. A Harness's inner loop is its native model/tool loop. Fleet does not
   implement or emulate it.

## Process and dependency boundary

The Fleet executable is `pactline-fleet` and the package is
`@pactline/fleet`. Its source lives in the repository-root `fleet/` directory,
not under `bundles/`.

Fleet Core may depend on:

- Node.js standard libraries;
- the installed Pactline CLI machine contract;
- runtime-neutral schema, logging, configuration, and process utilities;
- local Git and coordinator-owned repository-provider clients.

Fleet Core must not depend on:

- Cordis or any `@deepseek-ai/*` Agent/Session/tool type;
- Codex SDK Thread/event types;
- a model-provider credential;
- Pactline Go packages, handlers, stores, migrations, or database schema;
- a provider-specific Task field or lifecycle variant.

Each Harness dependency belongs only to its Adapter module. Selecting Codex
must not require DeepSeek Harness to be installed. Selecting DeepSeek must not
require Codex to be installed.

## Authority boundaries

### Pactline owns

- Task definition and lifecycle;
- Claim exclusivity, expiry, stage, and outcome;
- Task and Claim versions;
- Acceptance Criteria and Claim-owned checks;
- Main and Issue Threads;
- execution submissions and frozen review delivery;
- review acceptance, changes requests, and typed resolution;
- Project repository membership and optional Connection enrichment;
- authentication, authorization, idempotency, and business audit.

### Fleet Core owns

- bounded work discovery and admission;
- deterministic Adapter selection;
- one Fleet Run per Claim-stage attempt;
- disposable execution and review workspaces;
- exact repository base and delivery revision checks;
- coordinator-owned branch, push, and Draft PR/MR operations;
- fixed-command verification and Git audit;
- validation of Agent proposals;
- Pactline settlement and uncertain-response reconciliation;
- local Run recovery state and bounded operational evidence.

### A Harness Adapter owns

- Harness availability and capability probing;
- Session or Thread creation, resume, cancellation, and disposal;
- translation of the common Fleet packet into Harness-native input;
- Harness-native model, prompt, tool, and sandbox configuration;
- collection of Harness events and usage;
- translation of one terminal Harness result into the common Fleet proposal.

An Adapter does not own work discovery, Claim lifecycle, verification truth,
repository delivery, or settlement.

## Adapter contract

The initial contract is internal to `@pactline/fleet`. It is not a stable
third-party plugin API until both Codex and DeepSeek implementations pass the
same contract and live qualification suites.

```ts
interface HarnessAdapter {
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

The common request includes identity, stage, bounded work packet, workspace,
exact repository revision, sandbox, expected scope, fixed verification
commands, result schema, deadline, and frozen policy. It excludes every
control-plane credential and mutation callback.

The common result includes Adapter/version, runtime Session identity, model
provenance, terminal state, a schema-valid proposal, bounded event summary,
and token usage when available.

The observer receives the runtime Session identity before the first Agent
effect so Fleet can persist the recovery mapping. A common `AbortSignal`
propagates timeout, graceful shutdown, and operator cancellation without
leaking provider-specific cancellation types into Core.

## Capability admission

An Adapter declares:

- supported stages;
- read-only and workspace-write sandbox support;
- structured-result support;
- event-stream support;
- cancellation support;
- Session-resume support;
- native coding-tool support.

Fleet converts a stage policy into required capabilities and checks them
before Claim creation. Missing capabilities reject admission. Fleet does not
weaken a stage policy after claiming work.

Resume is not assumed to be universally available. An Adapter without resume
support may still run only when policy defines a safe release/quarantine path
for interruptions.

## Runtime routing

The initial router is static and configuration-driven:

```yaml
runtime:
  defaults:
    execution: codex
    review: codex
    correction: codex
    resolution_analysis: codex
  automaticFallback: false
```

The selection is frozen before dispatch and stored with the Run. A global
configuration change does not change an active Run. A provider failure does
not silently switch Harnesses. Recovery retries or resumes the selected
Adapter, releases work safely, or quarantines ambiguity.

Task- or risk-based routing is deferred until comparative evidence exists. No
model selects its own Harness.

## Common execution contract

The execution Adapter receives a disposable clone with workspace-write access
and may inspect, modify, and test it with native Harness tools. It receives no
remote-write or Pactline credential.

The terminal proposal contains:

- summary and limitations;
- exact changed paths;
- criterion revision outcomes and evidence;
- reported verification commands and outcomes;
- `complete` or `request_resolution` recommendation.

After the Harness exits, Fleet independently observes the Git diff and reruns
fixed verification commands. Only a validated complete proposal can be
committed, pushed, linked, submitted, and completed by the coordinator. A
resolution request is accepted only from an unmodified workspace and unchanged
HEAD.

## Common review contract

Review uses a new Harness Session and a credential-free detached clone at the
frozen delivery SHA. It receives no execution conversation. The Adapter must
provide read-only policy; Fleet also rejects any post-run HEAD, status, branch,
or tracked-file change.

The terminal proposal contains:

- summary and limitations;
- bounded findings with path, line, severity, category, and evidence;
- current criterion revision outcomes and evidence;
- reported verification commands and outcomes;
- `accept`, `request_changes`, or `request_resolution` recommendation.

Fleet validates cited files and lines, reruns fixed checks, and re-reads the
current Task, Claim, review cycle, criteria, and delivery before settlement.

## Correction and typed resolution

A changes request ends the Review Claim. Correction uses a new Execution Claim
and a new Harness Session. Its workspace begins at the exact frozen candidate
or current delivery defined by the coordinator. A later review cycle always
uses a new independent Review Claim and Session.

A typed resolution request ends the Claim and opens a Pactline Issue. Fleet
does not mutate the repository before resolution. After resolution, a new Claim
and Harness Session receive the conclusion in their work packet. Only that
explicit post-resolution path may record a superseded criterion as `waived`;
ordinary completion and acceptance require current criteria to pass.

## Codex Adapter

Codex is the default quality-first Adapter. It uses the official Codex SDK when
the SDK satisfies the required contract. A finite compatibility spike must
verify working-directory control, sandbox modes, structured result capture,
event streaming, cancellation, resume, model provenance, and credential
isolation.

If a required SDK seam is unavailable, the Adapter may use the documented
non-interactive Codex CLI JSONL and output-schema interface internally. This
does not alter the Fleet contract.

Execution uses workspace-write. Review uses read-only. Every Claim uses a new
Thread except a tested recovery of that exact Run. Correction and
post-resolution work never resume a terminated earlier Claim's Thread.

## DeepSeek Harness Adapter

DeepSeek compatibility includes an Adapter-owned DSH plugin shim. The shim may
register the Fleet terminal result tool, add the bounded Fleet prompt, collect
Session evidence, and request finite launcher shutdown. It does not contain the
Fleet scheduler or Pactline settlement logic.

All Cordis configuration, DSH profile composition, DeepSeek model policy, and
`@deepseek-ai/*` types stay under the Adapter. The rest of Fleet must compile
and run when these dependencies are absent.

## Repository delivery

Harnesses never push. Fleet Core:

1. audits the workspace against the admitted base;
2. reruns fixed verification;
3. creates a coordinator-owned commit when required;
4. pushes only an exact allowlisted branch;
5. creates or reconciles one Draft PR/MR;
6. freezes exact URL and revision evidence;
7. links it through the Pactline CLI;
8. completes execution only after authoritative re-reads.

GitHub and GitLab provider clients implement a separate repository-provider
boundary, not Harness Adapters. Pactline Repository Connections remain optional
read-only evidence enrichment.

## Persistence and recovery

Pactline and the repository provider remain authoritative. Fleet persists only
coordination facts that neither system models, including Run ID, Claim ID,
Adapter/version, runtime Session ID, exact revisions, protected idempotency
material, workspace reference, and recovery state.

The first implementation uses private atomic JSON Run records. It writes a new
file, synchronizes it, atomically renames it, and synchronizes the containing
directory where supported. SQLite is an optional future implementation behind
the same registry interface.

Startup reconciliation reads Pactline, the repository provider, the workspace,
and Adapter capability before deciding to resume, retry an exact idempotent
operation, release, mark settled, or quarantine. It never reconstructs
business truth from local JSON alone.

## Credential model

- The Fleet coordinator owns the Pactline Token.
- The repository-delivery component owns provider write credentials.
- Harness Adapters receive neither.
- Harness authentication must not be exposed to repository commands, tests,
  package scripts, or model-visible environment inspection.
- If a Harness cannot isolate its master credential from tool subprocesses,
  the integration must use a credential proxy or equivalent process boundary
  before writable live use.
- The canonical checkout and real user home are outside the Agent workspace.
- Retained artifacts are bounded, private, sanitized, and never tracked when
  they contain runtime evidence.

## Observability

Fleet records the outer timeline in normalized events:

- scan, admission, Claim, and Run identities;
- selected Adapter, model, policy, and runtime Session;
- workspace and repository revisions;
- Harness terminal state and bounded tool/event counts;
- verification and proposal-validation outcomes;
- repository delivery and Pactline settlement decisions;
- reconciliation, retry, quarantine, and cleanup outcomes.

Raw Harness event formats and reasoning remain Adapter-private. Operators must
be able to reconstruct the Fleet state transition without reading raw model
reasoning.

## Source migration and cleanup

The implementation lives only in `fleet/`. The former DeepSeek-specific
Bundle was never imported by the standalone application and was retired with
owner approval after M5.4 established Adapter parity, Codex qualification,
continuous scheduling, representative recovery, and bounded usability.

There is no compatibility alias for `@pactline/dsh-fleet`. DeepSeek remains a
supported Adapter through its separate pinned runtime under
`fleet/runtime/deepseek/`.

## Promotion boundary

Harness-neutral architecture and a passing synthetic corpus do not authorize
unattended production work. Low-risk real work requires a separate owner
approval after Codex/DeepSeek qualification, recovery, credential-isolation,
and operational gates pass. Game Design requires a further explicit decision
based on that pilot evidence.
