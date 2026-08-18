import type { HarnessAdapter, HarnessStage } from '../core/harness-adapter.js'
import { StaticRuntimeRouter } from '../core/runtime-router.js'
import type { RuntimeRoute, RuntimeRoutes } from '../core/runtime-router.js'
import type {
  FleetExternalEffectDecision,
  FleetExternalEffectRecord,
  FleetRun,
} from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import {
  observeRemoteRevision,
  observeWorkspaceRevision,
  prepareWorkspace,
  removeWorkspace,
  verifyWorkspace,
} from '../repository/workspace.js'
import type { FleetWorkspace, RepositoryRevision } from '../repository/workspace.js'
import type { FleetFaultInjector, FleetRunMaterializer, MaterializedFleetRun } from '../scheduler/run-coordinator.js'
import {
  ExecutableFleetWorkPlugin,
  frozenPluginPolicy,
  validatePluginDelivery,
  workPluginEnvironment,
} from './executable-plugin.js'

function routes(route: RuntimeRoute): RuntimeRoutes {
  return {
    execution: route,
    review: route,
    correction: route,
    resolution_analysis: route,
  }
}

function workspaceFromRecord(value: Readonly<Record<string, unknown>>): FleetWorkspace {
  const required = ['mode', 'root', 'temporaryParent', 'repositoryPath', 'source', 'baseRevision'] as const
  for (const key of required) if (typeof value[key] !== 'string' || String(value[key]).trim() === '') throw new Error('Frozen Fleet workspace is invalid')
  if (!['execution', 'review'].includes(String(value.mode))) throw new Error('Frozen Fleet workspace mode is invalid')
  return {
    mode: value.mode as FleetWorkspace['mode'],
    root: String(value.root),
    temporaryParent: String(value.temporaryParent),
    repositoryPath: String(value.repositoryPath),
    source: String(value.source),
    baseRevision: String(value.baseRevision),
    ...(typeof value.branch === 'string' ? { branch: value.branch } : {}),
  }
}

export interface PluginRunMaterializerOptions {
  readonly adapters: () => readonly HarnessAdapter[]
  readonly environment: NodeJS.ProcessEnv
  readonly now?: () => Date
  readonly registry: FleetRegistry
  readonly faultInjector?: FleetFaultInjector
}

/** Recreates a Run only from its frozen plugin, route, repository, and verification policy. */
export class PluginRunMaterializer implements FleetRunMaterializer {
  private readonly now: () => Date

  constructor(private readonly options: PluginRunMaterializerOptions) {
    this.now = options.now ?? (() => new Date())
  }

  materialize(
    run: FleetRun,
    signal: AbortSignal,
  ): Promise<MaterializedFleetRun> {
    return Promise.resolve(this.fromFrozen(run, signal))
  }

  resume(run: FleetRun, signal: AbortSignal): Promise<MaterializedFleetRun | undefined> {
    if (run.workspace === undefined || run.runtimeSessionId === undefined) return Promise.resolve(undefined)
    return Promise.resolve(this.fromFrozen(run, signal, workspaceFromRecord(run.workspace)))
  }

  async cleanupRecovered(run: FleetRun): Promise<void> {
    if (run.workspace === undefined) return
    const workspace = workspaceFromRecord(run.workspace)
    await verifyWorkspace(workspace, this.options.environment)
    await removeWorkspace(workspace)
  }

