import type { HarnessStage } from '../core/harness-adapter.js'

export const fleetStages: readonly HarnessStage[] = [
  'execution',
  'review',
  'correction',
  'resolution_analysis',
]

export interface FleetRouteConfig {
  readonly adapter: string
  readonly model: string
  readonly reasoning?: string
  readonly promptVersion: string
  readonly resultContractVersion: number
}

export type FleetRoutingConfig = Readonly<Record<HarnessStage, FleetRouteConfig>>

export interface FleetDefinitionConfig {
  readonly id: string
  readonly projectNumber: number
  readonly enabled: boolean
  readonly maxConcurrentRuns: number
  readonly workspaceRoot: string
  readonly routing: FleetRoutingConfig
  readonly credentials: {
    readonly git?: string
    readonly codeChange?: string
  }
  readonly workPlugin?: {
    readonly executable: string
    readonly args: readonly string[]
    readonly timeoutMs: number
  }
}

export interface FleetServiceConfig {
  readonly version: 1
  readonly service: {
    readonly pactline: {
      readonly server: string
      readonly tokenEnv: string
      readonly executable: string
    }
    readonly stateDirectory: string
    readonly pollIntervalMs: number
    readonly maxConcurrentRuns: number
    readonly shutdownDeadlineMs: number
    readonly http: {
      readonly address: '127.0.0.1' | '::1' | 'localhost'
      readonly port: number
    }
  }
  readonly fleets: Readonly<Record<string, FleetDefinitionConfig>>
}

export interface FleetConfigSnapshot {
  readonly sourcePath: string
  readonly revision: string
  readonly loadedAt: string
  readonly config: FleetServiceConfig
}

export interface FleetConfigLoadOptions {
  readonly knownAdapterIds?: readonly string[]
  readonly now?: () => Date
}
