import { join } from 'node:path'
import type { HarnessAdapter, HarnessCapabilities, HarnessProbeRequest, HarnessSandbox, HarnessStage } from '../core/harness-adapter.js'
import type { FleetConfigReloadResult } from '../config/manager.js'
import { FleetConfigManager } from '../config/manager.js'
import type { FleetConfigSnapshot, FleetServiceConfig } from '../config/types.js'
import { fleetStages } from '../config/types.js'
import { CodexHarnessAdapter } from '../adapters/codex/codex-adapter.js'
import { DeepSeekHarnessAdapter } from '../adapters/deepseek/deepseek-adapter.js'
import { FleetHealthStore, sanitizeHealthDiagnostic } from '../health/store.js'
import type { FleetServiceHealth } from '../health/model.js'
import { FleetHealthServer, type FleetHealthServerAddress } from '../http/health-server.js'
import { fleetWebAssetsPath } from '../http/static-assets.js'
import { FleetObservationProjector } from '../observation/projection.js'
import { PactlineCLI } from '../pactline/client.js'
import type { PactlinePreflightResult } from '../pactline/types.js'
import { FleetRegistry } from '../registry/fleet-registry.js'
import { FairFleetScheduler, type FleetSchedulerCycle } from '../scheduler/fair-scheduler.js'
import { FleetRunReconciler } from '../recovery/reconciler.js'
import { ClaimStageRunCoordinator, type FleetFaultInjector } from '../scheduler/run-coordinator.js'
import { ExecutableWorkDefinitionResolver } from '../work-plugin/executable-plugin.js'
import { assertWorkPluginExecutable } from '../work-plugin/executable-plugin.js'
import { PluginRunMaterializer } from '../work-plugin/materializer.js'
import { FLEET_VERSION } from '../version.js'
import { ensurePrivateDirectory } from './state-directory.js'
import { acquireFleetServiceLock, type FleetServiceLock } from './service-lock.js'
import { JSONFleetLogger, type FleetLogger } from './logger.js'

interface PactlinePreflightClient {
  preflight(options?: {
    readonly protocol?: number
    readonly verifyAuthentication?: boolean
    readonly sessionId?: string
    readonly signal?: AbortSignal
  }): Promise<PactlinePreflightResult>
}

export interface FleetSchedulingRuntime {
  readonly scheduler: Pick<FairFleetScheduler, 'cycle' | 'beginDrain' | 'waitForActive' | 'activeRunCount'>
  readonly reconciler: Pick<FleetRunReconciler, 'reconcile'>
}

export interface FleetServiceOptions {
  readonly adapters?: readonly HarnessAdapter[]
  readonly environment?: NodeJS.ProcessEnv
  readonly logger?: FleetLogger
  readonly now?: () => Date
  readonly configWatchIntervalMs?: number
  readonly createPactlineClient?: (
    snapshot: FleetConfigSnapshot,
    serviceId: string,
  ) => PactlinePreflightClient
  readonly openRegistry?: (path: string, now: () => Date) => Promise<FleetRegistry>
  readonly acquireLock?: (stateDirectory: string) => Promise<FleetServiceLock>
  readonly createHealthServer?: (health: () => FleetServiceHealth) => FleetHealthServer
  /** Harness-neutral integration boundary; omitted services retain M5.1 observation-only behavior. */
  readonly createSchedulingRuntime?: (context: {
    readonly snapshot: () => FleetConfigSnapshot
    readonly registry: FleetRegistry
    readonly pactline: PactlinePreflightClient
    readonly serviceId: string
    readonly logger: FleetLogger
  }) => FleetSchedulingRuntime
  readonly faultInjector?: FleetFaultInjector
  readonly webAssetsDirectory?: string
}

interface AdapterRequirement {
  readonly stages: Set<HarnessStage>
  readonly sandboxes: Set<HarnessSandbox>
  nativeTools: boolean
}

function aggregateAdapterRequirements(config: FleetServiceConfig): ReadonlyMap<string, AdapterRequirement> {
  const requirements = new Map<string, AdapterRequirement>()
  for (const fleet of Object.values(config.fleets)) {
    if (!fleet.enabled) continue
    for (const stage of fleetStages) {
      const route = fleet.routing[stage]
      const current = requirements.get(route.adapter) ?? {
        stages: new Set<HarnessStage>(),
        sandboxes: new Set<HarnessSandbox>(),
        nativeTools: false,
      }
      current.stages.add(stage)
      current.sandboxes.add(stage === 'resolution_analysis' ? 'read_only' : 'workspace_write')
      current.nativeTools ||= stage === 'execution' || stage === 'correction'
      requirements.set(route.adapter, current)
    }
  }
  return requirements
}

