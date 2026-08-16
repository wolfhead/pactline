import { lock } from 'proper-lockfile'

export interface FleetServiceLock {
  readonly path: string
  readonly compromisedError: Error | undefined
  release(): Promise<void>
}

export interface FleetServiceLockOptions {
  readonly staleMs?: number
  readonly updateMs?: number
}

export async function acquireFleetServiceLock(
  stateDirectory: string,
  options: FleetServiceLockOptions = {},
): Promise<FleetServiceLock> {
  let compromisedError: Error | undefined
  let released = false
  let releaseOwnedLock: (() => Promise<void>)
  try {
    releaseOwnedLock = await lock(stateDirectory, {
      realpath: true,
      retries: 0,
      stale: options.staleMs ?? 30_000,
      update: options.updateMs ?? 10_000,
      onCompromised: error => { compromisedError = error },
    })
  } catch (error) {
    throw new Error(`Fleet Service state directory is already locked: ${stateDirectory}`, { cause: error })
  }
  return {
    path: stateDirectory,
    get compromisedError() { return compromisedError },
    async release(): Promise<void> {
      if (released) return
      released = true
      await releaseOwnedLock()
    },
  }
}
