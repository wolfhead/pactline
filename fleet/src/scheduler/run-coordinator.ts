import type { StaticRuntimeRouter } from '../core/runtime-router.js'
import { continueClaimStageAfterHarness, runClaimStage } from '../core/claim-stage.js'
import type { ClaimStageClient, ClaimStageOptions, ClaimWorkflowStage } from '../core/claim-stage.js'
import type { HarnessRunResult } from '../core/harness-result.js'
import type { GitObservation } from '../core/verification.js'
import type { FleetWorkDefinition } from '../core/work-definition.js'
import { PactlineClientError } from '../pactline/client.js'
import { replaySettlement } from '../pactline/settlement.js'
import type { PactlineClaimMutationResult } from '../pactline/types.js'
import type { RepositoryDelivery } from '../repository/delivery.js'
import type { FleetWorkspace } from '../repository/workspace.js'
import type { FleetExternalEffectDecision, FleetRun, FleetRunDecision } from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import type { TaskRole } from '../registry/fleet-registry.js'
import { hasAmbiguousExternalEffect } from '../run/external-effect.js'
import { isTerminalRunState, requireClaimedRun, requireRun, requireSessionRun } from '../run/run.js'
import { decideRunRecovery } from '../run/recovery.js'
import type { RecoveryClaimAuthority, RunRecoveryDecision, RunRecoveryFacts } from '../run/recovery.js'
import type { FleetRunOutcome, ScheduledRunExecutor } from './candidate.js'
import { sanitizeHealthDiagnostic } from '../health/store.js'

export interface MaterializedFleetRun {
  readonly definition: FleetWorkDefinition
  readonly router: StaticRuntimeRouter
  readonly deadline: string
  readonly prepareWorkspace: (signal: AbortSignal) => Promise<FleetWorkspace>
  readonly publishDelivery?: ClaimStageOptions['publishDelivery']
  readonly deliveryOwnsCheckpoints?: boolean
  readonly validateObservation?: ClaimStageOptions['validateObservation']
  readonly retireTask?: (workspace: FleetWorkspace) => Promise<void>
}

export interface FleetRunMaterializer {
  materialize(
    run: FleetRun,
    signal: AbortSignal,
  ): Promise<MaterializedFleetRun>
  /** Recreate only explicitly persisted authority; never select a different Adapter. */
  resume?(run: FleetRun, signal: AbortSignal): Promise<MaterializedFleetRun | undefined>
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

export type FleetFaultInjector = (checkpoint: FleetCrashCheckpoint, run: FleetRun) => Promise<void> | void

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

function gitObservation(value: unknown): GitObservation {
  const item = record(value)
  if (typeof item.head !== 'string' || !/^[a-f0-9]{40}$/.test(item.head)
    || !Array.isArray(item.changedPaths) || item.changedPaths.some(path => typeof path !== 'string')
    || typeof item.porcelain !== 'string') {
    throw new Error('Persisted Harness baseline is invalid')
  }
  return { head: item.head, changedPaths: item.changedPaths as string[], porcelain: item.porcelain }
}

function safeReason(error: unknown): string {
  return sanitizeHealthDiagnostic(error instanceof Error ? error.message : String(error)).slice(0, 2_000)
}

function taskRole(stage: ClaimWorkflowStage): TaskRole {
  return stage === 'review' ? 'reviewer' : 'implementer'
}

function taskIsTerminal(result: PactlineClaimMutationResult): boolean {
  return result.task.phase === 'done' || result.task.phase === 'cancelled'
}

/** Bridges one scheduled Run into the finite Claim-stage Core with durable callbacks. */
export class ClaimStageRunCoordinator implements ScheduledRunExecutor {
  constructor(private readonly options: ClaimStageRunCoordinatorOptions) {}

