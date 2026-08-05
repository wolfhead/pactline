# Agent Conversation Evaluation

This harness evaluates how the first-party Pactline Agent converts an
explicitly mentioned Lark conversation into work. It does not scan group
messages autonomously: every scenario includes an `@Pactline`-equivalent
trigger that is normalized in the same way as a production Lark event.

The harness has no expected Task or fixed expected Task count. A conversation
may justify one Task, clarification, or no Task. It captures the Agent's actual
tool calls and lets a separate LLM judge assess fidelity, uncertainty,
actionability, and likely human editing cost.

Production permits at most one new Task per trigger. The Judge evaluates
whether the Agent selected the clearest boundary and surfaced other material
follow-up work, rather than recommending an impossible multi-Task mutation.

## Production parity and isolation

Evaluation reuses the production command classifier, system prompt, tool
definitions, Tool Ledger, and Eino Agent assembly. Only the side-effect
boundary is replaced:

- Lark history comes from an embedded synthetic scenario;
- Project and user search use in-memory fixtures;
- optional conversation defaults and bounded business context use the same
  serialized production input contract as a real Agent Run;
- current-conversation configuration reads and updates use an in-memory
  OpenAPI sandbox with the same model-visible tool contract;
- `create_task` captures a proposed Task instead of calling `/api/v1`;
- `respond` and `ask_clarification` capture messages without sending them; and
- Run, checkpoint, and tool-call records remain in memory.

This boundary is intentional. A successful evaluation must never create a
real Task, post a Lark message, or require a Pactline database.

## Running locally

List the embedded scenarios without model credentials:

```bash
go run ./cmd/agent-eval --list
```

Run one scenario with generation and LLM evaluation:

```bash
DEEPSEEK_API_KEY_FILE=/path/to/deepseek-key \
  go run ./cmd/agent-eval \
  --scenario immediate-and-long-term \
  --judge=true \
  --format markdown
```

Run the complete corpus:

```bash
make agent-eval
```

Supported configuration:

- `DEEPSEEK_API_KEY` or `DEEPSEEK_API_KEY_FILE` is required for model runs;
- `DEEPSEEK_BASE_URL` and `DEEPSEEK_MODEL` use the same meanings as the API;
- `AGENT_TENANT_TIMEZONE` defaults to `Asia/Shanghai`; and
- `AGENT_EVAL_JUDGE_MODEL` optionally overrides the Judge model. It defaults to
  `deepseek-v4-pro` even when generation uses the faster Flash model; evaluation
  should not rely on a Judge weaker than the system being assessed.

Image scenarios use a separately configured OpenAI-compatible multimodal
model because the DeepSeek work model is text-only. The default provider/model
is JieKou AI `gemini-2.5-flash-lite`. Configure its API key; base URL and model
remain optional overrides:

- `AGENT_VISION_API_KEY` or `AGENT_VISION_API_KEY_FILE`;
- `AGENT_VISION_BASE_URL` (default `https://api.jiekou.ai/openai`); and
- `AGENT_VISION_MODEL` (default `gemini-2.5-flash-lite`).

Without a vision API key, the real image reader still validates the image, but
the inspection result says image analysis is unavailable. It never falls back
to OCR.

The generator and Judge may use the same Pro model, but the report records this
limitation explicitly. Comparative or release evaluation should still add
human review and may use an independent strong Judge provider when available.

## Evaluation artifacts

Markdown is intended for product discussion. JSON is intended for retaining
and comparing runs:

```bash
go run ./cmd/agent-eval --scenario all --judge=true --format json
```

Each scenario result records:

- scenario and production prompt versions;
- model name, token usage, and duration;
- captured Task, clarification, or no-Task outcome;
- ordered tool trace with complete synthetic arguments, plus a sanitized
  terminal error; and
- judge verdict, concerns, risks, preferred action, and suggested direction.

For artifact scenarios, the Judge also receives a bounded evaluation-only
reference derived directly from the synthetic fixture. This reference lets it
check whether `inspect_artifact` and the resulting Task are factually faithful;
it is not shown to the conversion Agent and does not prescribe an expected
Task. Markdown reports include the natural-language `inspect_artifact` result
so a human can review the same evidence handoff.

Artifact scenarios materialize real synthetic Markdown, CSV, XLSX, and image
files. They call the same one-shot `inspect_artifact` description path as
production; only the resolver differs. Production resolves opaque artifact IDs
through Lark, whereas evaluation resolves IDs to embedded public-safe fixtures.
CSV and XLSX provide bounded leading samples to one description-model call;
the attachment model cannot request more ranges.

The corpus also includes a reaction-image control. A clear text-only Task with
an unrelated reaction image must complete without calling `inspect_artifact`.
Production Lark `sticker` messages are rejected before artifact registration;
ordinary images remain available only when surrounding text makes them
decision-relevant.

Judge output is qualitative evidence, not a correctness oracle. Product
decisions should compare the source conversation, conversion artifact, and
critique together.

## Adding scenarios

Add public-safe synthetic JSON under
`internal/agent/evaluation/testdata/scenarios/`. Scenarios should represent a
real conversation shape and preserve ambiguity; do not add a hidden golden
Task merely to make a test deterministic.

Every discussion-conversion scenario must:

- contain an explicit discussion-conversion trigger;
- place all source messages before the trigger;
- use unique synthetic message IDs;
- declare the visible Projects and users needed by Agent tools; and
- describe the evaluation focus without prescribing one correct output.

Scenarios that evaluate group configuration may add `conversation_configuration`
with a visible `default_project_number` and up to 4,000 characters of
`business_context`. The Judge receives this configuration with the rest of the
scenario and should assess whether it improved grounding without treating it as
a hidden expected answer.

Natural-language configuration scenarios should exercise
`get_current_conversation_configuration` and
`update_current_conversation_configuration`; do not encode fixed command
phrases or bypass the model.

Never commit screenshots, copied conversation text, names, account IDs,
project names, infrastructure details, or other company information. Rewrite
examples into synthetic conversations before adding them to the corpus.
