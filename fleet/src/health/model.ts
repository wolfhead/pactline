import type { HarnessCapabilities } from '../core/harness-adapter.js'

export type FleetServiceMode = 'starting' | 'checking' | 'ready' | 'degraded' | 'draining' | 'stopped'
export type HealthCheckStatus = 'unknown' | 'ok' | 'error'
export type FleetOperationalStatus = 'healthy' | 'degraded' | 'disabled'

export interface DependencyHealth {
  readonly status: HealthCheckStatus
  readonly checkedAt?: string
  readonly message?: string
}

export interface PactlineHealth extends DependencyHealth {
  readonly server: string
  readonly cliVersion?: string
  readonly protocol?: number
  readonly featureCount?: number
}

export interface AdapterHealth extends DependencyHealth {
  readonly id: string
  readonly version?: string
  readonly capabilities?: HarnessCapabilities
}

export interface FleetHealth {
  readonly id: string
  readonly projectNumber: number
  readonly enabled: boolean
  readonly status: FleetOperationalStatus
  readonly adapters: readonly string[]
  readonly message?: string
}

export interface FleetServiceHealth {
  readonly serviceId: string
  readonly version: string
  readonly mode: FleetServiceMode
  readonly live: boolean
  readonly ready: boolean
  readonly startedAt: string
  readonly updatedAt: string
  readonly config: {
    readonly revision: string
    readonly loadedAt: string
    readonly lastReloadAt?: string
    readonly lastReloadError?: string
  }
  readonly registry: DependencyHealth & {
    readonly path: string
    readonly schemaVersion: number
    readonly nonTerminalRuns: number
  }
  readonly pactline: PactlineHealth
  readonly adapters: readonly AdapterHealth[]
  readonly fleets: readonly FleetHealth[]
}
