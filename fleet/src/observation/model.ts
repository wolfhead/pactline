import type { HarnessCapabilities } from '../core/harness-adapter.js'
import type { FleetRunStage, FleetRunState } from '../registry/fleet-registry.js'

export interface ObservationEnvelope<T> {
  readonly ok: true
  readonly data: T
  readonly meta: {
    readonly generatedAt: string
    readonly revision: string
  }
}

export interface ObservationList<T> {
  readonly items: readonly T[]
  readonly nextBefore?: string
}

export interface ObservationAttention {
  readonly id: string
  readonly scope: 'service' | 'fleet' | 'run' | 'adapter'
  readonly severity: 'warning' | 'critical'
  readonly title: string
  readonly detail: string
  readonly fleetId?: string
  readonly runId?: string
  readonly checkedAt?: string
}

export interface ObservationRoute {
  readonly adapter: string
  readonly model: string
  readonly reasoning?: string
}

export interface ObservationRunSummary {
  readonly runId: string
  readonly fleetId: string
  readonly projectNumber: number
  readonly taskNumber?: number
  readonly stage?: FleetRunStage
  readonly state: FleetRunState
  readonly adapter?: string
  readonly model?: string
  readonly reasoning?: string
  readonly checkpoint?: string
  readonly disposition?: string
  readonly error?: string
  readonly createdAt: string
  readonly updatedAt: string
}

export interface ObservationTimelineItem {
  readonly sequence: number
  readonly at: string
  readonly kind: string
  readonly title: string
  readonly detail?: string
  readonly state?: FleetRunState
  readonly checkpoint?: string
}

export interface ObservationEffect {
  readonly kind: string
  readonly status: 'intended' | 'observed'
  readonly title: string
  readonly detail?: Readonly<Record<string, string | number | boolean>>
  readonly updatedAt: string
}

export interface ObservationRunDetail extends ObservationRunSummary {
  readonly serviceId: string
  readonly configRevision: string
  readonly taskVersion?: number
  readonly claimId?: string
  readonly claimVersion?: number
  readonly claimTaskVersion?: number
  readonly runtimeSessionId?: string
  readonly workspace?: Readonly<Record<string, string>>
  readonly timeline: readonly ObservationTimelineItem[]
  readonly effects: readonly ObservationEffect[]
}

export interface ObservationFleet {
  readonly id: string
  readonly projectNumber: number
  readonly enabled: boolean
  readonly status: 'healthy' | 'degraded' | 'disabled'
  readonly message?: string
  readonly maxConcurrentRuns: number
  readonly workPluginConfigured: boolean
  readonly workspaceRoot: string
  readonly routing: Readonly<Record<string, ObservationRoute>>
  readonly activeRunCount: number
  readonly recentRunCount: number
  readonly discovery: {
    readonly status: 'unknown' | 'ok' | 'error' | 'backoff'
    readonly candidateCount: number
    readonly checkedAt?: string
    readonly retryAt?: string
  }
}

export interface ObservationAdapter {
  readonly id: string
  readonly status: 'unknown' | 'ok' | 'error'
  readonly version?: string
  readonly checkedAt?: string
  readonly message?: string
  readonly capabilities?: HarnessCapabilities
}

export interface ObservationOverview {
  readonly attention: readonly ObservationAttention[]
  readonly fleets: readonly ObservationFleet[]
  readonly activeRuns: readonly ObservationRunSummary[]
  readonly recentRuns: readonly ObservationRunSummary[]
}
