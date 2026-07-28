# Lark Identity Operations

## Scope

This application serves one company and one Lark tenant. It supports one
Administrator, invitation-only Members, Lark bot delivery for invitations,
server-owned application sessions, and read-only Administrator
impersonation. It does not support multiple tenants, Administrator promotion,
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
invite.

Register this exact redirect URI:

```text
https://<application-origin>/api/auth/lark/callback
```

`APP_BASE_URL` and `LARK_REDIRECT_URI` must use the same HTTPS origin in
production. Obtain `LARK_TENANT_KEY` from the intended company tenant; the
backend rejects OAuth principals from any other tenant.

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
6. Confirm the Users and Invitations navigation entries are visible.

Only the bootstrap flow may create the Administrator. Do not manually
promote a Member in the database.

## Inviting a Member

1. Open **Invitations**.
2. Enter at least two characters of the employee's name.
3. Verify the exact Lark identity using name, avatar, and email.
4. Send the invitation.
5. If delivery is `FAILED`, generate and copy a new link, then deliver it
   through an approved channel. Generating a new link invalidates the old
   link.

Invitation links expire after seven days and can be used once. The raw token
exists only in the URL fragment and request body; the database stores only a
hash. Resend, copy-new-link, and revoke invalidate every outstanding OAuth
state tied to the previous link.

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
   do not bypass tenant or invitation checks.
5. Schema migrations are additive. Use a forward corrective migration
   instead of editing or dropping identity tables in place.

## Real-Lark acceptance record

Record date, application version, Lark application version, tester, and
pass/fail evidence without secrets:

- [ ] Bootstrap Administrator
- [ ] Directory search and exact-person selection
- [ ] Bot direct-message delivery
- [ ] Fragment invitation, OAuth, and Member account creation
- [ ] Existing Member login
- [ ] Refresh-token rotation
- [ ] 15-minute active-principal revalidation
- [ ] Invalid-principal denial and session revocation
- [ ] Controlled transient failure and one-hour grace behavior
- [ ] Enter, enforce, and exit read-only impersonation