function probeRequest(requirement: AdapterRequirement): HarnessProbeRequest {
  return {
    requiredStages: [...requirement.stages],
    requiredSandbox: requirement.sandboxes.has('workspace_write') ? 'workspace_write' : 'read_only',
    requireNativeTools: requirement.nativeTools,
    requireStructuredResult: true,
    requireEventStream: true,
    requireCancellation: true,
    requireSessionResume: false,
  }
}

function validateCapabilities(
  adapterId: string,
  capabilities: HarnessCapabilities,
  requirement: AdapterRequirement,
): void {
  const missing: string[] = []
  for (const stage of requirement.stages) {
    if (!capabilities.supportedStages.includes(stage)) missing.push(`stage:${stage}`)
  }
  for (const sandbox of requirement.sandboxes) {
    if (!capabilities.sandboxModes.includes(sandbox)) missing.push(`sandbox:${sandbox}`)
  }
  if (requirement.nativeTools && !capabilities.nativeTools) missing.push('nativeTools')
  if (!capabilities.structuredResult) missing.push('structuredResult')
  if (!capabilities.eventStream) missing.push('eventStream')
  if (!capabilities.cancellation) missing.push('cancellation')
  if (missing.length > 0) {
    throw new Error(`Harness Adapter ${adapterId} is missing: ${missing.join(', ')}`)
  }
}

function message(error: unknown): string {
  return sanitizeHealthDiagnostic(error instanceof Error ? error.message : String(error))
}

export class FleetService {
  private readonly adapters: Map<string, HarnessAdapter>
  private readonly defaultAdapters: boolean
  private readonly environment: NodeJS.ProcessEnv
  private readonly logger: FleetLogger
  private readonly now: () => Date
  private readonly manager: FleetConfigManager
  private registry: FleetRegistry | undefined
  private lock: FleetServiceLock | undefined
  private healthStore: FleetHealthStore | undefined
  private healthServer: FleetHealthServer | undefined
  private healthProbeTimer: NodeJS.Timeout | undefined
  private schedulerTimer: NodeJS.Timeout | undefined
  private schedulingRuntime: FleetSchedulingRuntime | undefined
  private schedulingCycle: Promise<FleetSchedulerCycle> | undefined
  private started = false
  private stopping: Promise<void> | undefined
  private readonly stoppedPromise: Promise<void>
  private resolveStopped!: () => void

  constructor(
    readonly configPath: string,
    private readonly options: FleetServiceOptions = {},
  ) {
    const adapters = options.adapters ?? [
      new CodexHarnessAdapter(),
      new DeepSeekHarnessAdapter(),
    ]
    const indexed = new Map<string, HarnessAdapter>()
    for (const adapter of adapters) {
      if (indexed.has(adapter.id)) throw new Error(`Duplicate Harness Adapter ID: ${adapter.id}`)
      indexed.set(adapter.id, adapter)
    }
    this.adapters = indexed
    this.defaultAdapters = options.adapters === undefined
    this.environment = { ...(options.environment ?? process.env) }
    this.now = options.now ?? (() => new Date())
    this.logger = options.logger ?? new JSONFleetLogger(process.stderr, this.now)
    this.manager = new FleetConfigManager(configPath, {
      knownAdapterIds: [...this.adapters.keys()],
      ...(options.configWatchIntervalMs === undefined ? {} : { watchIntervalMs: options.configWatchIntervalMs }),
      now: this.now,
    })
    this.stoppedPromise = new Promise(resolvePromise => { this.resolveStopped = resolvePromise })
  }

  get health(): FleetServiceHealth {
    if (this.healthStore === undefined) throw new Error('Fleet Service has not initialized health')
    return this.healthStore.snapshot()
  }

  get address(): FleetHealthServerAddress {
    if (this.healthServer === undefined) throw new Error('Fleet Service health endpoint is not started')
    return this.healthServer.address
  }

