import type { HarnessCapabilities } from '../core/harness-adapter.js'
import type { FleetConfigSnapshot } from '../config/types.js'
import type {
  AdapterHealth,
  DependencyHealth,
  FleetHealth,
  FleetServiceHealth,
  FleetServiceMode,
  PactlineHealth,
} from './model.js'
import type { FleetDiscoveryCycle } from '../scheduler/fair-scheduler.js'

const MAX_DIAGNOSTIC_LENGTH = 2_048

export function sanitizeHealthDiagnostic(value: string): string {
  return value
    .slice(0, MAX_DIAGNOSTIC_LENGTH)
    .replace(/\bBearer\s+\S+/gi, 'Bearer [REDACTED]')
    .replace(/\bsk-[A-Za-z0-9_-]{8,}\b/g, '[REDACTED]')
    .replace(/\b(token|api[_-]?key|secret|password|credential)(\s*[=:]\s*)\S+/gi, '$1$2[REDACTED]')
}

export class FleetHealthStore {
  private modeValue: FleetServiceMode = 'starting'
  private updatedAtValue: string
  private configSnapshot: FleetConfigSnapshot
  private reloadAt: string | undefined
  private reloadError: string | undefined
  private registryValue: DependencyHealth & { path: string; schemaVersion: number; nonTerminalRuns: number }
  private pactlineValue: PactlineHealth
  private readonly adapterValues = new Map<string, AdapterHealth>()
  private readonly discoveryValues = new Map<string, FleetDiscoveryCycle>()

  constructor(
    private readonly serviceId: string,
    private readonly serviceVersion: string,
    snapshot: FleetConfigSnapshot,
    registryPath: string,
    private readonly startedAt: string,
    private readonly now: () => Date = () => new Date(),
  ) {
    this.updatedAtValue = startedAt
    this.configSnapshot = snapshot
    this.registryValue = {
      status: 'unknown',
      path: registryPath,
      schemaVersion: 3,
      nonTerminalRuns: 0,
    }
    this.pactlineValue = {
      status: 'unknown',
      server: snapshot.config.service.pactline.server,
    }
  }

  setMode(mode: FleetServiceMode): void {
    this.modeValue = mode
    this.touch()
  }

  setRegistry(status: 'ok' | 'error', nonTerminalRuns: number, message?: string): void {
    this.registryValue = {
      ...this.registryValue,
      status,
      checkedAt: this.timestamp(),
      nonTerminalRuns,
      ...(message === undefined ? {} : { message: sanitizeHealthDiagnostic(message) }),
    }
  }

  setPactline(result: {
    readonly status: 'ok' | 'error'
    readonly cliVersion?: string
    readonly protocol?: number
    readonly featureCount?: number
    readonly message?: string
  }): void {
    this.pactlineValue = {
      status: result.status,
      server: this.configSnapshot.config.service.pactline.server,
      checkedAt: this.timestamp(),
      ...(result.cliVersion === undefined ? {} : { cliVersion: result.cliVersion }),
      ...(result.protocol === undefined ? {} : { protocol: result.protocol }),
      ...(result.featureCount === undefined ? {} : { featureCount: result.featureCount }),
      ...(result.message === undefined ? {} : { message: sanitizeHealthDiagnostic(result.message) }),
    }
  }

  setAdapter(result: {
    readonly id: string
    readonly status: 'ok' | 'error'
    readonly version?: string
    readonly capabilities?: HarnessCapabilities
    readonly message?: string
  }): void {
    this.adapterValues.set(result.id, {
      id: result.id,
      status: result.status,
      checkedAt: this.timestamp(),
      ...(result.version === undefined ? {} : { version: result.version }),
      ...(result.capabilities === undefined ? {} : { capabilities: result.capabilities }),
      ...(result.message === undefined ? {} : { message: sanitizeHealthDiagnostic(result.message) }),
    })
  }

  resetAdapters(): void {
    this.adapterValues.clear()
    this.touch()
  }

