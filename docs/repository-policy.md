# Public Repository Privacy Policy

## Public-by-default rule

This is a **public repository**. Treat every tracked file, commit, branch,
issue, pull request, review, workflow log, screenshot, and artifact as
immediately visible outside the organization. Content must be safe for public
exposure before it enters Git history; a later deletion is not a privacy
control.

## Information that must never be committed

Do not commit:

- passwords, API keys, OAuth secrets, access tokens, session material,
  invitation links, or private keys;
- company names, corporate domains, tenant identifiers, internal project
  codenames, customer names, or production business data;
- personal names, personal or corporate email addresses, phone numbers,
  account identifiers, avatars, or screenshots containing identifiable users;
- internal hostnames, network addresses, SSH aliases, deployment paths, or
  infrastructure topology; or
- logs, database dumps, traces, or fixtures derived from a real environment.

Use reserved examples such as `example.com`, `.test` domains, documentation IP
ranges, synthetic identifiers, and fictional data. A value being non-secret
does not make it appropriate to publish.

Commit metadata is part of repository history. Contributors must use a public
developer identity and a GitHub `noreply` email or another address they
explicitly intend to publish.

## Local-only material

`docs/superpowers/` contains local brainstorming, design, and implementation
notes. It is ignored by Git and is not part of the distributable repository.
Stable product decisions required to understand the code must be distilled into
tracked documentation such as `docs/product-model.md`.

## First-push and history-replacement gate

Before the first source push, or before replacing published Git history:

1. Select and add an open-source license with the owner's explicit approval.
2. Run secret and personal-information scans against both the working tree and
   the complete Git history.
3. Rewrite or replace Git history if author metadata or old content contains
   information that is not approved for publication.
4. Rotate every credential that has ever been shared outside its intended
   secret store, even when it was never committed.
5. Resolve production dependency vulnerabilities and document any accepted
   development-only risk.
6. Verify that ignored local documents are absent from the remote repository
   and that no tracked document links to them.
7. Confirm GitHub branch protection, required CI checks, vulnerability alerts,
   private vulnerability reporting, and public repository visibility.
8. Review the rendered README, contribution process, security contact path,
   API examples, fixtures, screenshots, and generated artifacts as a fresh
   external reader.

Passing automated scans is necessary but not sufficient. A maintainer must
complete a manual review before the first source push or history replacement.

## Incident response

If restricted information is committed:

1. stop sharing and do not push the affected branch;
2. revoke or rotate credentials immediately;
3. record the affected commits and information categories without copying the
   sensitive value into another document or ticket;
4. clean the working tree and repository history; and
5. verify the cleaned history before any push or visibility change.

Deleting a later commit does not remove data from Git history.
