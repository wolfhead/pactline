import { execFile } from 'node:child_process'
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import { ReplayHarnessAdapter } from '../../src/adapters/replay/replay-adapter.js'
import { parseFleetConfig } from '../../src/config/load.js'
import type { FleetDefinitionConfig } from '../../src/config/types.js'
import type { ExecutionProposal, HarnessRunResult, ReviewProposal } from '../../src/core/harness-result.js'
import type { HarnessAdapter, HarnessCapabilities, HarnessRunObserver, HarnessRunRequest } from '../../src/core/harness-adapter.js'
import { StaticRuntimeRouter } from '../../src/core/runtime-router.js'
import type { RuntimeRoutes } from '../../src/core/runtime-router.js'
import type { FleetWorkDefinition } from '../../src/core/work-definition.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import { ClaimStageRunCoordinator, FleetInjectedCrash } from '../../src/scheduler/run-coordinator.js'
import { prepareWorkspace, removeWorkspace } from '../../src/repository/workspace.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { InMemoryPactlineClient } from '../contract/in-memory-pactline.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'
import { frozenPluginPolicy } from '../../src/work-plugin/executable-plugin.js'

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

function persistedWorkspace(): Readonly<Record<string, unknown>> {
  return {
    mode: 'execution', root: '/tmp/fleet-test', temporaryParent: '/tmp',
    repositoryPath: '/tmp/fleet-test/repository', source: '/tmp/fleet-test/origin.git',
    baseRevision: 'a'.repeat(40), branch: 'fleet/test',
  }
}

function unableProposal(runId: string, claimId: string): ExecutionProposal {
  return {
    schemaVersion: 1,
    kind: 'execution',
    runId,
    claimId,
    taskNumber: 21,
    recommendation: 'unable_to_complete',
    summary: 'Unable.',
    verification: [],
    criteria: [{ criterionId: 'criterion-1', criterionRevision: 1, outcome: 'unable', evidence: 'blocked' }],
    limitations: ['blocked'],
    changedPaths: [],
  }
}

