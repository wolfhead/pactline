import { execFile } from 'node:child_process'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import { ReplayHarnessAdapter } from '../../src/adapters/replay/replay-adapter.js'
import { parseFleetConfig } from '../../src/config/load.js'
import type { FleetDefinitionConfig } from '../../src/config/types.js'
import type { ExecutionProposal, HarnessRunResult } from '../../src/core/harness-result.js'
import type { HarnessAdapter, HarnessCapabilities, HarnessRunObserver, HarnessRunRequest } from '../../src/core/harness-adapter.js'
import { StaticRuntimeRouter } from '../../src/core/runtime-router.js'
import type { RuntimeRoutes } from '../../src/core/runtime-router.js'
import type { FleetWorkDefinition } from '../../src/core/work-definition.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import type { FleetWorkCandidate } from '../../src/scheduler/candidate.js'
import { ClaimStageRunCoordinator, FleetInjectedCrash } from '../../src/scheduler/run-coordinator.js'
import { prepareWorkspace, removeWorkspace } from '../../src/repository/workspace.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { InMemoryPactlineClient } from '../contract/in-memory-pactline.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const exec = promisify(execFile)
const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

async function fixture(): Promise<{
  directory: string
  origin: string
  revision: string
  registry: FleetRegistry
  fleet: FleetDefinitionConfig
}> {
  const directory = await mkdtemp(join(tmpdir(), 'fleet-coordinator-test-'))
  directories.push(directory)
  const origin = join(directory, 'origin.git')
  const seed = join(directory, 'seed')
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
  const state = await ensurePrivateDirectory(join(directory, 'state'))
  const snapshot = parseFleetConfig(serviceConfigYAML({
    stateDirectory: state, firstWorkspace: join(directory, 'work'),
  }), join(directory, 'fleet.yml'), { knownAdapterIds: ['replay', 'codex'] })
  const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
  registry.recordConfiguration(snapshot)
  return { directory, origin, revision, registry, fleet: snapshot.config.fleets.first! }
}

function routes(): RuntimeRoutes {
  const route = { adapterId: 'replay', model: 'quality', reasoning: 'max', promptVersion: 'm5.2', resultContractVersion: 1 }
  return { execution: route, correction: route, review: route, resolution_analysis: route }
}

