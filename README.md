# Pactline

Pactline is a project and task management platform built for work shared by
people and AI agents. Long-lived Projects provide context, Milestones define
delivery windows, and structured Tasks carry expected outcomes and verifiable
acceptance criteria.

> **Public repository:** treat every tracked file, commit, branch, issue, and
> artifact as public. Follow the
> [repository privacy policy](docs/repository-policy.md) and never include
> company information, personal information, production data, or credentials.

## Product model

- A Project is a long-lived workspace.
- A Project may have multiple concurrently active Milestones.
- Projects are private workspaces with multiple administrators and ordinary
  members; the creator is the initial administrator.
- Every Task belongs to one Project; a Task without a Milestone is in the
  Project Backlog.
- Task creation requires context and an expected result. Acceptance criteria
  are optional, revisioned, and independently checkable.
- Tasks support one-level parent-child grouping, cycle-free same-Project
  dependencies, optional schedules, and shared List/Gantt collection views.
- People use browser sessions. External Agents use personal scoped tokens;
  Pactline's built-in Lark Agent uses a short-lived initiating-user delegation.
  Both operate through the same contract-first `/api/v1` API.
- Tasks explicitly opt into external execution. A real Codex session claims
  one assigned eligible Task, records a separate Agent conversation,
  repeatable delivery updates, and acceptance evidence, then explicitly
  completes execution to make the Task available for review.
- Tasks support private attachments and threaded comments with structured
  Project-member mentions. Reliable notification intents flow through a
  PostgreSQL outbox and RabbitMQ without coupling comments to one IM provider.
- Access requests and approvals use the same event contract and currently
  deliver fixed Lark DM cards; a future inbox can consume the same events.
- Production identity uses single-tenant Lark OAuth with one Administrator;
  new Members require Administrator approval before accessing the product.
- Administrative impersonation is read-only and keeps the Administrator as the
  audited actor.

The durable entity semantics and invariants are documented in
[`docs/product-model.md`](docs/product-model.md).

## Technology

- Go 1.24 HTTP API
- PostgreSQL 16
- RabbitMQ 4
- React 18, TypeScript, Vite, Tailwind CSS, and Radix UI
- ogen-generated OpenAPI transport
- CloudWeGo Eino with DeepSeek for the built-in Agent Harness
- Vitest and Playwright

## Local development

Requirements:

- Go 1.24 or later
- Node.js 22 or later
- Docker with Compose

Start the database, API, and frontend in separate terminals:

```bash
make up
make run
make web-install
make web-dev
```

Open <http://localhost:5173>. Local development uses the explicit development
identity provider; production rejects it at startup.

For a production-equivalent local container run:

```bash
make stack-up
make stack-logs
make stack-down
```

The full deployment model, production configuration, backups, upgrades, and
rollback constraints are documented in
[`docs/operations/deployment.md`](docs/operations/deployment.md). Never commit
populated environment files or provider credentials.

## Verification

```bash
make test
make web-test
make web-build
make openapi-check
make e2e
```

`make test` and `make e2e` start PostgreSQL through Docker Compose. Go
integration tests share one database and must not run concurrently.

## Work API

`/api/v1` is the supported work API for Projects, Milestones, Tasks, comments,
labels, acceptance criteria, checks, activity, and active-user references.
The canonical contract is [`api/openapi.yaml`](api/openapi.yaml); authenticated
users can read it at `/api/openapi.yaml` and browse it at `/api-docs`.

Browser mutations require same-origin session and CSRF credentials. External
Agent requests use personal Bearer Tokens with `work:read`, least-privilege
`work:execute`, or `work:write`.
The built-in Agent receives a short-lived internal delegation for its Run.
Both use the documented idempotency and optimistic-concurrency headers.

```bash
make openapi-generate
make openapi-check
```

Do not edit `internal/api/v1generated` manually.

### External Agent worker

Pactline ships a standalone CGO-free CLI for the complete execution and review
Claim flow, from Task discovery through change requests or final acceptance.
Its independent installation and command guide is
[`cmd/pactline/README.md`](cmd/pactline/README.md).

External Harnesses can inspect `pactline capabilities --json`, discover
stage-aware bounded queues, and read one server-aggregated compact work packet
for a Task or explicit Claim. Pull Request and Merge Request delivery is linked
through Claim-centric CLI commands; repository workers do not need the Pactline Token.
Reviewers claim `in_review.available` work explicitly, record server-derived
acceptance evidence, and either request changes or accept the Task. After a
Claim opens a blocking Issue, the same CLI can inspect and discuss its Thread,
resolve it with explicit Task and Thread versions, and reclaim the available
phase without inferring or reviving the ended Claim.

System Administrators can create encrypted, repository-scoped, read-only
Connections for GitLab or GitHub, including GitHub Enterprise Server. Project
Administrators authorize one by pasting its repository URL. GitHub uses a
fine-grained personal access token limited to the selected repository with
repository metadata and Pull Request read access. Pactline performs no provider
writes, polling, CI queries, reviews, or merges.

Create an executor-scoped personal Token in the account UI, install the CLI or
the `pactline-work` Codex skill, and keep developer-specific Project/repository
mapping outside this repository.

The standalone CLI uses `~/.pactline/config.json` with mode `0600`; configure it
with `pactline config set --server ... --token-stdin`. The Codex skill retains
its own helper configuration and optional workspace guidance:

```text
~/.pactline/
├── config.json   # mode 0600; standalone CLI configuration
├── .env          # mode 0600; Codex skill helper configuration
├── pactline.md   # optional personal Agent instructions
└── projects.md   # optional free-form local Project/repository mapping
```

Claim ID is the explicit continuation handle. `CODEX_THREAD_ID` is sent as
request provenance and may change when another authorized subagent continues
the same Claim with the exact same Token. Local repository layouts remain
user-owned and are intentionally not part of the Pactline domain.

## Repository guidance

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — development and review workflow
- [`SECURITY.md`](SECURITY.md) — private vulnerability reporting
- [`docs/coding-standards.md`](docs/coding-standards.md) — implementation rules
- [`docs/repository-policy.md`](docs/repository-policy.md) — privacy and
  publication controls
- [`docs/operations/agent-api.md`](docs/operations/agent-api.md) — Agent API
  operation
- [`docs/operations/agent-evaluation.md`](docs/operations/agent-evaluation.md) —
  isolated evaluation of explicit-mention conversation conversion
- [`docs/operations/lark-identity.md`](docs/operations/lark-identity.md) —
  identity operation
- [`docs/operations/deployment.md`](docs/operations/deployment.md) —
  container deployment, backup, and rollback

Accepted designs and implementation plans under `docs/superpowers/specs/` and
`docs/superpowers/plans/` are distributable project records; other local
Superpowers working notes remain ignored.

## Legacy boundary

The original bounty and credits mechanism remains isolated under
`internal/legacy` and `/api/legacy/*`. It is retained for compatibility and is
not the model for new Pactline behavior. See
[`internal/legacy/README.md`](internal/legacy/README.md).

## License

Pactline is licensed under the
[Apache License 2.0](LICENSE).
