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
- Every Task belongs to one Project; a Task without a Milestone is in the
  Project Backlog.
- Task creation requires context and an expected result. Acceptance criteria
  are optional, revisioned, and independently checkable.
- People use browser sessions. Agents use personal scoped tokens against the
  same contract-first `/api/v1` API.
- Production identity is invitation-only, single-tenant Lark OAuth with one
  Administrator.
- Administrative impersonation is read-only and keeps the Administrator as the
  audited actor.

The durable entity semantics and invariants are documented in
[`docs/product-model.md`](docs/product-model.md).

## Technology

- Go 1.24 HTTP API
- PostgreSQL 16
- React 18, TypeScript, Vite, Tailwind CSS, and Radix UI
- ogen-generated OpenAPI transport
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

Copy `.env.example` when preparing a deployment. Never commit the resulting
environment file or real provider credentials.

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

Browser mutations require same-origin session and CSRF credentials. Agent
requests use personal Bearer Tokens with `work:read` or `work:write`, plus the
documented idempotency and optimistic-concurrency headers.

```bash
make openapi-generate
make openapi-check
```

Do not edit `internal/api/v1generated` manually.

## Repository guidance

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — development and review workflow
- [`SECURITY.md`](SECURITY.md) — private vulnerability reporting
- [`docs/coding-standards.md`](docs/coding-standards.md) — implementation rules
- [`docs/repository-policy.md`](docs/repository-policy.md) — privacy and
  publication controls
- [`docs/operations/agent-api.md`](docs/operations/agent-api.md) — Agent API
  operation
- [`docs/operations/lark-identity.md`](docs/operations/lark-identity.md) —
  identity operation

Local design notes under `docs/superpowers/` are intentionally ignored and are
not part of the distributable repository.

## Legacy boundary

The original bounty and credits mechanism remains isolated under
`internal/legacy` and `/api/legacy/*`. It is retained for compatibility and is
not the model for new Pactline behavior. See
[`internal/legacy/README.md`](internal/legacy/README.md).

## License

Pactline is licensed under the
[Apache License 2.0](LICENSE).
