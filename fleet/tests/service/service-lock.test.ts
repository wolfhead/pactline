import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { acquireFleetServiceLock } from '../../src/service/service-lock.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

describe('Fleet Service local lock', () => {
  it('allows one owner and releases idempotently', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-service-lock-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const first = await acquireFleetServiceLock(state)

    await expect(acquireFleetServiceLock(state)).rejects.toThrow('already locked')

    await first.release()
    await first.release()
    const next = await acquireFleetServiceLock(state)
    await next.release()
  })
})
