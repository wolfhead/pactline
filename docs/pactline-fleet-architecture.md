# Pactline Fleet Architecture

Pactline Fleet is a standalone, Harness-neutral orchestration application. It
coordinates Pactline work through interchangeable Agent Harness Adapters while
keeping workflow transitions, repository delivery, verification, recovery,
and audit deterministic.

Pactline Fleet is not a DeepSeek Harness Bundle and does not implement an Agent
Loop. Codex, DeepSeek Harness, and future Harnesses own their respective inner
Agent Loops behind a common Adapter boundary.

## System boundary

```text
Pactline
  Task / Claim / Thread / Issue / Criteria / Code Change
                         |
                         | installed Pactline CLI machine contract
                         v
Standalone Pactline Fleet process
  +-- work admission and scheduling
  +-- Claim-stage orchestration
  +-- Harness routing
  +-- disposable workspaces
  +-- deterministic verification and Git audit
  +-- branch / push / PR-MR delivery
  +-- Pactline settlement and reconciliation
  +-- local Run recovery and bounded evidence
                         |
                         v
Harness Adapter boundary
  +-- Codex Adapter ---------> Codex Agent Loop
  +-- DeepSeek Adapter ------> DeepSeek Harness Agent Loop
  +-- future Adapter --------> another Harness Agent Loop
```

## Ownership

Pactline is authoritative for:

- Task and Claim lifecycle;
- Task, Claim, criterion, and review-cycle versions;
- Main and Issue Threads;
- execution submissions and frozen review delivery;
- acceptance checks, changes requests, acceptance, and typed resolution;
- repository membership and optional Connection enrichment;
- authentication, authorization, idempotency, and business audit.

Fleet Core owns:

- bounded discovery and admission;
- deterministic Harness selection;
- disposable execution and review workspaces;
- exact base and delivery revision checks;
- fixed verification and Git audit;
- coordinator-owned branch, push, and Draft PR/MR operations;
- validation of Harness proposals;
- Pactline settlement and uncertain-response reconciliation;
- local coordination state and secret-safe operational evidence.

A Harness Adapter owns only:

- Harness availability and capability probing;
- Session or Thread creation, resume, cancellation, and disposal;
- conversion of a common Fleet request into Harness-native input;
- Harness-native model, prompt, tool, and sandbox configuration;
- bounded event and usage capture;
- conversion of one terminal result into a common Fleet proposal.

An Adapter never discovers or claims Pactline work, applies a lifecycle
transition, pushes a branch, creates a PR/MR, or receives the credentials that
would allow those operations.

## Process and dependencies

The source lives at repository-root `fleet/`. The package is
`@pactline/fleet`, and the executable is `pactline-fleet`.

Fleet Core must compile and test without any Harness installed. Cordis and
`@deepseek-ai/*` dependencies belong only to the DeepSeek Adapter. The pinned
Codex Agent CLI and its event types belong only to the Codex Adapter. Neither
kind of type may cross into Core contracts or common persisted Run records.

Pactline integration uses only the installed CLI machine contract. Fleet does
not import Pactline Go packages, handlers, stores, migrations, generated API
clients, or database implementation details.

## Adapter contract

The Adapter contract is defined around one Pactline Claim stage:

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

The common request carries the Run and Claim identity, stage, bounded work
packet, exact repository revision, disposable workspace, sandbox, allowed
scope, fixed verification commands, result schema, deadline, and frozen
policy. It carries no Pactline Token, repository-provider write credential,
settlement callback, or mutable registry handle.

The common result carries the Adapter and runtime Session identities, model
provenance, terminal state, schema-valid proposal, bounded event summary, and
usage when available. The proposal is not a lifecycle decision. Fleet validates
it and re-reads authoritative state before settlement.

The Adapter reports its runtime Session identity through the observer before
the first Agent effect so Fleet can persist the recovery mapping. The common
`AbortSignal` carries timeout, shutdown, and operator cancellation into the
Harness without relying on provider-specific cancellation types.

The contract remains internal until Codex and DeepSeek pass the same contract
and live qualification suites. A public third-party Adapter API can then be
versioned from evidence rather than assumptions from one Harness.

### DeepSeek implementation

