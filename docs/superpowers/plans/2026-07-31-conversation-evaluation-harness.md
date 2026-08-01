# Conversation Evaluation Harness Implementation Plan

**Status:** Approved direction; implementation in progress

## Outcome

Build a development-only harness that runs Pactline's current first-party Agent
prompt and Eino tool loop against explicit-mention conversation scenarios while
replacing Lark and `/api/v1` effects with an in-memory sandbox. Capture the Task
the current Agent would create, any clarification it would ask, its final
response selection, tool trace, latency, and token usage. Evaluate that output
with a separately versioned LLM judge instead of a fixed expected Task.

## Boundaries

- Explicit `@Pactline` trigger only.
- No autonomous group scanning or production notification.
- No production database, Lark, OpenAPI, or Task mutation.
- Reuse the production prompt, production model adapter, production tool
  schemas, Tool Ledger, and Agent loop.
- Scenario files contain anonymized or synthetic public-safe conversations.
- A scenario has no golden Task and no fixed expected Task count.
- Deterministic tests enforce harness contracts; LLM evaluation remains
  non-deterministic and records model/prompt versions.

## Current workflow facts

The production flow is:

```text
explicit Lark mention
  -> normalized text/post event
  -> encrypted Agent Run input
  -> queued PostgreSQL Run
  -> production system prompt and tool loop
  -> optional preceding-message fetch
  -> search Projects/users
  -> zero or one create_task call, or clarification
  -> fixed reply through the Outbox
```

The current contract forces multiple plausible Tasks into one clarification and
has no TaskDraft. The Harness must preserve those limits initially so its first
report is a baseline of the actual product rather than a proposed replacement.

## Implementation phases

### 1. Extract reusable production assembly

- Expose the versioned production system prompt through a small runtime API.
- Extract the untrusted initial-query encoder so production and evaluation use
  identical command/context serialization.
- Keep the production Worker behavior unchanged.

### 2. In-memory sandbox

Add a sandbox that implements the existing tool dependencies:

- bounded conversation context adapter;
- Project and user lookup client;
- Task read stubs required by the current tool interface;
- synthetic `create_task` receipt without a real mutation;
- Run attachment and context accounting;
- Tool Call claim, completion, failure, and evidence replay.

Use `tools.NewSet` rather than reimplementing model-visible tools.

### 3. Scenario model and initial corpus

Add versioned JSON scenarios with:

- scenario ID and short business context;
- explicit trigger message;
- preceding messages with sender roles and timestamps;
- synthetic active Projects and users;
- optional notes visible only to the judge.

Initial cases:

1. a problem and possible solution buried before later unrelated discussion;
2. immediate diagnostic action plus a longer-term fix;
3. a technical brainstorm that may need a discovery/design TODO rather than an
   implementation Task;
4. a mature Project whose work is still discussed primarily in IM;
5. a discussion that should plausibly result in no immediate Task.

### 4. Conversion runner

Run one scenario with the selected DeepSeek model and capture:

- prompt/model/scenario versions;
- created Task input and synthetic receipt, if any;
- clarification question and candidates, if any;
- terminal response selection;
- ordered tool trace with complete synthetic arguments and results;
- token usage, duration, and terminal error.

Emit machine-readable JSON and a concise Markdown report.

### 5. LLM judge

Use a separately versioned judge prompt and structured terminal tool. The judge
receives the scenario and Conversion artifact, but no expected Task. It assesses:

- conversation fidelity;
- task-boundary judgment;
- treatment of immediate versus long-term work;
- preservation of brainstorming and uncertainty;
- unsupported certainty or invented ownership;
- actionability and likely human editing cost;
- whether clarification or no Task would be more useful.

The judge emits a verdict, strengths, concerns, risks, and suggested direction.
Support a different judge model through configuration; record when generation
and judgment use the same model.

### 6. Development command and verification

- Add a command that lists scenarios, runs one or all scenarios, optionally
  invokes the judge, and writes only to stdout or an explicit output path.
- Read model credentials from the same environment/file convention as the
  server without printing them.
- Keep the command local to the development checkout; it does not require an
  application deployment or access to the Pactline database.
- Unit-test scenario validation, sandbox non-mutation, context boundaries,
  capture, judge result validation, and production prompt reuse.
- Run focused Agent tests and the broader Go suite.

## Acceptance criteria

1. The Harness can run the production Agent prompt against a synthetic explicit
   mention without PostgreSQL, Lark, or HTTP business mutations.
2. The sandbox exposes the same model-visible tool names and schemas as
   production.
3. A captured Task exactly reflects the `create_task` arguments selected by the
   production Agent loop.
4. A clarification is captured without requiring an actual Lark reply.
5. Scenarios do not contain expected Tasks or golden output text.
6. The LLM judge produces structured criticism and never mutates Pactline.
7. Every report records scenario, generation model, production prompt, judge
   model, and judge prompt versions.
8. Synthetic scenarios and emitted reports are safe for the public repository;
   real conversation bodies are not committed.