  async execute(
    runId: string,
    signal: AbortSignal,
  ): Promise<FleetRunOutcome> {
    const initial = this.currentRun(runId)
    try {
      const materialized = await this.options.materializer.materialize(initial, signal)
      return await this.run(initial, initial.stage, materialized, signal)
    } catch (error) {
      const current = this.options.registry.getRun(initial.runId)
      if (current !== undefined && current.state === 'admitted') {
        return this.releaseLocal(requireRun(current), safeReason(error))
      }
      throw error
    }
  }

  async recover(runId: string, signal: AbortSignal): Promise<FleetRunOutcome> {
    const run = this.currentRun(runId)
    const claimAuthority = run.claimId === undefined
      ? { kind: 'not_read' } as const
      : await this.readClaimAuthority(run, signal)
    const facts: RunRecoveryFacts = {
      state: run.state,
      claimAuthority,
      sessionResumable: run.runtimeSessionId !== undefined && this.options.materializer.resume !== undefined,
      hasSettlementIntent: this.options.registry.getEffect(run.runId, 'pactline_settlement') !== undefined,
    }
    const decision = decideRunRecovery(facts)
    this.options.registry.recordRecoveryDecision(run.runId, facts, decision)
    return await this.applyRecoveryDecision(run, decision, signal)
  }

  private async applyRecoveryDecision(
    run: FleetRun,
    decision: RunRecoveryDecision,
    signal: AbortSignal,
  ): Promise<FleetRunOutcome> {
    switch (decision.kind) {
      case 'no_action': return isTerminalRunState(run.state)
        ? run.state === 'completed' ? { kind: 'completed' }
          : run.state === 'quarantined' ? { kind: 'quarantined', reason: run.disposition ?? 'quarantined' }
            : { kind: 'released', reason: run.disposition ?? run.state }
        : this.quarantine(run, 'recovery_policy_returned_no_action')
      case 'release_local':
        this.options.registry.transitionRun(run.runId, run.state, 'released', {
          checkpoint: 'recovery_no_claim', disposition: decision.reason,
        })
        return { kind: 'released', reason: decision.reason }
      case 'reconcile_claim': return await this.reconcileClaimIntent(run, signal)
      case 'restore_workspace': {
        try {
          const materialized = await this.options.materializer.materialize(run, signal)
          return await this.run(run, run.stage, materialized, signal, true)
        } catch (error) {
          return await this.releaseKnownClaim(run, `workspace_recovery_failed:${safeReason(error)}`, signal)
        }
      }
      case 'resume_harness': {
        const materialized = await this.options.materializer.resume?.(run, signal)
        if (materialized !== undefined) return await this.run(run, run.stage, materialized, signal, true)
        return await this.releaseRecoveredClaim(run, 'adapter_session_not_resumable', signal)
      }
      case 'release_claim': return await this.releaseRecoveredClaim(run, decision.reason, signal)
      case 'finish_terminal': {
        const reconciled = await this.reconcileTerminalClaim(run, signal)
        return reconciled ?? this.quarantine(run, 'claim_changed_during_terminal_reconciliation')
      }
      case 'reconcile_release': return await this.releaseKnownClaim(run, run.error ?? 'recovery_release', signal)
      case 'revalidate_result':
      case 'reconcile_delivery': return await this.continuePostResult(run, signal)
      case 'replay_settlement': return await this.replaySettlementIntent(run, signal)
      case 'quarantine': return this.quarantine(run, decision.reason)
    }
  }

  private async releaseRecoveredClaim(run: FleetRun, reason: string, signal: AbortSignal): Promise<FleetRunOutcome> {
    return await this.releaseKnownClaim(run, reason, signal)
  }

  private async readClaimAuthority(run: FleetRun, signal: AbortSignal): Promise<RecoveryClaimAuthority> {
    try {
      const claimedRun = requireClaimedRun(run)
      const packet = await this.options.client.showClaim(claimedRun.claimId, 20, {
        sessionId: this.options.clientSessionId, signal,
      })
      const claim = record(packet.data.claim)
      const task = record(packet.data.task)
      const identityMatches = claim.id === claimedRun.claimId
        && task.number === run.taskNumber
        && claim.stage === (run.stage === 'review' ? 'review' : 'execution')
      if (claim.status === 'active') return { kind: 'active', identityMatches }
      return {
        kind: 'terminal',
        identityMatches,
        status: claim.status === 'released' ? 'released' : 'completed',
      }
    } catch {
      return { kind: 'unavailable' }
    }
  }

