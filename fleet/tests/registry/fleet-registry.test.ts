import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

describe('FleetRegistry', () => {
  it('persists a stable service identity with private file permissions', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-id-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const first = await FleetRegistry.open(path, () => new Date('2026-08-15T10:00:00Z'))
    const serviceId = first.serviceId
    first.close()

    const second = await FleetRegistry.open(path)
    expect(second.serviceId).toBe(serviceId)
    expect(second.healthCheck()).toBe(true)
    expect((await stat(path)).mode & 0o777).toBe(0o600)
    second.close()
  })

  it('freezes a Run to its admitted configuration revision', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-run-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const configPath = join(parent, 'fleet.yml')
    const firstSource = serviceConfigYAML({
      stateDirectory: state,
      firstWorkspace: join(parent, 'work', 'first'),
    })
    await writeFile(configPath, firstSource)
    const firstConfig = parseFleetConfig(firstSource, configPath, { knownAdapterIds: ['codex', 'deepseek'] })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(firstConfig)
    const run = registry.createRun('first', new Date('2026-08-15T10:00:00Z'))

    const nextSource = serviceConfigYAML({
      stateDirectory: state,
      firstWorkspace: join(parent, 'work', 'first'),
      firstModel: 'gpt-5.6-sol-updated',
    })
    const nextConfig = parseFleetConfig(nextSource, configPath, { knownAdapterIds: ['codex', 'deepseek'] })
    registry.recordConfiguration(nextConfig)

    expect(registry.getRun(run.runId)).toMatchObject({
      runId: run.runId,
      configRevision: firstConfig.revision,
      state: 'admitted',
    })
    expect(registry.listNonTerminalRuns()).toHaveLength(1)
    registry.close()
  })

  it('stores only credential references and never resolves environment secrets', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-secret-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const secret = 'not-a-real-secret-value-for-registry-test'
    const source = serviceConfigYAML({
      stateDirectory: state,
      firstWorkspace: join(parent, 'work', 'first'),
    })
    const previous = process.env.TEST_PACTLINE_TOKEN
    process.env.TEST_PACTLINE_TOKEN = secret
    try {
      const snapshot = parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex', 'deepseek'] })
      const registry = await FleetRegistry.open(path)
      registry.recordConfiguration(snapshot)
      registry.close()
    } finally {
      if (previous === undefined) delete process.env.TEST_PACTLINE_TOKEN
      else process.env.TEST_PACTLINE_TOKEN = previous
    }

    expect((await readFile(path)).includes(Buffer.from(secret))).toBe(false)
    expect((await readFile(path)).includes(Buffer.from('local-test-git'))).toBe(true)
  })

  it('atomically replaces the local Fleet ID for one Project', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-replace-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const initialSource = serviceConfigYAML({
      stateDirectory: state,
      firstWorkspace: join(parent, 'work', 'first'),
    })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(initialSource, join(parent, 'fleet.yml'), {
      knownAdapterIds: ['codex'],
    }))
    const replacementSource = initialSource.replace('  first:\n', '  replacement:\n')
    registry.recordConfiguration(parseFleetConfig(replacementSource, join(parent, 'fleet.yml'), {
      knownAdapterIds: ['codex'],
    }))

    expect(registry.createRun('replacement')).toMatchObject({
      fleetId: 'replacement',
      projectNumber: 5,
    })
    expect(() => registry.createRun('first')).toThrow('Enabled Fleet is not registered')
    registry.close()
  })

  it('persists frozen admission, monotonic transitions, and external effects across reopen', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-recovery-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({
      stateDirectory: state,
      firstWorkspace: join(parent, 'work', 'first'),
    })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), {
      knownAdapterIds: ['codex'],
    }))
    const run = registry.admitRun('first', {
      taskNumber: 41,
      taskVersion: 7,
      stage: 'execution',
      frozenPolicy: {
        route: { adapter: 'codex', model: 'gpt-5.6-sol', reasoning: 'high' },
        repository: { revision: 'a'.repeat(40) },
        allowedPaths: ['fleet/'],
        verificationCommands: ['npm test'],
      },
    }, new Date('2026-08-16T09:00:00Z'))
    registry.transitionRun(run.runId, 'admitted', 'claiming', { checkpoint: 'claim_intent' })
    registry.recordEffectIntent(run.runId, 'claim', `${run.runId}-claim`, {
      taskNumber: 41, taskVersion: 7, stage: 'execution',
    })
    registry.observeEffect(run.runId, 'claim', { claimId: 'claim-41', claimVersion: 1 })
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: 'claim-41', claimVersion: 1, checkpoint: 'claim_observed',
    })
    expect(() => registry.transitionRun(run.runId, 'claimed', 'running_harness')).toThrow('Invalid Fleet Run transition')
    expect(() => registry.admitRun('first', {
      taskNumber: 41, taskVersion: 7, stage: 'execution', frozenPolicy: {},
    })).toThrow()
    registry.close()

    const reopened = await FleetRegistry.open(path)
    expect(reopened.getRun(run.runId)).toMatchObject({
      taskNumber: 41,
      taskVersion: 7,
      stage: 'execution',
      state: 'claimed',
      claimId: 'claim-41',
      checkpoint: 'claim_observed',
      frozenPolicy: { allowedPaths: ['fleet/'] },
    })
    expect(reopened.listEffects(run.runId)).toEqual([expect.objectContaining({
      kind: 'claim',
      status: 'observed',
      idempotencyKey: `${run.runId}-claim`,
      observation: { claimId: 'claim-41', claimVersion: 1 },
    })])
    reopened.close()
  })

  it('allows a later terminal Run for the same Task and stage', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-repeat-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work', 'first') })
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const first = registry.admitRun('first', { taskNumber: 8, taskVersion: 2, stage: 'review', frozenPolicy: {} })
    registry.transitionRun(first.runId, 'admitted', 'released', { disposition: 'candidate_changed' })
    expect(registry.admitRun('first', {
      taskNumber: 8, taskVersion: 3, stage: 'review', frozenPolicy: {},
    })).toMatchObject({ taskNumber: 8, taskVersion: 3, state: 'admitted' })
    registry.close()
  })
})