  async start(): Promise<FleetHealthServerAddress> {
    if (this.started) throw new Error('Fleet Service is already started')
    this.started = true
    let snapshot: FleetConfigSnapshot | undefined
    try {
      snapshot = await this.manager.loadInitial()
      this.configureDefaultAdapters(snapshot)
      const stateDirectory = await this.prepareDirectories(snapshot)
      this.lock = await (this.options.acquireLock ?? acquireFleetServiceLock)(stateDirectory)
      this.registry = await (this.options.openRegistry ?? FleetRegistry.open)(
        join(stateDirectory, 'fleet.sqlite3'),
        this.now,
      )
      this.registry.recordConfiguration(snapshot)
      const startedAt = this.now().toISOString()
      this.healthStore = new FleetHealthStore(
        this.registry.serviceId,
        FLEET_VERSION,
        snapshot,
        this.registry.path,
        startedAt,
        this.now,
      )
      this.healthStore.setMode('checking')
      this.healthStore.setRegistry('ok', this.registry.listNonTerminalRuns().length)
      await Promise.all([
        this.checkPactline(snapshot),
        this.checkAdapters(snapshot),
      ])
      await this.ensureSchedulingRuntime(snapshot)
      this.healthStore.settleMode()
      this.healthServer = this.options.createHealthServer !== undefined
        ? this.options.createHealthServer(() => this.health)
        : new FleetHealthServer(() => this.health, {
            observation: new FleetObservationProjector(
              () => this.health,
              () => this.manager.snapshot,
              this.registry,
              this.now,
            ),
            staticDirectory: this.options.webAssetsDirectory ?? fleetWebAssetsPath(),
          })
      const address = await this.healthServer.start(
        snapshot.config.service.http.address,
        snapshot.config.service.http.port,
      )
      this.manager.watch(
        result => { void this.applyReloadResult(result) },
        async candidate => {
          await this.prepareDirectories(candidate)
          this.registry!.recordConfiguration(candidate)
        },
      )
      this.scheduleHealthProbe()
      this.scheduleWorkCycle()
      this.logger.log('info', 'fleet.service.started', {
        serviceId: this.registry.serviceId,
        configRevision: snapshot.revision,
        fleetCount: Object.keys(snapshot.config.fleets).length,
        ready: this.health.ready,
        address: address.url,
      })
      return address
    } catch (error) {
      this.logger.log('error', 'fleet.service.start_failed', {
        ...(snapshot === undefined ? {} : { configRevision: snapshot.revision }),
        error: message(error),
      })
      await this.cleanupAfterFailedStart()
      throw error
    }
  }

  async reload(): Promise<FleetConfigReloadResult> {
    if (!this.started || this.stopping !== undefined) throw new Error('Fleet Service is not accepting reloads')
    const result = await this.manager.reload(async candidate => {
      await this.prepareDirectories(candidate)
      this.registry!.recordConfiguration(candidate)
    })
    await this.applyReloadResult(result)
    return result
  }

  async stop(reason = 'operator request'): Promise<void> {
    this.stopping ??= this.stopOwnedResources(reason)
    return await this.stopping
  }

  async waitUntilStopped(): Promise<void> {
    await this.stoppedPromise
  }

  /** Run one finite discovery/admission cycle and wait for every admitted Run. */
  async runOnce(): Promise<FleetSchedulerCycle> {
    return await this.runSchedulingCycle(true)
  }

  private async runSchedulingCycle(waitForAdmitted: boolean): Promise<FleetSchedulerCycle> {
    if (!this.started || this.stopping !== undefined) throw new Error('Fleet Service is not accepting scheduler cycles')
    if (this.schedulingRuntime === undefined) throw new Error('Fleet Service has no scheduling runtime')
    if (this.schedulingCycle !== undefined) return await this.schedulingCycle
    this.schedulingCycle = this.schedulingRuntime.scheduler.cycle(undefined, waitForAdmitted)
    try {
      const result = await this.schedulingCycle
      if (this.registry !== undefined && this.healthStore !== undefined) {
        this.healthStore.setFleetDiscovery(result.fleets)
        this.healthStore.setRegistry('ok', this.registry.listNonTerminalRuns().length)
      }
      return result
    } finally {
      this.schedulingCycle = undefined
    }
  }