  private async run(
    initial: FleetRun,
    stage: ClaimWorkflowStage,
    materialized: MaterializedFleetRun,
    signal: AbortSignal,
    recovery = false,
  ): Promise<FleetRunOutcome> {
    let workspace: FleetWorkspace | undefined
    let settlementDelivery: RepositoryDelivery | undefined
    let run = this.currentRun(initial.runId)
    try {
      if (!recovery) {
        run = this.decide(run.runId, {
          transition: { state: 'claiming', update: { checkpoint: 'claim_intent' } },
          effect: {
            type: 'intent', kind: 'claim', idempotencyKey: `${run.runId}-claim`,
            intent: { taskNumber: run.taskNumber, taskVersion: run.taskVersion, stage },
          },
        })
        await this.inject('before_claim_creation', run)
      }
      const recovered = recovery ? requireClaimedRun(run) : undefined
      const dispatchTaskVersion = recovered === undefined ? run.taskVersion : recovered.claimTaskVersion
      const role = taskRole(stage)
      const taskRuntime = this.options.registry.getTaskRuntime(run.projectNumber, run.taskNumber)
      const taskSession = taskRuntime?.sessions[role]
      const routeAdapterId = materialized.router.routeFor(stage).adapterId
      if (taskSession !== undefined && taskSession.adapterId !== routeAdapterId) {
        throw new Error(`Task ${role} Session belongs to a different Harness Adapter`)
      }
      const resumeRuntimeSessionId = run.runtimeSessionId ?? taskSession?.runtimeSessionId
      const result = await runClaimStage({
        client: this.options.client,
        router: materialized.router,
        definition: materialized.definition,
        stage,
        taskVersion: dispatchTaskVersion,
        runId: run.runId,
        clientSessionId: this.options.clientSessionId,
        idempotencyKey: run.runId,
        deadline: materialized.deadline,
        signal,
        ...(recovered === undefined ? {} : {
          existingClaimId: recovered.claimId,
        }),
        ...(resumeRuntimeSessionId === undefined ? {} : { resumeRuntimeSessionId }),
        onClaimed: async (claimId, taskVersion, claimVersion) => {
          const current = this.currentRun(run.runId)
          if (!recovery) {
            await this.inject('after_claim_creation_before_persistence', run)
            run = this.decide(run.runId, {
              transition: {
                state: 'claimed',
                update: {
                  claimId, claimTaskVersion: taskVersion,
                  ...(claimVersion === undefined ? {} : { claimVersion }), checkpoint: 'claim_observed',
                },
              },
              effect: {
                type: 'observation', kind: 'claim',
                observation: { claimId, taskVersion, ...(claimVersion === undefined ? {} : { claimVersion }) },
              },
            })
            await this.inject('after_claim_persistence_before_session', run)
          } else if (current.claimId !== claimId) {
            throw new Error('Recovery attempted to change the frozen Claim identity')
          }
          const latest = this.currentRun(run.runId)
          if (latest.state === 'claimed') {
            run = this.decide(run.runId, {
              transition: { state: 'preparing_workspace', update: { checkpoint: 'workspace_intent' } },
              effect: {
                type: 'intent', kind: 'workspace', idempotencyKey: `${run.runId}-workspace`,
                intent: { mode: stage === 'review' ? 'review' : 'execution' },
              },
            })
          }
        },
        workspace: async () => {
          workspace = await materialized.prepareWorkspace(signal)
          await this.inject('after_workspace_effect_before_persistence', run)
          const current = this.currentRun(run.runId)
          if (current.state === 'preparing_workspace') {
            run = this.decide(run.runId, {
              transition: {
                state: 'starting_harness',
                update: { workspace: workspaceRecord(workspace), checkpoint: 'workspace_observed' },
              },
              effect: { type: 'observation', kind: 'workspace', observation: workspaceRecord(workspace) },
            })
          }
          run = this.decide(run.runId, {
            effect: {
              type: 'intent', kind: 'adapter_session', idempotencyKey: `${run.runId}-adapter-session`,
              intent: { adapter: materialized.router.routeFor(stage).adapterId },
            },
          })
          run = this.decide(run.runId, {
            effect: { type: 'intent', kind: 'harness_result', idempotencyKey: `${run.runId}-result`, intent: {} },
          })
          return workspace
        },
        onRuntimeSession: async runtimeSessionId => {
          const current = this.currentRun(run.runId)
          if (current.state === 'starting_harness') {
            run = this.decide(run.runId, {
              transition: {
                state: 'running_harness',
                update: { runtimeSessionId, checkpoint: 'adapter_session_observed' },
              },
              effect: { type: 'observation', kind: 'adapter_session', observation: { runtimeSessionId } },
            })
          }
          if (this.options.registry.getTaskRuntime(run.projectNumber, run.taskNumber) !== undefined) {
            this.options.registry.bindTaskRoleSession(run.projectNumber, run.taskNumber, role, {
              adapterId: routeAdapterId,
              runtimeSessionId,
            })
          }
          await this.inject('after_session_persistence_before_agent', run)
        },
        onHarnessResult: async (harnessResult, _dispatch, baseline) => {
          await this.inject('after_harness_result_before_persistence', run)
          const current = this.currentRun(run.runId)
          if (current.state === 'running_harness') {
            run = this.decide(run.runId, {
              transition: { state: 'validating', update: { checkpoint: 'harness_result_observed' } },
              effect: {
                type: 'observation', kind: 'harness_result',
                observation: {
                  terminalState: harnessResult.terminalState,
                  runtimeSessionId: harnessResult.runtimeSessionId,
                  result: harnessResult,
                  baseline,
                },
              },
            })
          }
          await this.inject('after_harness_result_persistence_before_delivery', run)
        },
        ...(materialized.validateObservation === undefined ? {} : { validateObservation: materialized.validateObservation }),
        ...(materialized.publishDelivery === undefined ? {} : {
          publishDelivery: async (dispatch, proposal, observation): Promise<RepositoryDelivery> => {
            const current = this.currentRun(run.runId)
            if (current.state === 'validating') {
              const effect: FleetExternalEffectDecision = materialized.deliveryOwnsCheckpoints === true
                ? { type: 'intent', kind: 'git_commit', idempotencyKey: `${run.runId}-commit`, intent: {} }
                : { type: 'intent', kind: 'repository_delivery', idempotencyKey: `${run.runId}-delivery`, intent: {} }
              run = this.decide(run.runId, {
                transition: { state: 'delivering', update: { checkpoint: 'delivery_intent' } },
                effect,
              })
            }
            const delivery = await materialized.publishDelivery!(dispatch, proposal, observation)
            settlementDelivery = delivery
            if (materialized.deliveryOwnsCheckpoints !== true) {
              run = this.decide(run.runId, {
                effect: {
                  type: 'observation', kind: 'repository_delivery',
                  observation: {
                    codeChangeUrl: delivery.codeChangeUrl, revision: delivery.revision, branch: delivery.branch,
                  },
                },
              })
            }
            return delivery
          },
        }),
        onBeforeSettlement: async (dispatch, proposal) => {
          const current = this.currentRun(run.runId)
          if (current.state === 'validating' || current.state === 'delivering') {
            run = this.decide(run.runId, {
              transition: { state: 'settling', update: { checkpoint: 'settlement_intent' } },
              effect: {
                type: 'intent', kind: 'pactline_settlement', idempotencyKey: `${run.runId}-settle`,
                intent: {
                  stage: dispatch.stage === 'review' ? 'review' : 'execution',
                  taskVersion: dispatch.taskVersion,
                  proposal,
                  ...(settlementDelivery === undefined ? {} : { delivery: settlementDelivery }),
                },
              },
            })
          }
        },
        onSettled: async settlement => {
          await this.inject('after_settlement_before_terminal_persistence', run)
          const terminal = settlement.claim.status === 'released' ? 'released' : 'completed'
          run = this.decide(run.runId, {
            transition: {
              state: terminal,
              update: {
                checkpoint: 'settlement_observed', disposition: settlement.claim.outcome ?? settlement.claim.status,
              },
            },
            effect: {
              type: 'observation', kind: 'pactline_settlement',
              observation: {
                claimId: settlement.claim.id,
                claimStatus: settlement.claim.status,
                claimOutcome: settlement.claim.outcome,
                taskVersion: settlement.task.version,
              },
            },
          })
        },
      })
      if (workspace !== undefined && taskIsTerminal(result.settlement)) {
        await materialized.retireTask?.(workspace)
      }
      return result.settlement.claim.status === 'released'
        ? { kind: 'released', reason: result.settlement.claim.outcome ?? 'released' }
        : { kind: 'completed' }
    } catch (error) {
      if (error instanceof FleetInjectedCrash) throw error
      if (contention(error)) {
        const current = this.currentRun(run.runId)
        if (current.state === 'claiming') {
          this.options.registry.transitionRun(run.runId, 'claiming', 'released', {
            checkpoint: 'claim_contended', disposition: 'claim_contention',
          })
        }
        return { kind: 'contention', reason: (error as PactlineClientError).pactlineError!.code }
      }
      const current = this.currentRun(run.runId)
      const ambiguous = hasAmbiguousExternalEffect(this.options.registry.listEffects(run.runId))
      const outcome = ambiguous
        ? this.quarantine(current, `ambiguous_external_effect: ${safeReason(error)}`)
        : current.claimId === undefined
          ? this.releaseLocal(current, 'failed_before_claim')
          : await this.releaseKnownClaim(current, safeReason(error), signal)
      return outcome
    }
  }

