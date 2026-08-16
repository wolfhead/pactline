import type { FleetServiceHealth } from '../health/model.js'
import { FleetService, type FleetServiceOptions } from '../service/fleet-service.js'

export interface FleetServiceSignalSource {
  on(event: 'SIGINT' | 'SIGTERM' | 'SIGHUP', listener: () => void): unknown
  off(event: 'SIGINT' | 'SIGTERM' | 'SIGHUP', listener: () => void): unknown
}

interface FleetServiceRuntime {
  readonly health: FleetServiceHealth
  start(): Promise<{ readonly url: string }>
  reload(): Promise<unknown>
  stop(reason?: string): Promise<void>
  waitUntilStopped(): Promise<void>
  runOnce?(): Promise<unknown>
}

export interface FleetServeOptions {
  readonly configPath: string
  readonly serviceOptions?: FleetServiceOptions
  readonly service?: FleetServiceRuntime
  readonly signals?: FleetServiceSignalSource
  readonly once?: boolean
  readonly onStarted?: (result: {
    readonly url: string
    readonly serviceId: string
    readonly ready: boolean
  }) => void
}

export async function runFleetServe(options: FleetServeOptions): Promise<void> {
  if (options.configPath.trim() === '') throw new Error('Fleet Service configuration path is required')
  const service = options.service ?? new FleetService(options.configPath, options.serviceOptions)
  const signals = options.signals ?? process
  const address = await service.start()
  options.onStarted?.({
    url: address.url,
    serviceId: service.health.serviceId,
    ready: service.health.ready,
  })

  if (options.once === true) {
    if (service.runOnce === undefined) throw new Error('Fleet Service does not support finite scheduling')
    try {
      await service.runOnce()
    } finally {
      await service.stop('finite cycle completed')
    }
    return
  }

  let stopping = false
  let stopTask: Promise<void> | undefined
  const stop = (signal: 'SIGINT' | 'SIGTERM'): void => {
    if (stopping) return
    stopping = true
    stopTask = service.stop(`received ${signal}`)
  }
  const onInterrupt = (): void => { stop('SIGINT') }
  const onTerminate = (): void => { stop('SIGTERM') }
  const onReload = (): void => {
    if (!stopping) void service.reload()
  }
  signals.on('SIGINT', onInterrupt)
  signals.on('SIGTERM', onTerminate)
  signals.on('SIGHUP', onReload)
  try {
    await service.waitUntilStopped()
    await stopTask
  } finally {
    signals.off('SIGINT', onInterrupt)
    signals.off('SIGTERM', onTerminate)
    signals.off('SIGHUP', onReload)
  }
}