  private async applyReloadResult(result: FleetConfigReloadResult): Promise<void> {
    if (this.stopping !== undefined || this.healthStore === undefined) return
    if (!result.applied) {
      this.healthStore.rejectReload(result.error ?? 'Configuration reload was rejected')
      this.logger.log('warn', 'fleet.config.reload_rejected', {
        configRevision: result.snapshot.revision,
        error: result.error ?? 'unknown error',
      })
      return
    }
    this.healthStore.setMode('checking')
    this.healthStore.applyConfiguration(result.snapshot)
    await this.checkAdapters(result.snapshot)
    await this.ensureSchedulingRuntime(result.snapshot)
    this.healthStore.settleMode()
    this.scheduleHealthProbe()
    this.scheduleWorkCycle(0)
    this.logger.log('info', 'fleet.config.reloaded', {
      configRevision: result.snapshot.revision,
      fleetCount: Object.keys(result.snapshot.config.fleets).length,
      ready: this.health.ready,
    })
  }

  private async prepareDirectories(snapshot: FleetConfigSnapshot): Promise<string> {
    const stateDirectory = await ensurePrivateDirectory(snapshot.config.service.stateDirectory)
    await Promise.all([
      ensurePrivateDirectory(join(stateDirectory, 'runs')),
      ensurePrivateDirectory(join(stateDirectory, 'evidence')),
      ...Object.values(snapshot.config.fleets).map(fleet => ensurePrivateDirectory(fleet.workspaceRoot)),
      ...Object.values(snapshot.config.fleets)
        .filter(fleet => fleet.enabled && fleet.workPlugin !== undefined)
        .map(fleet => assertWorkPluginExecutable(fleet.workPlugin!.executable)),
    ])
    return stateDirectory
  }

  private async ensureSchedulingRuntime(snapshot: FleetConfigSnapshot): Promise<void> {
    if (this.registry === undefined || this.healthStore === undefined) return
    let created = false
    if (this.schedulingRuntime === undefined && this.options.createSchedulingRuntime !== undefined) {
      const pactline = this.pactlineClient(snapshot)
      this.schedulingRuntime = this.options.createSchedulingRuntime({
        snapshot: () => this.manager.snapshot,
        registry: this.registry,
        pactline,
        serviceId: this.registry.serviceId,
        logger: this.logger,
      })
      created = true
    } else if (this.schedulingRuntime === undefined
      && Object.values(snapshot.config.fleets).some(fleet => fleet.enabled && fleet.workPlugin !== undefined)) {
      const pactline = this.pactlineClient(snapshot)
      if (!(pactline instanceof PactlineCLI)) throw new Error('Default scheduling requires the production Pactline CLI client')
      const sessionId = `fleet-service-${this.registry.serviceId}`
      const materializer = new PluginRunMaterializer({
        adapters: () => [...this.adapters.values()],
        environment: this.environment,
        pactlineTokenEnv: () => this.manager.snapshot.config.service.pactline.tokenEnv,
        now: this.now,
        registry: this.registry,
        ...(this.options.faultInjector === undefined ? {} : { faultInjector: this.options.faultInjector }),
      })
      const executor = new ClaimStageRunCoordinator({
        registry: this.registry,
        client: pactline,
        materializer,
        clientSessionId: sessionId,
        ...(this.options.faultInjector === undefined ? {} : { faultInjector: this.options.faultInjector }),
      })
      this.schedulingRuntime = {
        scheduler: new FairFleetScheduler({
          snapshot: () => this.manager.snapshot,
          discovery: pactline,
          registry: this.registry,
          resolver: new ExecutableWorkDefinitionResolver(
            pactline,
            () => this.manager.snapshot,
            this.environment,
            sessionId,
          ),
          executor,
          logger: this.logger,
          sessionId,
          now: this.now,
        }),
        reconciler: new FleetRunReconciler(
          this.registry,
          executor,
          pactline,
          () => this.manager.snapshot,
          sessionId,
        ),
      }
      created = true
    }
    if (this.schedulingRuntime === undefined || !created) return
    const recovered = await this.schedulingRuntime.reconciler.reconcile()
    this.logger.log('info', 'fleet.recovery.completed', {
      runCount: recovered.length,
      quarantined: recovered.filter(item => item.outcome.kind === 'quarantined').length,
    })
    this.healthStore.setRegistry('ok', this.registry.listNonTerminalRuns().length)
  }

