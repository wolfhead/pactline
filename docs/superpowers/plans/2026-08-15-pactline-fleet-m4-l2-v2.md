# Pactline Fleet M4 L2 v2 Implementation and Run Plan

**Date:** 2026-08-15

**Status:** complete

## Objective

Run a new isolated six-case Pactline cohort through Codex execution and
independent Codex review, then compare bounded mixed Adapter routes against the
frozen DeepSeek baseline. Produce enough lifecycle, repository, correctness,
cost, and intervention evidence to decide whether Codex is ready to be Fleet's
default Harness. Do not begin M5.

## Frozen inputs

- repository: `https://github.com/wolfhead/pactline`;
- remote base: `abc3599c863fbc2041e0cd463776d3d8ca8c7fb1`;
- observed remote ref: `refs/heads/main` on 2026-08-15;
- local control-plane checkout: never used as an Agent workspace;
- Project name: `Pactline Fleet L2 v2 Evaluation`;
- remote namespace: `fleet-eval/l2-v2/`;
- source ref: `fleet-eval/l2-v2/source`;
- case refs: `fleet-eval/l2-v2/base/<case-id>`;
- candidate refs: `fleet-eval/l2-v2/candidate/<case-id>`;
- delivery refs: `fleet-eval/l2-v2/run/<task-number>-<run-id>`;
- repository Connection: absent and unnecessary;
- Codex route: `openai-codex/gpt-5.6-sol`, reasoning `high`;
- DeepSeek comparison route: frozen `deepseek-v4-pro`, reasoning `max`.

The remote base matches the v1 corpus base. L2-01 and L2-02 reuse the exact
frozen seeded-regression commits under new v2 refs. L2-05 reuses the exact
clean candidate. The initially reused L2-04 candidate failed its visible test
and was replaced before any L2V2-04 Claim with `badde97c...`, a corrected
visible-pass/hidden-fail seed. New Tasks, refs, PRs, Claims, Runs, and evidence
prevent lifecycle reuse.

## Six cases and expected paths

1. `L2V2-01`: nullable schedule PATCH semantics; execution, independent
   review, accept.
2. `L2V2-02`: compact Issue Thread ordering; execution, independent review,
   accept.
3. `L2V2-03`: oversized CLI response handling; execution, independent review,
   accept or one bounded correction, then accept.
4. `L2V2-04`: frozen defective Claim-stage candidate; import, independent
   review requests changes, correction execution, new review accepts.
5. `L2V2-05`: frozen clean schedule-validation candidate; import, independent
   review accepts with no blocking finding.
6. `L2V2-06`: contradictory oversized-response criteria; execution requests
   typed `decision_required` resolution before any branch or PR exists, then a
   new execution and independent review accept the predetermined conclusion.

Visible commands, allowed paths, hidden overlays, expected findings, and
criterion text remain behaviorally identical to the completed v1 corpus.
Hidden overlays and the answer key never enter an Agent workspace or remote
branch.

## Implementation slices

### 1. Provider-neutral v2 manifest and preflight

- add a private-manifest parser and a tracked non-secret case-definition
  template under `fleet/`;
- validate exact Project/Task/criterion versions, repository URL, refs, SHAs,
  Draft PR evidence, and absence of a required Connection;
- generate a complete dry-run effect inventory before provisioning;
- reject collisions with v1 Tasks, refs, PRs, and retained evidence.

### 2. Codex L2 runtime

- materialize a fresh credential-free clone under the host temp directory for
  every Agent stage;
- route the common Claim packet through `CodexHarnessAdapter`;
- preserve independent execution and review Sessions;
- run fixed verification and an external hidden overlay after Agent exit;
- audit allowed paths, exact base/delivery revisions, and clean read-only
  review trees;
- let only the coordinator push and create Draft PRs.

### 3. Pactline lifecycle coordinator

- use only the installed Pactline CLI for Claim work and settlement;
- treat Project/Task creation as local evaluation provisioning, not Fleet
  runtime behavior;
- persist Run/Claim/Session identity before Agent effects;
- reconcile uncertain responses from authoritative Pactline state;
- enforce new Claims after changes requests and typed resolution;
- never settle a proposal until independent Git and verification observations
  agree.

### 4. Comparison and report

- run all six cases through Codex/Codex first;
- select a bounded representative subset for DeepSeek/Codex and
  Codex/DeepSeek only after the primary cohort passes;
- retain the completed DeepSeek/DeepSeek v1 baseline unchanged;
- report correctness, issue evidence, false acceptance, false blocking,
  correction success, latency, tokens, tool errors, recovery, and human
  intervention;
- recommend Codex default, an explicit mixed policy, more qualification, or
  stop.

## Verification gates

- unit and contract tests for manifest, effect inventory, routes, lifecycle
  transitions, hidden evaluator boundary, and metric aggregation;
- complete Fleet typecheck/test/build and both Adapter doctor gates;
- Core build/test with Codex and DeepSeek runtime installations absent;
- preflight proves all exact remote refs and Pactline objects before first
  Claim;
- every final Task is `done` at the expected review cycle and exact delivery;
- all fixed and hidden verification passes;
- L2V2-04 is not falsely accepted and L2V2-05 is not falsely blocked;
- L2V2-06 has no branch, commit, or PR before resolution;
- no residual worktree, process group, recovery credential, or unclassified
  external artifact;
- old Bundle baseline remains 56 files at aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`.

## Authorized effects and stop conditions

Authorized effects are limited to the new local evaluation Project and Tasks,
the exact `fleet-eval/l2-v2/` refs, and individually recorded unmerged Draft
PRs in `wolfhead/pactline`. Never mutate or merge `main`, reuse Tasks #6-#11,
touch Game Design, or start M5.

Stop for owner direction if required authentication is unavailable, an exact
base/candidate ref has drifted, Project or Task state collides with an existing
cohort, a model requires control-plane credentials, hidden and visible
verification disagree in a way the fixed expectation does not classify, or a
remote action would exceed the recorded inventory.

## Completion evidence

- Codex/Codex Tasks #14 through #19 all reached `done` at the expected review
  cycle.
- DeepSeek/Codex Task #20 and Codex/DeepSeek Task #21 reached `done`.
- The owner-readable decision is recorded in
  `docs/superpowers/plans/2026-08-15-pactline-fleet-m4-report.md`.
