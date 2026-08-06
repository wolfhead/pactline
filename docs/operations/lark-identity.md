# Lark Identity Operations

## Scope

This application serves one company and one Lark tenant. It supports one
Administrator, Administrator-approved Members, restricted pending sessions,
server-owned application sessions, and read-only Administrator impersonation.
It does not support multiple tenants, Administrator promotion,
department-based authorization, or a general notification system.

## Lark application setup

Create an internal application in the international Lark developer console.
Enable web OAuth, the bot capability, contact search/read access, and message
delivery as the bot. Grant and publish these user OAuth scopes:

- `auth:user.id:read`
- `contact:contact.base:readonly`
- `contact:user.base:readonly`
- `contact:user.email:readonly`
- `contact:user:search`
- `offline_access`

The bot must also have the tenant permission required by Lark's Create
Message API to send a direct message as the application. Configure contact
visibility so the application can find every employee the Administrator may
admit. Grant the application-level `tenant:tenant:readonly` scope so Pactline
can discover and bind the application's company tenant during startup.

The published application's availability range must include every employee
who may request Pactline access. Lark OAuth still enforces the single-tenant
boundary; Pactline then keeps each new Member restricted until the
Administrator approves the access request.

Access requests and approvals are delivered as fixed bot DM cards through the
RabbitMQ `access.*` route. A request card links the Administrator to
`/admin/users`; an approval card links the admitted Member back to Pactline.
The consumer uses the application event ID as Lark's message UUID, retries
transient delivery failures, and dead-letters permanent or exhausted failures.
The bot availability range and `im:message:send_as_bot` permission must cover
both the Administrator and every employee who may request access.

Register this exact redirect URI:

```text
https://<application-origin>/api/auth/lark/callback
```

`APP_BASE_URL` and `LARK_REDIRECT_URI` must use the same HTTPS origin in
production. Pactline obtains the tenant key from Lark's Tenant API at startup
using the application credentials and rejects OAuth principals from any other
tenant. There is no manually configured `LARK_TENANT_KEY`.

## Required configuration

Copy `.env.example` into the deployment's secret/configuration system. Never
commit populated secrets.

Generate independent 32-byte values for the session HMAC and OAuth credential
encryption keys:

```bash
openssl rand -base64 32
openssl rand -base64 32
```

`BOOTSTRAP_ADMIN_EMAIL` must exactly match the email Lark returns for the
initial Administrator. Comparison is case-insensitive after trimming. There
must be no existing local Administrator before bootstrap.

`OAUTH_TOKEN_ENCRYPTION_KEY_ID` identifies the active encryption key. Key
rotation is not automated in this release: changing or removing the key before
stored credentials are re-encrypted makes those credentials unreadable.

## First-party Agent bot

The first-party Agent is opt-in through `AGENT_ENABLED=true`. Before enabling
it, publish a Lark application version that:

- enables the bot capability;
- selects **Use long connection to receive events** and subscribes
  `im.message.receive_v1`;
- grants `im:message.group_at_msg:readonly` to receive group mentions;
- grants `im:message.group_msg:readonly` and `im:message:readonly` to read the
  bounded history of groups in which the bot participates;
- grants `im:chat:readonly` so Pactline can resolve and periodically refresh
  the names of groups in which the bot participates;
- grants `im:resource` so the bot can download image and file resources from
  those messages;
- grants `im:message:send_as_bot` to reply as the bot; and
- grants `im:message.reactions:write_only` so the bot can acknowledge accepted
  commands with an `OnIt` reaction.

The API uses Lark's official Go SDK to establish the WebSocket connection with
`LARK_APP_ID` and `LARK_APP_SECRET`. No public event callback URL,
Verification Token, Encrypt Key, or configured Bot Open ID is required. During
startup the API obtains the bot identity from Lark's Bot Info API and refuses
to enable the Agent if the bot is missing or inactive. Every received event is
still checked against the configured application ID and the tenant key
discovered during startup.

The group-history permission can be sensitive and may require tenant
administrator approval plus publication of a new application version. This is
an external rollout prerequisite; the server can remain deployed with
`AGENT_ENABLED=false` until it is granted.

Generate independent delegation and checkpoint keys with
`openssl rand -base64 32`. Keep the DeepSeek key and Lark application secret
in secret files. Never reuse the OAuth credential-encryption key.

The Agent reacts only to an explicit bot mention. It may read at most 100
messages from the same conversation, never newer than the triggering message
or older than seven days. A clarification reply must be a direct reply to the
bot's question and must come from the initiating Pactline user.

Group configuration requests also require an explicit mention and use ordinary
natural language. The LLM may select current-conversation configuration tools,
but the OpenAPI layer derives the conversation from the authenticated Agent
Run and enforces Project ACLs, immutable revision history, and optimistic
version checks. A disabled group does not start the LLM and must be re-enabled
under **Agent → 群聊配置**.

