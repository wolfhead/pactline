# Pactline Fleet M5.1 Service Foundation Implementation Plan

**Date:** 2026-08-15

**Status:** Complete

## Contract

M5.1 creates a resident but non-scheduling Fleet Service. It validates and
reloads Project-bound Fleet configuration, owns a private local registry and
single-process lock, probes external dependencies, exposes bounded health, and
stops cleanly. It does not discover Tasks, create Claims, create Worktrees,
start Harness Runs for work, deliver repository changes, or serve the M5.3 Web
UI.

## Implementation decisions

- Use better-sqlite3 13.0.3 instead of the experimental Node 22 node:sqlite API.
- Use proper-lockfile 4.1.2 for a heartbeat-backed local state-directory lock.
- Keep YAML as the only configuration authority.
- Treat structural configuration, state, registry, and lock errors as fatal.
- Treat Pactline and Adapter probe failures as visible degraded health.
- Require restart for process-bound configuration changes: Pactline endpoint,
  state directory, and HTTP listener.
- Apply Fleet/routing/concurrency changes atomically after full validation.
- Do not persist secret values, raw external diagnostics, prompts, or reasoning.

## Files

### Configuration

- fleet/src/config/types.ts
- fleet/src/config/load.ts
- fleet/src/config/manager.ts
- fleet/tests/config/load.test.ts
- fleet/tests/config/manager.test.ts

The parser protects version, Project cardinality, loopback binding, absolute
workspace paths, route shape, concurrency bounds, duration values, and
credential references. The manager hashes canonical sanitized configuration,
rejects invalid reloads without replacing the active snapshot, and watches the
source file with a bounded debounce.

### Local state and registry

- fleet/src/service/state-directory.ts
- fleet/src/service/service-lock.ts
- fleet/src/registry/fleet-registry.ts
- fleet/tests/service/state-directory.test.ts
- fleet/tests/service/service-lock.test.ts
- fleet/tests/registry/fleet-registry.test.ts

The state directory and Fleet workspace roots are private. The registry applies
ordered local schema migrations, persists one stable service identity,
configuration revisions, Fleet mappings, and the initial Run identity/state
contract. Configuration snapshots contain references only.

### Health and lifecycle

- fleet/src/health/model.ts
- fleet/src/health/store.ts
- fleet/src/http/health-server.ts
- fleet/src/service/fleet-service.ts
- fleet/tests/health/store.test.ts
- fleet/tests/http/health-server.test.ts
- fleet/tests/service/fleet-service.test.ts

The service owns startup phases, Pactline preflight, Adapter probes, empty
startup reconciliation, readiness calculation, atomic reload application, and
graceful draining. The HTTP server exposes GET-only livez, readyz, healthz, and
api/v1/service on loopback.

### Composition and documentation

- fleet/src/commands/serve.ts
- fleet/src/cli.ts
- fleet/src/index.ts
- fleet/tests/commands.test.ts
- fleet/package.json
- fleet/package-lock.json
- fleet/README.md
- fleet/config.example.yml
- docs/pactline-fleet-service-architecture.md
- docs/pactline-fleet-service-milestones.md
- docs/superpowers/plans/2026-08-15-pactline-fleet-master-runbook.md

The serve command owns signal registration and waits until termination. Existing
finite commands remain unchanged and do not acquire the service lock.

## Verification order

1. Configuration parser and reload tests.
2. State-directory, lock, and registry tests.
3. Health HTTP and lifecycle tests with injected Pactline and Adapter probes.
4. CLI tests.
5. Typecheck.
6. Full Fleet unit suite.
7. Build and package dry run.
8. A real idle serve smoke test against a temporary state directory.
9. git diff --check and exact status review.

## Acceptance

- two Fleets mapped to different Projects load successfully;
- duplicate enabled Project configuration is rejected;
- invalid reload leaves the old revision active;
- active Run identity retains its original configuration revision;
- a second process cannot own the same local state directory;
- external probe failures are visible without terminating diagnostics;
- readiness stays false when no Fleet can safely schedule;
- graceful stop closes HTTP, SQLite, watcher, and lock;
- no discovery, Claim, Harness work Run, or repository mutation exists in M5.1;
- no secret value enters SQLite, JSON health, or logs.

## Completion evidence

- `make pactline-fleet-check` passed after a clean `npm ci`: the frozen old
  Bundle baseline remained 56 files at aggregate SHA-256
  `79a69995e998db89db85423bad93fe1bd92c8cf641d776991bbb0e0d5ccd8025`,
  strict typecheck passed, 32 test files and 120 tests passed, and the package
  built.
- The real child-process smoke started `pactline-fleet serve` from the
  repository root, observed `livez=200` and `readyz=503` for the intentional
  no-Fleet fixture, rejected a second process on the same state directory,
  stopped on SIGTERM, and found no Pactline credential in SQLite.
- The authenticated local Docker integration passed protocol 2 with 16
  features, performed bounded Project discovery, revoked its ephemeral Token,
  and mutated no work resource.
- Codex runtime verification passed 12 Adapter tests and the real keyless
  `codex-doctor`. The default version probe now uses the same credential-
  filtered environment as other Codex subprocesses.
- The production dependency audit reported zero vulnerabilities. Package
  dry-run included the service modules and `config.example.yml`.
- M5.1 has no scheduler, Task discovery loop, Claim mutation, Worktree
  creation, Harness work dispatch, repository delivery, or Web UI.