  private configureDefaultAdapters(snapshot: FleetConfigSnapshot): void {
    if (!this.defaultAdapters) return
    const environment = { ...this.environment }
    delete environment.PACTLINE_TOKEN
    delete environment[snapshot.config.service.pactline.tokenEnv]
    const adapters: HarnessAdapter[] = [
      new CodexHarnessAdapter({ environment }),
      new DeepSeekHarnessAdapter({ environment }),
    ]
    this.adapters.clear()
    for (const adapter of adapters) this.adapters.set(adapter.id, adapter)
  }

  private pactlineClient(snapshot: FleetConfigSnapshot): PactlinePreflightClient {
    if (this.registry === undefined) throw new Error('Fleet registry is unavailable')
    if (this.options.createPactlineClient !== undefined) {
      return this.options.createPactlineClient(snapshot, this.registry.serviceId)
    }
    const token = this.environment[snapshot.config.service.pactline.tokenEnv]
    const environment = { ...this.environment }
    if (token === undefined || token === '') delete environment.PACTLINE_TOKEN
    else environment.PACTLINE_TOKEN = token
    return new PactlineCLI({
      executable: snapshot.config.service.pactline.executable,
      server: snapshot.config.service.pactline.server,
      clientKind: 'pactline-fleet-service',
    }, { environment })
  }

  private async checkPactline(snapshot: FleetConfigSnapshot): Promise<void> {
    const previous = this.healthStore!.snapshot().pactline
    try {
      const result = await this.pactlineClient(snapshot).preflight({
        protocol: 2,
        verifyAuthentication: true,
        sessionId: `fleet-service-${this.registry!.serviceId}`,
      })
      this.healthStore!.setPactline({
        status: 'ok',
        cliVersion: result.capabilities.cli_version,
        protocol: result.capabilities.protocol,
        featureCount: result.capabilities.features.length,
      })
      if (previous.status !== 'ok') {
        this.logger.log('info', 'fleet.pactline.probe_succeeded', {
          server: snapshot.config.service.pactline.server,
          cliVersion: result.capabilities.cli_version,
          protocol: result.capabilities.protocol,
          featureCount: result.capabilities.features.length,
        })
      }
    } catch (error) {
      const diagnostic = message(error)
      this.healthStore!.setPactline({ status: 'error', message: diagnostic })
      if (previous.status !== 'error' || previous.message !== diagnostic) {
        this.logger.log('warn', 'fleet.pactline.probe_failed', {
          server: snapshot.config.service.pactline.server,
          error: diagnostic,
        })
      }
    }
  }

  private async checkAdapters(snapshot: FleetConfigSnapshot): Promise<void> {
    const previous = new Map(this.healthStore!.snapshot().adapters.map(adapter => [adapter.id, adapter]))
    const requirements = aggregateAdapterRequirements(snapshot.config)
    const results = await Promise.all([...requirements].map(async ([adapterId, requirement]) => {
      const adapter = this.adapters.get(adapterId)
      if (adapter === undefined) {
        const diagnostic = `Configured Harness Adapter is unavailable: ${adapterId}`
        if (previous.get(adapterId)?.status !== 'error') {
          this.logger.log('warn', 'fleet.adapter.probe_failed', {
            adapterId,
            error: diagnostic,
          })
        }
        return {
          id: adapterId,
          status: 'error' as const,
          message: diagnostic,
        }
      }
      try {
        const capabilities = await adapter.probe(probeRequest(requirement))
        validateCapabilities(adapterId, capabilities, requirement)
        const result = {
          id: adapter.id,
          version: adapter.version,
          status: 'ok' as const,
          capabilities,
        }
        if (previous.get(adapter.id)?.status !== 'ok') {
          this.logger.log('info', 'fleet.adapter.probe_succeeded', {
            adapterId: adapter.id,
            adapterVersion: adapter.version,
          })
        }
        return result
      } catch (error) {
        const diagnostic = message(error)
        const result = {
          id: adapter.id,
          version: adapter.version,
          status: 'error' as const,
          message: diagnostic,
        }
        const previousAdapter = previous.get(adapter.id)
        if (previousAdapter?.status !== 'error' || previousAdapter.message !== diagnostic) {
          this.logger.log('warn', 'fleet.adapter.probe_failed', {
            adapterId: adapter.id,
            adapterVersion: adapter.version,
            error: diagnostic,
          })
        }
        return result
      }
    }))
    if (this.manager.snapshot.revision === snapshot.revision) {
      this.healthStore!.replaceAdapters(results)
    }
  }

