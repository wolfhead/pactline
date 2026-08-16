export type ServiceMode = 'starting' | 'checking' | 'ready' | 'degraded' | 'draining' | 'stopped'
export type RunState = 'admitted' | 'claiming' | 'claimed' | 'preparing_workspace' | 'starting_harness' | 'running_harness' | 'validating' | 'delivering' | 'settling' | 'releasing' | 'completed' | 'released' | 'quarantined' | 'failed'
export type RunStage = 'execution' | 'review' | 'correction'

export interface Envelope<T> { ok: true; data: T; meta?: { generatedAt: string; revision: string } }
export interface ListData<T> { items: T[]; nextBefore?: string }
export interface DependencyHealth { status: 'unknown' | 'ok' | 'error'; checkedAt?: string; message?: string }
export interface AdapterHealth extends DependencyHealth { id: string; version?: string; capabilities?: Record<string, unknown> }
export interface DiscoveryHealth { status: 'unknown' | 'ok' | 'error' | 'backoff'; candidateCount: number; checkedAt?: string; retryAt?: string }
export interface FleetHealth { id: string; projectNumber: number; enabled: boolean; status: 'healthy' | 'degraded' | 'disabled'; adapters: string[]; message?: string; discovery: DiscoveryHealth }
export interface ServiceHealth {
  serviceId: string; version: string; mode: ServiceMode; live: boolean; ready: boolean; startedAt: string; updatedAt: string
  config: { revision: string; loadedAt: string; lastReloadAt?: string; lastReloadError?: string }
  registry: DependencyHealth & { path: string; schemaVersion: number; nonTerminalRuns: number }
  pactline: DependencyHealth & { server: string; cliVersion?: string; protocol?: number; featureCount?: number }
  adapters: AdapterHealth[]; fleets: FleetHealth[]
}
export interface Attention { id: string; scope: 'service' | 'fleet' | 'run' | 'adapter'; severity: 'warning' | 'critical'; title: string; detail: string; fleetId?: string; runId?: string; checkedAt?: string }
export interface Route { adapter: string; model: string; reasoning?: string }
export interface Fleet { id: string; projectNumber: number; enabled: boolean; status: 'healthy' | 'degraded' | 'disabled'; message?: string; maxConcurrentRuns: number; workPluginConfigured: boolean; workspaceRoot: string; routing: Record<string, Route>; activeRunCount: number; recentRunCount: number; discovery: DiscoveryHealth }
export interface RunSummary { runId: string; fleetId: string; projectNumber: number; taskNumber?: number; stage?: RunStage; state: RunState; adapter?: string; model?: string; reasoning?: string; checkpoint?: string; disposition?: string; error?: string; createdAt: string; updatedAt: string }
export interface TimelineItem { sequence: number; at: string; kind: string; title: string; detail?: string; state?: RunState; checkpoint?: string }
export interface Effect { kind: string; status: 'intended' | 'observed'; title: string; detail?: Record<string, string | number | boolean>; updatedAt: string }
export interface RunDetail extends RunSummary { serviceId: string; configRevision: string; taskVersion?: number; claimId?: string; claimVersion?: number; claimTaskVersion?: number; runtimeSessionId?: string; workspace?: Record<string, string>; timeline: TimelineItem[]; effects: Effect[] }
export interface Overview { attention: Attention[]; fleets: Fleet[]; activeRuns: RunSummary[]; recentRuns: RunSummary[] }
