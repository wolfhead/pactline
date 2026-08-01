# Deployment Operations

## Supported topology

Pactline's standard deployment target is one Linux host running Docker Compose.
The same API and Web Dockerfiles are used by local stack verification and
production image publishing.

The production stack contains:

- a Caddy-based Web gateway that serves the React build, provides SPA
  fallback, and proxies `/api/*`, `/healthz`, and `/readyz`;
- one non-root Go API container;
- one PostgreSQL 16 container with a durable named volume; and
- one RabbitMQ container with a durable named volume for reliable domain-event
  delivery; and
- separate application and database networks. PostgreSQL is not published on a
  host port.

On a shared server, the Web gateway binds to loopback only. The server's
existing HTTPS edge proxy owns public ports and forwards the complete
application origin to Pactline. Do not install or replace a host-level proxy
without first reviewing the other applications on that host.

Pactline does not currently need Kubernetes, multiple API replicas, a service
mesh, or an automated deployment agent.

Task attachment objects are private. The default deployment stores them in the
API container's `attachment_data` volume. `ATTACHMENT_STORAGE_PROVIDER=oss` or
`cos` switches to a private cloud bucket; never enable anonymous bucket access.
Cloud credentials use the matching `*_FILE` secret variables. OSS requires
region, bucket, access key ID, and access key secret. COS requires an absolute
bucket URL, secret ID, and secret key; service URL and session token are
optional. Authorization remains in Pactline even when a short-lived upload URL
is issued.

Enable exactly one cloud-storage override when needed:

```bash
docker compose --env-file deploy/.env \
  -f compose.production.yaml \
  -f deploy/compose.attachments-oss.yaml config --quiet

docker compose --env-file deploy/.env \
  -f compose.production.yaml \
  -f deploy/compose.attachments-cos.yaml config --quiet
```

Temporary COS credentials additionally include
`deploy/compose.attachments-cos-session.yaml` and its session-token secret.

Configure the private bucket's CORS rule to permit `PUT` from the exact
`PACTLINE_APP_BASE_URL` origin with `Content-Type` and `Content-Length` request
headers. Do not permit wildcard origins with credentials.

## Local workflows

The fast edit loop remains:

```bash
make up
make run
make web-dev
```

Use the containerized stack before a release or when validating deployment
behavior:

```bash
make stack-up
open http://localhost:5173
make stack-logs
make stack-down
```

`make stack-up` builds the production Dockerfiles but enables the explicit
Development identity provider. It starts its own PostgreSQL volume and does not
reuse the integration-test database on port 5433.

Override the host port when another local service already uses 5173:

```bash
PACTLINE_HTTP_PORT=15173 make stack-up
```

`make stack-down` preserves the local stack's database volume. Running
`docker compose -f compose.yaml down --volumes` intentionally deletes that
local data and must not be used against production.

## Image publication

`.github/workflows/release-images.yml` verifies the source and publishes
multi-platform API and Web images to:

```text
ghcr.io/wolfhead/pactline-api
ghcr.io/wolfhead/pactline-web
```

A Git tag such as `v0.1.0` publishes both a semantic-version tag and an
immutable `sha-<full-commit>` tag. Production must set `PACTLINE_VERSION` to
the immutable SHA tag. Do not deploy `latest`.

The workflow may also be run manually to publish a commit SHA without creating
a release tag. After the first publication, verify that both GHCR packages are
public. If they remain private, authenticate the deployment host with a
read-only package token before running the deploy script.

Images contain no runtime credentials. Lark, session, encryption, and database
secrets are injected only on the deployment host.

The built-in Agent additionally requires independent DeepSeek, delegation
signing, and checkpoint encryption secrets. Its Lark WebSocket connection
reuses the application ID and secret; it does not require callback secrets.
Keep `AGENT_ENABLED=false` until the Lark bot application version and
permissions described in `docs/operations/lark-identity.md` are published.

DeepSeek V4 is the Agent's text and tool model. To let the Agent understand
conversation screenshots, configure a separate OpenAI-compatible multimodal
model and include the optional Compose override:

```bash
docker compose \
  --env-file deploy/.env \
  -f compose.production.yaml \
  -f deploy/compose.agent-vision.yaml \
  config --quiet
```

Create `deploy/secrets/agent_vision_api_key` with mode `0600`. The override
defaults to JieKou AI at `https://api.jiekou.ai/openai` with
`gemini-2.5-flash-lite`; set `AGENT_VISION_BASE_URL` or `AGENT_VISION_MODEL` in
`deploy/.env` only to override them. Omitting the Compose override keeps image
inspection safe but unavailable, and never falls back to OCR. Markdown, CSV,
and XLSX inspection do not require the vision override.

## Host preparation

Requirements:

- Docker Engine with Docker Compose v2;
- outbound HTTPS access to Lark and GHCR;
- a stable DNS name and trusted HTTPS certificate;
- enough disk space for PostgreSQL data, images, and backups; and
- an existing edge proxy on shared hosts.

A conventional example installation layout is:

```text
<deployment-root>/
├── compose.production.yaml
├── deploy/
│   ├── .env
│   ├── backup.sh
│   ├── deploy.sh
│   ├── backups/
│   └── secrets/
└── docs/
```

