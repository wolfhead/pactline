# Pactline Fleet

Pactline Fleet is a standalone, Harness-neutral orchestration application for
Pactline work. It is not a DeepSeek Harness Bundle and does not implement an
Agent Loop. Codex, DeepSeek Harness, and future Agent Harnesses integrate
through internal Adapters while Pactline remains authoritative for workflow
state.

Milestones M1 through M5 provide the Harness-neutral Core, Codex and DeepSeek
Adapters, durable multi-Fleet scheduling and recovery, and the local Operations
Console. The Core owns the Pactline CLI boundary, routing, Claim-stage
coordination, proposal validation, disposable Git workspaces, fixed
verification, delivery evidence, and settlement. Harness-specific runtimes
remain behind their Adapter contracts.

The current release is accepted for bounded local trials. It is not approved
for production: exhaustive crash qualification, distributed competition,
history retention, provider failure handling, and production sandbox and
credential hardening remain M6 work. See the
[M5.4 acceptance report](../docs/pactline-fleet-m5-4-report.md) and the
[Fleet domain model](../docs/pactline-fleet-domain-model.md) for the verified
boundary and review map.

## Codex Adapter

Install and check the Adapter-owned pinned runtime separately:

```bash
npm --prefix runtime/codex ci --ignore-scripts
npm run build
node lib/bin.js codex-doctor --json
```

M3 fixes the quality-first route at `gpt-5.6-sol` with reasoning `high`.
`max` is intentionally not used; `medium` is reserved for a later measured
cost and latency comparison. The Adapter uses the official Codex Agent CLI,
native coding tools, strict structured output, JSONL events, workspace
sandboxing, Session resume, and full process-tree cancellation. It ignores
user configuration and rules so unattended Runs do not inherit local plugins,
MCP servers, or approval policy.

Pactline and repository-provider credentials are removed from the Codex
process environment. The Codex login cache remains a trusted runtime
dependency in this milestone; the prompt forbids reading outside the supplied
workspace, while the current Codex read-only sandbox is not represented as a
filesystem read allowlist.

Run the finite shared read-only review gate with:

```bash
npm run codex:l1
```

Private sanitized evidence is retained under the gitignored
`.fleet/codex-l1-results/` directory. This gate does not create a Pactline
Task, branch, push, or PR.

## Development

```bash
npm install
npm run typecheck
npm test
npm run build
node lib/bin.js version --json
node lib/bin.js doctor --json --pactline /absolute/path/to/pactline
```

For a first local service start:

1. Build the Pactline CLI and make the local Pactline instance reachable.
2. Install Fleet and the runtime for every configured Adapter.
3. Copy `config.example.yml`, replace every absolute path and Project number,
   and export the configured Pactline Token environment variable.
4. Add a trusted `workPlugin` to each Fleet that should admit work. A Fleet
   without one remains observable but intentionally does not claim Tasks.
5. Start `serve`, then use `ui --open` from a second terminal.

```bash
make pactline-cli
cd fleet
npm install
npm run codex:install
npm run build
cp config.example.yml /absolute/path/to/fleet.yml
export PACTLINE_TOKEN=replace-locally
node lib/bin.js doctor --json --pactline /absolute/path/to/pactline
node lib/bin.js codex-doctor --json
node lib/bin.js serve --config /absolute/path/to/fleet.yml
```

Never place Pactline, model, or repository-provider credentials in Fleet YAML.

## Resident Service Foundation

M5.1 adds a resident, non-scheduling service foundation. It validates
Project-bound Fleet configuration, owns a private SQLite registry and local
single-process lock, probes Pactline and configured Adapters, reloads safe
configuration changes atomically, and exposes read-only health on loopback.

Copy `config.example.yml`, replace its absolute paths and Project number, then
start the service:

```bash
export PACTLINE_TOKEN=replace-locally
node lib/bin.js serve --config /absolute/path/to/fleet.yml
```

The resident service exposes:

