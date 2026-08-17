# Pactline Fleet M5.4 Acceptance Report

**Date:** 2026-08-17

**Decision:** Go for bounded local trials. This is not production-readiness
approval.

## Accepted capability

The resident Pactline Fleet can currently discover assigned work, create and
continue Claims, run either supported Harness Adapter, verify and publish a
local Git delivery, settle execution and review, recover one representative
non-resumable interruption, and expose the result through its local read-only
Operations Console without manual database or workflow repair.

## Evidence

| Scenario | Authoritative result | Fleet result |
|---|---|---|
| Replay deterministic delivery | Task #24 reached `in_review.available`; one completed execution Claim | one completed Run at `settlement_observed`; exact local Git revision verified |
| Replay correction workflow | Task #26 reached `done`; four completed Claims | execution, changes-request review, correction, and acceptance review all completed |
| Bounded restart | Task #27 returned to available after recovery and then reached `in_review` | interrupted Run released before Agent effect; replacement Run completed; one delivery only |
| DeepSeek/Codex live | Task #30 reached `done`; two completed Claims | DeepSeek Pro/max execution and Codex high review completed with two runtime Sessions |
| Codex/Codex live | Task #31 reached `done`; two completed Claims | Codex high execution and review completed with two runtime Sessions |

All delivery scenarios used the committed repository revision recorded by
preflight, disposable workspaces, a local bare Git remote, a temporary
Pactline Token, and no GitHub or GitLab credential. Temporary Tokens were
revoked in cleanup paths.

## UI observation

Real-browser inspection confirmed:

- Overview reported Ready, one healthy Project-bound Fleet, zero active Runs,
  and the two recent terminal outcomes;
- Fleet detail showed DeepSeek Pro/max for execution and Codex high for review,
  healthy discovery, and both completed Runs;
- Run detail showed Task, Claim, Adapter Session, frozen configuration,
  workspace revision, causal timeline, and accepted settlement;
- System reported Pactline and registry Ok, both Adapters Ok, and their actual
  capability differences, including Codex Session resume and DeepSeek
  non-resumability;
- the browser console reported zero errors.

## Defects found and fixed

1. An empty `pactline claim list --json` encoded `items` as `null`. It now
   emits `[]`, preserving the CLI list contract.
2. Pactline rate limiting was not actionable through the CLI Adapter. The CLI
   now exposes bounded retry delay and Fleet retries with the same idempotency
   key up to three times.
3. Codex strict structured output rejected shell-command literals containing
   quotes. The Adapter transport schema now accepts command text while Fleet
   Core still requires an exact fixed-command match.
4. The result schema allowed models to report extra verification commands that
   Fleet had not observed. Its verification array cardinality is now frozen to
   the coordinator-owned command set.

## Operator command

Run the complete finite gate with:

```bash
make pactline-cli
cd fleet
npm run m5-4:acceptance
```

Individual modes are available as `m5-4:preflight`, `m5-4:deterministic`,
`m5-4:correction`, `m5-4:restart`, and `m5-4:live`. Evidence is private under
`fleet/.fleet/m5-4-usability/`.

## Remaining boundary

M5.4 deliberately does not certify exhaustive crash recovery, two distributed
Fleet Services competing for one Project, SQLite or disk faults, 24-hour soak,
live GitHub/GitLab failure, production retention, or production sandbox and
credential isolation. Those remain M6 qualification before any production or
real-work pilot.