  replaceAdapters(results: readonly {
    readonly id: string
    readonly status: 'ok' | 'error'
    readonly version?: string
    readonly capabilities?: HarnessCapabilities
    readonly message?: string
  }[]): void {
    this.adapterValues.clear()
    for (const result of results) this.setAdapter(result)
    this.touch()
  }

  setFleetDiscovery(results: readonly FleetDiscoveryCycle[]): void {
    for (const result of results) this.discoveryValues.set(result.fleetId, result)
    this.touch()
  }

  applyConfiguration(snapshot: FleetConfigSnapshot): void {
    this.configSnapshot = snapshot
    this.reloadAt = this.timestamp()
    this.reloadError = undefined
    this.touch()
  }

  rejectReload(message: string): void {
    this.reloadAt = this.timestamp()
    this.reloadError = sanitizeHealthDiagnostic(message)
    this.touch()
  }

  settleMode(): void {
    if (this.modeValue === 'draining' || this.modeValue === 'stopped') return
    const registryReady = this.registryValue.status === 'ok'
    const pactlineReady = this.pactlineValue.status === 'ok'
    const fleetReady = this.fleets().some(fleet => fleet.enabled && fleet.status === 'healthy')
    this.modeValue = registryReady && pactlineReady && fleetReady ? 'ready' : 'degraded'
    this.touch()
  }

  snapshot(): FleetServiceHealth {
    const adapters = [...this.adapterValues.values()].sort((first, second) => first.id.localeCompare(second.id))
    return {
      serviceId: this.serviceId,
      version: this.serviceVersion,
      mode: this.modeValue,
      live: this.modeValue !== 'stopped',
      ready: this.modeValue === 'ready',
      startedAt: this.startedAt,
      updatedAt: this.updatedAtValue,
      config: {
        revision: this.configSnapshot.revision,
        loadedAt: this.configSnapshot.loadedAt,
        ...(this.reloadAt === undefined ? {} : { lastReloadAt: this.reloadAt }),
        ...(this.reloadError === undefined ? {} : { lastReloadError: this.reloadError }),
      },
      registry: { ...this.registryValue },
      pactline: { ...this.pactlineValue },
      adapters,
      fleets: this.fleets(),
    }
  }

  private fleets(): FleetHealth[] {
    return Object.values(this.configSnapshot.config.fleets)
      .sort((first, second) => first.id.localeCompare(second.id))
      .map(fleet => {
        const adapters = [...new Set(Object.values(fleet.routing).map(route => route.adapter))].sort()
        if (!fleet.enabled) {
          return {
            id: fleet.id,
            projectNumber: fleet.projectNumber,
            enabled: false,
            status: 'disabled',
            adapters,
            discovery: { status: 'unknown', candidateCount: 0 },
          }
        }
        const unavailable = adapters.filter(adapter => this.adapterValues.get(adapter)?.status !== 'ok')
        return {
          id: fleet.id,
          projectNumber: fleet.projectNumber,
          enabled: true,
          status: unavailable.length === 0 ? 'healthy' : 'degraded',
          adapters,
          discovery: this.discovery(fleet.id),
          ...(unavailable.length === 0 ? {} : { message: `Unavailable Adapter: ${unavailable.join(', ')}` }),
        }
      })
  }

  private discovery(fleetId: string): FleetHealth['discovery'] {
    const value = this.discoveryValues.get(fleetId)
    if (value === undefined) return { status: 'unknown', candidateCount: 0 }
    return {
      status: value.status,
      candidateCount: value.candidateCount,
      checkedAt: value.checkedAt,
      ...(value.retryAt === undefined ? {} : { retryAt: value.retryAt }),
    }
  }

  private timestamp(): string {
    const value = this.now().toISOString()
    this.updatedAtValue = value
    return value
  }

  private touch(): void {
    this.updatedAtValue = this.now().toISOString()
  }
}