  private async reconcileClaimIntent(run: FleetRun, signal: AbortSignal): Promise<FleetRunOutcome> {
    try {
      const claimed = await this.options.client.claimTask(run.taskNumber, run.taskVersion, run.stage === 'review' ? 'review' : 'execution', {
        sessionId: this.options.clientSessionId,
        idempotencyKey: `${run.runId}-claim`,
        signal,
      })
      const claimedRun = this.decide(run.runId, {
        transition: {
          state: 'claimed',
          update: {
            claimId: claimed.data.claim.id, claimVersion: claimed.data.claim.version,
            checkpoint: 'claim_reconciled', claimTaskVersion: claimed.data.task.version,
          },
        },
        effect: {
          type: 'observation', kind: 'claim',
          observation: { claimId: claimed.data.claim.id, taskVersion: claimed.data.task.version },
        },
      })
      return await this.releaseKnownClaim(claimedRun, 'recovered_claim_without_dispatch', signal)
    } catch (error) {
      if (contention(error)) return this.quarantine(run, 'claim_intent_conflicted_during_recovery')
      return this.quarantine(run, 'claim_intent_could_not_be_reconciled')
    }
  }

  private async releaseKnownClaim(run: FleetRun, reason: string, signal: AbortSignal): Promise<FleetRunOutcome> {
    try {
      const claimedRun = requireClaimedRun(run)
      const packet = await this.options.client.showClaim(claimedRun.claimId, 20, {
        sessionId: this.options.clientSessionId, signal,
      })
      const claim = record(packet.data.claim)
      const task = record(packet.data.task)
      if (claim.id !== claimedRun.claimId || task.number !== run.taskNumber) return this.quarantine(run, 'claim_identity_mismatch')
      if (claim.status !== 'active') {
        const terminal = claim.status === 'released' ? 'released' : 'completed'
        return this.finishRecovered(run, terminal, `claim_already_${String(claim.status)}`)
      }
      const current = this.currentRun(run.runId)
      if (current.state !== 'releasing') {
        this.decide(run.runId, {
          transition: { state: 'releasing', update: { checkpoint: 'release_intent', error: reason } },
          effect: {
            type: 'intent', kind: 'claim_release', idempotencyKey: `${run.runId}-recovery-release`,
            intent: { reason },
          },
        })
      }
      const released = await this.options.client.releaseClaim(claimedRun.claimId, Number(task.version), `Fleet recovery release: ${reason}`, {
        sessionId: this.options.clientSessionId,
        idempotencyKey: `${run.runId}-recovery-release`,
        signal,
      })
      this.decide(run.runId, {
        transition: { state: 'released', update: { checkpoint: 'release_observed', disposition: reason } },
        effect: {
          type: 'observation', kind: 'claim_release',
          observation: { claimStatus: released.data.claim.status, taskVersion: released.data.task.version },
        },
      })
      return { kind: 'released', reason }
    } catch {
      return this.quarantine(this.currentRun(run.runId), 'known_claim_release_ambiguous')
    }
  }