describe('ClaimStageRunCoordinator', () => {
  it('checkpoints Claim, workspace, Session, result, delivery, and settlement', async () => {
    const { directory, origin, revision, registry, fleet } = await fixture()
    const criterion = { id: 'criterion-1', revision: 1 }
    const client = new InMemoryPactlineClient(21, [criterion])
    const candidate: FleetWorkCandidate = {
      fleetId: fleet.id, projectNumber: fleet.projectNumber, stage: 'execution',
      task: { id: 'task-21', number: 21, title: 'Test', version: 1, phase: 'ready', activity: 'available' },
    }
    const definition: FleetWorkDefinition = {
      caseId: 'M5.2', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision },
      repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
    }
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'replay-session',
      effect: async request => { await writeFile(join(request.workspace, 'README.md'), 'implemented\n') },
      result: request => {
        const proposal: ExecutionProposal = {
          schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId,
          taskNumber: 21, recommendation: 'complete', summary: 'Implemented.', changedPaths: ['README.md'],
          verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
          criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'verified' }],
          limitations: [],
        }
        const result: HarnessRunResult = {
          adapterId: 'replay', adapterVersion: '1.0.0', runtimeSessionId: 'replay-session',
          model: { provider: 'replay', model: 'quality', reasoning: 'max' }, terminalState: 'completed',
          proposal, usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
        }
        return result
      },
    }])
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'fleet-service-test',
      materializer: {
        materialize() {
          return Promise.resolve({
            definition, router: new StaticRuntimeRouter([adapter], routes()), deadline: '2026-08-17T00:00:00Z',
            prepareWorkspace: () => prepareWorkspace({ input: definition.base, mode: 'execution', runId: 'task-21', temporaryDirectory: directory }),
            publishDelivery: () => Promise.resolve({
              repository: definition.repository,
              codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/52', revision: 'b'.repeat(40), branch: 'fleet/run/21',
            }),
            cleanup: workspace => removeWorkspace(workspace),
          })
        },
      },
    })
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution',
      frozenPolicy: { route: routes().execution, definition },
    })

    await expect(coordinator.execute(run, candidate, fleet, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
    expect(registry.getRun(run.runId)).toMatchObject({
      state: 'completed', claimId: expect.any(String), runtimeSessionId: 'replay-session', checkpoint: 'settlement_observed',
    })
    expect(new Set(registry.listEffects(run.runId).map(effect => `${effect.kind}:${effect.status}`))).toEqual(new Set([
      'claim:observed', 'workspace:observed', 'adapter_session:observed', 'harness_result:observed',
      'repository_delivery:observed', 'pactline_settlement:observed',
    ]))
    registry.close()
  })

  it('releases a known active Claim when its Adapter Session cannot resume', async () => {
    const { registry, fleet } = await fixture()
    const client = new InMemoryPactlineClient(21, [{ id: 'criterion-1', revision: 1 }])
    const claimed = await client.claimTask(21, 1, 'execution', {
      sessionId: 'other', idempotencyKey: 'external-claim',
    })
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution', frozenPolicy: { adapter: 'deepseek' },
    })
    registry.transitionRun(run.runId, 'admitted', 'claiming')
    registry.transitionRun(run.runId, 'claiming', 'claimed', { claimId: claimed.data.claim.id })
    registry.transitionRun(run.runId, 'claimed', 'preparing_workspace')
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness')
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', { runtimeSessionId: 'deepseek-session' })
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'fleet-service-test',
      materializer: { materialize: () => Promise.reject(new Error('not used')) },
    })

    await expect(coordinator.recover(registry.getRun(run.runId)!, new AbortController().signal)).resolves.toEqual({
      kind: 'released', reason: 'adapter_session_not_resumable',
    })
    expect(client.phase).toBe('in_progress')
    expect(client.activity).toBe('available')
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'released', checkpoint: 'release_observed' })
    registry.close()
  })

  it('persists the Session before the first Agent effect and converges after an injected crash', async () => {
    const { directory, origin, revision, registry, fleet } = await fixture()
    const criterion = { id: 'criterion-1', revision: 1 }
    const client = new InMemoryPactlineClient(21, [criterion])
    const definition: FleetWorkDefinition = {
      caseId: 'crash-test', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision },
      repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
    }
    let agentEffect = false
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'crash-session',
      effect: () => { agentEffect = true },
      result: () => { throw new Error('must not reach result') },
    }])
    let retainedWorkspace: Awaited<ReturnType<typeof prepareWorkspace>> | undefined
    const materializer = {
      materialize: () => Promise.resolve({
        definition, router: new StaticRuntimeRouter([adapter], routes()), deadline: '2026-08-17T00:00:00Z',
        prepareWorkspace: async () => {
          retainedWorkspace = await prepareWorkspace({ input: definition.base, mode: 'execution', runId: 'crash-21', temporaryDirectory: directory })
          return retainedWorkspace
        },
      }),
      cleanupRecovered: async () => {
        if (retainedWorkspace !== undefined) await removeWorkspace(retainedWorkspace)
      },
    }
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, materializer, clientSessionId: 'fleet-service-test',
      faultInjector: checkpoint => {
        if (checkpoint === 'after_session_persistence_before_agent') throw new FleetInjectedCrash(checkpoint)
      },
    })
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution', frozenPolicy: { adapter: 'deepseek' },
    })
    const candidate: FleetWorkCandidate = {
      fleetId: fleet.id, projectNumber: fleet.projectNumber, stage: 'execution',
      task: { id: 'task-21', number: 21, title: 'Crash', version: 1, phase: 'ready', activity: 'available' },
    }

    await expect(coordinator.execute(run, candidate, fleet, new AbortController().signal)).rejects.toMatchObject({
      checkpoint: 'after_session_persistence_before_agent',
    })
    expect(agentEffect).toBe(false)
    expect(registry.getRun(run.runId)).toMatchObject({
      state: 'running_harness', runtimeSessionId: 'crash-session', checkpoint: 'adapter_session_observed',
    })
    const recovery = new ClaimStageRunCoordinator({
      registry, client, materializer, clientSessionId: 'fleet-service-test',
    })
    await expect(recovery.recover(registry.getRun(run.runId)!, new AbortController().signal)).resolves.toMatchObject({ kind: 'released' })
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'released', checkpoint: 'release_observed' })
    registry.close()
  })

  it('resumes the exact recorded Codex-style Session with the post-Claim Task version', async () => {
    const { directory, origin, revision, registry, fleet } = await fixture()
    const criterion = { id: 'criterion-1', revision: 1 }
    const client = new InMemoryPactlineClient(21, [criterion])
    const claimed = await client.claimTask(21, 1, 'execution', { sessionId: 'first-service', idempotencyKey: 'claim' })
    const definition: FleetWorkDefinition = {
      caseId: 'resume-test', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision },
      repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
    }
    const workspace = await prepareWorkspace({ input: definition.base, mode: 'execution', runId: 'resume-21', temporaryDirectory: directory })
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'resumed\n')
    let resumedSession: string | undefined
    const capabilities: HarnessCapabilities = {
      nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: true,
      sandboxModes: ['workspace_write'], supportedStages: ['execution'],
    }
    const adapter: HarnessAdapter = {
      id: 'codex', version: 'resume-test',
      probe: async () => capabilities,
      run: () => Promise.reject(new Error('recovery must resume')),
      async resume(runtimeSessionId: string, request: HarnessRunRequest, observer: HarnessRunObserver): Promise<HarnessRunResult> {
        resumedSession = runtimeSessionId
        await observer.onSessionStarted({ runtimeSessionId })
        const proposal: ExecutionProposal = {
          schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId, taskNumber: 21,
          recommendation: 'complete', summary: 'Resumed.', changedPaths: ['README.md'],
          verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
          criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'resumed verification' }],
          limitations: [],
        }
        return {
          adapterId: 'codex', adapterVersion: 'resume-test', runtimeSessionId,
          model: { provider: 'openai-codex', model: 'gpt-5.6-sol', reasoning: 'high' }, terminalState: 'completed', proposal,
          usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
        }
      },
    }
    const route = { adapterId: 'codex', model: 'gpt-5.6-sol', reasoning: 'high', promptVersion: 'm5.2', resultContractVersion: 1 }
    const router = new StaticRuntimeRouter([adapter], { execution: route, correction: route, review: route, resolution_analysis: route })
    const materialized = {
      definition, router, deadline: '2026-08-17T00:00:00Z',
      prepareWorkspace: async () => workspace,
      publishDelivery: async () => ({
        repository: definition.repository,
        codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/53', revision: 'c'.repeat(40), branch: workspace.branch!,
      }),
      cleanup: async () => { await removeWorkspace(workspace) },
    }
    const materializer = {
      materialize: () => Promise.reject(new Error('not used')),
      resume: () => Promise.resolve(materialized),
    }
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution', frozenPolicy: { adapter: 'codex' },
    })
    registry.transitionRun(run.runId, 'admitted', 'claiming')
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: claimed.data.claim.id,
      claimVersion: claimed.data.claim.version,
      claimTaskVersion: claimed.data.task.version,
    })
    registry.transitionRun(run.runId, 'claimed', 'preparing_workspace')
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', { workspace: {
      mode: workspace.mode, root: workspace.root, temporaryParent: workspace.temporaryParent,
      repositoryPath: workspace.repositoryPath, source: workspace.source, baseRevision: workspace.baseRevision, branch: workspace.branch,
    } })
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', {
      runtimeSessionId: 'codex-session-21',
    })
    const coordinator = new ClaimStageRunCoordinator({ registry, client, materializer, clientSessionId: 'second-service-session' })

    await expect(coordinator.recover(registry.getRun(run.runId)!, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
    expect(resumedSession).toBe('codex-session-21')
    expect(client.phase).toBe('in_review')
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'completed', claimTaskVersion: 2 })
    registry.close()
  })

  it('reconciles a terminal Pactline settlement before attempting Harness resume', async () => {
    const { registry, fleet } = await fixture()
    const client = new InMemoryPactlineClient(21, [{ id: 'criterion-1', revision: 1 }])
    const claimed = await client.claimTask(21, 1, 'execution', { sessionId: 'first', idempotencyKey: 'claim' })
    await client.releaseClaim(claimed.data.claim.id, claimed.data.task.version, 'Unable.', {
      sessionId: 'first', idempotencyKey: 'release',
    })
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution', frozenPolicy: { adapter: 'deepseek' },
    })
    registry.transitionRun(run.runId, 'admitted', 'claiming')
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: claimed.data.claim.id, claimVersion: 1, claimTaskVersion: claimed.data.task.version,
    })
    registry.transitionRun(run.runId, 'claimed', 'preparing_workspace')
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness')
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', { runtimeSessionId: 'deepseek-session' })
    registry.transitionRun(run.runId, 'running_harness', 'validating')
    registry.transitionRun(run.runId, 'validating', 'settling')
    registry.recordEffectIntent(run.runId, 'pactline_settlement', `${run.runId}-settle`, {})
    let resumeCalls = 0
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'recovery',
      materializer: {
        materialize: () => Promise.reject(new Error('not used')),
        resume: () => { resumeCalls += 1; return Promise.reject(new Error('must not resume')) },
      },
    })

    await expect(coordinator.recover(registry.getRun(run.runId)!, new AbortController().signal)).resolves.toMatchObject({ kind: 'released' })
    expect(resumeCalls).toBe(0)
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'released', checkpoint: 'settlement_reconciled' })
    expect(registry.getEffect(run.runId, 'pactline_settlement')).toMatchObject({ status: 'observed' })
    registry.close()
  })
})