- `GET /livez`
- `GET /readyz`
- `GET /healthz`
- `GET /api/v1/service`
- `GET /api/v1/overview`
- `GET /api/v1/fleets` and `/api/v1/fleets/:fleetId`
- `GET /api/v1/runs` and `/api/v1/runs/:runId`
- `GET /api/v1/adapters`
- `GET /api/v1/events`
- `GET /metrics`

Its HTTP surface is loopback-only and has no mutation routes. `stateDirectory`,
the Pactline endpoint, and the HTTP listener require restart when changed;
Fleet routing and concurrency changes can reload atomically.

## Durable scheduler and work plugins

M5.2 adds one fair scheduler across every enabled Project-bound Fleet, durable
Run and external-effect checkpoints, startup reconciliation, global and
per-Fleet concurrency, bounded backoff, and graceful drain. Pactline Claim
creation remains the distributed lock between Fleet Services on different
machines.

Repository and verification authority does not come from Task prose. An
enabled Fleet opts into scheduling by configuring a trusted executable work
plugin:

```yaml
fleets:
  pactline-development:
    project: 5
    workspaceRoot: /absolute/path/to/fleet-work/project-5
    workPlugin:
      executable: /absolute/path/to/project-work-plugin
      args: []
      timeout: 2m
    routing:
      execution: { adapter: deepseek, model: deepseek-v4-pro, reasoning: max }
      review: { adapter: deepseek, model: deepseek-v4-pro, reasoning: max }
      correction: { adapter: deepseek, model: deepseek-v4-pro, reasoning: max }
      resolutionAnalysis: { adapter: deepseek, model: deepseek-v4-pro, reasoning: max }
```

The service invokes the plugin as `<executable> [args...] resolve`, `commit`,
`push`, or `open-code-change`, sends one JSON document on stdin, and expects
one `{"ok":true,"data":...}` document on stdout. `resolve` receives the
discovered candidate and bounded Pactline Task packet. It must return a complete
`FleetWorkDefinition`: exact Task identity, credential-free repository source,
base ref and 40-character revision, provider identity, allowed paths, fixed
verification commands, and criterion IDs/revisions. Review definitions also
return the frozen delivery candidate. The three delivery operations receive the
verified proposal, Git observation, Task-scoped workspace, and preceding
observed facts. `commit` and `push` return the exact revision and branch;
`open-code-change` returns a validated `RepositoryDelivery` containing the
actual PR/MR URL, revision, and branch. Fleet persists intent and observation
around every operation.

The plugin is a trusted coordinator extension and owns provider-specific Git
and PR/MR behavior. Fleet supplies an allowlisted process environment and
removes Pactline and model API credentials. The plugin receives the configured
Git credential reference in its request; when that reference is an environment
variable name, only that referenced value is additionally forwarded. Secret
values remain outside the YAML and SQLite registry. Plugin
implementations must use the Run ID as their idempotency namespace and reconcile
an uncertain publish before creating another branch or PR/MR.

Run one finite discovery and drain cycle with:

```bash
node lib/bin.js serve --config /absolute/path/to/fleet.yml --once
```

Fleets without `workPlugin` remain observable but do not admit Tasks. This
preserves M5.1 configurations and prevents an incomplete repository policy from
creating a Claim.

The deterministic end-to-end service test uses Replay. The same bounded fixture
can exercise the real DeepSeek Pro/max route (and consumes model tokens):

```bash
npm run service:m5-2-deepseek-demo
```

## Local Operations Console

M5.3 serves a read-only React Operations Console from the same loopback
listener as the observation API. No Vite process is required after the package
has been built. The console includes Overview, Fleet detail, Runs, Run detail,
and System routes, receives bounded revision notifications through SSE, and
falls back to polling when the event stream is unavailable.

Start the resident service, then print or open its configured URL:

```bash
node lib/bin.js serve --config /absolute/path/to/fleet.yml
node lib/bin.js ui --config /absolute/path/to/fleet.yml
node lib/bin.js ui --config /absolute/path/to/fleet.yml --open
```

