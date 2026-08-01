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
  one assigned eligible Task, records a separate Agent conversation and
  acceptance evidence, then submits it for human review.
- Tasks support private attachments and threaded comments with structured
  Project-member mentions. Reliable notification intents flow through a
  PostgreSQL outbox and RabbitMQ without coupling comments to one IM provider.
- Production identity is invitation-only, single-tenant Lark OAuth with one
  Administrator.
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

### External Codex worker

Create an executor-scoped personal Token in the account UI, then install or
create a `pactline-work` Codex skill. Keep developer-specific configuration
outside this repository:

```text
~/.pactline/
├── .env          # mode 0600; base URL and personal Token
├── pactline.md   # optional personal Agent instructions
└── projects.md   # optional free-form local Project/repository mapping
```

The skill must bind Claims to the real `CODEX_THREAD_ID`; it must not invent a
transferable worker identity. Local repository layouts remain user-owned and
are intentionally not part of the Pactline domain.

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
