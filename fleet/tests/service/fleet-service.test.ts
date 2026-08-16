import { createServer } from 'node:net'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import type {
  HarnessAdapter,
  HarnessCapabilities,
  HarnessProbeRequest,
  HarnessRunObserver,
  HarnessRunRequest,
} from '../../src/core/harness-adapter.js'
import type { HarnessRunResult } from '../../src/core/harness-result.js'
import { FleetService } from '../../src/service/fleet-service.js'
import { NullFleetLogger } from '../../src/service/logger.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

const capabilities: HarnessCapabilities = {
  nativeTools: true,
  structuredResult: true,
  eventStream: true,
  cancellation: true,
  sessionResume: true,
  sandboxModes: ['read_only', 'workspace_write'],
  supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'],
}

class ProbeAdapter implements HarnessAdapter {
  readonly id = 'codex'
  readonly version = 'test'
  probes = 0
  runs = 0

  constructor(public probeError?: Error) {}

  async probe(_request: HarnessProbeRequest): Promise<HarnessCapabilities> {
    this.probes += 1
    if (this.probeError !== undefined) throw this.probeError
    return capabilities
  }

  run(
    _request: HarnessRunRequest,
    _observer: HarnessRunObserver,
    _signal: AbortSignal,
  ): Promise<HarnessRunResult> {
    this.runs += 1
    return Promise.reject(new Error('M5.1 must not start Harness work'))
  }
}

async function availablePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolvePromise, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolvePromise)
  })
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('Test port was not allocated')
  await new Promise<void>((resolvePromise, reject) => {
    server.close(error => error === undefined ? resolvePromise() : reject(error))
  })
  return address.port
}

async function fixture(adapter: ProbeAdapter): Promise<{
  directory: string
  configPath: string
  service: FleetService
}> {
  const directory = await mkdtemp(join(tmpdir(), 'fleet-service-test-'))
  directories.push(directory)
  const configPath = join(directory, 'fleet.yml')
  await writeFile(configPath, serviceConfigYAML({
    stateDirectory: join(directory, 'state'),
    firstWorkspace: join(directory, 'work', 'first'),
    httpPort: await availablePort(),
  }))
  let preflights = 0
  const service = new FleetService(configPath, {
    adapters: [adapter],
    logger: new NullFleetLogger(),
    environment: { TEST_PACTLINE_TOKEN: 'not-a-real-test-token' },
    createPactlineClient: () => ({
      async preflight() {
        preflights += 1
        return {
          capabilities: {
            cli_version: '0.1.0-test',
            protocol: 2,
            features: Array.from({ length: 16 }, (_, index) => `feature-${String(index)}`),
          },
        }
      },
    }),
  })
  expect(preflights).toBe(0)
  return { directory, configPath, service }
}

async function eventually(assertion: () => void, timeoutMs = 2_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  let latest: unknown
  while (Date.now() < deadline) {
    try {
      assertion()
      return
    } catch (error) {
      latest = error
      await new Promise(resolvePromise => setTimeout(resolvePromise, 10))
    }
  }
  throw latest
}

describe('FleetService M5.1 lifecycle', () => {
  it('starts ready, reloads atomically, and stops without starting work', async () => {
    const adapter = new ProbeAdapter()
    const { directory, configPath, service } = await fixture(adapter)
    const address = await service.start()
    const originalRevision = service.health.config.revision
    expect(service.health).toMatchObject({
      mode: 'ready',
      ready: true,
      registry: { status: 'ok', nonTerminalRuns: 0 },
      pactline: { status: 'ok', protocol: 2 },
      fleets: [{ id: 'first', projectNumber: 5, status: 'healthy' }],
    })
    expect(adapter.probes).toBe(1)
    expect(adapter.runs).toBe(0)
    const response = await fetch(`${address.url}/api/v1/service`)
    expect(response.status).toBe(200)
    expect(await response.json()).toMatchObject({ ok: true, data: { ready: true } })

    await writeFile(configPath, 'version: invalid\n')
    await expect(service.reload()).resolves.toMatchObject({ applied: false })
    expect(service.health.config).toMatchObject({ revision: originalRevision })
    expect(service.health.config.lastReloadError).toContain('configuration.version')

    await writeFile(configPath, serviceConfigYAML({
      stateDirectory: join(directory, 'state'),
      firstWorkspace: join(directory, 'work', 'first'),
      secondWorkspace: join(directory, 'work', 'second'),
      httpPort: address.port,
    }))
    await expect(service.reload()).resolves.toMatchObject({ applied: true })
    expect(service.health.fleets).toHaveLength(2)
    expect(service.health.config.revision).not.toBe(originalRevision)
    expect(adapter.runs).toBe(0)

    await service.stop('test complete')
    expect(service.health).toMatchObject({ mode: 'stopped', live: false, ready: false })
    await expect(fetch(`${address.url}/livez`)).rejects.toThrow()
  })

  it('stays observable but not ready after an Adapter probe failure', async () => {
    const adapter = new ProbeAdapter(new Error('Bearer hidden-value adapter unavailable'))
    const { service } = await fixture(adapter)
    const address = await service.start()
    try {
      expect(service.health).toMatchObject({
        mode: 'degraded',
        live: true,
        ready: false,
        adapters: [{ id: 'codex', status: 'error' }],
        fleets: [{ id: 'first', status: 'degraded' }],
      })
      expect(JSON.stringify(service.health)).not.toContain('hidden-value')
      expect((await fetch(`${address.url}/readyz`)).status).toBe(503)
      expect((await fetch(`${address.url}/healthz`)).status).toBe(200)
      expect(adapter.runs).toBe(0)
    } finally {
      await service.stop('test complete')
    }
  })

  it('refreshes dependency health without discovering or running work', async () => {
    const adapter = new ProbeAdapter()
    const { configPath, service } = await fixture(adapter)
    const initial = await readFile(configPath, 'utf8')
    await writeFile(configPath, initial.replace('pollInterval: 5s', 'pollInterval: 20ms'))
    await service.start()
    try {
      expect(service.health.ready).toBe(true)
      adapter.probeError = new Error('runtime became unavailable')

      await eventually(() => {
        expect(service.health).toMatchObject({
          mode: 'degraded',
          ready: false,
          adapters: [{ id: 'codex', status: 'error' }],
        })
      })
      expect(adapter.probes).toBeGreaterThan(1)
      expect(adapter.runs).toBe(0)
    } finally {
      await service.stop('test complete')
    }
  })
})
