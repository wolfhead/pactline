import { join } from 'node:path'
import { readFile } from 'node:fs/promises'
import { describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const adapters = ['codex', 'deepseek']

describe('Fleet service configuration', () => {
  it('loads two Project-bound Fleets with normalized defaults', () => {
    const source = serviceConfigYAML({
      stateDirectory: '/tmp/fleet-state',
      firstWorkspace: '/tmp/fleet-work/first',
      secondWorkspace: '/tmp/fleet-work/second',
    })
    const snapshot = parseFleetConfig(source, '/tmp/fleet.yml', {
      knownAdapterIds: adapters,
      now: () => new Date('2026-08-15T10:00:00Z'),
    })

    expect(snapshot.loadedAt).toBe('2026-08-15T10:00:00.000Z')
    expect(snapshot.revision).toMatch(/^[a-f0-9]{64}$/)
    expect(snapshot.config.service).toMatchObject({
      stateDirectory: '/tmp/fleet-state',
      pollIntervalMs: 5_000,
      shutdownDeadlineMs: 15_000,
      maxConcurrentRuns: 2,
      http: { address: '127.0.0.1', port: 7_331 },
    })
    expect(snapshot.config.fleets.first).toMatchObject({
      projectNumber: 5,
      enabled: true,
      maxConcurrentRuns: 1,
      credentials: { git: 'local-test-git' },
    })
    expect(snapshot.config.fleets.second?.projectNumber).toBe(12)
    expect(snapshot.config.fleets.first?.routing.execution).toMatchObject({
      adapter: 'codex',
      promptVersion: 'v1',
      resultContractVersion: 1,
    })
  })

  it('rejects duplicate enabled Project ownership inside one service', () => {
    const source = serviceConfigYAML({
      stateDirectory: '/tmp/fleet-state',
      firstWorkspace: '/tmp/fleet-work/first',
      secondWorkspace: '/tmp/fleet-work/second',
      firstProject: 5,
      secondProject: 5,
    })
    expect(() => parseFleetConfig(source, '/tmp/fleet.yml', { knownAdapterIds: adapters }))
      .toThrow('Project 5 is enabled by both first and second')
  })

  it.each([
    ['non-loopback listener', { httpAddress: '0.0.0.0' }, 'must be a loopback address'],
    ['relative state path', { stateDirectory: 'state' }, 'service.stateDirectory must be an absolute path'],
    ['unknown Adapter', { firstAdapter: 'unknown' }, 'adapter is unavailable: unknown'],
  ])('rejects %s', (_name, changes, message) => {
    const source = serviceConfigYAML({
      stateDirectory: '/tmp/fleet-state',
      firstWorkspace: '/tmp/fleet-work/first',
      ...changes,
    })
    expect(() => parseFleetConfig(source, '/tmp/fleet.yml', { knownAdapterIds: adapters })).toThrow(message)
  })

  it('rejects overlapping Fleet workspace roots', () => {
    const source = serviceConfigYAML({
      stateDirectory: '/tmp/fleet-state',
      firstWorkspace: '/tmp/fleet-work',
      secondWorkspace: join('/tmp/fleet-work', 'nested'),
    })
    expect(() => parseFleetConfig(source, '/tmp/fleet.yml', { knownAdapterIds: adapters }))
      .toThrow('Fleet workspace roots overlap')
  })

  it('keeps the shipped example configuration valid', async () => {
    const source = await readFile(new URL('../../config.example.yml', import.meta.url), 'utf8')
    expect(parseFleetConfig(source, '/tmp/config.example.yml', { knownAdapterIds: adapters }).config)
      .toMatchObject({
        version: 1,
        fleets: {
          'pactline-development': {
            projectNumber: 5,
            routing: { execution: { adapter: 'codex', model: 'gpt-5.6-sol' } },
          },
        },
      })
  })
})