  private fromFrozen(run: FleetRun, signal: AbortSignal, recoveredWorkspace?: FleetWorkspace): MaterializedFleetRun {
    const policy = frozenPluginPolicy(run)
    const route: RuntimeRoute = {
      adapterId: policy.route.adapter,
      model: policy.route.model,
      ...(policy.route.reasoning === undefined ? {} : { reasoning: policy.route.reasoning }),
      promptVersion: policy.route.promptVersion,
      resultContractVersion: policy.route.resultContractVersion,
    }
    const router = new StaticRuntimeRouter(this.options.adapters(), routes(route))
    const pluginEnvironment = workPluginEnvironment(
      this.options.environment,
      policy.pactlineTokenEnv,
      policy.gitCredentialReference,
    )
    const plugin = new ExecutableFleetWorkPlugin(policy.plugin, pluginEnvironment)
    const mode = run.stage === 'review' ? 'review' : 'execution'
    const input: RepositoryRevision = mode === 'review'
      ? {
          source: policy.definition.base.source,
          ref: policy.definition.candidate!.ref,
          revision: policy.definition.candidate!.revision,
        }
      : policy.definition.base
    let activeWorkspace = recoveredWorkspace
    return {
      definition: policy.definition,
      router,
      deadline: new Date(this.now().getTime() + 30 * 60_000).toISOString(),
      prepareWorkspace: async () => {
        if (recoveredWorkspace !== undefined) {
          await verifyWorkspace(recoveredWorkspace, this.options.environment)
          return recoveredWorkspace
        }
        activeWorkspace = await prepareWorkspace({
          input,
          mode,
          runId: run.runId,
          branchPrefix: `fleet/${run.fleetId}/`,
          temporaryDirectory: policy.workspaceRoot,
          environment: this.options.environment,
        })
        return activeWorkspace
      },
      ...(mode === 'review' ? {} : {
        deliveryOwnsCheckpoints: true,
        publishDelivery: async (dispatch, proposal, observation) => {
          const baseRequest = {
            schemaVersion: 1,
            run: {
            runId: run.runId,
            fleetId: run.fleetId,
            projectNumber: run.projectNumber,
            taskNumber: run.taskNumber,
            taskVersion: dispatch.taskVersion,
            claimId: dispatch.claimId,
            stage: run.stage,
            },
            definition: policy.definition,
            workspace: dispatch.request.workspace,
            proposal,
            observation,
            gitCredentialReference: policy.gitCredentialReference,
          }
          const priorCommit = this.options.registry.getEffect(run.runId, 'git_commit')
          this.commitEffect(run.runId, {
            type: 'intent', kind: 'git_commit', idempotencyKey: `${run.runId}-commit`, intent: {},
          })
          const recordedCommit = this.options.registry.getEffect(run.runId, 'git_commit')
          const commitRecord = recordedCommit?.status === 'observed'
            ? deliveryStep(recordedCommit.observation, 'recorded commit')
            : priorCommit?.status === 'intended'
              ? await this.reconcileCommit(activeWorkspace, policy.definition.base.revision)
                ?? deliveryStep(await plugin.invoke('commit', { ...baseRequest, operation: 'commit' }, signal), 'commit')
              : deliveryStep(await plugin.invoke('commit', { ...baseRequest, operation: 'commit' }, signal), 'commit')
          if (recordedCommit?.status !== 'observed') {
            await this.options.faultInjector?.('after_commit_before_persistence', run)
            this.commitEffect(run.runId, { type: 'observation', kind: 'git_commit', observation: commitRecord })
          }
          await this.options.faultInjector?.('after_commit_persistence_before_push', run)

          const priorPush = this.options.registry.getEffect(run.runId, 'git_push')
          this.commitEffect(run.runId, {
            type: 'intent', kind: 'git_push', idempotencyKey: `${run.runId}-push`, intent: commitRecord,
          })
          const recordedPush = this.options.registry.getEffect(run.runId, 'git_push')
          const push = recordedPush?.status === 'observed'
            ? deliveryStep(recordedPush.observation, 'recorded push')
            : priorPush?.status === 'intended'
              ? await this.reconcilePush(activeWorkspace, commitRecord)
                ?? deliveryStep(await plugin.invoke('push', {
                  ...baseRequest, operation: 'push', commit: commitRecord,
                }, signal), 'push')
              : deliveryStep(await plugin.invoke('push', {
                  ...baseRequest, operation: 'push', commit: commitRecord,
                }, signal), 'push')
          if (push.revision !== commitRecord.revision || push.branch !== commitRecord.branch) {
            throw new Error('Fleet work plugin push changed the committed revision or branch')
          }
          if (recordedPush?.status !== 'observed') {
            await this.options.faultInjector?.('after_push_before_persistence', run)
            this.commitEffect(run.runId, { type: 'observation', kind: 'git_push', observation: push })
          }
          await this.options.faultInjector?.('after_push_persistence_before_code_change', run)

          const priorCodeChange = this.options.registry.getEffect(run.runId, 'code_change_creation')
          this.commitEffect(run.runId, {
            type: 'intent', kind: 'code_change_creation', idempotencyKey: `${run.runId}-code-change`, intent: push,
          })
          const recordedCodeChange = this.options.registry.getEffect(run.runId, 'code_change_creation')
          if (priorCodeChange?.status === 'intended') {
            throw new Error('Code-change creation intent has no authoritative recovery observation')
          }
          const delivery = recordedCodeChange?.status === 'observed'
            ? deliveryFromObservation(policy.definition.repository, recordedCodeChange.observation)
            : validatePluginDelivery(await plugin.invoke('open-code-change', {
                ...baseRequest, operation: 'open-code-change', commit: commitRecord, push,
              }, signal))
          if (delivery.revision !== push.revision || delivery.branch !== push.branch) {
            throw new Error('Fleet work plugin code change changed the pushed revision or branch')
          }
          if (recordedCodeChange?.status !== 'observed') {
            await this.options.faultInjector?.('after_code_change_before_persistence', run)
            this.commitEffect(run.runId, {
              type: 'observation', kind: 'code_change_creation',
              observation: {
                codeChangeUrl: delivery.codeChangeUrl, revision: delivery.revision, branch: delivery.branch,
              },
            })
          }
          await this.options.faultInjector?.('after_code_change_persistence_before_link', run)
          return delivery
        },
      }),
      cleanup: async workspace => { await removeWorkspace(workspace) },
    }
  }

