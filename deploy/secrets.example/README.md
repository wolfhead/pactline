# Production secret files

Create a separate, untracked `deploy/secrets/` directory on the deployment
host. Restrict the directory to the deployment operator and create these files
without a trailing explanation or label:

- `postgres_password`: the PostgreSQL role password.
- `database_url`: the complete PostgreSQL URL using host `postgres`, port
  `5432`, and the password above.
- `session_secret`: 32 random bytes encoded as base64.
- `oauth_token_encryption_key`: a different 32 random bytes encoded as base64.
- `lark_app_secret`: the current Lark application secret.

Example database URL shape:

```text
postgres://pactline:replace-me@postgres:5432/pactline?sslmode=disable
```

Generate the encoded keys independently:

```bash
openssl rand -base64 32
openssl rand -base64 32
```

Never commit populated secret files, copy them into an image, or print them in
deployment logs.
