import type { StaticRuntimeRouter } from '../core/runtime-router.js'
import { runClaimStage } from '../core/claim-stage.js'
import type { ClaimStageClient, ClaimStageOptions, ClaimWorkflowStage } from '../core/claim-stage.js'
import type { FleetWorkDefinition } from '../core/work-definition.js'
import { PactlineClientError } from '../pactline/client.js'
import type { RepositoryDelivery } from '../repository/delivery.js'
import type { FleetWorkspace } from '../repository/workspace.js'
import type { FleetDefinitionConfig } from '../config/types.js'
import type { FleetRunRecord, FleetRunState } from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import type { FleetRunOutcome, FleetWorkCandidate, ScheduledRunExecutor } from './candidate.js'
import { sanitizeHealthDiagnostic } from '../health/store.js'

export interface MaterializedFleetRun {
  readonly definition: FleetWorkDefinition
  readonly router: StaticRuntimeRouter
  readonly deadline: string
  readonly prepareWorkspace: (signal: AbortSignal) => Promise<FleetWorkspace>
  readonly publishDelivery?: ClaimStageOptions['publishDelivery']
  readonly deliveryOwnsCheckpoints?: boolean
  readonly validateObservation?: ClaimStageOptions['validateObservation']
  readonly cleanup?: (workspace: FleetWorkspace, terminal: boolean) => Promise<void>
}

export interface FleetRunMaterializer {
  materialize(
    run: FleetRunRecord,
    candidate: FleetWorkCandidate,
    fleet: FleetDefinitionConfig,
    signal: AbortSignal,
  ): Promise<MaterializedFleetRun>
  /** Recreate only explicitly persisted authority; never select a different Adapter. */
  resume?(run: FleetRunRecord, signal: AbortSignal): Promise<MaterializedFleetRun | undefined>
  cleanupRecovered?(run: FleetRunRecord): Promise<void>
}

export interface ClaimStageRunCoordinatorOptions {
  readonly registry: FleetRegistry
  readonly client: ClaimStageClient
  readonly materializer: FleetRunMaterializer
  readonly clientSessionId: string
  readonly faultInjector?: FleetFaultInjector
}

export type FleetCrashCheckpoint =
  | 'before_claim_creation'
  | 'after_claim_creation_before_persistence'
  | 'after_claim_persistence_before_session'
  | 'after_workspace_effect_before_persistence'
  | 'after_session_persistence_before_agent'
  | 'after_harness_result_before_persistence'
  | 'after_harness_result_persistence_before_delivery'
  | 'after_commit_before_persistence'
  | 'after_commit_persistence_before_push'
  | 'after_push_before_persistence'
  | 'after_push_persistence_before_code_change'
  | 'after_code_change_before_persistence'
  | 'after_code_change_persistence_before_link'
  | 'after_settlement_before_terminal_persistence'

export type FleetFaultInjector = (checkpoint: FleetCrashCheckpoint, run: FleetRunRecord) => Promise<void> | void

export class FleetInjectedCrash extends Error {
  constructor(readonly checkpoint: FleetCrashCheckpoint) {
    super(`Injected Fleet crash at ${checkpoint}`)
    this.name = 'FleetInjectedCrash'
  }
}

function contention(error: unknown): boolean {
  return error instanceof PactlineClientError
    && error.code === 'CLI_ERROR'
    && ['ACTIVE_CLAIM', 'VERSION_CONFLICT'].includes(error.pactlineError?.code ?? '')
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Pactline recovery packet is invalid')
  return value as Record<string, unknown>
}

function workspaceRecord(workspace: FleetWorkspace): Readonly<Record<string, unknown>> {
  return {
    mode: workspace.mode,
    root: workspace.root,
    temporaryParent: workspace.temporaryParent,
    repositoryPath: workspace.repositoryPath,
    source: workspace.source,
    baseRevision: workspace.baseRevision,
    ...(workspace.branch === undefined ? {} : { branch: workspace.branch }),
  }
}