  private async reconcileTerminalClaim(run: FleetRun, signal: AbortSignal): Promise<FleetRunOutcome | undefined> {
    try {
      const claimedRun = requireClaimedRun(run)
      const packet = await this.options.client.showClaim(claimedRun.claimId, 20, {
        sessionId: this.options.clientSessionId, signal,
      })
      const claim = record(packet.data.claim)
      const task = record(packet.data.task)
      if (claim.id !== claimedRun.claimId || task.number !== run.taskNumber) return this.quarantine(run, 'claim_identity_mismatch')
      if (claim.status === 'active') return undefined
      if (typeof claim.id !== 'string' || typeof claim.status !== 'string' || typeof task.version !== 'number') {
        throw new Error('Pactline settlement recovery packet is invalid')
      }
      const terminal = claim.status === 'released' ? 'released' : 'completed'
      const reason = `claim_reconciled_${String(claim.status)}`
      this.decide(run.runId, {
        transition: { state: terminal, update: { checkpoint: 'settlement_reconciled', disposition: reason } },
        effect: {
          type: 'observation', kind: 'pactline_settlement',
          observation: {
            claimId: claim.id,
            claimStatus: claim.status,
            taskVersion: task.version,
            recovered: true,
            ...(typeof claim.outcome === 'string' ? { claimOutcome: claim.outcome } : {}),
          },
        },
      })
      return terminal === 'completed' ? { kind: 'completed' } : { kind: 'released', reason }
    } catch {
      return this.quarantine(run, 'settlement_could_not_be_reconciled')
    }
  }

