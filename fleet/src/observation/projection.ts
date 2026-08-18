import { createHash } from 'node:crypto'
import type { FleetConfigSnapshot, FleetDefinitionConfig, FleetRouteConfig } from '../config/types.js'
import type { FleetServiceHealth } from '../health/model.js'
import { sanitizeHealthDiagnostic } from '../health/store.js'
import type {
  FleetExternalEffectRecord,
  FleetRunEventRecord,
  FleetRunListOptions,
  FleetRunRecord,
} from '../registry/fleet-registry.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import { isTerminalRunState } from '../run/run.js'
import { RUN_STATES } from '../run/run.js'
import type { FleetRunState } from '../run/run.js'
import type {
  ObservationAdapter,
  ObservationAttention,
  ObservationEffect,
  ObservationEnvelope,
  ObservationFleet,
  ObservationList,
  ObservationOverview,
  ObservationRoute,
  ObservationRunDetail,
  ObservationRunSummary,
  ObservationTimelineItem,
} from './model.js'
import { OBSERVATION_EFFECT_PROJECTION } from './model.js'

function object(value: unknown): Readonly<Record<string, unknown>> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Readonly<Record<string, unknown>>
    : undefined
}

function text(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? sanitizeHealthDiagnostic(value) : undefined
}

function route(value: FleetRouteConfig): ObservationRoute {
  return {
    adapter: value.adapter,
    model: value.model,
    ...(value.reasoning === undefined ? {} : { reasoning: value.reasoning }),
  }
}

function frozenRoute(run: FleetRunRecord): ObservationRoute | undefined {
  const value = object(run.frozenPolicy?.route)
  const adapter = text(value?.adapter)
  const model = text(value?.model)
  if (adapter === undefined || model === undefined) return undefined
  const reasoning = text(value?.reasoning)
  return { adapter, model, ...(reasoning === undefined ? {} : { reasoning }) }
}

function runSummary(run: FleetRunRecord): ObservationRunSummary {
  const selectedRoute = frozenRoute(run)
  return {
    runId: run.runId,
    fleetId: run.fleetId,
    projectNumber: run.projectNumber,
    ...(run.taskNumber === undefined ? {} : { taskNumber: run.taskNumber }),
    ...(run.stage === undefined ? {} : { stage: run.stage }),
    state: run.state,
    ...(selectedRoute === undefined ? {} : selectedRoute),
    ...(run.checkpoint === undefined ? {} : { checkpoint: text(run.checkpoint) ?? 'redacted' }),
    ...(run.disposition === undefined ? {} : { disposition: text(run.disposition) ?? 'redacted' }),
    ...(run.error === undefined ? {} : { error: text(run.error) ?? 'redacted' }),
    createdAt: run.createdAt,
    updatedAt: run.updatedAt,
  }
}

function safeWorkspace(value: Readonly<Record<string, unknown>> | undefined): Readonly<Record<string, string>> | undefined {
  if (value === undefined) return undefined
  const result: Record<string, string> = {}
  for (const key of ['mode', 'root', 'repositoryPath', 'baseRevision', 'branch'] as const) {
    const selected = text(value[key])
    if (selected !== undefined) result[key] = selected
  }
  return Object.keys(result).length === 0 ? undefined : result
}

function safeExternalURL(value: string): string | undefined {
  try {
    const parsed = new URL(value)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return undefined
    parsed.username = ''
    parsed.password = ''
    parsed.search = ''
    parsed.hash = ''
    return parsed.toString()
  } catch { return undefined }
}

function timeline(event: FleetRunEventRecord): ObservationTimelineItem {
  const from = text(event.payload.from)
  const to = text(event.payload.to) as FleetRunState | undefined
  const checkpoint = text(event.payload.checkpoint)
  if (event.eventType === 'run.admitted') {
    return { sequence: event.sequence, at: event.createdAt, kind: event.eventType, title: 'Run admitted', state: 'admitted' }
  }
  if (event.eventType === 'run.transitioned' && to !== undefined) {
    return {
      sequence: event.sequence,
      at: event.createdAt,
      kind: event.eventType,
      title: `Run entered ${to.replaceAll('_', ' ')}`,
      ...(from === undefined ? {} : { detail: `From ${from.replaceAll('_', ' ')}` }),
      state: to,
      ...(checkpoint === undefined ? {} : { checkpoint }),
    }
  }
  if (event.eventType === 'run.recovery_decided') {
    const decision = text(event.payload.decision) ?? 'unknown'
    const authority = text(event.payload.claimAuthority) ?? 'unknown'
    const state = text(event.payload.state) as FleetRunState | undefined
    return {
      sequence: event.sequence,
      at: event.createdAt,
      kind: event.eventType,
      title: `Recovery chose ${decision.replaceAll('_', ' ')}`,
      detail: `Claim authority: ${authority.replaceAll('_', ' ')}`,
      ...(state === undefined ? {} : { state }),
    }
  }
  return { sequence: event.sequence, at: event.createdAt, kind: event.eventType, title: 'Run event recorded' }
}

