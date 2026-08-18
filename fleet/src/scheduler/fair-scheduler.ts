import type { FleetConfigSnapshot, FleetDefinitionConfig } from '../config/types.js'
import type { FleetRun } from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import type {
  CandidateDiscoveryClient,
  FleetRunOutcome,
  FleetWorkCandidate,
  ScheduledRunExecutor,
  WorkDefinitionResolver,
} from './candidate.js'
import { compareCandidates } from './candidate.js'
import { FleetBackoff } from './backoff.js'

export interface FleetSchedulerLogger {
  log(level: 'debug' | 'info' | 'warn' | 'error', event: string, fields?: Readonly<Record<string, unknown>>): void
}

export interface FairFleetSchedulerOptions {
  readonly snapshot: () => FleetConfigSnapshot
  readonly discovery: CandidateDiscoveryClient
  readonly registry: FleetRegistry
  readonly resolver: WorkDefinitionResolver
  readonly executor: ScheduledRunExecutor
  readonly logger: FleetSchedulerLogger
  readonly sessionId: string
  readonly discoveryLimit?: number
  readonly now?: () => Date
  readonly random?: () => number
}

export interface FleetSchedulerCycle {
  readonly discovered: number
  readonly admitted: number
  readonly skipped: number
  readonly contentions: number
  readonly outcomes: readonly FleetRunOutcome[]
  readonly fleets: readonly FleetDiscoveryCycle[]
}

export interface FleetDiscoveryCycle {
  readonly fleetId: string
  readonly projectNumber: number
  readonly status: 'ok' | 'error' | 'backoff'
  readonly candidateCount: number
  readonly checkedAt: string
  readonly retryAt?: string
}

interface ActiveRun {
  readonly run: FleetRun
  readonly promise: Promise<FleetRunOutcome>
  readonly controller: AbortController
}

function enabledFleets(snapshot: FleetConfigSnapshot): FleetDefinitionConfig[] {
  return Object.values(snapshot.config.fleets).filter(fleet => fleet.enabled).sort((a, b) => a.id.localeCompare(b.id))
}

/** One scheduler shared by every Project-bound Fleet in a service. */
export class FairFleetScheduler {
  private cursor = 0
  private draining = false
  private cycleController: AbortController | undefined
  private readonly active = new Map<string, ActiveRun>()
  private readonly blockedUntil = new Map<string, number>()
  private readonly backoffs = new Map<string, FleetBackoff>()
  private readonly now: () => Date
  private readonly random: () => number
  private readonly discoveryLimit: number

  constructor(private readonly options: FairFleetSchedulerOptions) {
    this.now = options.now ?? (() => new Date())
    this.random = options.random ?? Math.random
    this.discoveryLimit = options.discoveryLimit ?? 50
  }

  get activeRunCount(): number { return this.active.size }

