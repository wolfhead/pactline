# Pactline Fleet M3 Codex Adapter Implementation Plan

**Date:** 2026-08-15

**Status:** implemented and verified; M3 complete (2026-08-15)

## Objective

Implement Codex as Fleet's default quality-first Harness Adapter without
changing the Harness-neutral Core contract or exposing Pactline and repository
delivery credentials to model tools. M3 ends after one finite real read-only
qualification Run. It does not create Pactline Tasks, branches, pushes, PRs,
or begin the M4 comparison cohort.

## Spike decision

The official TypeScript SDK `@openai/codex-sdk` 0.147.0 exposes Thread start,
resume, streamed JSONL events, structured output, sandbox selection, working
directory, usage, and `AbortSignal`. It delegates each turn to the pinned
`@openai/codex` CLI.

Fleet will drive the matching pinned `@openai/codex` 0.147.0 CLI directly
instead of importing the SDK wrapper. This preserves the same Codex Agent
runtime and model quality while adding controls the SDK does not expose:

- `--ignore-user-config` and `--ignore-rules`, preventing user MCP servers,
  plugins, rules, and defaults from entering an unattended Fleet Run;
- deterministic stdin ownership and bounded stdout/stderr parsing;
- a private output-schema/result directory with enforced 0700/0600 modes;
- full process-tree termination and awaited reap;
- persisted Codex Threads for explicit resume by recorded Session ID;
- exact CLI version and argument provenance.

The real read-only spike passed with `gpt-5.6-sol`, reasoning `high`, a
schema-valid final result, a captured Thread ID, token usage, and a clean Git
tree. A deliberately supplied Pactline and GitHub sentinel was absent from the
model command environment. The initial WebSocket request timed out and Codex
successfully fell back to HTTPS, proving that reconnect/error events may be
non-terminal and must remain visible in bounded diagnostics.

## Model policy

- provider: `openai-codex`;
- model: `gpt-5.6-sol`, the current official Sol capability model;
- reasoning: `high` for M3, per owner direction;
- `max` is not used;
- `medium` is a later cost/latency comparison, not an automatic fallback;
- no silent model, reasoning, or Adapter fallback.

If a later installed Codex runtime does not expose this exact route, admission
fails and reports the selected version and route. Fleet does not silently use
an older model.

## Components

### Adapter-owned runtime

- `fleet/runtime/codex/package.json` and lockfile pin `@openai/codex` 0.147.0.
- Runtime installation is independent from Fleet Core and DeepSeek runtime.
- Fleet Core and Fake/Replay tests must compile and run with this runtime
  absent.

### Adapter

- `fleet/src/adapters/codex/codex-adapter.ts` implements probe, run, resume,
  cancel, prompt/result translation, fixed policy, and active Run ownership.
- `fleet/src/adapters/codex/wire.ts` owns the CLI process, JSONL parser,
  output bounds, stderr sanitization, timeout/cancel termination, and actual
  process-tree reap.
- Codex-native event and CLI types remain private to the Adapter.

### Process and credential boundary

The Codex process receives only the variables required for local ChatGPT/API
authentication, transport, locale, temp storage, and executable discovery. It
never receives Pactline, GitHub, or GitLab credentials. Every Codex invocation
forces:

- `--ignore-user-config` and `--ignore-rules`;
- approval policy `never`;
- multi-agent disabled for the single-Agent M3 contract;
- web search and workspace network disabled;
- shell environment `inherit = none` with an explicit non-secret PATH/locale;
- the exact Fleet sandbox and working directory;
- the common Fleet JSON Schema and a private final-result path.

Pactline, GitHub, and GitLab credentials are removed from the Codex process
environment. Codex provider authentication remains a trusted Harness runtime
dependency. The current 0.147.0 read-only sandbox does not provide a portable
filesystem read allowlist, and the owner explicitly deferred a separate auth
broker or OS-level wrapper for M3. Prompt policy still forbids reading outside
the supplied workspace.

### Result and event translation

- The first `thread.started` event becomes Fleet's runtime Session identity
  before any command or file effect.
- `item.started`, `item.updated`, and `item.completed` become bounded common
  events without raw reasoning or unbounded command output.
- reconnect/error events are retained as non-terminal diagnostics unless a
  `turn.failed` or failed process exit establishes terminal failure.
- `turn.completed` supplies token usage.
- only the private `--output-last-message` JSON document is accepted as the
  terminal proposal; intermediate agent messages are not terminal results.
- Fleet parses and validates the common proposal again before returning it to
  Core.

### Qualification fixture

The DeepSeek and Codex live L1 Runs use the same deterministic review fixture
and scoring semantics so later comparison is meaningful. Shared fixture and
scoring code will be provider-neutral; Adapter-specific runners retain their
own model, event, and evidence handling.

## Verification

### Offline and installed-runtime tests

- command construction pins model, reasoning, sandbox, config isolation, and
  result paths;
- JSONL translation covers Thread start, command/file/MCP events, reconnects,
  completion, failure, malformed frames, output bounds, and usage;
- structured output rejects missing, invalid, intermediate-only, or mismatched
  proposals;
- read-only and workspace-write shared Adapter contracts pass;
- environment sentinels prove control-plane and provider credentials are not
  visible to model commands;
- AbortSignal, explicit cancel, deadline, and shutdown reap the complete process
  tree;
- resume uses the recorded Thread ID with the same frozen Run policy;
- Core builds and tests with `runtime/codex/node_modules` absent.

### Finite live gate

Run one real read-only review against the shared L1 fixture. Require:

- `gpt-5.6-sol` with reasoning `high`;
- schema-valid review proposal and captured Thread ID;
- all seeded issues recalled with bounded false positives;
- fixed tests pass and agree with the proposal;
- exact Git revision/tree remains unchanged;
- no sensitive environment name or credential-shaped evidence;
- private bounded evidence and no residual Codex process.

## Files and commands

Expected implementation paths:

- `fleet/runtime/codex/*`;
- `fleet/src/adapters/codex/*`;
- `fleet/src/commands/codex-doctor.ts`;
- `fleet/src/evaluation/codex-l1-live.ts` and CLI entry;
- provider-neutral shared L1 fixture helpers under `fleet/src/evaluation`;
- focused tests under `fleet/tests/adapters` and `fleet/tests/evaluation`;
- `fleet/package.json`, `fleet/README.md`, `Makefile`, architecture and master
  runbook updates.

Primary gates:

```bash
make pactline-fleet-codex-test
make pactline-fleet-check
make pactline-fleet-deepseek-test
make pactline-fleet-codex-l1
```

## Stop conditions

Stop for owner direction if Codex requires Max, user configuration/plugins,
Pactline or provider credentials, danger-full-access, remote writes, or a Core
contract weaker than the M1/M2 boundary. Do not proceed to M4 automatically.

## Verified outcome

- all focused Adapter, wire, schema, type, and doctor checks passed;
- the shared real L1 review passed with `gpt-5.6-sol/high`;
- issue recall was 3/3 with zero false positives;
- fixed verification passed and the Git tree remained unchanged;
- the final accepted Run emitted 18 normalized events and left no residual Run
  process;
- M4 was not started.
