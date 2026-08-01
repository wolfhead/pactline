# Production secret files

Create a separate, untracked `deploy/secrets/` directory on the deployment
host. Restrict the directory to the deployment operator and create these files
without a trailing explanation or label:

- `postgres_password`: the PostgreSQL role password.
- `database_url`: the complete PostgreSQL URL using host `postgres`, port
  `5432`, and the password above.
- `rabbitmq_password`: the RabbitMQ application user's password.
- `rabbitmq_url`: the complete private AMQP URL using host `rabbitmq`, port
  `5672`, and the password above. Percent-encode reserved password characters.
- `session_secret`: 32 random bytes encoded as base64.
- `oauth_token_encryption_key`: a different 32 random bytes encoded as base64.
- `lark_app_secret`: the current Lark application secret.
- `deepseek_api_key`: the DeepSeek API key used only by the built-in Agent.
- `agent_vision_api_key`: optional; the key for the separately configured
  multimodal model used by `deploy/compose.agent-vision.yaml`. DeepSeek V4 is
  kept as the text/tool model and this key is never shared with it.
- `agent_delegation_signing_key`: 32 random bytes encoded as base64.
- `agent_checkpoint_encryption_key`: a separate 32 random bytes encoded as
  base64; it encrypts Eino checkpoints and pending Agent input.
- `attachment_oss_access_key_secret`: required only when the attachment
  provider is `oss`.
- `attachment_cos_secret_key`: required only when the attachment provider is
  `cos`.
- `attachment_cos_session_token`: optional temporary COS credential token.

Example database URL shape:

```text
postgres://pactline:replace-me@postgres:5432/pactline?sslmode=disable
amqp://pactline:replace-me@rabbitmq:5672/
```

Generate the encoded keys independently:

```bash
openssl rand -base64 32
openssl rand -base64 32
openssl rand -base64 32
openssl rand -base64 32
```

Never commit populated secret files, copy them into an image, or print them in
deployment logs.