Milestone M2 implements `DeepSeekHarnessAdapter` as a JSON-RPC client of one
owned `dsh-jsonrpc-agent` subprocess per Fleet Run. The Adapter sends
`initialize` and `session/prompt`, consumes `session.event` and
`session.status` notifications until the submitted prompt returns to idle, and
accepts a proposal only after the Adapter-owned `submit_fleet_result` Cordis
plugin validates and commits the terminal tool call.

The runtime closure and its pinned DeepSeek Harness `0.1.0-rc.6` dependencies
live under `fleet/runtime/deepseek`; Fleet Core imports no DSH or Cordis type.
The qualification route is deterministic:
`deepseek-official/deepseek-v4-pro`, reasoning `max`. The profile provides
native Bash and filesystem tools under DSH's local sandbox, JSONL Session
persistence, checkpoints, token metering, and bounded event translation.

The current DSH JSON-RPC surface has no per-prompt cancel, Session-close, or
resume operation. Fleet does not advertise weaker semantics: each Run gets its
own runtime process, and cancellation or timeout terminates and reaps the whole
process. `sessionResume` is false. A keyless doctor proves finite runtime boot
and shutdown without a model call.

DSH deliberately supplies managed shell facts (`DSH_HOME`, `DSH_SHELL`,
`DSH_SESSION_ID`, and where applicable `DSH_SESSION_JSONL`) to native tools.
The Adapter removes stale ambient `DSH_*` state before launch, and DSH replaces
it with facts for the current Session. These values are not model credentials.
DeepSeek API, Pactline, GitHub, and GitLab credentials remain excluded from
model tools and coordinator verification.

### Codex implementation

Milestone M3 implements `CodexHarnessAdapter` over one owned, pinned Codex
Agent CLI 0.147.0 process per Run. The Adapter fixes the qualification route at
`openai-codex/gpt-5.6-sol`, reasoning `high`; Max is intentionally not used and
Medium is reserved for later measured optimization. It owns CLI configuration,
strict structured-output translation, bounded JSONL events, token usage,
Session resume, timeout/cancellation, and process-tree reap.

Every Run ignores user configuration and rules, disables multi-agent and web
search, uses approval `never`, and selects the exact read-only or
workspace-write sandbox. Pactline, GitHub, and GitLab credentials are removed
before launch. Codex provider authentication remains a trusted Harness runtime
dependency for the M3 read-only gate; current Codex 0.147.0 read-only mode is
not described as a filesystem read allowlist.

## Admission and routing

An Adapter declares supported stages, sandbox modes, native tools, structured
results, events, cancellation, and Session resume. Fleet checks required
capabilities before Claim creation and refuses admission rather than weakening
a policy after work is claimed.

Initial capability-first defaults are Codex for execution, review, correction,
and resolution analysis. There is no automatic cross-Adapter fallback. An
active Run retains its selected Adapter, version, model, reasoning, prompt,
result-contract, and sandbox policy.

Harness selection is Fleet configuration, not Pactline Task state. A model
never selects its own Harness.

## Execution, review, and resolution

Execution receives a disposable workspace-write clone but no remote-write or
Pactline credential. Fleet independently observes its diff and reruns fixed
verification before publishing delivery or completing execution.

Review uses a new Harness Session and a credential-free detached clone at the
frozen delivery revision. It receives no execution conversation. Fleet rejects
any review workspace mutation and validates every cited file and line.

Changes requests create a new Execution Claim and later a new Review Claim.
Typed resolution ends the current Claim and permits no repository mutation
before the Issue is resolved. Post-resolution work uses a new Claim and may
record only an explicitly superseded criterion as waived.

## Credentials

- Fleet coordinator owns the Pactline Token.
- Repository delivery owns GitHub/GitLab write credentials.
- Harness Adapters receive neither.
- Pactline and repository-provider credentials must not enter Harness
  processes, repository commands, tests, or package scripts.
- Strong isolation of Harness provider authentication is a later writable-live
  hardening gate when the Harness sandbox cannot express a filesystem read
  allowlist; it is not claimed by the M3 read-only qualification.
- The canonical checkout and real user home are outside the Agent workspace.
- Logs and common evidence exclude credentials, raw reasoning, and unbounded
  command or model output.

## Persistence and recovery

Pactline and repository providers remain authoritative. Fleet stores only
coordination facts they do not model, including Run, Claim, Adapter, runtime
Session, workspace, exact revision, protected idempotency, and recovery state.

