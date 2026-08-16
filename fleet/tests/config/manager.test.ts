import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { FleetConfigManager } from '../../src/config/manager.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

async function fixture(): Promise<{ directory: string; configPath: string; manager: FleetConfigManager }> {
  const directory = await mkdtemp(join(tmpdir(), 'fleet-config-manager-'))
  directories.push(directory)
  const configPath = join(directory, 'fleet.yml')
  await writeFile(configPath, serviceConfigYAML({
    stateDirectory: join(directory, 'state'),
    firstWorkspace: join(directory, 'work', 'first'),
  }))
  const manager = new FleetConfigManager(configPath, {
    knownAdapterIds: ['codex', 'deepseek'],
    now: () => new Date('2026-08-15T10:00:00Z'),
    watchIntervalMs: 10,
  })
  return { directory, configPath, manager }
}

describe('FleetConfigManager', () => {
  it('keeps the active revision after an invalid reload', async () => {
    const { configPath, manager } = await fixture()
    const initial = await manager.loadInitial()
    await writeFile(configPath, 'version: invalid\n')

    const result = await manager.reload()

    expect(result.applied).toBe(false)
    expect(result.snapshot.revision).toBe(initial.revision)
    expect(manager.snapshot.revision).toBe(initial.revision)
    expect(result.error).toContain('configuration.version')
  })

  it('atomically applies a valid Fleet policy change', async () => {
    const { directory, configPath, manager } = await fixture()
    const initial = await manager.loadInitial()
    await writeFile(configPath, serviceConfigYAML({
      stateDirectory: join(directory, 'state'),
      firstWorkspace: join(directory, 'work', 'first'),
      firstModel: 'gpt-5.6-sol-updated',
    }))

    const result = await manager.reload()

    expect(result.applied).toBe(true)
    expect(result.snapshot.revision).not.toBe(initial.revision)
    expect(manager.snapshot.config.fleets.first?.routing.execution.model).toBe('gpt-5.6-sol-updated')
  })

  it('rejects process-bound changes until restart', async () => {
    const { directory, configPath, manager } = await fixture()
    const initial = await manager.loadInitial()
    await writeFile(configPath, serviceConfigYAML({
      stateDirectory: join(directory, 'other-state'),
      firstWorkspace: join(directory, 'work', 'first'),
    }))

    const result = await manager.reload()

    expect(result).toMatchObject({
      applied: false,
      error: 'service.stateDirectory requires a restart',
    })
    expect(manager.snapshot.revision).toBe(initial.revision)
  })

  it('watches and applies a valid configuration replacement', async () => {
    const { directory, configPath, manager } = await fixture()
    const initial = await manager.loadInitial()
    const observed = new Promise<{ applied: boolean; revision: string }>((resolvePromise, reject) => {
      const timer = setTimeout(() => reject(new Error('Configuration watch timed out')), 2_000)
      manager.watch(result => {
        clearTimeout(timer)
        resolvePromise({ applied: result.applied, revision: result.snapshot.revision })
      })
    })
    await writeFile(configPath, serviceConfigYAML({
      stateDirectory: join(directory, 'state'),
      firstWorkspace: join(directory, 'work', 'first'),
      firstModel: 'gpt-5.6-sol-watched',
    }))

    try {
      await expect(observed).resolves.toMatchObject({ applied: true })
      expect(manager.snapshot.revision).not.toBe(initial.revision)
      expect(manager.snapshot.config.fleets.first?.routing.execution.model).toBe('gpt-5.6-sol-watched')
    } finally {
      manager.stopWatching()
    }
  })
})
