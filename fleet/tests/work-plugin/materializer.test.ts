import { execFile } from 'node:child_process'
import { chmod, mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import type { ExecutionProposal } from '../../src/core/harness-result.js'
import type { FleetWorkDefinition } from '../../src/core/work-definition.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import { prepareWorkspace } from '../../src/repository/workspace.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { PluginRunMaterializer } from '../../src/work-plugin/materializer.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const exec = promisify(execFile)
const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

async function setup() {
  const directory = await mkdtemp(join(tmpdir(), 'fleet-materializer-recovery-'))
  directories.push(directory)
  const origin = join(directory, 'origin.git')
  const seed = join(directory, 'seed')
  const plugin = join(directory, 'plugin.mjs')
  const log = join(directory, 'plugin.log')
  await mkdir(seed)
  await exec('git', ['init', '--quiet', '--bare', origin])
  await exec('git', ['init', '--quiet', seed])
  await writeFile(join(seed, 'README.md'), 'baseline\n')
  await exec('git', ['-C', seed, 'add', 'README.md'])
  await exec('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'baseline'])
  const revision = (await exec('git', ['-C', seed, 'rev-parse', 'HEAD'])).stdout.trim()
  await exec('git', ['-C', seed, 'branch', '-M', 'main'])
  await exec('git', ['-C', seed, 'remote', 'add', 'origin', origin])
  await exec('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])
  await writeFile(plugin, `#!/usr/bin/env node
import { appendFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
let input = ''; for await (const chunk of process.stdin) input += chunk
const request = JSON.parse(input)
const log = process.argv[2]
const operation = process.argv.at(-1)
appendFileSync(log, operation + '\\n')
if (operation === 'commit') {
  execFileSync('git', ['push', 'origin', 'HEAD:refs/heads/main'], { cwd: request.workspace })
  process.exit(91)
} else if (operation === 'push') {
  execFileSync('git', ['push', 'origin', 'HEAD:refs/heads/main'], { cwd: request.workspace })
  process.exit(92)
} else {
  if ('workspace' in request || 'source' in request.definition.base || process.env.LOCAL_TEST_GIT) process.exit(93)
  process.stdout.write(JSON.stringify({ ok: true, data: {
    repository: request.definition.repository,
    codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/77',
    revision: request.push.revision,
    branch: request.push.branch,
    baseRef: request.definition.base.ref
  } }))
}
`)
  await chmod(plugin, 0o700)
  const state = await ensurePrivateDirectory(join(directory, 'state'))
  const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(directory, 'work') })
    .replace('    credentials:\n', `    workPlugin:\n      executable: ${plugin}\n      args: [${log}]\n      timeout: 30s\n    credentials:\n`)
  const snapshot = parseFleetConfig(source, join(directory, 'fleet.yml'), { knownAdapterIds: ['codex'] })
  const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
  registry.recordConfiguration(snapshot)
  await mkdir(join(directory, 'work'))
  const definition: FleetWorkDefinition = {
    caseId: 'materializer-recovery', taskNumber: 21, taskVersion: 1,
    base: { source: origin, ref: 'refs/heads/main', revision },
    repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
    allowedPaths: ['README.md'], verificationCommands: ['true'],
    criteria: [{ id: 'criterion-1', revision: 1 }],
  }
  const run = registry.admitRun('first', {
    taskNumber: 21, taskVersion: 1, stage: 'execution',
    frozenPolicy: {
      definition,
      route: { adapter: 'codex', model: 'test', promptVersion: 'm5.2', resultContractVersion: 1 },
      plugin: { executable: plugin, args: [log], timeoutMs: 30_000 },
      workspaceRoot: join(directory, 'work'),
      pactlineTokenEnv: 'TEST_PACTLINE_TOKEN',
    },
  })
  const workspace = await prepareWorkspace({
    input: definition.base, mode: 'execution', runId: run.runId,
    branchPrefix: 'fleet/first/', temporaryDirectory: join(directory, 'work'),
    taskIdentity: { projectNumber: 5, taskNumber: 21 },
  })
  await writeFile(join(workspace.repositoryPath, 'README.md'), 'implemented\n')
  registry.transitionRun(run.runId, 'admitted', 'claiming')
  registry.transitionRun(run.runId, 'claiming', 'claimed', {
    claimId: 'claim-21', claimTaskVersion: 2,
  })
  registry.transitionRun(run.runId, 'claimed', 'preparing_workspace')
  registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', { workspace: {
    mode: workspace.mode, root: workspace.root, temporaryParent: workspace.temporaryParent,
    repositoryPath: workspace.repositoryPath, source: workspace.source,
    baseRevision: workspace.baseRevision, branch: workspace.branch,
  } })
  registry.transitionRun(run.runId, 'starting_harness', 'running_harness', { runtimeSessionId: 'session-21' })
  const proposal: ExecutionProposal = {
    schemaVersion: 1, kind: 'execution', runId: run.runId, claimId: 'claim-21', taskNumber: 21,
    recommendation: 'complete', summary: 'Implemented.', changedPaths: ['README.md'],
    verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
    criteria: [{ criterionId: 'criterion-1', criterionRevision: 1, outcome: 'passed', evidence: 'verified' }],
    limitations: [],
  }
  registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
  registry.observeEffect(run.runId, 'harness_result', {
    terminalState: 'completed', runtimeSessionId: 'session-21', result: { proposal },
    baseline: { head: revision, changedPaths: [], porcelain: '' },
  })
  registry.transitionRun(run.runId, 'running_harness', 'validating')
  registry.transitionRun(run.runId, 'validating', 'delivering')
  const reopen = async () => {
    const path = registry.path
    registry.close()
    const reopened = await FleetRegistry.open(path)
    const materializer = new PluginRunMaterializer({
      adapters: () => [], environment: { PATH: process.env.PATH }, registry: reopened,
    })
    const invoke = async () => {
      const materialized = await materializer.resume(reopened.getRun(run.runId) as never, new AbortController().signal)
      const publish = materialized?.publishDelivery
      if (publish === undefined) throw new Error('Execution delivery is unavailable')
      return await publish({
        claimId: 'claim-21', taskVersion: 2,
        request: { workspace: workspace.repositoryPath },
      } as never, proposal, { git: { head: revision, changedPaths: ['README.md'], porcelain: '' }, commands: [] })
    }
    return { registry: reopened, invoke }
  }
  const operations = async () => (await readFile(log, 'utf8').catch(() => '')).trim().split('\n').filter(Boolean)
  return { directory, origin, plugin, log, definition, registry, run, workspace, revision, reopen, operations }
}

