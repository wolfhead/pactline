# Test Environment Deployment

This runbook covers the single-host Pactline test environment. It complements
`docs/operations/deployment.md`, which remains the source of truth for the
production topology, secret formats, backups, and rollback constraints.

The repository is public. Do not put real SSH targets, hostnames, account
identifiers, infrastructure details, or credentials in this tracked document.
Environment-specific, non-secret values belong in the Git-ignored helper at
`deploy/script/deploy_test.sh`. Runtime secrets exist only on the deployment
host under `deploy/secrets/`.

## Test topology

The test environment uses:

- the immutable API and Web images published by
  `.github/workflows/release-images.yml`;
- `compose.production.yaml` as the base deployment;
- `deploy/compose.preview-lark.yaml` as the test-only override;
- `deploy/compose.agent-vision.yaml` to enable screenshot inspection;
- a stable Docker Compose project name so upgrades reuse the existing
  PostgreSQL volume;
- a host-level reverse proxy forwarding the public test origin to the Web
  gateway; and
- Lark OAuth and the built-in Lark Agent enabled with host-owned secrets.

The remote deployment directory is a deployment artifact directory, not a Git
checkout. Do not run `git pull` on the server. Deploy the Compose definitions
from the same commit as the selected immutable images.

## Local operator helper

On the primary operator workstation, the environment-specific helper is:

```text
deploy/script/deploy_test.sh
```

Git intentionally ignores this file because it contains the real SSH alias,
remote path, Compose project name, and test URL. It must not contain API keys,
application secrets, passwords, session secrets, or encryption keys.

The helper requires:

- `git`, authenticated `gh`, `ssh`, `scp`, `curl`, and a working Docker
  connection on the remote host;
- permission to dispatch the `Release container images` workflow;
- an SSH alias resolving to the test host; and
- an already initialized remote `deploy/.env` and `deploy/secrets/`.

Run the normal deployment from any local branch:

```bash
deploy/script/deploy_test.sh
```

The default deploys the current remote `main` commit, not local uncommitted
files. To deploy a pushed branch or tag for acceptance testing:

```bash
deploy/script/deploy_test.sh --ref <remote-branch-or-tag>
```

To roll back application images to an already published immutable version:

```bash
deploy/script/deploy_test.sh --version sha-<full-commit>
```

The helper performs these operations:

1. Resolves the selected ref from `origin`.
2. Reuses a successful image-publication run for that commit, or dispatches and
   waits for one.
3. Exports the three Compose files from that exact commit and copies them to the
   deployment directory.
4. Updates only `PACTLINE_VERSION` in the remote environment file.
5. Validates the effective three-file Compose configuration.
6. Creates a PostgreSQL custom-format backup when the database is running.
7. Pulls the immutable images and starts the stack with `--wait`.
8. Checks `/healthz` and `/readyz` inside the containers and through the public
   test origin.

If any step fails, the script stops. It does not delete volumes, secrets, or
backups.

## First-time host initialization

Follow the host preparation and secret generation sections in
`docs/operations/deployment.md`. In addition:

1. Create the remote deployment directory and its `deploy/secrets/` directory.
2. Populate `deploy/.env` from `deploy/.env.production.example`.
3. Set a stable, environment-specific Compose project name in the ignored
   helper and never change it after PostgreSQL has data.
4. Configure the test origin, port binding, Lark application ID, OAuth callback
   URL, bootstrap Administrator email, and Agent settings in `deploy/.env`.
5. Create every secret file listed in
   `deploy/secrets.example/README.md` with mode `0600`.
   Screenshot evaluation specifically requires `agent_vision_api_key`.
6. Install or update the host reverse proxy only after checking other
   applications and listeners on the shared host.
7. Register the exact callback below in the Lark application and publish the
   application version:

   ```text
   <test-origin>/api/auth/lark/callback
   ```

8. Run the helper and complete the acceptance checklist below.

The test override changes `APP_ENV` only. Authentication remains Lark because
the production base Compose file sets `AUTH_PROVIDER=lark`.

## Acceptance checklist

After every deployment, verify:

- the helper reports both internal and public health checks as successful;
- the login page loads from the test origin;
- Lark OAuth returns to the same origin and `/api/me` identifies the expected
  account;
- an existing Project and Task can be read;
- a reversible test Task mutation succeeds;
- `GET /api/admin/agent/status` reports the Lark channel and worker as ready;
- an `@bot` Task detail or Project status request receives the immediate emoji
  acknowledgement and a final Card; and
- an `@bot` discussion conversion with a screenshot uses its visible evidence,
  while a CSV explicitly described as a sample is not treated as the complete
  population;
- an unrelated sticker or reaction image in the preceding discussion does not
  trigger attachment inspection; and
- API logs contain no repeated worker, outbox, migration, or Lark reconnect
  failures.

The public health endpoints do not prove that the Lark WebSocket is connected.
Always perform the Agent status and real-message checks when Agent code,
configuration, Lark permissions, or secrets changed.

## Failure handling

### Image publication fails

Do not change the remote version. Open the failed GitHub Actions run and fix
the source or build configuration first. Never substitute a mutable image tag.

### Compose validation fails

Compare the selected commit's environment requirements with the keys in the
remote `deploy/.env` and the expected secret filenames. Do not print secret
contents.

### Database backup fails

Deployment must stop. Diagnose PostgreSQL health, disk space, directory
permissions, and the Compose project name before retrying.

### Stack starts but public health fails

Check container health first, then the Web gateway binding, host reverse proxy,
upstream forwarding headers, DNS, and the outer load balancer. Do not replace a
shared host proxy without reviewing its complete configuration.

### Lark login works but the Agent does not respond

Check the Administrator Agent status endpoint and structured API logs. Confirm
that the Lark application version is published, bot permissions and message
events are active, `AGENT_ENABLED=true`, and the DeepSeek secret file exists.
Do not add callback verification secrets when the application uses the Lark
long connection.

## Rollback

Use the ignored helper with the last verified immutable SHA. The helper creates
another database backup before replacing containers. Application rollback is
safe only when the older binary can read the current schema; migrations remain
forward-only. Follow the restore constraints in
`docs/operations/deployment.md` if schema compatibility is uncertain.
