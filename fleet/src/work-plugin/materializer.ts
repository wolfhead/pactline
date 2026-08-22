import { lstat } from 'node:fs/promises'
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
  observeWorkspaceRevision,
  decodeFleetWorkspace,
  prepareWorkspace,
  removeWorkspace,
  verifyWorkspace,
} from '../repository/workspace.js'
import type { FleetWorkspace, RepositoryRevision } from '../repository/workspace.js'
import { commitDelivery, pushDelivery, verifyPublishedDelivery } from '../repository/publisher.js'
import type { FleetFaultInjector, FleetRunMaterializer, MaterializedFleetRun } from '../scheduler/run-coordinator.js'
import type { PactlineCLI } from '../pactline/client.js'
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
    return Promise.resolve(this.fromFrozen(run, signal, decodeFleetWorkspace(run.workspace)))
  }

  async retireTerminalTasks(
    client: Pick<PactlineCLI, 'showTask'>,
    sessionId: string,
    signal?: AbortSignal,
  ): Promise<number> {
    let retired = 0
    const activeTasks = new Set(this.options.registry.listNonTerminalRuns().map(run => `${String(run.projectNumber)}:${String(run.taskNumber)}`))
    for (const runtime of this.options.registry.listTaskRuntimes()) {
      if (activeTasks.has(`${String(runtime.projectNumber)}:${String(runtime.taskNumber)}`)) continue
      const packet = await client.showTask(runtime.taskNumber, 1, { sessionId, ...(signal === undefined ? {} : { signal }) })
      const rawTask = packet.data.task
      if (typeof rawTask !== 'object' || rawTask === null || Array.isArray(rawTask)) {
        throw new Error(`Pactline cleanup packet is invalid for Project ${String(runtime.projectNumber)} Task ${String(runtime.taskNumber)}`)
      }
      const phase = String((rawTask as Record<string, unknown>).phase ?? '')
      if (phase !== 'done' && phase !== 'cancelled') continue
      await this.retireRuntime(runtime.projectNumber, runtime.taskNumber, runtime.workspace)
      retired += 1
    }
    return retired
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
      policy.codeChangeCredentialReference,
    )
    const plugin = new ExecutableFleetWorkPlugin(policy.plugin, pluginEnvironment)
    const mode = run.stage === 'review' ? 'review' : 'execution'
    const input: RepositoryRevision = policy.definition.base
    const candidate = run.stage === 'execution'
      ? undefined
      : {
          source: policy.definition.base.source,
          ref: policy.definition.candidate!.ref,
          revision: policy.definition.candidate!.revision,
        }
    const taskRuntime = this.options.registry.getTaskRuntime(run.projectNumber, run.taskNumber)
    let activeWorkspace = recoveredWorkspace ?? taskRuntime?.workspace
    return {
      definition: policy.definition,
      router,
      deadline: new Date(this.now().getTime() + 30 * 60_000).toISOString(),
      prepareWorkspace: async () => {
        if (activeWorkspace !== undefined) {
          await verifyWorkspace(activeWorkspace, this.options.environment)
          return activeWorkspace
        }
        activeWorkspace = await prepareWorkspace({
          input,
          ...(candidate === undefined ? {} : { candidate }),
          mode: 'execution',
          runId: run.runId,
          branchPrefix: `fleet/${run.fleetId}/`,
          temporaryDirectory: policy.workspaceRoot,
          environment: this.options.environment,
          taskIdentity: { projectNumber: run.projectNumber, taskNumber: run.taskNumber },
        })
        this.options.registry.bindTaskWorkspace(run.projectNumber, run.taskNumber, activeWorkspace)
        return activeWorkspace
      },
      ...(mode === 'review' ? {} : {
        deliveryOwnsCheckpoints: true,
        publishDelivery: async (dispatch, proposal, observation) => {
          if (activeWorkspace === undefined || activeWorkspace.branch === undefined) {
            throw new Error('Fleet delivery has no Task Workspace branch')
          }
          const deliveryRef = `refs/heads/${activeWorkspace.branch}`
          if (policy.definition.candidate !== undefined
            && policy.definition.candidate.branch !== activeWorkspace.branch) {
            throw new Error('Fleet correction candidate does not use the stable Task delivery ref')
          }
          const codeChangeRequest = {
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
            definition: {
              caseId: policy.definition.caseId,
              taskNumber: policy.definition.taskNumber,
              taskVersion: policy.definition.taskVersion,
              repository: policy.definition.repository,
              base: { ref: policy.definition.base.ref, revision: policy.definition.base.revision },
              ...(policy.definition.candidate === undefined ? {} : { candidate: policy.definition.candidate }),
            },
            proposal,
            observation,
          }
          const priorCommit = this.options.registry.getEffect(run.runId, 'git_commit')
          this.commitEffect(run.runId, {
            type: 'intent', kind: 'git_commit', idempotencyKey: `${run.runId}-commit`, intent: {},
          })
          const recordedCommit = this.options.registry.getEffect(run.runId, 'git_commit')
          const commitRecord = recordedCommit?.status === 'observed'
            ? deliveryStep(recordedCommit.observation, 'recorded commit')
            : priorCommit?.status === 'intended'
              ? await this.reconcileCommit(
                  activeWorkspace,
                  policy.definition.candidate?.revision ?? policy.definition.base.revision,
                ) ?? await commitDelivery(
                  activeWorkspace, policy.definition.allowedPaths, run.taskNumber, this.options.environment,
                )
              : await commitDelivery(
                  activeWorkspace, policy.definition.allowedPaths, run.taskNumber, this.options.environment,
                )
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
          const deliveryAuthority = {
            remote: policy.definition.base.source,
            baseRef: policy.definition.base.ref,
            baseRevision: policy.definition.base.revision,
            deliveryRef,
            ...(policy.definition.candidate === undefined ? {} : {
              priorDeliveryRevision: policy.definition.candidate.revision,
            }),
          }
          const push = recordedPush?.status === 'observed'
            ? await verifyPublishedDelivery(
                activeWorkspace,
                { ...deliveryStep(recordedPush.observation, 'recorded push') },
                deliveryAuthority,
                this.gitCredential(policy),
                this.options.environment,
              )
            : priorPush?.status === 'intended'
              ? await this.reconcilePush(activeWorkspace, commitRecord, deliveryAuthority, this.gitCredential(policy))
                ?? await pushDelivery(activeWorkspace, commitRecord, deliveryAuthority, this.gitCredential(policy), this.options.environment)
              : await pushDelivery(activeWorkspace, commitRecord, deliveryAuthority, this.gitCredential(policy), this.options.environment)
          if (push.revision !== commitRecord.revision || push.branch !== commitRecord.branch) {
            throw new Error('Fleet push changed the committed revision or branch')
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
                ...codeChangeRequest, operation: 'open-code-change', commit: commitRecord, push,
              }, signal), {
                baseRef: policy.definition.base.ref,
                ...(policy.definition.candidate === undefined ? {} : {
                  existingCodeChangeUrl: policy.definition.candidate.codeChangeUrl,
                }),
              })
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
    }
  }

  private gitCredential(policy: ReturnType<typeof frozenPluginPolicy>): string | undefined {
    if (policy.gitCredentialReference === undefined) return undefined
    const credential = this.options.environment[policy.gitCredentialReference]
    if (credential === undefined || credential === '') {
      throw new Error(`Fleet Git credential is unavailable: ${policy.gitCredentialReference}`)
    }
    return credential
  }

  private async retireRuntime(projectNumber: number, taskNumber: number, workspace: FleetWorkspace): Promise<void> {
    try {
      await removeWorkspace(workspace)
    } catch (error) {
      const rootStillExists = await lstat(workspace.root).then(() => true).catch((statError: unknown) => {
        if (typeof statError === 'object' && statError !== null && 'code' in statError && statError.code === 'ENOENT') return false
        throw statError
      })
      if (rootStillExists) throw error
    }
    this.options.registry.retireTaskRuntime(projectNumber, taskNumber)
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
    commit: Parameters<typeof verifyPublishedDelivery>[1],
    authority: Parameters<typeof verifyPublishedDelivery>[2],
    credential: string | undefined,
  ): Promise<Parameters<typeof verifyPublishedDelivery>[1] | undefined> {
    if (workspace === undefined) throw new Error('Push recovery has no persisted workspace')
    try {
      return await verifyPublishedDelivery(workspace, commit, authority, credential, this.options.environment)
    } catch (error) {
      if (error instanceof Error && error.message === 'Fleet delivery ref does not match the committed revision') return undefined
      throw error
    }
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
