import type { HarnessAdapter, HarnessStage } from '../core/harness-adapter.js'
import { StaticRuntimeRouter } from '../core/runtime-router.js'
import type { RuntimeRoute, RuntimeRoutes } from '../core/runtime-router.js'
import type { FleetRunRecord } from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import { prepareWorkspace, removeWorkspace, verifyWorkspace } from '../repository/workspace.js'
import type { FleetWorkspace, RepositoryRevision } from '../repository/workspace.js'
import type { FleetFaultInjector, FleetRunMaterializer, MaterializedFleetRun } from '../scheduler/run-coordinator.js'
import type { FleetDefinitionConfig } from '../config/types.js'
import type { FleetWorkCandidate } from '../scheduler/candidate.js'
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
  readonly pactlineTokenEnv: () => string
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
    run: FleetRunRecord,
    _candidate: FleetWorkCandidate,
    _fleet: FleetDefinitionConfig,
    signal: AbortSignal,
  ): Promise<MaterializedFleetRun> {
    return Promise.resolve(this.fromFrozen(run, false, signal))
  }

  resume(run: FleetRunRecord, signal: AbortSignal): Promise<MaterializedFleetRun | undefined> {
    if (run.workspace === undefined || run.runtimeSessionId === undefined) return Promise.resolve(undefined)
    return Promise.resolve(this.fromFrozen(run, true, signal))
  }

  async cleanupRecovered(run: FleetRunRecord): Promise<void> {
    if (run.workspace === undefined) return
    const workspace = workspaceFromRecord(run.workspace)
    await verifyWorkspace(workspace, this.options.environment)
    await removeWorkspace(workspace)
  }

  private fromFrozen(run: FleetRunRecord, resume: boolean, signal: AbortSignal): MaterializedFleetRun {
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
      this.options.pactlineTokenEnv(),
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
    return {
      definition: policy.definition,
      router,
      deadline: new Date(this.now().getTime() + 30 * 60_000).toISOString(),
      prepareWorkspace: async () => {
        if (resume) {
          const workspace = workspaceFromRecord(run.workspace!)
          await verifyWorkspace(workspace, this.options.environment)
          return workspace
        }
        return await prepareWorkspace({
          input,
          mode,
          runId: run.runId,
          branchPrefix: `fleet/${run.fleetId}/`,
          temporaryDirectory: policy.workspaceRoot,
          environment: this.options.environment,
        })
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
          this.options.registry.recordEffectIntent(run.runId, 'git_commit', `${run.runId}-commit`, {})
          const recordedCommit = this.options.registry.getEffect(run.runId, 'git_commit')
          const commitRecord = recordedCommit?.status === 'observed'
            ? deliveryStep(recordedCommit.observation, 'recorded commit')
            : deliveryStep(await plugin.invoke('commit', { ...baseRequest, operation: 'commit' }, signal), 'commit')
          if (recordedCommit?.status !== 'observed') {
            await this.options.faultInjector?.('after_commit_before_persistence', run)
            this.options.registry.observeEffect(run.runId, 'git_commit', commitRecord)
          }
          await this.options.faultInjector?.('after_commit_persistence_before_push', run)

          this.options.registry.recordEffectIntent(run.runId, 'git_push', `${run.runId}-push`, commitRecord)
          const recordedPush = this.options.registry.getEffect(run.runId, 'git_push')
          const push = recordedPush?.status === 'observed'
            ? deliveryStep(recordedPush.observation, 'recorded push')
            : deliveryStep(await plugin.invoke('push', {
                ...baseRequest, operation: 'push', commit: commitRecord,
              }, signal), 'push')
          if (push.revision !== commitRecord.revision || push.branch !== commitRecord.branch) {
            throw new Error('Fleet work plugin push changed the committed revision or branch')
          }
          if (recordedPush?.status !== 'observed') {
            await this.options.faultInjector?.('after_push_before_persistence', run)
            this.options.registry.observeEffect(run.runId, 'git_push', push)
          }
          await this.options.faultInjector?.('after_push_persistence_before_code_change', run)

          this.options.registry.recordEffectIntent(run.runId, 'code_change_creation', `${run.runId}-code-change`, push)
          const recordedCodeChange = this.options.registry.getEffect(run.runId, 'code_change_creation')
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
            this.options.registry.observeEffect(run.runId, 'code_change_creation', {
              codeChangeUrl: delivery.codeChangeUrl, revision: delivery.revision, branch: delivery.branch,
            })
          }
          await this.options.faultInjector?.('after_code_change_persistence_before_link', run)
          return delivery
        },
      }),
      cleanup: async workspace => { await removeWorkspace(workspace) },
    }
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
