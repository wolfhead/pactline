# Pactline CLI

`pactline` is a single, CGO-free executable for people and Agents executing and
reviewing Pactline Tasks. It has no Python, Node.js, Docker, or repository
dependency. The CLI covers the complete Claim workflow from assigned execution
discovery through review acceptance or a request for changes, plus the Thread
discussion and explicit Issue resolution needed after a Claim blocks.

## Install

Download the archive for your operating system and architecture, verify it
against `pactline_checksums.txt`, extract it, and place `pactline` on `PATH`.

```bash
sha256sum -c pactline_checksums.txt --ignore-missing
tar -xzf pactline_0.1.0_linux_amd64.tar.gz
install pactline_0.1.0_linux_amd64/pactline /usr/local/bin/pactline
pactline version
```

Release archives are provided for Darwin and Linux on `amd64` and `arm64`.

## Configure safely

Create a personal Token with `work:execute`, then pipe it through stdin:

```bash
printf '%s' "$PACTLINE_BOOTSTRAP_TOKEN" | \
  pactline config set --server https://pactline.example --token-stdin
```

The sole profile is `~/.pactline/config.json`; its directory is mode `0700`
and its file is mode `0600`. The CLI refuses an existing config readable by
group or others. It never accepts a `--token` flag or prints the Token.

Automation can instead set:

```text
PACTLINE_SERVER
PACTLINE_TOKEN
PACTLINE_CLIENT_KIND
PACTLINE_SESSION_ID
```

`CODEX_THREAD_ID` is the final Session ID fallback. Session ID is non-secret
audit provenance, not Claim ownership. The exact Token owns the Claim.

## Quick start

```bash
pactline doctor
pactline capabilities --json
pactline task list --stage execution --project 12 --limit 50
pactline task show 142 --compact
pactline task claim 142 --task-version 4
pactline claim show <claim-id> --compact
pactline claim progress <claim-id> --message "Focused tests pass."
pactline claim verify <claim-id> <criterion-id> \
  --task-version 4 --criterion-revision 2 --outcome passed \
  --evidence "go test ./... passed"
pactline claim submit <claim-id> --task-version 4 --message "Delivery update"
pactline claim change link <claim-id> \
  --url https://github.com/owner/repository/pull/42 \
  --task-version 4
pactline claim change link <claim-id> \
  --url https://gitlab.example/team/repository/-/merge_requests/43 \
  --task-version 5
pactline claim change list <claim-id>
pactline claim complete <claim-id> --task-version 4 --message "Ready for review"

pactline task list --stage review --project 12 --limit 50
pactline task show 142 --compact
pactline task claim 142 --stage review --task-version 5
pactline claim show <review-claim-id> --compact
pactline claim verify <review-claim-id> <criterion-id> \
  --task-version 6 --criterion-revision 2 --outcome passed \
  --evidence "Reviewed the frozen code change and reran the acceptance checks"
pactline claim accept <review-claim-id> \
  --task-version 6 --message "Acceptance contract satisfied"
```

`submit` is repeatable and keeps execution owned. `complete` explicitly ends
the execution Claim and moves the Task to `in_review.available`. Neither a
code-change state nor a submission changes Task phase implicitly.

The same `claim change` commands handle GitHub Pull Requests and GitLab Merge
Requests. The server's configured Repository Connections determine which
providers and repositories are available; the CLI does not infer or store
provider credentials.

A reviewer claims the same Task with `--stage review`. This flag is a local
safety assertion: Pactline still derives the actual Claim stage from the Task's
authoritative phase. `claim verify` records `acceptance` evidence for a Review
Claim because the server derives purpose and review cycle; the caller never
supplies either value. After review, use exactly one explicit outcome:

```bash
pactline claim request-changes <review-claim-id> \
  --task-version 6 --message "The error path still lacks coverage"
pactline claim accept <review-claim-id> \
  --task-version 6 --message "Acceptance contract satisfied"
```

