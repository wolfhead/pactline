# Pactline Fleet M4 L2 v2 Qualification Report

**Date:** 2026-08-15

**Status:** complete

## Decision

Use Codex `gpt-5.6-sol/high` as Fleet's default execution, correction, review,
and resolution-analysis Adapter. Retain DeepSeek `deepseek-v4-pro/max` as an
explicit opt-in sibling Adapter. Do not implement automatic fallback.

This decision qualifies the Adapter architecture and the Codex default for the
next Fleet milestone. It does not authorize M5, production scheduling, merging
evaluation pull requests, or Game Design work.

## Results

| Task | Route | Case | Result | Review cycle | Final delivery |
| --- | --- | --- | --- | ---: | --- |
| #14 | Codex/Codex | L2V2-01 | passed | 1 | PR #40, `377bc8a` |
| #15 | Codex/Codex | L2V2-02 | passed | 1 | PR #41, `e9eea39` |
| #16 | Codex/Codex | L2V2-03 | passed | 2 | PR #43, `8178d99` |
| #17 | Codex/Codex | L2V2-04 | passed | 2 | PR #44, `d63e0ee` |
| #18 | Codex/Codex | L2V2-05 | passed | 1 | PR #39, `0babaf9` |
| #19 | Codex/Codex | L2V2-06 | passed | 1 | PR #45, `678e212` |
| #20 | DeepSeek/Codex | L2V2-03 | passed | 2 | PR #47, `a734295` |
| #21 | Codex/DeepSeek | L2V2-04 | passed | 2 | PR #48, `46e8a90` |

All eight Tasks reached authoritative `done`. The six-case Codex/Codex primary
cohort had zero false acceptance and zero false blocking. L2V2-06 opened and
resolved a typed Issue before any delivery branch or pull request existed.

The mixed cohort showed that DeepSeek can be used behind the same Adapter
contract:

- DeepSeek executed L2V2-03, Codex found a real defect, DeepSeek corrected it,
  and a new Codex Reviewer accepted it.
- DeepSeek rejected the seeded L2V2-04 defect, Codex corrected it, and a new
  DeepSeek Reviewer accepted it. There was no false acceptance or false
  blocking in this control.

The frozen DeepSeek/DeepSeek v1 baseline remains unchanged: Tasks #6 through
#11 are done and Draft PRs #31 through #37 remain the historical evidence.

## Performance and usage

The new cohort recorded 20 Codex stage Sessions and four DeepSeek stage
Sessions. Mean observed stage duration was approximately 81 seconds for Codex
and 318 seconds for DeepSeek. Mean output was approximately 4,550 tokens per
Codex Session and 18,978 tokens per DeepSeek Session.

These are capability-baseline observations, not a price comparison. The two
Harnesses report cached input differently, the sample sizes differ, and M4
intentionally used the strongest configured models. Cost optimization remains
deferred.

## Quality observations

- Codex completed all six primary lifecycle paths and correctly accepted the
  clean control.
- Codex Review found a malformed-but-valid JSON diagnostic leak that the
  original L2V2-03 hidden overlay did not cover. The correction passed a new
  independent Review.
- DeepSeek found the hidden stage/outcome authorization defect without seeing
  the hidden overlay and later accepted the correction.
- Both Codex and DeepSeek needed one correction cycle on L2V2-03. The result
  supports independent review rather than same-Agent self-certification.
- DeepSeek's mixed Sessions were materially slower and emitted materially more
  output than Codex in this bounded sample. Its correctness was acceptable,
  but latency and inner-loop observability should improve before default use.

## Coordinator and corpus incidents

The following incidents are excluded from model-quality scoring but retained
as Fleet engineering evidence:

1. Public HTTPS Git fetches intermittently stalled. Agent workspaces now read
   exact frozen refs from a private local mirror; the coordinator still pushes
   validated delivery refs to GitHub and creates Draft PRs there.
2. Codex `workspace-write` blocked localhost listeners used by Go `httptest`.
   M4 execution now uses `danger-full-access` for Codex while Fleet Core keeps
   allowed-path, Git observation, fixed verification, hidden verification, and
   settlement gates authoritative.
3. The original L2V2-04 seed failed its visible test. Before Task #17 created a
   Claim, it was replaced with `badde97c...`, which passes the visible test and
   fails only the hidden full matrix at `execution/changes_requested`.
4. Candidate-import idempotency originally omitted Task number and replayed a
   prior response when the comparison reused L2V2-04. Task #21 remained ready
   with no Claim; the key now includes Task number.
5. A validation rule initially assumed every valid `request_changes` must make
   the hidden overlay fail. It now preserves independently evidenced findings
   even when the bounded hidden overlay passes.

## Evidence and limits

- Machine report: `fleet/.fleet/l2-v2/report.json` (private, mode 0600).
- Run evidence: `fleet/.fleet/l2-v2/runs/` (private).
- Primary Tasks: #14 through #19.
- Mixed Tasks: #20 and #21.
- Evaluation pull requests remain Draft and none is authorized for merge.
- Repository Connection was absent and unnecessary.
- The remote base and `main` were not mutation targets.

M4 proves a finite, single-coordinator qualification workflow. Durable restart
reconciliation, continuous scheduling, multi-run locking, production sandbox
policy, and operational retention remain M5/M6 work.