  private async replaySettlementIntent(run: FleetRun, signal: AbortSignal): Promise<FleetRunOutcome> {
    const effect = this.options.registry.getEffect(run.runId, 'pactline_settlement')
    if (effect === undefined || effect.status !== 'intended') {
      return this.quarantine(run, 'settlement_intent_is_not_replayable')
    }
    try {
      const claimedRun = requireClaimedRun(run)
      const settlement = await replaySettlement(this.options.client, effect.intent, {
        taskNumber: run.taskNumber,
        claimId: claimedRun.claimId,
        taskVersion: claimedRun.claimTaskVersion,
        stage: run.stage === 'review' ? 'review' : 'execution',
        sessionId: this.options.clientSessionId,
        idempotencyKey: effect.idempotencyKey,
        signal,
      })
      await this.inject('after_settlement_before_terminal_persistence', run)
      return this.persistSettlement(this.currentRun(run.runId), settlement)
    } catch (error) {
      return this.quarantine(this.currentRun(run.runId), `settlement_replay_failed:${safeReason(error)}`)
    }
  }

  private async continuePostResult(run: FleetRun, signal: AbortSignal): Promise<FleetRunOutcome> {
    const resultEffect = this.options.registry.getEffect(run.runId, 'harness_result')
    if (resultEffect === undefined || resultEffect.status !== 'observed') {
      return this.quarantine(run, 'persisted_harness_result_missing')
    }
    try {
      const sessionRun = requireSessionRun(run)
      const materialized = await this.options.materializer.resume?.(run, signal)
      if (materialized === undefined) return this.quarantine(run, 'post_result_workspace_not_recoverable')
      if (run.state === 'delivering' && materialized.deliveryOwnsCheckpoints !== true) {
        const deliveryEffect = this.options.registry.getEffect(run.runId, 'repository_delivery')
        if (deliveryEffect?.status !== 'observed') {
          return this.quarantine(run, 'repository_delivery_intent_cannot_be_reconciled')
        }
      }
      const workspace = await materialized.prepareWorkspace(signal)
      const harnessResult = record(resultEffect.observation.result) as unknown as HarnessRunResult
      const baseline = gitObservation(resultEffect.observation.baseline)
      let settlementDelivery: RepositoryDelivery | undefined
      const result = await continueClaimStageAfterHarness({
        client: this.options.client,
        router: materialized.router,
        definition: materialized.definition,
        stage: run.stage,
        taskVersion: sessionRun.claimTaskVersion,
        runId: run.runId,
        clientSessionId: this.options.clientSessionId,
        idempotencyKey: run.runId,
        deadline: materialized.deadline,
        signal,
        existingClaimId: sessionRun.claimId,
        resumeRuntimeSessionId: sessionRun.runtimeSessionId,
        workspace,
        harnessResult,
        baseline,
        ...(materialized.validateObservation === undefined ? {} : { validateObservation: materialized.validateObservation }),
        ...(materialized.publishDelivery === undefined ? {} : {
          publishDelivery: async (dispatch, proposal, observation): Promise<RepositoryDelivery> => {
            const current = this.currentRun(run.runId)
            if (current.state === 'validating') {
              const effect: FleetExternalEffectDecision = materialized.deliveryOwnsCheckpoints === true
                ? { type: 'intent', kind: 'git_commit', idempotencyKey: `${run.runId}-commit`, intent: {} }
                : { type: 'intent', kind: 'repository_delivery', idempotencyKey: `${run.runId}-delivery`, intent: {} }
              this.decide(run.runId, {
                transition: { state: 'delivering', update: { checkpoint: 'delivery_intent' } },
                effect,
              })
            }
            const deliveryEffect = this.options.registry.getEffect(run.runId, 'repository_delivery')
            const delivery = materialized.deliveryOwnsCheckpoints !== true && deliveryEffect?.status === 'observed'
              ? this.deliveryFromObservation(materialized.definition, deliveryEffect.observation)
              : await materialized.publishDelivery!(dispatch, proposal, observation)
            settlementDelivery = delivery
            if (materialized.deliveryOwnsCheckpoints !== true && deliveryEffect?.status !== 'observed') {
              this.decide(run.runId, {
                effect: {
                  type: 'observation', kind: 'repository_delivery',
                  observation: {
                    codeChangeUrl: delivery.codeChangeUrl, revision: delivery.revision, branch: delivery.branch,
                  },
                },
              })
            }
            return delivery
          },
        }),
        onBeforeSettlement: async (dispatch, proposal) => {
          const current = this.currentRun(run.runId)
          if (current.state === 'validating' || current.state === 'delivering') {
            this.decide(run.runId, {
              transition: { state: 'settling', update: { checkpoint: 'settlement_intent' } },
              effect: {
                type: 'intent', kind: 'pactline_settlement', idempotencyKey: `${run.runId}-settle`,
                intent: {
                  stage: dispatch.stage === 'review' ? 'review' : 'execution',
                  taskVersion: dispatch.taskVersion,
                  proposal,
                  ...(settlementDelivery === undefined ? {} : { delivery: settlementDelivery }),
                },
              },
            })
          }
        },
        onSettled: async settlement => {
          await this.inject('after_settlement_before_terminal_persistence', run)
          this.persistSettlement(this.currentRun(run.runId), settlement)
        },
      })
      if (taskIsTerminal(result.settlement)) await materialized.retireTask?.(workspace)
      return result.settlement.claim.status === 'released'
        ? { kind: 'released', reason: result.settlement.claim.outcome ?? 'released' }
        : { kind: 'completed' }
    } catch (error) {
      if (error instanceof FleetInjectedCrash) throw error
      return this.quarantine(this.currentRun(run.runId), `post_result_recovery_failed:${safeReason(error)}`)
    }
  }

