# Security Policy

## Supported versions

Pactline is in early development. Only the latest commit on the maintained
default branch is supported.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability, exposed credential,
privacy incident, or internal infrastructure disclosure.

Until GitHub private vulnerability reporting is enabled, contact a repository
maintainer through an existing private channel. Do not include live secrets or
production personal data in the initial report. Provide:

- the affected component and revision;
- reproduction steps using synthetic data;
- expected and observed behavior;
- likely impact; and
- any safe mitigation already attempted.

A maintainer should acknowledge a report within five business days. Remediation
and disclosure timing depend on severity and whether credentials or personal
data require immediate containment.

## Scope

Security issues include authentication or authorization bypass, impersonation
write access, token or session leakage, cross-tenant access, injection, unsafe
redirects, sensitive logging, and API behavior that bypasses documented
idempotency or concurrency controls.