function stageFor(candidate: FleetWorkCandidate): ClaimWorkflowStage {
  return candidate.stage
}

function safeReason(error: unknown): string {
  return sanitizeHealthDiagnostic(error instanceof Error ? error.message : String(error)).slice(0, 2_000)
}

/** Bridges one scheduled Run into the finite Claim-stage Core with durable callbacks. */
export class ClaimStageRunCoordinator implements ScheduledRunExecutor {
  constructor(private readonly options: ClaimStageRunCoordinatorOptions) {}

  async execute(
    initial: FleetRunRecord,
    candidate: FleetWorkCandidate,
    fleet: FleetDefinitionConfig,
    signal: AbortSignal,
  ): Promise<FleetRunOutcome> {
    try {
      const materialized = await this.options.materializer.materialize(initial, candidate, fleet, signal)
      return await this.run(initial, stageFor(candidate), materialized, signal)
    } catch (error) {
      const current = this.options.registry.getRun(initial.runId)
      if (current !== undefined && current.state === 'admitted') {
        return this.releaseLocal(current, safeReason(error))
      }
      throw error
    }
  }

  async recover(run: FleetRunRecord, signal: AbortSignal): Promise<FleetRunOutcome> {
    if (run.state === 'admitted') {
      this.options.registry.transitionRun(run.runId, 'admitted', 'released', {
        checkpoint: 'recovery_no_claim', disposition: 'recovered_before_claim',
      })
      return { kind: 'released', reason: 'recovered_before_claim' }
    }
    if (run.state === 'claiming' && run.claimId === undefined) {
      return await this.reconcileClaimIntent(run, signal)
    }
    if (run.claimId === undefined) return this.quarantine(run, 'non_terminal_run_has_no_claim_identity')
    if (run.state === 'settling') {
      const terminal = await this.reconcileTerminalClaim(run, signal)
      if (terminal !== undefined) return terminal
    }
    if (run.runtimeSessionId !== undefined && this.options.materializer.resume !== undefined) {
      const materialized = await this.options.materializer.resume(run, signal)
      if (materialized !== undefined) {
        return await this.run(run, run.stage ?? 'execution', materialized, signal, true)
      }
    }
    const outcome = await this.releaseKnownClaim(run, 'adapter_session_not_resumable', signal)
    if (outcome.kind === 'released') await this.options.materializer.cleanupRecovered?.(run)
    return outcome
  }