  private deliveryFromObservation(
    definition: FleetWorkDefinition,
    observation: Readonly<Record<string, unknown>>,
  ): RepositoryDelivery {
    if (typeof observation.codeChangeUrl !== 'string' || typeof observation.revision !== 'string'
      || typeof observation.branch !== 'string') throw new Error('Recorded repository delivery is invalid')
    return {
      repository: definition.repository,
      codeChangeUrl: observation.codeChangeUrl,
      revision: observation.revision,
      branch: observation.branch,
    }
  }

  private persistSettlement(run: FleetRun, settlement: PactlineClaimMutationResult): FleetRunOutcome {
    const terminal = settlement.claim.status === 'released' ? 'released' : 'completed'
    const reason = settlement.claim.outcome ?? settlement.claim.status
    this.decide(run.runId, {
      transition: {
        state: terminal,
        update: { checkpoint: 'settlement_observed', disposition: reason },
      },
      effect: {
        type: 'observation', kind: 'pactline_settlement',
        observation: {
          claimId: settlement.claim.id,
          claimStatus: settlement.claim.status,
          claimOutcome: settlement.claim.outcome,
          taskVersion: settlement.task.version,
        },
      },
    })
    return terminal === 'released' ? { kind: 'released', reason } : { kind: 'completed' }
  }