The deployment definitions may be obtained from a repository checkout or a
release artifact. Application source is never compiled on the production host.

Copy the environment template:

```bash
cp deploy/.env.production.example deploy/.env
```

Populate every placeholder in `deploy/.env`. It contains deployment metadata
and personal configuration such as the bootstrap email, so it remains
untracked even though the actual secrets live in separate files.

Create `deploy/secrets/` with mode `0700` and create the files documented in
`deploy/secrets.example/README.md` with mode `0600`.

The `database_url` file must use the Compose hostname `postgres`, not
`localhost`. Percent-encode reserved characters in its password component.

Before the first production deployment, rotate any Lark application secret
that has previously appeared in chat, logs, or another non-secret store.

## HTTPS edge

The production Compose file defaults to:

```text
127.0.0.1:18080
```

Forward the entire application origin to that address. A minimal Caddy edge
site is:

```caddyfile
tasks.example.com {
	reverse_proxy 127.0.0.1:18080
}
```

An Nginx, Traefik, load balancer, or ingress installation is equally valid.
Preserve the original `Host` and HTTPS forwarding information. Do not split
frontend and API onto different public origins; browser sessions, CSRF
protection, and the Lark OAuth callback intentionally use one origin.

Set these values to that exact HTTPS origin:

```text
PACTLINE_APP_BASE_URL
LARK_REDIRECT_URI
```

Register the same callback URL in the Lark application:

```text
https://<application-origin>/api/auth/lark/callback
```

## First deployment

1. Publish or select an immutable `sha-<full-commit>` image version.
2. Verify the production environment file and secret-file permissions.
3. Verify the Lark application redirect URI and active secret.
4. Validate the effective Compose configuration:

   ```bash
   docker compose \
     --env-file deploy/.env \
     -f compose.production.yaml \
     config --quiet
   ```

5. Run:

   ```bash
   deploy/deploy.sh
   ```

6. Confirm PostgreSQL, RabbitMQ, API, and Web services are healthy.
7. Confirm the edge endpoint returns `ok` from `/healthz` and `ready` from
   `/readyz`.
8. Sign in with the configured bootstrap account and confirm `/api/me` reports
   `ADMIN`.
9. Exercise a Project page, a Task mutation, logout, and a fresh login.

The API applies embedded migrations before it becomes ready. Migration bodies
and their records are committed atomically, and an advisory lock serializes
concurrent migration callers. The production topology nevertheless starts with
one API replica because there is no current availability need for more.

## Health and shutdown

- `GET /healthz` proves the API process is serving requests.
- `GET /readyz` performs a bounded PostgreSQL ping.

Both endpoints return only plain status text and no infrastructure details.
Container health checks use `/readyz`; the gateway exposes both endpoints for
the edge monitor. A temporary Lark WebSocket disconnect does not fail either
endpoint. Administrators can inspect the built-in Agent connection separately
through `GET /api/admin/agent/status`.

The API handles `SIGTERM` and `SIGINT`, stops accepting new work, and allows up
to 15 seconds for active HTTP requests to finish. Compose grants a 20-second
stop period.

## Upgrade

1. Review migrations and compatibility notes for the target commit.
2. Change only `PACTLINE_VERSION` in `deploy/.env`.
3. Run `deploy/deploy.sh`.

When PostgreSQL is already running, the script creates a custom-format
`pg_dump` before pulling or replacing containers. A backup failure aborts the
deployment.

The script then pulls both immutable images, starts the stack with
`--wait`, and checks the gateway and API from inside their containers.

Retain backups outside the Docker volume and copy them to a separately managed
storage location. Define a retention policy before backups become operational
data.

## Rollback and restore

An application rollback changes `PACTLINE_VERSION` to a previously verified SHA
and runs the deployment script again. This is safe only when the older binary
can read the migrated schema.

Database migrations are forward-only. Use additive expand/contract migrations
whenever a version may need application rollback. A destructive or
backward-incompatible migration requires explicit owner approval and a
version-specific restore plan.

Test a backup without replacing production by restoring it into a separate
database:

```bash
docker compose --env-file deploy/.env -f compose.production.yaml \
  exec -T postgres \
  sh -c 'createdb --username="$POSTGRES_USER" pactline_restore'

docker compose --env-file deploy/.env -f compose.production.yaml \
  exec -T postgres \
  sh -c 'pg_restore \
    --username="$POSTGRES_USER" \
    --dbname=pactline_restore \
    --no-owner \
    --no-acl' \
  < deploy/backups/<backup-file>.dump
```

Do not automate replacement of the live database. A production restore must
stop writes, preserve the failed database, restore into a verified target, and
receive explicit operator confirmation before traffic is switched.

## Logs and diagnosis

The API emits structured JSON to stdout. Caddy and PostgreSQL also log to their
container streams. Compose applies bounded local JSON log rotation.

Inspect status and recent logs:

```bash
docker compose --env-file deploy/.env -f compose.production.yaml ps
docker compose --env-file deploy/.env -f compose.production.yaml logs --tail=200 api web postgres rabbitmq
```

Never paste raw production logs into the public repository. Remove personal
data and internal infrastructure context before sharing diagnostic excerpts.