`request-changes` ends the Review Claim and returns the Task to
`in_progress.available`. `accept` ends the Review Claim and moves the Task to
`done`. A reviewer may instead use `claim release` to leave the Task in
`in_review.available`, or `claim request-resolution` to open a blocking Issue
Thread while preserving the review phase.

## Blocking Issue collaboration

`claim request-resolution` ends the active Claim and opens a typed Issue
Thread. The old Claim is intentionally no longer a valid continuation handle.
Use the Task and Thread identities returned by the server:

```bash
pactline claim request-resolution <claim-id> \
  --task-version 7 --issue-type decision_required \
  --message "Choose the release strategy before implementation continues"

pactline task show 142 --compact
pactline task threads 142
pactline thread items <issue-thread-id> --limit 50
pactline thread post <issue-thread-id> \
  --message "The staged option keeps rollback available" \
  --reply-to <item-id> --mention <user-id>

pactline issue resolve 142 <issue-thread-id> \
  --task-version 8 --thread-version 2 \
  --message "Use the staged rollout"

pactline task show 142 --compact
pactline task claim 142 --task-version 9
```

Resolution returns the same execution or review phase to `available`; it does
not revive the old Claim or claim work for the resolver. `thread edit` and
`thread delete` require the explicit Item version and retain the server's
message-ownership and tombstone rules. Complete Thread history is read one
bounded page at a time with `thread items`; use its returned `next_cursor` for
the next page.

`task list` defaults to assigned execution work. `--stage review` lists visible
`in_review.available` work without treating the Task assignee as a reviewer
assignment. `--project` and `--limit` are server-side bounds; results are
ordered by Task number.

`task show --compact` and `claim show --compact` use one bounded server request.
They include recent Main Thread Items, the active Issue Thread when present,
acceptance context, and delivery evidence. Use `--thread-items-limit 1..100` to
change the per-Thread bound. The Claim packet reports checks for that exact
Claim. Omit `--compact` only when complete historical Thread reads are needed.

Use these built-in explanations without consulting external documentation:

```bash
pactline --help
pactline help workflow
pactline help identity
pactline help output
pactline claim complete --help
pactline claim request-changes --help
pactline claim accept --help
pactline claim change --help
pactline thread --help
pactline issue resolve --help
```

## Explicit targeting and concurrency

Claim creation starts from a Task because no Claim exists yet. The response
prints the Claim ID. Every later mutation requires that ID; the CLI never
searches for a “current Claim” by Session ID. The server derives Task identity
and current Claim version. Commands making a Task lifecycle decision still
require `--task-version`, representing the Task version the caller inspected.

An external Harness should retain the Token and Claim ID in its parent
orchestrator and expose only typed Pactline operations to repository workers.
The repository worker does not need the Pactline Token. `pactline capabilities
--json` is offline and reports the stable protocol and features implemented by
the installed binary. This release reports protocol 2 and capability
`repository_code_change_links`; the removed protocol-1 `claim mr` command is
not retained as an alias.

## Output and diagnostics

Text output is the default. `--json` emits exactly one JSON document on success
or failure. `-v`/`--verbose` writes redacted method, route, status, duration,
request ID, and ETag diagnostics to stderr. It never logs credentials, bodies,
evidence, raw headers, or response bodies.

Successful JSON output keeps the `ok` and `data` fields. When available it also
includes `meta.request_id`, `meta.etag`, and the exact caller-provided or
CLI-generated `meta.idempotency_key`. This is the key to reuse only after an
uncertain mutation outcome.

Exit codes are: `0` success, `2` usage/configuration, `3` authentication or
authorization, `4` domain/version conflict, and `5` network/provider/server
failure. Mutations are not retried automatically. After an uncertain outcome,
inspect state and repeat the exact command with the reported
`--idempotency-key` only when appropriate.

## Build from source

```bash
CGO_ENABLED=0 go build -trimpath -o pactline ./cmd/pactline
```

Pactline is licensed under the Apache License 2.0; see `LICENSE` in the release
archive.
