# Pactline Fleet M5.2 Scheduler and Recovery Implementation Plan

**Status:** complete

## Objective

Add durable, fair multi-Fleet discovery, admission, execution, and restart
recovery to the M5.1 resident service without moving workflow authority out of
Pactline or coupling the scheduler to one Harness.

The deterministic test suite uses the Replay Adapter and fake time. The bounded
live demonstration prefers the DeepSeek Adapter with its fixed
`deepseek-v4-pro` / `max` route. Codex remains a compatibility and resumability
contract, not the primary paid test path for this milestone.

## Contract decision discovered during implementation

Pactline work packets intentionally contain Task, criteria, delivery, and
Thread context. They do not define repository source, base ref, allowed paths,
fixed verification commands, or provider write policy. Fleet must never infer
those authority boundaries from Task prose.

M5.2 therefore introduces a Harness-neutral `WorkDefinitionResolver` boundary.
The scheduler freezes the resolver output into the local Run before Claim or
Harness dispatch. A Fleet that cannot resolve a complete definition is skipped
with a diagnosable admission result and does not create a Claim. A later
project-specific bundle or plugin may implement the resolver without changing
the scheduler, registry, or Harness Adapter contracts.

## Phase 1 — durable Run ledger

Files:

- `fleet/src/registry/fleet-registry.ts`
- `fleet/tests/registry/fleet-registry.test.ts`

Changes:

- migrate the local registry forward without rewriting schema version 1;
- persist Task/stage/version, frozen route and work definition, Claim and
  Adapter Session identity, workspace identity, last safe checkpoint,
  disposition, and sanitized failure detail;
- validate monotonic state transitions transactionally;
- persist intent/observed external-effect checkpoints with unique idempotency
  material;
- expose non-terminal recovery records and terminal history.

## Phase 2 — fair scheduler

Files:

- `fleet/src/scheduler/candidate.ts`
- `fleet/src/scheduler/fair-scheduler.ts`
- `fleet/src/scheduler/backoff.ts`
- `fleet/tests/scheduler/fair-scheduler.test.ts`

Changes:

- discover execution and review candidates per enabled Project;
- sort deterministically by Task number and stage;
- visit eligible Fleets round-robin;
- enforce global and per-Fleet concurrency limits;
- treat Claim conflicts as expected contention;
- apply bounded exponential backoff with injectable jitter and time;
- expose a finite `once` cycle for diagnosis.

## Phase 3 — execution and recovery coordinator

Files:

- `fleet/src/scheduler/run-coordinator.ts`
- `fleet/src/core/claim-stage.ts`
- `fleet/src/recovery/reconciler.ts`
- `fleet/tests/scheduler/run-coordinator.test.ts`
- `fleet/tests/recovery/reconciler.test.ts`

Changes:

- create and freeze a Run before Claim mutation;
- checkpoint Claim, workspace, Session, result, delivery, settlement, and local
  terminal persistence boundaries;
- pass cancellation through to owned Harness processes;
- resume only an Adapter Session explicitly recorded by this service and only
  when the Adapter reports resume support;
- release a known active Claim when non-resumable recovery is unambiguous;
- quarantine unfamiliar, missing, or contradictory authority rather than
  adopting or releasing it;
- reconcile every non-terminal Run before discovery starts.

## Phase 4 — resident-service integration

Files:

- `fleet/src/service/fleet-service.ts`
- `fleet/src/commands/serve.ts`
- `fleet/src/cli.ts`
- `fleet/tests/service/fleet-service.test.ts`

Changes:

- start scheduling only after dependency checks and recovery complete;
- stop admission first during drain, cancel active Runs, wait to the configured
  deadline, and preserve the latest durable checkpoint;
- reload routing/concurrency for new Runs while existing Runs keep their frozen
  configuration;
- add `serve --once` as a finite discovery/drain path;
- keep liveness available while reconciling or draining.

## Phase 5 — verification

- registry transition, reopen, and migration tests;
- fake-time fairness, concurrency, contention, jitter, and backoff tests;
- fault injection at every durable/external-effect checkpoint;
- two-service same-Task competition with different and same principals;
- Replay end-to-end recovery tests;
- Codex resume/cancellation contract regression;
- DeepSeek non-resumable release/quarantine regression;
- serialized local Pactline integration with two Fleets and global concurrency
  one;
- one bounded real DeepSeek demo when a suitable disposable Pactline Task and
  repository policy are available;
- `npm run typecheck`, `npm test`, `npm run build`, and the repository Fleet
  check target.

## Acceptance criteria

- no Harness starts before a successful Claim is durably associated with its
  Run;
- one Fleet cannot starve another under a global concurrency budget of one;
- Claim contention does not degrade service health or spin;
- restart reconciliation finishes before new admission;
- every injected failure converges to completed, released, or quarantined;
- recovery never changes Harness Adapter or adopts an unfamiliar Claim;
- no protected external effect is duplicated;
- graceful shutdown leaves a durable recovery fact for every interrupted Run.

## Completion evidence

- registry schema v3 preserves discovery version, post-Claim Task version,
  Claim and Session identity, frozen policy, workspace, terminal disposition,
  and per-effect intent/observation facts;
- deterministic scheduling covers stable ordering, round-robin fairness,
  global/per-Fleet limits, slot replenishment, bounded backoff, drain deadline,
  correction routing, and two-service contention;
- recovery covers idempotent Claim-intent replay, unfamiliar same-principal
  Claim quarantine, exact Codex-style Session resume, DeepSeek-style
  non-resumable release, terminal settlement reconciliation, and retained
  workspace cleanup;
- the awaited observer contract proves Session persistence completes before an
  Adapter may emit the first Agent event;
- the executable work-plugin protocol freezes repository and verification
  authority and checkpoints `commit`, `push`, and `open-code-change`
  independently;
- all fourteen implemented durable/effect checkpoints are observed in the
  process-level service test, with an injected Session-boundary crash proving
  restart convergence;
- local Docker authentication verified protocol 2 / 16 features and read-only
  discovery across Projects 3 and 4, then revoked the temporary Token;
- the bounded real DeepSeek `deepseek-v4-pro` / `max` service demo completed
  execution through `in_review.available` with zero non-terminal Runs and no
  remote repository effect; private evidence is retained under
  `.fleet/m5-2-deepseek-demo/latest.json`;
- `make pactline-fleet-check`, package typecheck/build/tests, and npm audit pass.
