import type { FleetRunRecord } from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import { isLegacyRun } from '../run/run.js'
import type { FleetRunOutcome } from '../scheduler/candidate.js'
import type { RecoverableRunExecutor } from '../scheduler/run-coordinator.js'
import type { PactlineCLI } from '../pactline/client.js'
import type { FleetConfigSnapshot } from '../config/types.js'

export interface FleetRecoveryResult {
  readonly runId: string
  readonly before: FleetRunRecord['state']
  readonly outcome: FleetRunOutcome
}

/** Completes all local recovery serially before discovery may start. */
export class FleetRunReconciler {
  constructor(
    private readonly registry: FleetRegistry,
    private readonly executor: RecoverableRunExecutor,
    private readonly claimInventory?: Pick<PactlineCLI, 'listActiveClaims' | 'showClaim'>,
    private readonly snapshot?: () => FleetConfigSnapshot,
    private readonly sessionId = 'fleet-recovery',
  ) {}

  async reconcile(signal?: AbortSignal): Promise<readonly FleetRecoveryResult[]> {
    const controller = new AbortController()
    const forwardAbort = (): void => controller.abort(signal?.reason)
    signal?.addEventListener('abort', forwardAbort, { once: true })
    try {
      const results: FleetRecoveryResult[] = []
      for (const run of this.registry.listNonTerminalRuns()) {
        if (controller.signal.aborted) throw controller.signal.reason
        if (isLegacyRun(run)) {
          this.registry.transitionRun(run.runId, run.state, 'quarantined', {
            checkpoint: 'legacy_run_quarantined', disposition: 'legacy_policy_free_run',
          })
          results.push({
            runId: run.runId,
            before: run.state,
            outcome: { kind: 'quarantined', reason: 'legacy_policy_free_run' },
          })
          continue
        }
        const outcome = await this.executor.recover(run.runId, controller.signal)
        results.push({ runId: run.runId, before: run.state, outcome })
      }
      if (this.claimInventory !== undefined && this.snapshot !== undefined) {
        const active = await this.claimInventory.listActiveClaims({ sessionId: this.sessionId, signal: controller.signal })
        for (const claim of active.data) {
          if (this.registry.hasRegisteredClaim(claim.id)) continue
          const packet = await this.claimInventory.showClaim(claim.id, 1, { sessionId: this.sessionId, signal: controller.signal })
          const task = packet.data.task
          if (typeof task !== 'object' || task === null || Array.isArray(task)) continue
          const project = (task as Record<string, unknown>).project
          if (typeof project !== 'object' || project === null || Array.isArray(project)) continue
          const projectNumber = (project as Record<string, unknown>).number
          const taskVersion = (task as Record<string, unknown>).version
          const taskPhase = (task as Record<string, unknown>).phase
          if (typeof projectNumber !== 'number' || typeof taskVersion !== 'number') continue
          const fleet = Object.values(this.snapshot().config.fleets).find(item => item.enabled && item.projectNumber === projectNumber)
          if (fleet === undefined) continue
          const orphan = this.registry.admitRun(fleet.id, {
            taskNumber: claim.task_number,
            taskVersion,
            stage: claim.stage === 'execution' && taskPhase === 'in_progress' ? 'correction' : claim.stage,
            frozenPolicy: { recovery: 'unfamiliar_active_claim' },
          })
          this.registry.transitionRun(orphan.runId, 'admitted', 'quarantined', {
            claimId: claim.id,
            claimVersion: claim.version,
            checkpoint: 'unfamiliar_claim_observed',
            disposition: 'unfamiliar_same_principal_claim',
          })
          results.push({
            runId: orphan.runId,
            before: 'admitted',
            outcome: { kind: 'quarantined', reason: 'unfamiliar_same_principal_claim' },
          })
        }
      }
      return results
    } finally {
      signal?.removeEventListener('abort', forwardAbort)
    }
  }
}
