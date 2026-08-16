import { watchFile, unwatchFile } from 'node:fs'
import type { FleetConfigLoadOptions, FleetConfigSnapshot } from './types.js'
import { loadFleetConfig } from './load.js'

export interface FleetConfigReloadResult {
  readonly applied: boolean
  readonly snapshot: FleetConfigSnapshot
  readonly error?: string
}

export interface FleetConfigManagerOptions extends FleetConfigLoadOptions {
  readonly watchIntervalMs?: number
}

export type FleetConfigPrepare = (
  candidate: FleetConfigSnapshot,
  current: FleetConfigSnapshot,
) => Promise<void> | void

function processBoundaryChanged(current: FleetConfigSnapshot, candidate: FleetConfigSnapshot): string | undefined {
  const before = current.config.service
  const after = candidate.config.service
  if (before.stateDirectory !== after.stateDirectory) return 'service.stateDirectory requires a restart'
  if (before.http.address !== after.http.address || before.http.port !== after.http.port) {
    return 'service.http requires a restart'
  }
  if (before.pactline.server !== after.pactline.server
    || before.pactline.tokenEnv !== after.pactline.tokenEnv
    || before.pactline.executable !== after.pactline.executable) {
    return 'service.pactline requires a restart'
  }
  return undefined
}

export class FleetConfigManager {
  private snapshotValue: FleetConfigSnapshot | undefined
  private watching = false
  private reloadChain: Promise<FleetConfigReloadResult> | undefined

  constructor(
    readonly sourcePath: string,
    private readonly options: FleetConfigManagerOptions = {},
  ) {}

  get snapshot(): FleetConfigSnapshot {
    if (this.snapshotValue === undefined) throw new Error('Fleet configuration has not been loaded')
    return this.snapshotValue
  }

  async loadInitial(): Promise<FleetConfigSnapshot> {
    if (this.snapshotValue !== undefined) throw new Error('Fleet configuration is already loaded')
    const snapshot = await loadFleetConfig(this.sourcePath, this.options)
    this.snapshotValue = snapshot
    return snapshot
  }

  async reload(prepare?: FleetConfigPrepare): Promise<FleetConfigReloadResult> {
    const previous = this.reloadChain
    const next = (previous === undefined ? Promise.resolve() : previous.then(() => undefined))
      .then(async () => {
        const current = this.snapshot
        try {
          const candidate = await loadFleetConfig(this.sourcePath, this.options)
          const boundaryError = processBoundaryChanged(current, candidate)
          if (boundaryError !== undefined) return { applied: false, snapshot: current, error: boundaryError }
          await prepare?.(candidate, current)
          this.snapshotValue = candidate
          return { applied: true, snapshot: candidate }
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error)
          return { applied: false, snapshot: current, error: message.slice(0, 4_096) }
        }
      })
    this.reloadChain = next
    return await next
  }

  watch(onReload: (result: FleetConfigReloadResult) => void, prepare?: FleetConfigPrepare): void {
    if (this.watching) throw new Error('Fleet configuration is already being watched')
    this.watching = true
    watchFile(
      this.sourcePath,
      { interval: this.options.watchIntervalMs ?? 1_000, persistent: false },
      (current, previous) => {
        if (current.mtimeMs === previous.mtimeMs && current.size === previous.size) return
        void this.reload(prepare).then(onReload)
      },
    )
  }

  stopWatching(): void {
    if (!this.watching) return
    unwatchFile(this.sourcePath)
    this.watching = false
  }
}
