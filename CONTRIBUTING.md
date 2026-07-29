# Contributing to Pactline

Pactline is developed in a public repository. Treat commits, branches, issues,
pull requests, logs, screenshots, and artifacts as publicly visible.

## Before contributing

Read:

- `AGENTS.md`
- `docs/coding-standards.md`
- `docs/product-model.md`
- `docs/repository-policy.md`

Use English for code, identifiers, comments, logs, commit messages, and
developer-facing documentation. Non-English text belongs only in intentional
end-user copy or localization resources.

Configure Git with an identity you intend to expose publicly. Prefer a GitHub
`noreply` email.

## Development workflow

1. Inspect the current code, tests, and Git status.
2. Keep changes focused on one coherent outcome.
3. Add or update the cheapest test that proves changed behavior.
4. Run focused checks first, followed by the broader relevant suites.
5. Review the diff for secrets, personal information, generated churn, and
   unrelated edits.
6. Open a pull request using the repository template.

Breaking API, stored-data, workflow, or integration changes require explicit
owner approval before implementation.

## Common checks

```bash
make test
make web-test
make web-build
make openapi-check
make e2e
```

Store and API tests share PostgreSQL and must run serially.

## Pull requests

A pull request should state:

- the user or domain outcome;
- contracts and migrations affected;
- verification performed;
- security or privacy impact; and
- any follow-up or intentionally deferred work.

Never include production data, real identities, internal infrastructure, or
credentials in a pull request, issue, screenshot, test fixture, log, or commit.

## License

The project has no open-source license yet. External contributions cannot be
accepted until the owner selects a license and contribution terms.
