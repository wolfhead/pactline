import type {
  HarnessAdapter,
  HarnessCapabilities,
  HarnessProbeRequest,
  HarnessSandbox,
  HarnessStage,
} from './harness-adapter.js'

export interface RuntimeRoute {
  readonly adapterId: string
  readonly model: string
  readonly reasoning?: string
  readonly promptVersion: string
  readonly resultContractVersion: number
}

export type RuntimeRoutes = Readonly<Record<HarnessStage, RuntimeRoute>>

export interface AdmittedRuntime {
  readonly adapter: HarnessAdapter
  readonly capabilities: HarnessCapabilities
  readonly route: RuntimeRoute
  readonly probe: HarnessProbeRequest
}

export class RuntimeAdmissionError extends Error {
  readonly code: 'ADAPTER_NOT_FOUND' | 'CAPABILITY_MISSING' | 'INVALID_ROUTE'
  readonly adapterId: string
  readonly capability?: string

  constructor(
    code: RuntimeAdmissionError['code'],
    adapterId: string,
    message: string,
    capability?: string,
  ) {
    super(message)
    this.name = 'RuntimeAdmissionError'
    this.code = code
    this.adapterId = adapterId
    if (capability !== undefined) this.capability = capability
  }
}

export function requiredSandbox(stage: HarnessStage): HarnessSandbox {
  // Review tools need scratch/build writes even though the repository tree is
  // immutable. Claim-stage verification independently rejects every review
  // workspace mutation before settlement.
  return stage === 'resolution_analysis' ? 'read_only' : 'workspace_write'
}

export function requiredCapabilities(stage: HarnessStage): HarnessProbeRequest {
  return {
    requiredStages: [stage],
    requiredSandbox: requiredSandbox(stage),
    requireNativeTools: stage === 'execution' || stage === 'correction',
    requireStructuredResult: true,
    requireEventStream: true,
    requireCancellation: true,
    requireSessionResume: false,
  }
}

function nonEmpty(value: string, name: string, adapterId: string): void {
  if (value.trim() === '') throw new RuntimeAdmissionError('INVALID_ROUTE', adapterId, `${name} must be non-empty`)
}

function assertCapabilities(
  adapter: HarnessAdapter,
  capabilities: HarnessCapabilities,
  probe: HarnessProbeRequest,
): void {
  const missing: string[] = []
  for (const stage of probe.requiredStages) {
    if (!capabilities.supportedStages.includes(stage)) missing.push(`stage:${stage}`)
  }
  if (!capabilities.sandboxModes.includes(probe.requiredSandbox)) missing.push(`sandbox:${probe.requiredSandbox}`)
  if (probe.requireNativeTools && !capabilities.nativeTools) missing.push('nativeTools')
  if (probe.requireStructuredResult && !capabilities.structuredResult) missing.push('structuredResult')
  if (probe.requireCancellation && !capabilities.cancellation) missing.push('cancellation')
  if (probe.requireSessionResume && !capabilities.sessionResume) missing.push('sessionResume')
  if (probe.requireEventStream && !capabilities.eventStream) missing.push('eventStream')
  if (missing.length > 0) {
    throw new RuntimeAdmissionError(
      'CAPABILITY_MISSING',
      adapter.id,
      `Harness Adapter ${adapter.id} is missing required capability: ${missing.join(', ')}`,
      missing.join(','),
    )
  }
}

/** Deterministic, configuration-only Harness selection with no fallback path. */
export class StaticRuntimeRouter {
  private readonly adapters: ReadonlyMap<string, HarnessAdapter>

  constructor(
    adapters: readonly HarnessAdapter[],
    private readonly routes: RuntimeRoutes,
  ) {
    const indexed = new Map<string, HarnessAdapter>()
    for (const adapter of adapters) {
      nonEmpty(adapter.id, 'adapter.id', adapter.id)
      nonEmpty(adapter.version, 'adapter.version', adapter.id)
      if (indexed.has(adapter.id)) throw new RuntimeAdmissionError('INVALID_ROUTE', adapter.id, `Duplicate Harness Adapter ID: ${adapter.id}`)
      indexed.set(adapter.id, adapter)
    }
    this.adapters = indexed
  }

  routeFor(stage: HarnessStage): RuntimeRoute {
    const route = this.routes[stage]
    nonEmpty(route.adapterId, `runtime.${stage}.adapterId`, route.adapterId)
    nonEmpty(route.model, `runtime.${stage}.model`, route.adapterId)
    nonEmpty(route.promptVersion, `runtime.${stage}.promptVersion`, route.adapterId)
    if (!Number.isSafeInteger(route.resultContractVersion) || route.resultContractVersion < 1) {
      throw new RuntimeAdmissionError('INVALID_ROUTE', route.adapterId, `runtime.${stage}.resultContractVersion must be positive`)
    }
    return route
  }

  /** Probe and admit the exact configured Adapter before any Claim mutation. */
  async admit(stage: HarnessStage): Promise<AdmittedRuntime> {
    const route = this.routeFor(stage)
    const adapter = this.adapters.get(route.adapterId)
    if (adapter === undefined) {
      throw new RuntimeAdmissionError('ADAPTER_NOT_FOUND', route.adapterId, `Configured Harness Adapter is unavailable: ${route.adapterId}`)
    }
    const probe = requiredCapabilities(stage)
    const capabilities = await adapter.probe(probe)
    assertCapabilities(adapter, capabilities, probe)
    return { adapter, capabilities, route, probe }
  }
}