Operational smoke test:

1. Enable the Agent with one worker and restart the API.
2. Mention the bot in a dedicated test group with one clear Task request.
3. Confirm the trigger message receives an `OnIt` reaction promptly.
4. Confirm exactly one Task is created and the fixed reply contains its link
   and an eight-character Run reference.
5. Send a deliberately ambiguous two-topic discussion request; confirm no Task
   is created until the initiating user replies with one specific Task.
6. Repeat the original Lark message and confirm no second Task or duplicate
   acknowledgement appears.
7. Mention the bot after a discussion containing a screenshot; confirm the
   Task uses visible facts only after `inspect_artifact` succeeds.
8. Repeat with a small CSV explicitly described as a sample; confirm the Task
   does not treat the file row count as the complete business population.
9. Confirm a discussion without an explicit priority creates a Task with
   priority `none`.
10. Put an unrelated sticker or reaction image before a clear conversion
    request; confirm it is not downloaded or inspected as Task evidence.

If event delivery fails, check the startup bot-initialization log and the
administrator-only `GET /api/admin/agent/status` endpoint. The status reports
connection state, transition time, reconnect count, and a safe error category;
it never returns credentials or raw provider errors. Temporary Lark
disconnects do not fail `/readyz`; the official SDK reconnects while the main
application remains available. If a Run remains queued, inspect worker and
lease state. If a Task exists but no group reply arrived, inspect the Agent
Outbox; delivery retries do not repeat the Task mutation.

For the production frontend build, leave `VITE_AUTH_PROVIDER` unset or set it
to `lark`. Only local Development builds may use
`VITE_AUTH_PROVIDER=development`. Configure the edge server to send `/api/*`
to the Go service and all other unknown paths to the frontend `index.html`, so
OAuth callbacks can redirect into client-side routes.

## First deployment and bootstrap

1. Back up PostgreSQL.
2. Deploy the application and allow startup migrations to complete.
3. Confirm startup logs show `auth_provider=lark` and no configuration error.
4. Visit `/login` and choose Lark login with the bootstrap account.
5. Confirm `/api/me` reports that account as `ADMIN`.
6. Confirm the Users navigation entry is visible.

Only the bootstrap flow may create the Administrator. Do not manually
promote a Member in the database.

## Approving a Member

1. Configure the published Lark application availability range to include
   every employee who may request Pactline access.
2. The employee opens Pactline and completes Lark OAuth. A first login creates
   a restricted `PENDING` account and shows the approval waiting page.
3. Open **Users**. Pending requests appear first.
4. Verify the employee's Lark name, avatar, and email, then choose **Approve**
   or **Reject**.
5. Approval takes effect in the employee's existing session when they recheck
   or refresh. Rejection keeps only the status page and logout available.

Legacy invitation links remain accepted during the compatibility period, but
they create the same pending account and never bypass Administrator approval.
The active frontend no longer creates new invitations.

## Sessions and employee status

Application sessions roll forward while used. Once 15 minutes have elapsed
since the last provider verification, the next protected request verifies
the Lark principal.

- Active principals continue normally.
- Resigned, frozen, missing, revoked, or otherwise explicitly invalid
  principals are deactivated and all application sessions are revoked.
- Refreshable expired credentials are refreshed once and verification is
  retried.
- Timeouts, rate limits, and Lark 5xx responses receive a one-hour grace
  window. After that window, access is denied without deactivating the user.
- A later successful verification clears the transient failure state.

This is request-driven periodic verification, not a background directory
sync. An employee with no application activity is checked on their next
request.

## Read-only impersonation

The Administrator can choose **Read-only view** for an active Member. The
banner must name both the real Administrator and effective Member.

During impersonation:

- task, project, milestone, acceptance, comment, archive, and other writes
  are rejected by the backend;
- Administrator APIs are unavailable except the exit action;
- audit events retain the real Administrator as actor; and
- no Member Lark credential is loaded or used.

Use impersonation for support and diagnosis only. Exit it before performing
administrative or product changes.

## Personal API access

Each active user may issue personal API tokens for their own Agent or
integration. Tokens support `work:read` and `work:write`; write scope also
includes read access. Supported lifetimes are 30, 90, and 365 days, with 90
days as the product default.

The raw token is returned once from creation and is never stored or
recoverable. The database stores only its SHA-256 digest and a safe display
prefix. Store the raw value in an approved secret manager. Do not put it in
source control, logs, task descriptions, or chat messages.

Users can list and revoke only their own tokens. The Administrator can list
and revoke any token, but cannot create a token for another user or retrieve
its secret. Revocation is effective on the next request. Expired tokens and
tokens owned by inactive users are also rejected.