  private releaseLocal(run: FleetRun, reason: string): FleetRunOutcome {
    const current = this.currentRun(run.runId)
    this.options.registry.transitionRun(run.runId, current.state, 'released', {
      checkpoint: 'local_release', disposition: reason,
    })
    return { kind: 'released', reason }
  }

  private quarantine(run: FleetRun, reason: string): FleetRunOutcome {
    const current = this.currentRun(run.runId)
    if (!isTerminalRunState(current.state)) {
      this.options.registry.transitionRun(run.runId, current.state, 'quarantined', {
        checkpoint: 'recovery_quarantine', disposition: reason,
      })
    }
    return { kind: 'quarantined', reason }
  }

  private finishRecovered(run: FleetRun, terminal: 'completed' | 'released', reason: string): FleetRunOutcome {
    const current = this.currentRun(run.runId)
    this.options.registry.transitionRun(run.runId, current.state, terminal, {
      checkpoint: 'settlement_reconciled', disposition: reason,
    })
    return terminal === 'completed' ? { kind: 'completed' } : { kind: 'released', reason }
  }

  private async inject(checkpoint: FleetCrashCheckpoint, run: FleetRun): Promise<void> {
    await this.options.faultInjector?.(checkpoint, this.currentRun(run.runId))
  }

  private decide(runId: string, decision: Omit<FleetRunDecision, 'expected'>): FleetRun {
    const current = this.currentRun(runId)
    return requireRun(this.options.registry.commitRunDecision(runId, {
      expected: { state: current.state, updatedAt: current.updatedAt },
      ...decision,
    }).run)
  }

  private currentRun(runId: string): FleetRun {
    const run = this.options.registry.getRun(runId)
    if (run === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
    return requireRun(run)
  }
}

export interface RecoverableRunExecutor extends ScheduledRunExecutor {
  recover(runId: string, signal: AbortSignal): Promise<FleetRunOutcome>
}

export { isTerminalRunState } from '../run/run.js'