  private async reconcileCommit(
    workspace: FleetWorkspace | undefined,
    baseRevision: string,
  ): Promise<{ readonly revision: string; readonly branch: string } | undefined> {
    if (workspace === undefined) throw new Error('Commit recovery has no persisted workspace')
    const observed = await observeWorkspaceRevision(workspace, this.options.environment)
    if (observed.revision === baseRevision) return undefined
    if (!observed.clean) throw new Error('Recovered committed workspace also contains uncommitted changes')
    return { revision: observed.revision, branch: observed.branch }
  }

  private async reconcilePush(
    workspace: FleetWorkspace | undefined,
    commit: { readonly revision: string; readonly branch: string },
  ): Promise<{ readonly revision: string; readonly branch: string } | undefined> {
    if (workspace === undefined) throw new Error('Push recovery has no persisted workspace')
    const remoteRevision = await observeRemoteRevision(workspace, commit.branch, this.options.environment)
    if (remoteRevision === undefined) return undefined
    if (remoteRevision !== commit.revision) throw new Error('Recovered remote branch contradicts the committed revision')
    return commit
  }

  private commitEffect(runId: string, effect: FleetExternalEffectDecision): FleetExternalEffectRecord {
    const current = this.options.registry.getRun(runId)
    if (current === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
    const committed = this.options.registry.commitRunDecision(runId, {
      expected: { state: current.state, updatedAt: current.updatedAt },
      effect,
    }).effect
    if (committed === undefined) throw new Error('Fleet Effect decision did not return its Effect fact')
    return committed
  }
}

function deliveryStep(value: unknown, name: string): { readonly revision: string; readonly branch: string } {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`Fleet work plugin ${name} result is invalid`)
  const item = value as Record<string, unknown>
  if (typeof item.revision !== 'string' || !/^[a-f0-9]{40}$/.test(item.revision)
    || typeof item.branch !== 'string' || item.branch.trim() === '') throw new Error(`Fleet work plugin ${name} result is invalid`)
  return { revision: item.revision, branch: item.branch }
}

function deliveryFromObservation(
  repository: import('../repository/delivery.js').RepositoryIdentity,
  value: unknown,
): import('../repository/delivery.js').RepositoryDelivery {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Recorded code change observation is invalid')
  const item = value as Record<string, unknown>
  return validatePluginDelivery({
    repository,
    codeChangeUrl: item.codeChangeUrl,
    revision: item.revision,
    branch: item.branch,
  })
}