Finite runners may retain private atomic JSON evidence, but the resident Fleet
Service uses SQLite behind the registry interface. The resident process needs
transactional coordination across multiple Project-bound Fleets, Runs,
external-effect checkpoints, health history, and read-only observation
queries. SQLite remains local coordination state, never Pactline workflow
authority.

Startup recovery re-reads Pactline, repository, workspace, and Adapter
capability before it resumes, retries, releases, settles, or quarantines a Run.
The complete resident-process, distributed-competition, and local Web UI design
is documented in
[Pactline Fleet Service Architecture](pactline-fleet-service-architecture.md).
Its staged delivery gates are documented in
[Pactline Fleet Service Milestones](pactline-fleet-service-milestones.md).
The current entity classification, Run state model, invariants, and review
entry points are documented in the
[Pactline Fleet Domain Model](pactline-fleet-domain-model.md).

## Migration boundary

The former DeepSeek-specific Bundle was used only as a frozen comparison while
the standalone application was built. After M5.4 proved the independent Core,
both Adapters, resident scheduling, recovery, and bounded usability, the owner
approved retiring that untracked reference source and its obsolete commands and
hash gate. No standalone Fleet source imported it.

DeepSeek support remains active through `DeepSeekHarnessAdapter` and its pinned
runtime under `fleet/runtime/deepseek/`. There is no compatibility alias for
`@pactline/dsh-fleet`. Production hardening, controlled real work, and Game
Design remain separate promotion decisions.

## Implemented Fleet milestones

Milestone M1 implements the common boundary under `fleet/`. The implementation
includes:

- a strict Pactline CLI subprocess client with protocol and feature admission,
  idempotency, bounded output, timeout, cancellation, and secret-safe errors;
- a static Runtime Router that probes native tools, structured results, event
  streaming, cancellation, stage, and sandbox capability before Claim creation
  and never falls back automatically;
- exact Task, Claim, criterion-revision, Adapter, model, and runtime Session
  validation around each finite execution, correction, or review Run;
- common structured execution, review, and typed-resolution proposals plus
  bounded normalized operational events;
- disposable execution branches and detached review workspaces, coordinator-run
  fixed verification, Git observation, allowed-path enforcement, and review
  mutation rejection;
- provider-neutral GitHub and GitLab delivery evidence owned by the
  coordinator rather than an Adapter;
- deterministic Pactline settlement with a fresh authority read and
  idempotent reconciliation after an uncertain terminal response;
- deterministic Replay coverage for direct, review-first, correction, and
  typed-resolution paths.

The authenticated local Docker integration issues and revokes an ephemeral
development Token and performs only capability, authentication, and bounded
discovery reads. No M1 test delegates Pactline or repository-provider write
authority to a Harness.

Milestone M2 proves that boundary with the real DeepSeek Adapter. Installed
runtime contract tests cover native read-only and workspace-write tools,
structured direct execution, clean review, changes request, typed resolution,
cancellation, deadline, and process reap. The root Core suite also passes when
the separate DSH installation is absent. A finite live Pro/max L1 review found
all three seeded defects, preserved the Git tree, passed Fleet's fixed tests,
and retained only bounded sanitized evidence. A fresh six-Task Pactline cohort
was unnecessary because Core lifecycle tests plus real DSH replay established
the required lifecycle parity without external mutations.

Milestone M3 adds the Codex Adapter behind the same contract. Codex
`gpt-5.6-sol/high` is the accepted default route, while DeepSeek Pro/max remains
an explicit sibling route. Both Adapters expose their real capability
differences; Fleet never substitutes one Harness after admission.

Milestone M4 proves the Harness-neutral lifecycle across execution, review,
correction, and typed resolution cases. It establishes Codex as the default on
quality evidence without removing DeepSeek support or moving workflow and
repository authority into either Adapter.

Milestone M5 turns the finite coordinator into one recoverable resident
service managing multiple Project-bound Fleets. It adds fair scheduling,
durable Run and external-effect records, startup reconciliation, trusted work
plugins, bounded health and metrics, and a loopback-only read-only Operations
Console. M5.4 accepts bounded local trials after deterministic, live Adapter,
correction, restart, and browser-observation gates. Production hardening,
distributed competition qualification, retention, and exhaustive failure
testing remain M6.