  private scheduleHealthProbe(): void {
    if (this.healthProbeTimer !== undefined) clearTimeout(this.healthProbeTimer)
    if (this.stopping !== undefined) return
    const delay = this.manager.snapshot.config.service.pollIntervalMs
    this.healthProbeTimer = setTimeout(() => {
      this.healthProbeTimer = undefined
      void this.refreshHealth().finally(() => { this.scheduleHealthProbe() })
    }, delay)
    this.healthProbeTimer.unref()
  }

  private scheduleWorkCycle(delay?: number): void {
    if (this.schedulerTimer !== undefined) clearTimeout(this.schedulerTimer)
    if (this.stopping !== undefined || this.schedulingRuntime === undefined) return
    const base = delay ?? this.manager.snapshot.config.service.pollIntervalMs
    const jitter = delay === 0 ? 0 : Math.round(base * 0.1 * ((Math.random() * 2) - 1))
    this.schedulerTimer = setTimeout(() => {
      this.schedulerTimer = undefined
      void this.runSchedulingCycle(false).then(result => {
        this.logger.log('debug', 'fleet.scheduler.cycle_completed', {
          discovered: result.discovered,
          admitted: result.admitted,
          skipped: result.skipped,
          contentions: result.contentions,
        })
      }).catch(error => {
        this.logger.log('warn', 'fleet.scheduler.cycle_failed', { error: message(error) })
      }).finally(() => { this.scheduleWorkCycle() })
    }, Math.max(1, base + jitter))
    this.schedulerTimer.unref()
  }

  private async refreshHealth(): Promise<void> {
    if (this.stopping !== undefined || this.registry === undefined || this.healthStore === undefined) return
    const snapshot = this.manager.snapshot
    try {
      const compromised = this.lock?.compromisedError
      if (compromised !== undefined) throw compromised
      if (!this.registry.healthCheck()) throw new Error('Fleet registry health check failed')
      this.healthStore.setRegistry('ok', this.registry.listNonTerminalRuns().length)
    } catch (error) {
      this.healthStore.setRegistry('error', 0, message(error))
    }
    await Promise.all([
      this.checkPactline(snapshot),
      this.checkAdapters(snapshot),
    ])
    this.healthStore.settleMode()
  }

  private async stopOwnedResources(reason: string): Promise<void> {
    this.manager.stopWatching()
    if (this.healthProbeTimer !== undefined) {
      clearTimeout(this.healthProbeTimer)
      this.healthProbeTimer = undefined
    }
    if (this.schedulerTimer !== undefined) {
      clearTimeout(this.schedulerTimer)
      this.schedulerTimer = undefined
    }
    this.healthStore?.setMode('draining')
    this.schedulingRuntime?.scheduler.beginDrain(new Error(message(reason)))
    this.logger.log('info', 'fleet.service.draining', { reason: message(reason) })
    let failure: unknown
    await this.schedulingCycle?.catch(() => undefined)
    if (this.schedulingRuntime !== undefined) {
      const deadline = this.manager.snapshot.config.service.shutdownDeadlineMs
      const drained = await this.schedulingRuntime.scheduler.waitForActive(deadline)
      if (!drained) this.logger.log('warn', 'fleet.service.drain_deadline_expired', { deadlineMs: deadline })
    }
    try {
      await this.healthServer?.close()
    } catch (error) {
      failure = error
    }
    try {
      this.registry?.close()
    } catch (error) {
      failure ??= error
    }
    try {
      await this.lock?.release()
    } catch (error) {
      failure ??= error
    }
    this.healthStore?.setMode('stopped')
    this.resolveStopped()
    this.logger.log(failure === undefined ? 'info' : 'error', 'fleet.service.stopped', {
      reason: message(reason),
      ...(failure === undefined ? {} : { error: message(failure) }),
    })
    if (failure !== undefined) throw failure
  }

  private async cleanupAfterFailedStart(): Promise<void> {
    this.manager.stopWatching()
    await this.healthServer?.close().catch(() => undefined)
    try { this.registry?.close() } catch {}
    await this.lock?.release().catch(() => undefined)
    this.healthStore?.setMode('stopped')
    this.resolveStopped()
  }
}