describe('ClaimStageRunCoordinator', () => {
  it('resumes the Task implementer Session across a new Run and Claim without replacing the Workspace', async () => {
    const { directory, origin, revision, registry, fleet } = await fixture()
    const criterion = { id: 'criterion-1', revision: 1 }
    const client = new InMemoryPactlineClient(21, [criterion])
    const definition: FleetWorkDefinition = {
      caseId: 'task-runtime-continuity', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision },
      repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
    }
    const workspace = await prepareWorkspace({
      input: definition.base, mode: 'execution', runId: 'task-21-original', temporaryDirectory: directory,
      taskIdentity: { projectNumber: 5, taskNumber: 21 },
    })
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'retained implementation\n')
    registry.bindTaskWorkspace(5, 21, workspace)
    registry.bindTaskRoleSession(5, 21, 'implementer', {
      adapterId: 'replay', runtimeSessionId: 'implementer-task-21',
    })
    let resumedSession: string | undefined
    const capabilities: HarnessCapabilities = {
      nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: true,
      sandboxModes: ['workspace_write'], supportedStages: ['execution', 'correction'],
    }
    const adapter: HarnessAdapter = {
      id: 'replay', version: 'task-runtime-test', probe: async () => capabilities,
      run: () => Promise.reject(new Error('Task role Session must be resumed')),
      async resume(runtimeSessionId, request, observer) {
        resumedSession = runtimeSessionId
        await observer.onSessionStarted({ runtimeSessionId })
        return {
          adapterId: 'replay', adapterVersion: 'task-runtime-test', runtimeSessionId,
          model: { provider: 'replay', model: 'quality', reasoning: 'max' }, terminalState: 'completed',
          proposal: {
            schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId,
            taskNumber: 21, recommendation: 'complete', summary: 'Continued retained work.',
            changedPaths: ['README.md'],
            verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
            criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'continued' }],
            limitations: [],
          },
          usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
        }
      },
    }
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution', frozenPolicy: { definition, route: routes().execution },
    })
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'fleet-service-test',
      materializer: {
        materialize: async () => ({
          definition, router: new StaticRuntimeRouter([adapter], routes()),
          deadline: '2099-08-17T00:00:00Z', prepareWorkspace: async () => workspace,
          publishDelivery: async () => ({
            repository: definition.repository, codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/81',
            revision: 'b'.repeat(40), branch: workspace.branch!,
          }),
        }),
      },
    })

    await expect(coordinator.execute(run.runId, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
    expect(resumedSession).toBe('implementer-task-21')
    expect(registry.getTaskRuntime(5, 21)).toMatchObject({
      workspace: { root: workspace.root },
      sessions: { implementer: { runtimeSessionId: 'implementer-task-21' } },
    })
    expect(client.phase).toBe('in_review')
    registry.close()
  })

  it('uses a separate reviewer Session in the retained Workspace and retires it only when the Task is done', async () => {
    const { directory, origin, revision, registry, fleet } = await fixture()
    const criterion = { id: 'criterion-1', revision: 1 }
    const client = new InMemoryPactlineClient(21, [criterion], { phase: 'in_review' })
    const workspace = await prepareWorkspace({
      input: { source: origin, ref: 'refs/heads/main', revision },
      mode: 'execution', runId: 'task-21-review', temporaryDirectory: directory,
      taskIdentity: { projectNumber: 5, taskNumber: 21 },
    })
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'review candidate\n')
    await exec('git', ['-C', workspace.repositoryPath, 'add', 'README.md'])
    await exec('git', ['-C', workspace.repositoryPath, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'candidate'])
    const candidateRevision = (await exec('git', ['-C', workspace.repositoryPath, 'rev-parse', 'HEAD'])).stdout.trim()
    const repository = { provider: 'github' as const, host: 'github.com', owner: 'wolfhead', name: 'pactline' }
    const definition: FleetWorkDefinition = {
      caseId: 'task-review-continuity', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision }, repository,
      candidate: {
        repository, codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/82',
        revision: candidateRevision, branch: workspace.branch!, ref: `refs/heads/${workspace.branch!}`,
      },
      allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
    }
    registry.bindTaskWorkspace(5, 21, workspace)
    registry.bindTaskRoleSession(5, 21, 'implementer', {
      adapterId: 'replay', runtimeSessionId: 'implementer-task-21',
    })
    const capabilities: HarnessCapabilities = {
      nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: true,
      sandboxModes: ['workspace_write'], supportedStages: ['review'],
    }
    const adapter: HarnessAdapter = {
      id: 'replay', version: 'task-review-test', probe: async () => capabilities,
      async run(request, observer) {
        await observer.onSessionStarted({ runtimeSessionId: 'reviewer-task-21' })
        return {
          adapterId: 'replay', adapterVersion: 'task-review-test', runtimeSessionId: 'reviewer-task-21',
          model: { provider: 'replay', model: 'quality', reasoning: 'max' }, terminalState: 'completed',
          proposal: {
            schemaVersion: 1, kind: 'review', runId: request.runId, claimId: request.claimId,
            taskNumber: 21, recommendation: 'accept', summary: 'Reviewed retained delivery.', findings: [],
            verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
            criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'reviewed' }],
            limitations: [],
          },
          usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
        }
      },
    }
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'review', frozenPolicy: { definition, route: routes().review },
    })
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'fleet-service-test',
      materializer: {
        materialize: async () => ({
          definition, router: new StaticRuntimeRouter([adapter], routes()),
          deadline: '2099-08-17T00:00:00Z', prepareWorkspace: async () => workspace,
          retireTask: async value => {
            await removeWorkspace(value)
            registry.retireTaskRuntime(5, 21)
          },
        }),
      },
    })

    await expect(coordinator.execute(run.runId, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
    expect(client.phase).toBe('done')
    expect(registry.getTaskRuntime(5, 21)).toBeUndefined()
    await expect(readFile(join(workspace.repositoryPath, 'README.md'), 'utf8')).rejects.toThrow()
    registry.close()
  })

  it('checkpoints Claim, workspace, Session, result, delivery, and settlement', async () => {
    const { directory, origin, revision, registry, fleet } = await fixture()
    const criterion = { id: 'criterion-1', revision: 1 }
    const client = new InMemoryPactlineClient(21, [criterion])
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
    let deliveryState: string | undefined
    let settlementIntentBeforeDelivery = false
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'fleet-service-test',
      materializer: {
        materialize(...args) {
          expect(args).toHaveLength(2)
          const [admitted, signal] = args
          expect(signal).toBeInstanceOf(AbortSignal)
          expect(admitted).toMatchObject({
            taskNumber: 21,
            taskVersion: 1,
            stage: 'execution',
            frozenPolicy: { definition, pactlineTokenEnv: 'TEST_PACTLINE_TOKEN' },
          })
          const policy = frozenPluginPolicy(admitted)
          const route = {
            adapterId: policy.route.adapter,
            model: policy.route.model,
            ...(policy.route.reasoning === undefined ? {} : { reasoning: policy.route.reasoning }),
            promptVersion: policy.route.promptVersion,
            resultContractVersion: policy.route.resultContractVersion,
          }
          return Promise.resolve({
            definition: policy.definition,
            router: new StaticRuntimeRouter([adapter], {
              execution: route, correction: route, review: route, resolution_analysis: route,
            }),
            deadline: '2026-08-17T00:00:00Z',
            prepareWorkspace: () => prepareWorkspace({
              input: policy.definition.base, mode: 'execution', runId: 'task-21', temporaryDirectory: directory,
            }),
            publishDelivery: () => {
              deliveryState = registry.getRun(run.runId)?.state
              settlementIntentBeforeDelivery = registry.getEffect(run.runId, 'pactline_settlement') !== undefined
              return Promise.resolve({
                repository: policy.definition.repository,
                codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/52', revision: 'b'.repeat(40), branch: 'fleet/run/21',
              })
            },
            cleanup: workspace => removeWorkspace(workspace),
          })
        },
      },
    })
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution',
      frozenPolicy: {
        definition,
        route: {
          adapter: 'replay', model: 'quality', reasoning: 'max',
          promptVersion: 'm5.2', resultContractVersion: 1,
        },
        plugin: { executable: '/not-used/frozen-plugin', args: [], timeoutMs: 1_000 },
        workspaceRoot: directory,
        pactlineTokenEnv: 'TEST_PACTLINE_TOKEN',
      },
    })

    await expect(coordinator.execute(run.runId, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
    expect(registry.getRun(run.runId)).toMatchObject({
      state: 'completed', claimId: expect.any(String), runtimeSessionId: 'replay-session', checkpoint: 'settlement_observed',
    })
    expect(new Set(registry.listEffects(run.runId).map(effect => `${effect.kind}:${effect.status}`))).toEqual(new Set([
      'claim:observed', 'workspace:observed', 'adapter_session:observed', 'harness_result:observed',
      'repository_delivery:observed', 'pactline_settlement:observed',
    ]))
    expect(deliveryState).toBe('delivering')
    expect(settlementIntentBeforeDelivery).toBe(false)
    expect(registry.listRunEvents(run.runId)
      .filter(event => event.eventType === 'run.transitioned')
      .map(event => event.payload.to)).toEqual([
      'claiming', 'claimed', 'preparing_workspace', 'starting_harness', 'running_harness',
      'validating', 'delivering', 'settling', 'completed',
    ])
    registry.close()
  })

  it.each(['execution', 'review'] as const)(
    'settles a non-delivery %s Run without entering delivering',
    async stage => {
      const { directory, origin, revision, registry, fleet } = await fixture()
      const criterion = { id: 'criterion-1', revision: 1 }
      const client = new InMemoryPactlineClient(21, [criterion], stage === 'review' ? { phase: 'in_review' } : {})
      const definition: FleetWorkDefinition = {
        caseId: `non-delivery-${stage}`, taskNumber: 21, taskVersion: 1,
        base: { source: origin, ref: 'refs/heads/main', revision },
        repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
        allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
      }
      const adapter = new ReplayHarnessAdapter([{
        sessionId: `non-delivery-${stage}-session`,
        result: request => {
          const proposal: ExecutionProposal | ReviewProposal = stage === 'execution'
            ? {
                schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId,
                taskNumber: 21, recommendation: 'unable_to_complete', summary: 'Unable.', changedPaths: [],
                verification: [],
                criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'unable', evidence: 'blocked' }],
                limitations: [],
              }
            : {
                schemaVersion: 1, kind: 'review', runId: request.runId, claimId: request.claimId,
                taskNumber: 21, recommendation: 'accept', summary: 'Accepted.', findings: [],
                verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
                criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'reviewed' }],
                limitations: [],
              }
          return {
            adapterId: 'replay', adapterVersion: '1.0.0', runtimeSessionId: `non-delivery-${stage}-session`,
            model: { provider: 'replay', model: 'quality', reasoning: 'max' }, terminalState: 'completed',
            proposal, usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
          }
        },
      }])
      let deliveryCalls = 0
      const coordinator = new ClaimStageRunCoordinator({
        registry, client, clientSessionId: 'fleet-service-test',
        materializer: {
          materialize: () => Promise.resolve({
            definition, router: new StaticRuntimeRouter([adapter], routes()), deadline: '2026-08-17T00:00:00Z',
            prepareWorkspace: () => prepareWorkspace({
              input: definition.base, mode: stage === 'review' ? 'review' : 'execution',
              runId: `non-delivery-${stage}`, temporaryDirectory: directory,
            }),
            publishDelivery: () => {
              deliveryCalls += 1
              throw new Error('non-delivery settlement must not publish')
            },
            cleanup: workspace => removeWorkspace(workspace),
          }),
        },
      })
      const run = registry.admitRun(fleet.id, {
        taskNumber: 21, taskVersion: 1, stage, frozenPolicy: { route: routes()[stage], definition },
      })

      await expect(coordinator.execute(run.runId, new AbortController().signal)).resolves.toEqual(
        stage === 'execution' ? { kind: 'released', reason: 'released' } : { kind: 'completed' },
      )
      expect(deliveryCalls).toBe(0)
      expect(registry.getEffect(run.runId, 'repository_delivery')).toBeUndefined()
      expect(registry.listRunEvents(run.runId)
        .filter(event => event.eventType === 'run.transitioned')
        .map(event => event.payload.to)).toEqual([
        'claiming', 'claimed', 'preparing_workspace', 'starting_harness', 'running_harness',
        'validating', 'settling', stage === 'execution' ? 'released' : 'completed',
      ])
      registry.close()
    },
  )

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
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: claimed.data.claim.id,
      claimVersion: claimed.data.claim.version,
      claimTaskVersion: claimed.data.task.version,
    })
    registry.transitionRun(run.runId, 'claimed', 'preparing_workspace')
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', { workspace: persistedWorkspace() })
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', { runtimeSessionId: 'deepseek-session' })
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'fleet-service-test',
      materializer: { materialize: () => Promise.reject(new Error('not used')) },
    })

    await expect(coordinator.recover(run.runId, new AbortController().signal)).resolves.toEqual({
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
    await expect(coordinator.execute(run.runId, new AbortController().signal)).rejects.toMatchObject({
      checkpoint: 'after_session_persistence_before_agent',
    })
    expect(agentEffect).toBe(false)
    expect(registry.getRun(run.runId)).toMatchObject({
      state: 'running_harness', runtimeSessionId: 'crash-session', checkpoint: 'adapter_session_observed',
    })
    const recovery = new ClaimStageRunCoordinator({
      registry, client, materializer, clientSessionId: 'fleet-service-test',
    })
    await expect(recovery.recover(run.runId, new AbortController().signal)).resolves.toMatchObject({ kind: 'released' })
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'released', checkpoint: 'release_observed' })
    registry.close()
  })

  it('persists Claim intent and claiming state together before Claim creation', async () => {
    const { origin, revision, registry, fleet } = await fixture()
    const client = new InMemoryPactlineClient(21, [{ id: 'criterion-1', revision: 1 }])
    const definition: FleetWorkDefinition = {
      caseId: 'claim-intent-crash', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision },
      repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [{ id: 'criterion-1', revision: 1 }],
    }
    const adapter = new ReplayHarnessAdapter([])
    const coordinator = new ClaimStageRunCoordinator({
      registry,
      client,
      clientSessionId: 'fleet-service-test',
      materializer: {
        materialize: () => Promise.resolve({
          definition,
          router: new StaticRuntimeRouter([adapter], routes()),
          deadline: '2026-08-17T00:00:00Z',
          prepareWorkspace: () => Promise.reject(new Error('must not prepare workspace')),
        }),
      },
      faultInjector: checkpoint => {
        if (checkpoint === 'before_claim_creation') throw new FleetInjectedCrash(checkpoint)
      },
    })
    const run = registry.admitRun(fleet.id, {
      taskNumber: 21, taskVersion: 1, stage: 'execution', frozenPolicy: { route: routes().execution, definition },
    })
    await expect(coordinator.execute(run.runId, new AbortController().signal)).rejects.toMatchObject({
      checkpoint: 'before_claim_creation',
    })
    expect(client.phase).toBe('ready')
    expect(client.activity).toBe('available')
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'claiming', checkpoint: 'claim_intent' })
    expect(registry.getEffect(run.runId, 'claim')).toMatchObject({ status: 'intended' })
    const path = registry.path
    registry.close()

    const reopened = await FleetRegistry.open(path)
    expect(reopened.getRun(run.runId)).toMatchObject({ state: 'claiming' })
    expect(reopened.getEffect(run.runId, 'claim')).toMatchObject({ status: 'intended' })
    reopened.close()
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

    await expect(coordinator.recover(run.runId, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
    expect(resumedSession).toBe('codex-session-21')
    expect(client.phase).toBe('in_review')
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'completed', claimTaskVersion: 2 })
    registry.close()
  })

  it.each(['validating', 'delivering'] as const)(
    'continues a persisted post-result Run in %s without calling Harness',
    async recoveryState => {
      const { directory, origin, revision, registry, fleet } = await fixture()
      const criterion = { id: 'criterion-1', revision: 1 }
      const client = new InMemoryPactlineClient(21, [criterion])
      const claimed = await client.claimTask(21, 1, 'execution', {
        sessionId: 'first-service', idempotencyKey: 'claim',
      })
      const definition: FleetWorkDefinition = {
        caseId: 'post-result-recovery', taskNumber: 21, taskVersion: 1,
        base: { source: origin, ref: 'refs/heads/main', revision },
        repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
        allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
      }
      const workspace = await prepareWorkspace({
        input: definition.base, mode: 'execution', runId: `post-result-${recoveryState}`, temporaryDirectory: directory,
      })
      await writeFile(join(workspace.repositoryPath, 'README.md'), 'recovered result\n')
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
        repositoryPath: workspace.repositoryPath, source: workspace.source,
        baseRevision: workspace.baseRevision, branch: workspace.branch,
      } })
      registry.transitionRun(run.runId, 'starting_harness', 'running_harness', {
        runtimeSessionId: 'completed-codex-session',
      })
      const proposal: ExecutionProposal = {
        schemaVersion: 1, kind: 'execution', runId: run.runId, claimId: claimed.data.claim.id,
        taskNumber: 21, recommendation: 'complete', summary: 'Recovered.', changedPaths: ['README.md'],
        verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
        criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'verified' }],
        limitations: [],
      }
      const harnessResult: HarnessRunResult = {
        adapterId: 'codex', adapterVersion: 'post-result-test', runtimeSessionId: 'completed-codex-session',
        model: { provider: 'openai-codex', model: 'gpt-5.6-sol', reasoning: 'high' },
        terminalState: 'completed', proposal, usage: {},
        eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
      }
      registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
      registry.observeEffect(run.runId, 'harness_result', {
        terminalState: 'completed', runtimeSessionId: 'completed-codex-session',
        result: harnessResult,
        baseline: { head: revision, changedPaths: [], porcelain: '' },
      })
      registry.transitionRun(run.runId, 'running_harness', 'validating', {
        checkpoint: 'harness_result_observed',
      })
      if (recoveryState === 'delivering') {
        registry.transitionRun(run.runId, 'validating', 'delivering', { checkpoint: 'delivery_intent' })
        registry.recordEffectIntent(run.runId, 'repository_delivery', `${run.runId}-delivery`, {})
        registry.observeEffect(run.runId, 'repository_delivery', {
          codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/54',
          revision: 'd'.repeat(40),
          branch: workspace.branch!,
        })
      }
      let harnessCalls = 0
      let deliveryCalls = 0
      const capabilities: HarnessCapabilities = {
        nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: true,
        sandboxModes: ['workspace_write'], supportedStages: ['execution'],
      }
      const adapter: HarnessAdapter = {
        id: 'codex', version: 'post-result-test',
        probe: async () => capabilities,
        run: () => { harnessCalls += 1; return Promise.reject(new Error('must not run Harness after result persistence')) },
        resume: () => { harnessCalls += 1; return Promise.reject(new Error('must not resume Harness after result persistence')) },
      }
      const route = { adapterId: 'codex', model: 'gpt-5.6-sol', reasoning: 'high', promptVersion: 'm5.2', resultContractVersion: 1 }
      const registryPath = registry.path
      registry.close()
      const reopened = await FleetRegistry.open(registryPath)
      const coordinator = new ClaimStageRunCoordinator({
        registry: reopened, client, clientSessionId: 'recovery-service',
        materializer: {
          materialize: () => Promise.reject(new Error('not used')),
          resume: () => Promise.resolve({
            definition,
            router: new StaticRuntimeRouter([adapter], {
              execution: route, correction: route, review: route, resolution_analysis: route,
            }),
            deadline: '2026-08-17T00:00:00Z',
            prepareWorkspace: async () => workspace,
            publishDelivery: async () => {
              deliveryCalls += 1
              return {
                repository: definition.repository,
                codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/54',
                revision: 'd'.repeat(40),
                branch: workspace.branch!,
              }
            },
          }),
        },
      })

      await expect(coordinator.recover(run.runId, new AbortController().signal)).resolves.toEqual({ kind: 'completed' })
      expect(harnessCalls).toBe(0)
      expect(deliveryCalls).toBe(recoveryState === 'validating' ? 1 : 0)
      expect(client.activity).toBe('available')
      expect(reopened.getRun(run.runId)).toMatchObject({ state: 'completed', checkpoint: 'settlement_observed' })
      expect(reopened.getEffect(run.runId, 'pactline_settlement')).toMatchObject({ status: 'observed' })
      reopened.close()
    },
  )

  it('replays the exact settlement intent for an active settling Run without resuming Harness', async () => {
    const { registry, fleet } = await fixture()
    const client = new InMemoryPactlineClient(21, [{ id: 'criterion-1', revision: 1 }])
    const claimed = await client.claimTask(21, 1, 'execution', {
      sessionId: 'first-service', idempotencyKey: 'claim',
    })
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
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', { workspace: persistedWorkspace() })
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', {
      runtimeSessionId: 'completed-codex-session',
    })
    registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
    const proposal = unableProposal(run.runId, claimed.data.claim.id)
    registry.observeEffect(run.runId, 'harness_result', {
      terminalState: 'completed', runtimeSessionId: 'completed-codex-session',
      result: { proposal },
      baseline: { head: 'a'.repeat(40), changedPaths: [], porcelain: '' },
    })
    registry.transitionRun(run.runId, 'running_harness', 'validating')
    registry.transitionRun(run.runId, 'validating', 'settling', { checkpoint: 'settlement_intent' })
    registry.recordEffectIntent(run.runId, 'pactline_settlement', `${run.runId}-settle`, {
      stage: 'execution', taskVersion: claimed.data.task.version, proposal,
    })
    let resumeCalls = 0
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'recovery-service',
      materializer: {
        materialize: () => Promise.reject(new Error('not used')),
        resume: () => { resumeCalls += 1; return Promise.reject(new Error('must not resume during settlement')) },
      },
    })

    await expect(coordinator.recover(run.runId, new AbortController().signal)).resolves.toMatchObject({ kind: 'released' })
    expect(resumeCalls).toBe(0)
    expect(client.activity).toBe('available')
    expect(client.mutations.filter(item => item.operation === 'release')).toEqual([
      expect.objectContaining({ idempotencyKey: `${run.runId}-settle-release` }),
    ])
    expect(registry.getRun(run.runId)).toMatchObject({
      state: 'released',
      checkpoint: 'settlement_observed',
    })
    expect(registry.getEffect(run.runId, 'pactline_settlement')).toMatchObject({ status: 'observed' })
    expect(registry.listRunEvents(run.runId)).toContainEqual(expect.objectContaining({
      eventType: 'run.recovery_decided',
      payload: expect.objectContaining({
        state: 'settling', claimAuthority: 'active', claimIdentityMatches: true,
        hasSettlementIntent: true, decision: 'replay_settlement',
      }),
    }))
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
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', { workspace: persistedWorkspace() })
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', { runtimeSessionId: 'deepseek-session' })
    registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
    const proposal = unableProposal(run.runId, claimed.data.claim.id)
    registry.observeEffect(run.runId, 'harness_result', {
      terminalState: 'completed', runtimeSessionId: 'deepseek-session',
      result: { proposal },
      baseline: { head: 'a'.repeat(40), changedPaths: [], porcelain: '' },
    })
    registry.transitionRun(run.runId, 'running_harness', 'validating')
    registry.transitionRun(run.runId, 'validating', 'settling')
    registry.recordEffectIntent(run.runId, 'pactline_settlement', `${run.runId}-settle`, {
      stage: 'execution', taskVersion: claimed.data.task.version, proposal,
    })
    let resumeCalls = 0
    const coordinator = new ClaimStageRunCoordinator({
      registry, client, clientSessionId: 'recovery',
      materializer: {
        materialize: () => Promise.reject(new Error('not used')),
        resume: () => { resumeCalls += 1; return Promise.reject(new Error('must not resume')) },
      },
    })

    await expect(coordinator.recover(run.runId, new AbortController().signal)).resolves.toMatchObject({ kind: 'released' })
    expect(resumeCalls).toBe(0)
    expect(registry.getRun(run.runId)).toMatchObject({ state: 'released', checkpoint: 'settlement_reconciled' })
    expect(registry.getEffect(run.runId, 'pactline_settlement')).toMatchObject({ status: 'observed' })
    registry.close()
  })
})
