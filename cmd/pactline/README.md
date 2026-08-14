# Pactline CLI v0.1

`pactline` is a single, CGO-free executable for people and Agents executing
Pactline Tasks. It has no Python, Node.js, Docker, or repository dependency.
CLI v0.1 covers execution from assigned Task discovery through
`in_review.available`; Code Review and final acceptance remain in the Web UI.

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
pactline task list
pactline task show 142
pactline task claim 142 --task-version 4
pactline claim show <claim-id>
pactline claim progress <claim-id> --message "Focused tests pass."
pactline claim verify <claim-id> <criterion-id> \
  --task-version 4 --criterion-revision 2 --outcome passed \
  --evidence "go test ./... passed"
pactline claim submit <claim-id> --task-version 4 --message "Delivery update"
pactline claim complete <claim-id> --task-version 4 --message "Ready for review"
```

`submit` is repeatable and keeps execution owned. `complete` explicitly ends
the execution Claim and moves the Task to `in_review.available`. Neither an MR
state nor a submission changes Task phase implicitly.

Use these built-in explanations without consulting external documentation:

```bash
pactline --help
pactline help workflow
pactline help identity
pactline help output
pactline claim complete --help
```

## Explicit targeting and concurrency

Claim creation starts from a Task because no Claim exists yet. The response
prints the Claim ID. Every later mutation requires that ID; the CLI never
searches for a “current Claim” by Session ID. The server derives Task identity
and current Claim version. Commands making a Task lifecycle decision still
require `--task-version`, representing the Task version the caller inspected.

## Output and diagnostics

Text output is the default. `--json` emits exactly one JSON document on success
or failure. `-v`/`--verbose` writes redacted method, route, status, duration,
request ID, and ETag diagnostics to stderr. It never logs credentials, bodies,
evidence, raw headers, or response bodies.

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