function safeEffectDetail(effect: FleetExternalEffectRecord): Readonly<Record<string, string | number | boolean>> | undefined {
  const source = object(effect.status === 'observed' ? effect.observation : effect.intent)
  if (source === undefined) return undefined
  const result: Record<string, string | number | boolean> = {}
  for (const key of OBSERVATION_EFFECT_PROJECTION[effect.kind].safeFields) {
    const value = source[key]
    if (typeof value === 'string' && key.toLowerCase().endsWith('url')) {
      const safe = safeExternalURL(value)
      if (safe !== undefined) result[key] = safe
    } else if (typeof value === 'string') result[key] = sanitizeHealthDiagnostic(value)
    else if (typeof value === 'number' || typeof value === 'boolean') result[key] = value
  }
  return Object.keys(result).length === 0 ? undefined : result
}

function effect(value: FleetExternalEffectRecord): ObservationEffect {
  const detail = safeEffectDetail(value)
  return {
    kind: value.kind,
    status: value.status,
    title: OBSERVATION_EFFECT_PROJECTION[value.kind].title,
    ...(detail === undefined ? {} : { detail }),
    updatedAt: value.updatedAt,
  }
}

function fleetProjection(
  fleet: FleetDefinitionConfig,
  health: FleetServiceHealth,
  runs: readonly FleetRunRecord[],
): ObservationFleet {
  const healthValue = health.fleets.find(value => value.id === fleet.id)
  return {
    id: fleet.id,
    projectNumber: fleet.projectNumber,
    enabled: fleet.enabled,
    status: healthValue?.status ?? (fleet.enabled ? 'degraded' : 'disabled'),
    ...(healthValue?.message === undefined ? {} : { message: sanitizeHealthDiagnostic(healthValue.message) }),
    maxConcurrentRuns: fleet.maxConcurrentRuns,
    workPluginConfigured: fleet.workPlugin !== undefined,
    workspaceRoot: fleet.workspaceRoot,
    routing: Object.fromEntries(Object.entries(fleet.routing).map(([stage, value]) => [stage, route(value)])),
    activeRunCount: runs.filter(run => run.fleetId === fleet.id && !isTerminalRunState(run.state)).length,
    recentRunCount: runs.filter(run => run.fleetId === fleet.id && isTerminalRunState(run.state)).length,
    discovery: healthValue?.discovery ?? { status: 'unknown', candidateCount: 0 },
  }
}

export interface FleetObservationSource {
  service(): ObservationEnvelope<FleetServiceHealth>
  overview(): ObservationEnvelope<ObservationOverview>
  fleets(): ObservationEnvelope<ObservationList<ObservationFleet>>
  fleet(id: string): ObservationEnvelope<ObservationFleet> | undefined
  runs(options?: FleetRunListOptions): ObservationEnvelope<ObservationList<ObservationRunSummary>>
  run(id: string): ObservationEnvelope<ObservationRunDetail> | undefined
  adapters(): ObservationEnvelope<ObservationList<ObservationAdapter>>
  revision(): string
  metrics(): string
}

export class FleetObservationProjector implements FleetObservationSource {
  constructor(
    private readonly health: () => FleetServiceHealth,
    private readonly snapshot: () => FleetConfigSnapshot,
    private readonly registry: FleetRegistry,
    private readonly now: () => Date = () => new Date(),
  ) {}

  service(): ObservationEnvelope<FleetServiceHealth> { return this.envelope(this.health()) }

  overview(): ObservationEnvelope<ObservationOverview> {
    const runs = this.registry.listRuns({ limit: 100 })
    const activeRuns = runs.filter(run => !isTerminalRunState(run.state)).map(runSummary)
    const recentRuns = runs.filter(run => isTerminalRunState(run.state)).slice(0, 12).map(runSummary)
    return this.envelope({
      attention: this.attention(runs),
      fleets: this.fleetValues(runs),
      activeRuns,
      recentRuns,
    })
  }

  fleets(): ObservationEnvelope<ObservationList<ObservationFleet>> {
    return this.envelope({ items: this.fleetValues(this.registry.listRuns({ limit: 200 })) })
  }

  fleet(id: string): ObservationEnvelope<ObservationFleet> | undefined {
    const fleet = this.snapshot().config.fleets[id]
    if (fleet === undefined) return undefined
    return this.envelope(fleetProjection(fleet, this.health(), this.registry.listRuns({ fleetId: id, limit: 200 })))
  }

  runs(options: FleetRunListOptions = {}): ObservationEnvelope<ObservationList<ObservationRunSummary>> {
    const limit = options.limit ?? 50
    const records = this.registry.listRuns({ ...options, limit: limit + 1 })
    const hasMore = records.length > limit
    const selected = records.slice(0, limit)
    return this.envelope({
      items: selected.map(runSummary),
      ...(hasMore && selected.at(-1) !== undefined ? { nextBefore: selected.at(-1)!.updatedAt } : {}),
    })
  }