The `ui` command verifies `/livez`; it does not start another service. The UI
projects only bounded operational facts. It intentionally omits credentials,
raw prompts, model reasoning, unbounded Harness output, and raw SQLite rows.
Prometheus-compatible metrics use finite or configuration-bounded labels.

Verify the production-built and packed UI without Vite with:

```bash
npm run service:m5-3-package-smoke
```

The doctor calls only `pactline capabilities --json`. It does not authenticate,
start a Harness, create a Claim, mutate a Task, or access a repository.

## Bounded usability acceptance

M5.4 exercises the resident scheduler through public Pactline, Fleet Service,
work-plugin, local Git, Harness Adapter, and Web UI boundaries. Its finite
runner includes deterministic delivery, request-changes and correction, one
representative restart, DeepSeek Pro/max execution with Codex high review, and
Codex high execution and review.

With local Pactline running and both Harness credentials already configured:

```bash
make pactline-cli
cd fleet
npm run m5-4:preflight
npm run m5-4:acceptance
```

The complete live command consumes model tokens. Individual modes are
available as `m5-4:deterministic`, `m5-4:correction`, `m5-4:restart`, and
`m5-4:live`. Private evidence is retained under the gitignored
`.fleet/m5-4-usability/` directory.

## Optional DeepSeek Adapter

Install the Adapter-owned runtime separately from Fleet Core:

```bash
npm --prefix runtime/deepseek ci
npm run build
node lib/bin.js deepseek-doctor --json
```

`deepseek-doctor` performs a finite, keyless initialize/shutdown handshake. It
does not call a model. The production runtime is pinned to the DeepSeek Harness
`0.1.0-rc.6` package family and the fixed `deepseek-v4-pro` / `max` policy for
the qualification phase.

Live Runs resolve `DEEPSEEK_API_KEY` directly or from
`$DSH_CREDENTIALS_FILE`. When that variable is absent, the default document is
`~/.dsh/.credentials.yaml`:

```yaml
DEEPSEEK_API_KEY: replace-locally
```

The document must be a regular non-symlink file inaccessible to group and
other users, for example mode 0600. Never put the credential in source,
command-line arguments, logs, or chat.

Run the finite, read-only L1 qualification fixture with:

```bash
npm run deepseek:l1
```

Sanitized evidence is written under the gitignored
`.fleet/deepseek-l1-results/` directory with private file modes. The DSH Bash
tool intentionally exposes trusted current-session facts such as `DSH_HOME`,
`DSH_SHELL`, and `DSH_SESSION_ID`; Fleet separately prevents DeepSeek, Pactline,
and repository-provider credentials from reaching model tools and fixed
verification commands.

With the local Docker stack running, the authenticated CLI boundary can be
checked without mutating a Task, Claim, Project, or repository:

```bash
PACTLINE_FLEET_PACTLINE_BIN=/absolute/path/to/pactline npm run local:integration
```

The integration issues a temporary development `work:execute` Token, verifies
protocol 2 and all required features, performs bounded execution/review
discovery in one visible Project, then revokes the Token and logs out.

## Dependency boundary

Fleet Core must compile and test without Codex or DeepSeek Harness installed.
The root package has no DSH or Cordis dependency. DeepSeek packages, Cordis
profiles, and the terminal result plugin live under `runtime/deepseek/` and
load only when that Adapter is selected. Adapters never receive the Pactline
Token or repository-provider write credentials.

DeepSeek Harness does not expose prompt-level cancellation or Session close in
the current JSON-RPC protocol. Fleet therefore owns one runtime process per Run
and implements strong cancellation by terminating and reaping that process.
Session resume is deliberately reported as unsupported.

The Replay Adapter is deterministic evaluation infrastructure, not a provider
fallback. A configured Adapter failure remains attributed to that Adapter.

The former DeepSeek-specific Bundle was retired after the standalone Fleet
completed M5.4. DeepSeek remains supported through `runtime/deepseek/` and
`DeepSeekHarnessAdapter`; no active command, test, or package depends on the
retired Bundle source.