describe('PluginRunMaterializer recovery', () => {
  it('owns commit and push while keeping the base ref unchanged', async () => {
    const { origin, revision: baseRevision, workspace, reopen, operations } = await setup()

    const recovery = await reopen()
    const delivery = await recovery.invoke()
    const baseAfter = (await exec('git', ['ls-remote', '--refs', origin, 'refs/heads/main'])).stdout.split(/\s+/)[0]
    const deliveryAfter = (await exec('git', ['ls-remote', '--refs', origin, `refs/heads/${workspace.branch!}`])).stdout.split(/\s+/)[0]

    expect(workspace.branch).toBe('fleet/project-5/task-21')
    expect(baseAfter).toBe(baseRevision)
    expect(deliveryAfter).toBe(delivery.revision)
    expect(delivery.branch).toBe(workspace.branch)
    expect(await operations()).toEqual(['open-code-change'])
    recovery.registry.close()
  })

  it('materializes review-first work with both admitted base and candidate revisions', async () => {
    const { directory, origin, plugin, log, definition, registry, workspace, revision } = await setup()
    await exec('git', ['-C', workspace.repositoryPath, 'add', 'README.md'])
    await exec('git', ['-C', workspace.repositoryPath, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'candidate'])
    const candidateRevision = (await exec('git', ['-C', workspace.repositoryPath, 'rev-parse', 'HEAD'])).stdout.trim()
    await exec('git', ['-C', workspace.repositoryPath, 'push', '--quiet', 'origin', 'HEAD:refs/heads/candidate'])
    const reviewDefinition: FleetWorkDefinition = {
      ...definition,
      taskNumber: 22,
      candidate: {
        repository: definition.repository,
        codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/88',
        revision: candidateRevision,
        branch: 'candidate',
        ref: 'refs/heads/candidate',
      },
    }
    const reviewRun = registry.admitRun('first', {
      taskNumber: 22, taskVersion: 1, stage: 'review',
      frozenPolicy: {
        definition: reviewDefinition,
        route: { adapter: 'codex', model: 'test', promptVersion: 'm5.2', resultContractVersion: 1 },
        plugin: { executable: plugin, args: [log], timeoutMs: 30_000 },
        workspaceRoot: join(directory, 'work'),
        pactlineTokenEnv: 'TEST_PACTLINE_TOKEN',
      },
    })
    const materializer = new PluginRunMaterializer({
      adapters: () => [], environment: { PATH: process.env.PATH }, registry,
    })
    const materialized = await materializer.materialize(reviewRun, new AbortController().signal)
    const reviewWorkspace = await materialized.prepareWorkspace(new AbortController().signal)

    expect(reviewWorkspace.baseRevision).toBe(revision)
    expect((await exec('git', ['-C', reviewWorkspace.repositoryPath, 'rev-parse', 'HEAD'])).stdout.trim()).toBe(candidateRevision)
    await expect(exec('git', ['-C', reviewWorkspace.repositoryPath, 'cat-file', '-e', `${revision}^{commit}`])).resolves.toBeDefined()
    registry.close()
  })

  it('materializes delivery-free correction from the retained Task Workspace', async () => {
    const { directory, plugin, log, definition, registry, run, workspace } = await setup()
    registry.bindTaskWorkspace(5, 21, workspace)
    registry.bindTaskRoleSession(5, 21, 'implementer', {
      adapterId: 'codex', runtimeSessionId: 'retained-implementer-session',
    })
    registry.transitionRun(run.runId, 'delivering', 'releasing')
    registry.transitionRun(run.runId, 'releasing', 'released', { disposition: 'verification_mismatch' })
    const correctionRun = registry.admitRun('first', {
      taskNumber: 21, taskVersion: 2, stage: 'correction',
      frozenPolicy: {
        definition: { ...definition, taskVersion: 2 },
        route: { adapter: 'codex', model: 'test', promptVersion: 'm5.2', resultContractVersion: 1 },
        plugin: { executable: plugin, args: [log], timeoutMs: 30_000 },
        workspaceRoot: join(directory, 'work'),
        pactlineTokenEnv: 'TEST_PACTLINE_TOKEN',
      },
    })
    const materializer = new PluginRunMaterializer({
      adapters: () => [], environment: { PATH: process.env.PATH }, registry,
    })

    const materialized = await materializer.materialize(correctionRun, new AbortController().signal)
    await expect(materialized.prepareWorkspace(new AbortController().signal)).resolves.toMatchObject({
      root: workspace.root,
      branch: workspace.branch,
    })
    expect(registry.getTaskRuntime(5, 21)).toMatchObject({
      sessions: { implementer: { runtimeSessionId: 'retained-implementer-session' } },
    })
    registry.close()
  })

  it('retires persisted Task runtime only after Pactline reports done or cancelled', async () => {
    const { registry, run, workspace } = await setup()
    registry.bindTaskWorkspace(5, 21, workspace)
    const materializer = new PluginRunMaterializer({
      adapters: () => [], environment: { PATH: process.env.PATH }, registry,
    })
    const activeClient = {
      showTask: async () => ({ data: { task: { phase: 'in_review' } } }),
    }
    const terminalClient = {
      showTask: async () => ({ data: { task: { phase: 'cancelled' } } }),
    }

    await expect(materializer.retireTerminalTasks(activeClient as never, 'service')).resolves.toBe(0)
    expect(registry.getTaskRuntime(5, 21)).toBeDefined()
    await expect(materializer.retireTerminalTasks(terminalClient as never, 'service')).resolves.toBe(0)
    registry.transitionRun(run.runId, 'delivering', 'quarantined', { disposition: 'test terminal' })
    await expect(materializer.retireTerminalTasks(terminalClient as never, 'service')).resolves.toBe(1)
    expect(registry.getTaskRuntime(5, 21)).toBeUndefined()
    await expect(readFile(join(workspace.repositoryPath, 'README.md'), 'utf8')).rejects.toThrow()
    registry.close()
  })

  it('finishes Task runtime retirement after a crash removed the workspace first', async () => {
    const { registry, run, workspace } = await setup()
    registry.bindTaskWorkspace(5, 21, workspace)
    registry.transitionRun(run.runId, 'delivering', 'quarantined', { disposition: 'test terminal' })
    await rm(workspace.root, { recursive: true, force: false })
    const materializer = new PluginRunMaterializer({
      adapters: () => [], environment: { PATH: process.env.PATH }, registry,
    })

    await expect(materializer.retireTerminalTasks({
      showTask: async () => ({ data: { task: { phase: 'done' } } }),
    } as never, 'service')).resolves.toBe(1)
    expect(registry.getTaskRuntime(5, 21)).toBeUndefined()
    registry.close()
  })

  it('reconciles a committed local revision instead of invoking commit again', async () => {
    const { registry, run, workspace, reopen, operations } = await setup()
    registry.recordEffectIntent(run.runId, 'git_commit', `${run.runId}-commit`, {})
    await exec('git', ['-C', workspace.repositoryPath, 'add', 'README.md'])
    await exec('git', ['-C', workspace.repositoryPath, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'delivery'])

    const recovery = await reopen()
    await expect(recovery.invoke()).resolves.toMatchObject({ codeChangeUrl: expect.stringContaining('/pull/77') })
    expect(await operations()).toEqual(['open-code-change'])
    expect(recovery.registry.getEffect(run.runId, 'git_commit')).toMatchObject({ status: 'observed' })
    recovery.registry.close()
  })

  it('reconciles a matching remote branch instead of invoking push again', async () => {
    const { registry, run, workspace, reopen, operations } = await setup()
    await exec('git', ['-C', workspace.repositoryPath, 'add', 'README.md'])
    await exec('git', ['-C', workspace.repositoryPath, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'delivery'])
    const revision = (await exec('git', ['-C', workspace.repositoryPath, 'rev-parse', 'HEAD'])).stdout.trim()
    const branch = workspace.branch!
    await exec('git', ['-C', workspace.repositoryPath, 'push', '--quiet', 'origin', `HEAD:refs/heads/${branch}`])
    registry.recordEffectIntent(run.runId, 'git_commit', `${run.runId}-commit`, {})
    registry.observeEffect(run.runId, 'git_commit', { revision, branch })
    registry.recordEffectIntent(run.runId, 'git_push', `${run.runId}-push`, { revision, branch })

    const recovery = await reopen()
    await expect(recovery.invoke()).resolves.toMatchObject({ revision, branch })
    expect(await operations()).toEqual(['open-code-change'])
    expect(recovery.registry.getEffect(run.runId, 'git_push')).toMatchObject({ status: 'observed' })
    recovery.registry.close()
  })

  it('does not repeat an unobserved code-change creation', async () => {
    const { registry, run, workspace, reopen, operations } = await setup()
    await exec('git', ['-C', workspace.repositoryPath, 'add', 'README.md'])
    await exec('git', ['-C', workspace.repositoryPath, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'delivery'])
    const revision = (await exec('git', ['-C', workspace.repositoryPath, 'rev-parse', 'HEAD'])).stdout.trim()
    const branch = workspace.branch!
    await exec('git', ['-C', workspace.repositoryPath, 'push', '--quiet', 'origin', `HEAD:refs/heads/${branch}`])
    registry.recordEffectIntent(run.runId, 'git_commit', `${run.runId}-commit`, {})
    registry.observeEffect(run.runId, 'git_commit', { revision, branch })
    registry.recordEffectIntent(run.runId, 'git_push', `${run.runId}-push`, { revision, branch })
    registry.observeEffect(run.runId, 'git_push', { revision, branch })
    registry.recordEffectIntent(run.runId, 'code_change_creation', `${run.runId}-code-change`, { revision, branch })

    const recovery = await reopen()
    await expect(recovery.invoke()).rejects.toThrow('Code-change creation intent has no authoritative recovery observation')
    expect(await operations()).toEqual([])
    recovery.registry.close()
  })
})