  async cycle(signal?: AbortSignal, waitForAdmitted = true): Promise<FleetSchedulerCycle> {
    if (this.draining) return { discovered: 0, admitted: 0, skipped: 0, contentions: 0, outcomes: [], fleets: [] }
    const snapshot = this.options.snapshot()
    const fleets = enabledFleets(snapshot).filter(fleet => this.options.resolver.enabled?.(fleet) ?? true)
    if (fleets.length === 0) return { discovered: 0, admitted: 0, skipped: 0, contentions: 0, outcomes: [], fleets: [] }
    const controller = new AbortController()
    this.cycleController = controller
    const forwardAbort = (): void => controller.abort(signal?.reason)
    signal?.addEventListener('abort', forwardAbort, { once: true })
    try {
      const queues = new Map<string, FleetWorkCandidate[]>()
      const fleetCycles: FleetDiscoveryCycle[] = []
      let discovered = 0
      for (const fleet of fleets) {
        const checkedAt = this.now().toISOString()
        const blockedUntil = this.blockedUntil.get(fleet.id) ?? 0
        if (blockedUntil > this.now().getTime()) {
          fleetCycles.push({ fleetId: fleet.id, projectNumber: fleet.projectNumber, status: 'backoff', candidateCount: 0, checkedAt, retryAt: new Date(blockedUntil).toISOString() })
          continue
        }
        try {
          const [execution, review] = await Promise.all([
            this.options.discovery.listTasks('execution', fleet.projectNumber, this.discoveryLimit, {
              sessionId: this.options.sessionId, signal: controller.signal,
            }),
            this.options.discovery.listTasks('review', fleet.projectNumber, this.discoveryLimit, {
              sessionId: this.options.sessionId, signal: controller.signal,
            }),
          ])
          const candidates = [
            ...execution.data.map(candidate => ({ fleetId: fleet.id, projectNumber: fleet.projectNumber, ...candidate })),
            ...review.data.map(candidate => ({ fleetId: fleet.id, projectNumber: fleet.projectNumber, ...candidate })),
          ].sort(compareCandidates)
          queues.set(fleet.id, candidates)
          discovered += candidates.length
          fleetCycles.push({ fleetId: fleet.id, projectNumber: fleet.projectNumber, status: 'ok', candidateCount: candidates.length, checkedAt })
          this.backoff(fleet.id).success()
          this.blockedUntil.delete(fleet.id)
        } catch (error) {
          if (controller.signal.aborted) throw error
          const until = this.backoff(fleet.id).fail(this.now().getTime())
          this.blockedUntil.set(fleet.id, until)
          fleetCycles.push({ fleetId: fleet.id, projectNumber: fleet.projectNumber, status: 'error', candidateCount: 0, checkedAt, retryAt: new Date(until).toISOString() })
          this.options.logger.log('warn', 'fleet.discovery.failed', {
            fleetId: fleet.id,
            projectNumber: fleet.projectNumber,
            retryAt: new Date(until).toISOString(),
            error: error instanceof Error ? error.message : String(error),
          })
        }
      }

      const launched: Promise<FleetRunOutcome>[] = []
      let skipped = 0
      const globalAvailable = Math.max(0, snapshot.config.service.maxConcurrentRuns - this.active.size)
      let remaining = globalAvailable
      let emptyVisits = 0
      while (remaining > 0 && emptyVisits < fleets.length) {
        const index = this.cursor % fleets.length
        this.cursor = (this.cursor + 1) % fleets.length
        const fleet = fleets[index]!
        const fleetActive = [...this.active.values()].filter(item => item.run.fleetId === fleet.id).length
        if (fleetActive >= fleet.maxConcurrentRuns) { emptyVisits += 1; continue }
        const queue = queues.get(fleet.id) ?? []
        let selected: FleetWorkCandidate | undefined
        while (queue.length > 0) {
          const candidate = queue.shift()!
          if (!this.options.registry.hasNonTerminalRun(fleet.id, candidate.task.number, candidate.stage)) {
            selected = candidate
            break
          }
          skipped += 1
        }
        if (selected === undefined) { emptyVisits += 1; continue }
        emptyVisits = 0
        const resolved = await this.options.resolver.resolve(selected, fleet, controller.signal)
        if (resolved === undefined) {
          skipped += 1
          this.options.logger.log('warn', 'fleet.candidate.not_admitted', {
            fleetId: fleet.id, taskNumber: selected.task.number, stage: selected.stage,
            reason: 'work_definition_unavailable',
          })
          continue
        }
        if (resolved.admission.taskNumber !== selected.task.number
          || resolved.admission.taskVersion !== selected.task.version
          || (resolved.admission.stage !== selected.stage)) {
          throw new Error('WorkDefinitionResolver changed candidate identity')
        }
        const run = this.options.registry.admitRun(fleet.id, resolved.admission, this.now())
        const runController = new AbortController()
        const promise = this.options.executor.execute(run.runId, runController.signal)
          .then(outcome => {
            if (outcome.kind === 'contention') {
              const until = this.backoff(fleet.id).fail(this.now().getTime())
              this.blockedUntil.set(fleet.id, until)
            } else {
              this.backoff(fleet.id).success()
            }
            return outcome
          })
          .finally(() => { this.active.delete(run.runId) })
        this.active.set(run.runId, { run, promise, controller: runController })
        launched.push(promise)
        remaining -= 1
      }
      const outcomes = waitForAdmitted ? await Promise.all(launched) : []
      return {
        discovered,
        admitted: launched.length,
        skipped,
        contentions: outcomes.filter(outcome => outcome.kind === 'contention').length,
        outcomes,
        fleets: fleetCycles,
      }
    } finally {
      if (this.cycleController === controller) this.cycleController = undefined
      signal?.removeEventListener('abort', forwardAbort)
    }
  }

  beginDrain(reason: unknown = new Error('Fleet Service is draining')): void {
    this.draining = true
    this.cycleController?.abort(reason)
    for (const active of this.active.values()) active.controller.abort(reason)
  }

  async waitForActive(deadlineMs: number): Promise<boolean> {
    const pending = [...this.active.values()].map(item => item.promise.catch(() => undefined))
    if (pending.length === 0) return true
    let timer: NodeJS.Timeout | undefined
    const expired = new Promise<false>(resolvePromise => {
      timer = setTimeout(() => resolvePromise(false), deadlineMs)
      timer.unref()
    })
    const completed = Promise.all(pending).then(() => true as const)
    const result = await Promise.race([completed, expired])
    if (timer !== undefined) clearTimeout(timer)
    return result
  }

  private backoff(fleetId: string): FleetBackoff {
    let value = this.backoffs.get(fleetId)
    if (value === undefined) {
      value = new FleetBackoff({ random: this.random })
      this.backoffs.set(fleetId, value)
    }
    return value
  }
}