  private async run(
    initial: FleetRunRecord,
    stage: ClaimWorkflowStage,
    materialized: MaterializedFleetRun,
    signal: AbortSignal,
    resume = false,
  ): Promise<FleetRunOutcome> {
    let workspace: FleetWorkspace | undefined
    let run = this.options.registry.getRun(initial.runId)!
    try {
      if (!resume) {
        run = this.options.registry.transitionRun(run.runId, 'admitted', 'claiming', { checkpoint: 'claim_intent' })
        this.options.registry.recordEffectIntent(run.runId, 'claim', `${run.runId}-claim`, {
          taskNumber: run.taskNumber, taskVersion: run.taskVersion, stage,
        })
        await this.inject('before_claim_creation', run)
      }
      const result = await runClaimStage({
        client: this.options.client,
        router: materialized.router,
        definition: materialized.definition,
        stage,
        taskVersion: resume ? (run.claimTaskVersion ?? run.taskVersion!) : run.taskVersion!,
        runId: run.runId,
        clientSessionId: this.options.clientSessionId,
        idempotencyKey: run.runId,
        deadline: materialized.deadline,
        signal,
        ...(resume ? { existingClaimId: run.claimId!, resumeRuntimeSessionId: run.runtimeSessionId! } : {}),
        onClaimed: async (claimId, taskVersion, claimVersion) => {
          const current = this.options.registry.getRun(run.runId)!
          if (!resume) {
            await this.inject('after_claim_creation_before_persistence', run)
            this.options.registry.observeEffect(run.runId, 'claim', {
              claimId, taskVersion, ...(claimVersion === undefined ? {} : { claimVersion }),
            })
            run = this.options.registry.transitionRun(run.runId, 'claiming', 'claimed', {
              claimId, claimTaskVersion: taskVersion,
              ...(claimVersion === undefined ? {} : { claimVersion }), checkpoint: 'claim_observed',
            })
            await this.inject('after_claim_persistence_before_session', run)
          } else if (current.claimId !== claimId) {
            throw new Error('Recovery attempted to change the frozen Claim identity')
          }
          const latest = this.options.registry.getRun(run.runId)!
          if (latest.state === 'claimed') {
            run = this.options.registry.transitionRun(run.runId, 'claimed', 'preparing_workspace', {
              checkpoint: 'workspace_intent',
            })
            this.options.registry.recordEffectIntent(run.runId, 'workspace', `${run.runId}-workspace`, {
              mode: stage === 'review' ? 'review' : 'execution',
            })
          }
        },
        workspace: async () => {
          workspace = await materialized.prepareWorkspace(signal)
          await this.inject('after_workspace_effect_before_persistence', run)
          const current = this.options.registry.getRun(run.runId)!
          if (current.state === 'preparing_workspace') {
            this.options.registry.observeEffect(run.runId, 'workspace', workspaceRecord(workspace))
            run = this.options.registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', {
              workspace: workspaceRecord(workspace), checkpoint: 'workspace_observed',
            })
          }
          this.options.registry.recordEffectIntent(run.runId, 'adapter_session', `${run.runId}-adapter-session`, {
            adapter: materialized.router.routeFor(stage).adapterId,
          })
          return workspace
        },
        onRuntimeSession: async runtimeSessionId => {
          this.options.registry.observeEffect(run.runId, 'adapter_session', { runtimeSessionId })
          const current = this.options.registry.getRun(run.runId)!
          if (current.state === 'starting_harness') {
            run = this.options.registry.transitionRun(run.runId, 'starting_harness', 'running_harness', {
              runtimeSessionId, checkpoint: 'adapter_session_observed',
            })
          }
          await this.inject('after_session_persistence_before_agent', run)
        },
        onHarnessResult: async harnessResult => {
          await this.inject('after_harness_result_before_persistence', run)
          this.options.registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
          this.options.registry.observeEffect(run.runId, 'harness_result', {
            terminalState: harnessResult.terminalState,
            runtimeSessionId: harnessResult.runtimeSessionId,
            result: harnessResult,
          })
          const current = this.options.registry.getRun(run.runId)!
          if (current.state === 'running_harness') {
            run = this.options.registry.transitionRun(run.runId, 'running_harness', 'validating', {
              checkpoint: 'harness_result_observed',
            })
          }
          await this.inject('after_harness_result_persistence_before_delivery', run)
        },
        ...(materialized.validateObservation === undefined ? {} : { validateObservation: materialized.validateObservation }),
        ...(materialized.publishDelivery === undefined ? {} : {
          publishDelivery: async (dispatch, proposal, observation): Promise<RepositoryDelivery> => {
            const current = this.options.registry.getRun(run.runId)!
            if (current.state === 'validating') {
              run = this.options.registry.transitionRun(run.runId, 'validating', 'delivering', {
                checkpoint: 'delivery_intent',
              })
            }
            if (materialized.deliveryOwnsCheckpoints !== true) {
              this.options.registry.recordEffectIntent(run.runId, 'repository_delivery', `${run.runId}-delivery`, {})
            }
            const delivery = await materialized.publishDelivery!(dispatch, proposal, observation)
            if (materialized.deliveryOwnsCheckpoints !== true) {
              this.options.registry.observeEffect(run.runId, 'repository_delivery', {
                codeChangeUrl: delivery.codeChangeUrl, revision: delivery.revision, branch: delivery.branch,
              })
            }
            return delivery
          },
        }),
        onBeforeSettlement: async () => {
          const current = this.options.registry.getRun(run.runId)!
          if (current.state === 'validating' || current.state === 'delivering') {
            run = this.options.registry.transitionRun(run.runId, current.state, 'settling', {
              checkpoint: 'settlement_intent',
            })
          }
          this.options.registry.recordEffectIntent(run.runId, 'pactline_settlement', `${run.runId}-settle`, {})
        },
        onSettled: async settlement => {
          await this.inject('after_settlement_before_terminal_persistence', run)
          this.options.registry.observeEffect(run.runId, 'pactline_settlement', {
            claimId: settlement.claim.id,
            claimStatus: settlement.claim.status,
            claimOutcome: settlement.claim.outcome,
            taskVersion: settlement.task.version,
          })
          const terminal = settlement.claim.status === 'released' ? 'released' : 'completed'
          run = this.options.registry.transitionRun(run.runId, 'settling', terminal, {
            checkpoint: 'settlement_observed', disposition: settlement.claim.outcome ?? settlement.claim.status,
          })
        },
      })
      if (workspace !== undefined) await materialized.cleanup?.(workspace, true)
      return result.settlement.claim.status === 'released'
        ? { kind: 'released', reason: result.settlement.claim.outcome ?? 'released' }
        : { kind: 'completed' }
    } catch (error) {
      if (error instanceof FleetInjectedCrash) throw error
      if (contention(error)) {
        const current = this.options.registry.getRun(run.runId)!
        if (current.state === 'claiming') {
          this.options.registry.transitionRun(run.runId, 'claiming', 'released', {
            checkpoint: 'claim_contended', disposition: 'claim_contention',
          })
        }
        return { kind: 'contention', reason: (error as PactlineClientError).pactlineError!.code }
      }
      const current = this.options.registry.getRun(run.runId)!
      const ambiguous = this.options.registry.listEffects(run.runId).some(effect => (
        effect.status === 'intended' && [
          'repository_delivery', 'git_commit', 'git_push', 'code_change_creation', 'pactline_settlement', 'claim_release',
        ].includes(effect.kind)
      ))
      const outcome = ambiguous
        ? this.quarantine(current, `ambiguous_external_effect: ${safeReason(error)}`)
        : current.claimId === undefined
          ? this.releaseLocal(current, 'failed_before_claim')
          : await this.releaseKnownClaim(current, safeReason(error), signal)
      if (!ambiguous && workspace !== undefined) await materialized.cleanup?.(workspace, false).catch(() => undefined)
      return outcome
    }
  }

  private async reconcileClaimIntent(run: FleetRunRecord, signal: AbortSignal): Promise<FleetRunOutcome> {
    try {
      const claimed = await this.options.client.claimTask(run.taskNumber!, run.taskVersion!, run.stage === 'review' ? 'review' : 'execution', {
        sessionId: this.options.clientSessionId,
        idempotencyKey: `${run.runId}-claim`,
        signal,
      })
      this.options.registry.observeEffect(run.runId, 'claim', {
        claimId: claimed.data.claim.id, taskVersion: claimed.data.task.version,
      })
      const claimedRun = this.options.registry.transitionRun(run.runId, 'claiming', 'claimed', {
        claimId: claimed.data.claim.id, claimVersion: claimed.data.claim.version, checkpoint: 'claim_reconciled',
        claimTaskVersion: claimed.data.task.version,
      })
      return await this.releaseKnownClaim(claimedRun, 'recovered_claim_without_dispatch', signal)
    } catch (error) {
      if (contention(error)) return this.quarantine(run, 'claim_intent_conflicted_during_recovery')
      return this.quarantine(run, 'claim_intent_could_not_be_reconciled')
    }
  }

  private async releaseKnownClaim(run: FleetRunRecord, reason: string, signal: AbortSignal): Promise<FleetRunOutcome> {
    try {
      const packet = await this.options.client.showClaim(run.claimId!, 20, {
        sessionId: this.options.clientSessionId, signal,
      })
      const claim = record(packet.data.claim)
      const task = record(packet.data.task)
      if (claim.id !== run.claimId || task.number !== run.taskNumber) return this.quarantine(run, 'claim_identity_mismatch')
      if (claim.status !== 'active') {
        const terminal = claim.status === 'released' ? 'released' : 'completed'
        return this.finishRecovered(run, terminal, `claim_already_${String(claim.status)}`)
      }
      const current = this.options.registry.getRun(run.runId)!
      if (current.state !== 'releasing') {
        this.options.registry.transitionRun(run.runId, current.state, 'releasing', {
          checkpoint: 'release_intent', error: reason,
        })
      }
      this.options.registry.recordEffectIntent(run.runId, 'claim_release', `${run.runId}-recovery-release`, { reason })
      const released = await this.options.client.releaseClaim(run.claimId!, Number(task.version), `Fleet recovery release: ${reason}`, {
        sessionId: this.options.clientSessionId,
        idempotencyKey: `${run.runId}-recovery-release`,
        signal,
      })
      this.options.registry.observeEffect(run.runId, 'claim_release', {
        claimStatus: released.data.claim.status, taskVersion: released.data.task.version,
      })
      this.options.registry.transitionRun(run.runId, 'releasing', 'released', {
        checkpoint: 'release_observed', disposition: reason,
      })
      return { kind: 'released', reason }
    } catch {
      return this.quarantine(this.options.registry.getRun(run.runId)!, 'known_claim_release_ambiguous')
    }
  }

  private async reconcileTerminalClaim(run: FleetRunRecord, signal: AbortSignal): Promise<FleetRunOutcome | undefined> {
    try {
      const packet = await this.options.client.showClaim(run.claimId!, 20, {
        sessionId: this.options.clientSessionId, signal,
      })
      const claim = record(packet.data.claim)
      const task = record(packet.data.task)
      if (claim.id !== run.claimId || task.number !== run.taskNumber) return this.quarantine(run, 'claim_identity_mismatch')
      if (claim.status === 'active') return undefined
      this.options.registry.observeEffect(run.runId, 'pactline_settlement', {
        claimId: claim.id,
        claimStatus: claim.status,
        claimOutcome: claim.outcome,
        taskVersion: task.version,
        recovered: true,
      })
      return this.finishRecovered(run, claim.status === 'released' ? 'released' : 'completed', `claim_reconciled_${String(claim.status)}`)
    } catch {
      return this.quarantine(run, 'settlement_could_not_be_reconciled')
    }
  }

  private releaseLocal(run: FleetRunRecord, reason: string): FleetRunOutcome {
    const current = this.options.registry.getRun(run.runId)!
    this.options.registry.transitionRun(run.runId, current.state, 'released', {
      checkpoint: 'local_release', disposition: reason,
    })
    return { kind: 'released', reason }
  }

  private quarantine(run: FleetRunRecord, reason: string): FleetRunOutcome {
    const current = this.options.registry.getRun(run.runId)!
    if (!['completed', 'released', 'quarantined', 'failed'].includes(current.state)) {
      this.options.registry.transitionRun(run.runId, current.state, 'quarantined', {
        checkpoint: 'recovery_quarantine', disposition: reason,
      })
    }
    return { kind: 'quarantined', reason }
  }

  private finishRecovered(run: FleetRunRecord, terminal: 'completed' | 'released', reason: string): FleetRunOutcome {
    const current = this.options.registry.getRun(run.runId)!
    this.options.registry.transitionRun(run.runId, current.state, terminal, {
      checkpoint: 'settlement_reconciled', disposition: reason,
    })
    return terminal === 'completed' ? { kind: 'completed' } : { kind: 'released', reason }
  }

  private async inject(checkpoint: FleetCrashCheckpoint, run: FleetRunRecord): Promise<void> {
    await this.options.faultInjector?.(checkpoint, this.options.registry.getRun(run.runId) ?? run)
  }
}

export interface RecoverableRunExecutor extends ScheduledRunExecutor {
  recover(run: FleetRunRecord, signal: AbortSignal): Promise<FleetRunOutcome>
}

export function isTerminalRunState(state: FleetRunState): boolean {
  return ['completed', 'released', 'quarantined', 'failed'].includes(state)
}