Bearer authentication exists only for `/api/v1`. The versioned work contract
is delivered by the following implementation plan; until it is mounted, a
valid bearer token has no supported work-resource endpoint. Existing
unversioned task and project routes remain session-only.

### Token rotation

1. Issue a replacement token with the minimum required scope and an
   appropriate lifetime.
2. Save the new raw token before leaving the creation response.
3. Update the Agent's secret configuration and make one authenticated request.
4. Confirm the new token appears in the user's API activity.
5. Revoke the old token and verify subsequent use receives `TOKEN_REVOKED`.

If a raw token may have been disclosed, skip the overlap period: revoke it
immediately, inspect API activity by token and request ID, and then issue a
replacement.

### Lark status and API tokens

API tokens do not bypass company identity policy. A token request reuses the
same Lark principal verification policy as an application session. Explicitly
invalid or departed principals are deactivated and all their application
sessions and API tokens become unusable. Transient Lark failures receive the
same one-hour grace behavior described above.

## API traffic controls and audit

Bearer traffic is limited independently per token using a 30-request burst
bucket that refills at two requests per second, equivalent to 120 requests per
minute. Responses include `RateLimit-Limit`, `RateLimit-Remaining`, and
`RateLimit-Reset`. A rejected request returns `429` with `Retry-After`; clients
must wait rather than retry immediately.

Bearer mutations require an `Idempotency-Key` between 1 and 128 characters.
Reuse the same key only for an exact retry of the same method, route, and body.
Completed responses are replayable for 24 hours. Reusing a key for different
content is rejected, and concurrent duplicates report that the original
request is still processing.

Every `/api/v1` request records a request ID, authentication outcome, user and
token identifiers, token-name snapshot, method, route pattern, status,
duration, response size, replay state, user agent, and network address. Access
audit deliberately excludes request and response bodies, bearer values,
session secrets, and OAuth credentials. Access events are retained for 90 days
and expired records are deleted by maintenance at startup and every 24 hours.

Business mutations write a separate permanent audit event in the same database
transaction as the resource change. Task and project activity also preserve
the request ID, authentication method, token ID, and token-name snapshot. A
business change rolls back if its audit event cannot be stored. Business audit
events are not removed by the 90-day access-log maintenance.

Users can inspect their own API activity. The Administrator can filter all API
activity by user, token, method, route, status, request ID, and a time range of
at most 90 days. Use the request ID to correlate the UI, structured logs,
access audit, product activity, and business audit without exposing secrets.

The same Administrator screen has a separate Lark API view for outbound
provider calls. Each real HTTP attempt records only its operation, safe route
template, credential kind, outcome, status and provider codes, byte counts,
duration, provider request ID, and available Pactline correlation IDs. JSON
bodies, downloaded file contents, tokens, OAuth codes, search terms, provider
user/chat/message identifiers, concrete URLs, and filenames are excluded.
Lark audit writes use a bounded context independent from the caller's
cancellation; a failed audit write is logged but never changes the provider
call result. These records use the same 90-day transient retention policy as
API access audit.

## Diagnostics

Search structured logs by request ID, session ID, actor user ID, invitation
ID, provider request ID, and `error_category`. Secrets, OAuth codes, access
tokens, refresh tokens, session secrets, CSRF secrets, and raw invitation
tokens must never appear in logs.

Common categories:

- `directory`: Lark user search failed.
- `provider_contract`: an upstream response or configured contract is
  incompatible.
- `provider_unavailable`: timeout, rate limit, or upstream 5xx.
- `session_invalid`, `session_expired`, `session_revoked`: application
  session rejection.
- `session_revoke_failed`: logout could not persist revocation; browser
  cookies are still cleared.

## Rollback

1. Stop writes and preserve a database backup.
2. Roll back application binaries before rolling back schema.
3. Do not restore `X-User-Id` or Development authentication in production.
4. If Lark is unavailable, keep the application unavailable or read-only;
   do not bypass tenant or Administrator-approval checks.
5. Schema migrations are additive. Use a forward corrective migration
   instead of editing or dropping identity tables in place.

## Real-Lark acceptance record

Record date, application version, Lark application version, tester, and
pass/fail evidence without secrets:

- [ ] Bootstrap Administrator
- [ ] First-time same-tenant OAuth creates a restricted pending Member
- [ ] Pending and rejected Members cannot access product or Agent surfaces
- [ ] Administrator approve, reject, and later re-approve decisions
- [ ] Approval takes effect in the Member's existing session
- [ ] Legacy invitation OAuth creates a pending Member without bypassing approval
- [ ] Existing Member login
- [ ] Refresh-token rotation
- [ ] 15-minute active-principal revalidation
- [ ] Invalid-principal denial and session revocation
- [ ] Controlled transient failure and one-hour grace behavior
- [ ] Enter, enforce, and exit read-only impersonation
