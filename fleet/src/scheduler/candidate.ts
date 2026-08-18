import type { FleetDefinitionConfig } from '../config/types.js'
import type { FleetRunAdmission } from '../registry/fleet-registry.js'

export type FleetCandidateStage = 'execution' | 'review' | 'correction'

/** Fleet-owned projection of eligible Pactline work; transport lifecycle strings stop at the Adapter. */
export interface FleetCandidateTask {
  readonly id: string
  readonly number: number
  readonly title: string
  readonly version: number
}

export interface FleetDiscoveredCandidate {
  readonly stage: FleetCandidateStage
  readonly task: FleetCandidateTask
}

export interface FleetWorkCandidate extends FleetDiscoveredCandidate {
  readonly fleetId: string
  readonly projectNumber: number
}

export interface ResolvedFleetWork {
  readonly admission: FleetRunAdmission
}

/** Resolves repository and verification authority without interpreting Task prose. */
export interface WorkDefinitionResolver {
  enabled?(fleet: FleetDefinitionConfig): boolean
  resolve(candidate: FleetWorkCandidate, fleet: FleetDefinitionConfig, signal: AbortSignal): Promise<ResolvedFleetWork | undefined>
}

export type FleetRunOutcome =
  | { readonly kind: 'completed' }
  | { readonly kind: 'released'; readonly reason: string }
  | { readonly kind: 'quarantined'; readonly reason: string }
  | { readonly kind: 'contention'; readonly reason: string }

export interface ScheduledRunExecutor {
  execute(runId: string, signal: AbortSignal): Promise<FleetRunOutcome>
}

export interface CandidateDiscoveryClient {
  listTasks(
    stage: 'execution' | 'review',
    projectNumber: number,
    limit: number,
    options: { readonly sessionId: string; readonly signal?: AbortSignal },
  ): Promise<{ readonly data: readonly FleetDiscoveredCandidate[] }>
}

export function compareCandidates(first: FleetWorkCandidate, second: FleetWorkCandidate): number {
  if (first.task.number !== second.task.number) return first.task.number - second.task.number
  if (first.stage === second.stage) return 0
  const order: Readonly<Record<FleetCandidateStage, number>> = { execution: 0, correction: 1, review: 2 }
  return order[first.stage] - order[second.stage]
}
