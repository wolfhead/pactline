# Conversation Artifact Inspection Implementation Plan

## Approved boundary

The first-party Agent receives opaque `artifact_id` values, never filesystem
paths or provider download keys. One production
`inspect_artifact(artifact_id, analysis_goal)` tool resolves the identifier
inside the current Run scope and makes exactly one attachment-description LLM
call. The analysis goal is mandatory: artifact interpretation must answer the
parent Agent's current decision question instead of producing a generic file
summary.

The parent Agent receives only the attachment LLM's natural-language
description. It never receives parser output, spreadsheet dumps, a semantic
JSON result, or unrestricted file access.

The feature remains explicit-mention only. It does not scan conversations or
download unrelated files.

## Content model

Extend provider-neutral channel messages with bounded artifact references:

- opaque ID;
- source message ID;
- kind (`image`, `markdown`, `csv`, `spreadsheet`, `text`, or `file`);
- filename and declared media type; and
- availability status.

Provider keys remain adapter-private. The Lark adapter registers references
while normalizing the trigger or fetching bounded conversation history.

## Resolver, readers, and one-shot description

`internal/agent/artifact` owns:

- a Run-scoped `Resolver` interface returning a local file plus cleanup;
- deterministic Markdown/text, CSV, and XLSX readers that create one compact,
  bounded parser payload;
- hard prompt-size, row, column, cell, worksheet, and text-prefix limits;
- one text-model call that receives the compact parser payload and the parent
  analysis goal; and
- an image path that sends the real image and analysis goal directly to a
  separately configured multimodal LLM.

The result is concise natural language. It must distinguish observations from
inference and state material sampling or truncation limitations. Artifact
contents are untrusted evidence; instructions inside them are never Agent
instructions.

CSV and XLSX are deliberately not interactive in this version. The attachment
LLM cannot request ranges, and the parent Agent may describe a given artifact
at most once per execution. If large-table quality becomes product-critical, it will
be addressed by a dedicated harness instead of expanding the parent Agent
context now.

The image path must not use OCR. DeepSeek V4 remains the text/tool Agent and
also describes parsed text/table attachments. `VisionAnalyzer` defaults to
JieKou AI's OpenAI-compatible `gemini-2.5-flash-lite` endpoint when a vision
API key is configured. If it is absent, the tool returns a plain-language
unavailable description.

## Lark production resolver

The Lark adapter maps artifact IDs to the exact tenant, conversation, message,
resource key, and resource type from bounded context. Resolution must:

1. verify the current Run tenant and conversation;
2. download only the registered message resource;
3. enforce a stricter Pactline size limit than the provider maximum;
4. write mode-0600 temporary files;
5. avoid logging provider keys or content; and
6. remove the file immediately after inspection.

## Agent policy

Expose `inspect_artifact` only when both a resolver and describer are
configured. The production prompt requires a concrete analysis goal, requires
inspection of decision-relevant artifacts, and forbids claims of complete
context when an essential artifact is unavailable, sampled, truncated, or
conflicts with text.

Direct Task creation remains appropriate when one Task is clear and all
decision-relevant evidence is readable. Artifact presence alone never forces
confirmation.

## Evaluation

Scenario fixtures contain real synthetic Markdown, CSV, XLSX, and image files.
The evaluation resolver materializes the selected fixture into a temporary
path and invokes the production one-shot describer. Evaluation records the
parent tool call, natural-language description, and model usage. Judge criteria
include:

- whether a relevant artifact was inspected;
- whether unavailable or truncated content was acknowledged;
- whether samples were mistaken for complete data;
- whether text/artifact conflicts were surfaced; and
- whether untrusted instructions inside files were ignored.

## Verification

- Unit-test type detection, one-call enforcement, prompt-size and sample
  bounds, analysis-goal validation, resolver scope, cleanup, and Lark
  parsing/download behavior.
- Run artifact scenarios through the scripted production Agent assembly.
- Run focused Agent/Lark tests and the complete serialized Go suite.