  run(id: string): ObservationEnvelope<ObservationRunDetail> | undefined {
    const value = this.registry.getRun(id)
    if (value === undefined) return undefined
    return this.envelope({
      ...runSummary(value),
      serviceId: value.serviceId,
      configRevision: value.configRevision,
      ...(value.taskVersion === undefined ? {} : { taskVersion: value.taskVersion }),
      ...(value.claimId === undefined ? {} : { claimId: value.claimId }),
      ...(value.claimVersion === undefined ? {} : { claimVersion: value.claimVersion }),
      ...(value.claimTaskVersion === undefined ? {} : { claimTaskVersion: value.claimTaskVersion }),
      ...(value.runtimeSessionId === undefined ? {} : { runtimeSessionId: value.runtimeSessionId }),
      ...(safeWorkspace(value.workspace) === undefined ? {} : { workspace: safeWorkspace(value.workspace)! }),
      timeline: this.registry.listRunEvents(id, 200)
        .filter(event => ['run.admitted', 'run.transitioned', 'run.recovery_decided'].includes(event.eventType))
        .map(timeline),
      effects: this.registry.listEffects(id).map(effect),
    })
  }

  adapters(): ObservationEnvelope<ObservationList<ObservationAdapter>> {
    return this.envelope({ items: this.health().adapters.map(value => ({ ...value })) })
  }

  revision(): string {
    const health = this.health()
    const latest = this.registry.listRuns({ limit: 1 })[0]?.updatedAt ?? ''
    return createHash('sha256').update(`${health.updatedAt}\0${latest}`).digest('hex').slice(0, 16)
  }

  metrics(): string {
    const health = this.health()
    const runs = this.registry.listRuns({ limit: 200 })
    const lines = [
      '# HELP pactline_fleet_ready Whether the Fleet Service is ready.',
      '# TYPE pactline_fleet_ready gauge',
      `pactline_fleet_ready ${health.ready ? '1' : '0'}`,
      '# HELP pactline_fleet_runs Number of locally retained Runs by state.',
      '# TYPE pactline_fleet_runs gauge',
    ]
    for (const state of RUN_STATES) {
      lines.push(`pactline_fleet_runs{state="${state}"} ${String(runs.filter(run => run.state === state).length)}`)
    }
    lines.push('# HELP pactline_fleet_adapter_up Whether a configured Adapter probe succeeds.')
    lines.push('# TYPE pactline_fleet_adapter_up gauge')
    for (const adapter of health.adapters) lines.push(`pactline_fleet_adapter_up{adapter="${adapter.id.replaceAll('"', '')}"} ${adapter.status === 'ok' ? '1' : '0'}`)
    return `${lines.join('\n')}\n`
  }

  private envelope<T>(data: T): ObservationEnvelope<T> {
    return { ok: true, data, meta: { generatedAt: this.now().toISOString(), revision: this.revision() } }
  }

  private fleetValues(runs: readonly FleetRunRecord[]): ObservationFleet[] {
    return Object.values(this.snapshot().config.fleets)
      .sort((first, second) => first.id.localeCompare(second.id))
      .map(fleet => fleetProjection(fleet, this.health(), runs))
  }

  private attention(runs: readonly FleetRunRecord[]): ObservationAttention[] {
    const health = this.health()
    const values: ObservationAttention[] = []
    if (health.config.lastReloadError !== undefined) values.push({
      id: 'config-reload', scope: 'service', severity: 'warning', title: 'Configuration reload rejected',
      detail: sanitizeHealthDiagnostic(health.config.lastReloadError),
      ...(health.config.lastReloadAt === undefined ? {} : { checkedAt: health.config.lastReloadAt }),
    })
    if (health.registry.status === 'error') values.push({
      id: 'registry', scope: 'service', severity: 'critical', title: 'Registry unavailable',
      detail: health.registry.message ?? 'The local registry health check failed.',
      ...(health.registry.checkedAt === undefined ? {} : { checkedAt: health.registry.checkedAt }),
    })
    if (health.pactline.status === 'error') values.push({
      id: 'pactline', scope: 'service', severity: 'critical', title: 'Pactline unavailable',
      detail: health.pactline.message ?? 'The Pactline preflight check failed.',
      ...(health.pactline.checkedAt === undefined ? {} : { checkedAt: health.pactline.checkedAt }),
    })
    for (const adapter of health.adapters.filter(value => value.status === 'error')) values.push({
      id: `adapter-${adapter.id}`, scope: 'adapter', severity: 'warning', title: `${adapter.id} Adapter unavailable`,
      detail: adapter.message ?? 'The capability probe failed.',
      ...(adapter.checkedAt === undefined ? {} : { checkedAt: adapter.checkedAt }),
    })
    for (const run of runs.filter(value => value.state === 'quarantined' || value.state === 'failed').slice(0, 5)) values.push({
      id: `run-${run.runId}`, scope: 'run', severity: run.state === 'failed' ? 'critical' : 'warning',
      title: `Run ${run.state}`, detail: run.error ?? run.disposition ?? 'Inspect the recovery timeline.',
      fleetId: run.fleetId, runId: run.runId, checkedAt: run.updatedAt,
    })
    return values
  }
}
