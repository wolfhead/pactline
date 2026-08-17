# Pactline Fleet M5.4 Usability Acceptance Plan

**Status:** complete; bounded local trials accepted

## Decision objective

Answer one question with production-shaped local evidence:

> Can the current Pactline Fleet be used for bounded day-to-day trials without
> an operator manually repairing Task, Claim, repository, or local Run state?

This is a usability decision gate, not production reliability certification.

## Public test seams

Acceptance tests observe only interfaces available to an operator or external
system:

1. Pactline CLI and `/api/v1` for Project, Task, Claim, Thread, Check, and
   delivery state;
2. `pactline-fleet serve`, process exit, `/readyz`, and the read-only Fleet
   observation API for service and Run state;
3. the local Git remote through Git commands for exact revision and branch
   evidence;
4. the Fleet Web UI through a real browser for operator-visible state.

Tests do not query SQLite tables directly. SQLite remains an implementation
detail; persisted recovery state is verified through service restart and the
observation API.

## Scope

### U1 — deterministic vertical slice

- provision one isolated local Pactline Project and low-risk Task;
- use Replay with the real Pactline CLI, resident scheduler, work plugin,
  disposable worktree, local bare Git remote, verification, delivery, and
  settlement;
- verify the Task reaches review without manual intervention;
- verify the Fleet UI exposes Project, Task, Adapter, checkpoint, and outcome;
- remove the temporary Token and retain only sanitized evidence.

This slice proves the orchestration independently of model quality or spend.

### U2 — representative live Harness paths

- run one DeepSeek Pro/max execution followed by Codex `gpt-5.6-sol/high`
  review;
- run one Codex `gpt-5.6-sol/high` execution and review;
- use bounded, reversible repository changes and fixed verification;
- retain Adapter identity, Session identity, usage summary, exact Git
  revision, and Pactline outcome without raw reasoning.

These two Tasks answer whether both supported Adapters can participate in the
resident Fleet workflow. They are not a new capability benchmark.

### U3 — correction workflow

- use a deterministic seeded defect and Replay-controlled review decision;
- prove request-changes, correction Claim, new verification, and final review
  can complete without manual state repair;
- keep all repository delivery local and unmerged.

Deterministic control is intentional because the target is lifecycle behavior,
not whether a model happens to request changes.

### U4 — bounded restart

- prove a normal stop drains and a new process reopens the same Fleet state;
- inject one representative termination after Session persistence and before
  the first Agent effect using Replay;
- restart with the same state directory and prove the known Claim is safely
  released or resumed according to the frozen Adapter capability;
- verify no duplicate Claim, branch, delivery, or settlement is created.

This is one representative restart, not the deferred 14-checkpoint crash
matrix.

## Acceptance runner

The finite acceptance commands now:

- checks Docker Pactline, CLI protocol, Adapter availability, and one exact
  committed repository base before mutation; the shared checkout may remain
  dirty because acceptance clones only the recorded commit;
- creates an isolated Project, Tasks, temporary Token, local Git remote, Fleet
  configuration, state directory, and work roots;
- starts Fleet Service as a child process and waits through public health and
  observation endpoints;
- drive each bounded scenario; browser evidence is inspected separately against
  the same real service state;
- stops child processes and revokes the temporary Token in `finally` paths;
- writes private artifacts under `.fleet/m5-4-usability/` with mode 0600/0700;
- emit private JSON evidence; the sanitized decision is recorded in the tracked
  M5.4 acceptance report.

The runner must be safe to rerun. Each run uses a unique namespace and never
adopts an earlier active Claim or deletes an unresolved path.

## Files and components

Implemented additions:

- `fleet/src/evaluation/m5-4-usability.ts`
- `fleet/src/evaluation/m5-4-usability-bin.ts`
- `fleet/src/evaluation/m5-4-deterministic.ts`
- `fleet/tests/evaluation/m5-4-usability.test.ts`
- package scripts for preflight, individual scenarios, and the complete finite
  acceptance sequence
- `docs/pactline-fleet-m5-4-report.md`

The acceptance runner reuses the production scheduler, coordinator, local Git
delivery boundary, and work-plugin behavior. It does not add a test-only
orchestration path.

## TDD sequence

1. RED: the public preflight rejects a missing Pactline, incompatible CLI
   protocol, missing committed base, Adapter failure, or occupied evidence
   namespace with a useful error.
2. GREEN: implement the smallest finite preflight.
3. RED: U1 cannot yet complete through the acceptance runner's public result.
4. GREEN: provision and complete the deterministic vertical slice.
5. Repeat one vertical RED/GREEN cycle for U3, U4, and then each live U2 Task.
6. Run the existing Fleet gate after every integration boundary.

## Pass criteria

- discovery starts work without manual Claim creation;
- development, verification, delivery, review, and correction paths reach the
  expected authoritative Pactline state;
- every Task has one correct active or terminal Claim disposition;
- Git revision and delivery evidence match the verified workspace;
- the UI accurately reflects active and terminal Runs;
- normal restart and the one injected restart require no database repair;
- no duplicate Claim, commit, branch, delivery, or Thread submission occurs;
- no Fleet, Harness, verification, or work-plugin subprocess remains;
- evidence contains no credentials, raw prompts, or raw model reasoning.

## Explicit deferrals

The following are not required to answer the usability question:

- all 14 crash checkpoints;
- two independent Fleet Services competing for one Project;
- SQLite corruption or disk exhaustion;
- 24-hour soak;
- live GitHub/GitLab failure and reconciliation;
- production sandbox and credential-proxy qualification.

They remain optional reliability or M6 hardening work before production-scale
deployment.

## Owner inputs

No GitHub or GitLab credential is required. The runner uses local development
authentication, a temporary Pactline Token, the existing repository, local
Git delivery fixtures, and already configured Codex/DeepSeek credentials.
