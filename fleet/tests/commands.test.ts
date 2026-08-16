import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { EventEmitter } from 'node:events'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { isSupportedNodeVersion, runFleetDoctor } from '../src/commands/doctor.js'
import { fleetVersion } from '../src/commands/version.js'
import { runDeepSeekDoctor } from '../src/commands/deepseek-doctor.js'
import { runCodexDoctor } from '../src/commands/codex-doctor.js'
import { runFleetServe } from '../src/commands/serve.js'
import type { FleetServiceHealth } from '../src/health/model.js'

describe('finite Fleet commands', () => {
  it('reports the standalone application identity', () => {
    expect(fleetVersion()).toEqual({
      name: '@pactline/fleet', version: '0.0.0-development', executable: 'pactline-fleet',
    })
  })

  it.each([
    ['22.19.0', true], ['22.99.1', true], ['23.0.0', false], ['24.0.0', true], ['25.1.0', true], ['invalid', false],
  ])('classifies Node.js %s support as %s', (version, supported) => {
    expect(isSupportedNodeVersion(version)).toBe(supported)
  })

  it('checks Pactline protocol without authentication or Harness startup', async () => {
    await expect(runFleetDoctor({
      pactlineExecutable: '/test/bin/pactline', nodeVersion: '24.0.0',
      runCapabilities: async (executable, timeoutMs) => ({
        ok: executable === '/test/bin/pactline' && timeoutMs === 15_000,
        data: { cli_version: '0.1.0-test', protocol: 2, features: ['execution_claims', 'review_claims'] },
      }),
    })).resolves.toEqual({
      status: 'ok', application: '@pactline/fleet',
      node: { version: '24.0.0', supported: true },
      pactline: { executable: '/test/bin/pactline', cliVersion: '0.1.0-test', protocol: 2, featureCount: 2 },
      adapters: { configured: 0, note: 'No Harness is selected by this check; run deepseek-doctor for the optional DeepSeek runtime' },
    })
  })

  it('rejects an unsupported Pactline protocol', async () => {
    await expect(runFleetDoctor({
      pactlineExecutable: 'pactline', nodeVersion: '24.0.0',
      runCapabilities: async () => ({ ok: true, data: { cli_version: '0.1.0', protocol: 1, features: [] } }),
    })).rejects.toThrow('protocol 1 is unsupported')
  })

  it('reports the finite keyless DeepSeek Adapter policy and capabilities', async () => {
    const capabilities = {
      nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: false,
      sandboxModes: ['read_only', 'workspace_write'] as const,
      supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'] as const,
    }
    await expect(runDeepSeekDoctor({
      adapter: { id: 'deepseek', version: 'test', probe: async () => capabilities },
    })).resolves.toEqual({
      status: 'ok', adapter: { id: 'deepseek', version: 'test' },
      route: {
        provider: 'deepseek-official', model: 'deepseek-v4-pro', reasoning: 'max',
        resultTool: 'submit_fleet_result',
      },
      capabilities,
      liveModelCall: false,
    })
  })

  it('reports the pinned keyless Codex Adapter policy and capabilities', async () => {
    const capabilities = {
      nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: true,
      sandboxModes: ['read_only', 'workspace_write'] as const,
      supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'] as const,
    }
    await expect(runCodexDoctor({
      adapter: { id: 'codex', version: 'test', probe: async () => capabilities },
    })).resolves.toEqual({
      status: 'ok', adapter: { id: 'codex', version: 'test' },
      route: {
        provider: 'openai-codex', model: 'gpt-5.6-sol', reasoning: 'high', runtimeVersion: '0.147.0',
      },
      capabilities, liveModelCall: false,
    })
  })

  it('redacts credential-shaped Pactline diagnostics', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'pactline-fleet-doctor-'))
    const executable = join(directory, 'pactline')
    await writeFile(executable, '#!/bin/sh\nprintf "Bearer definitely-not-a-real-credential" >&2\nexit 7\n')
    await chmod(executable, 0o700)
    try {
      let observed: unknown
      try { await runFleetDoctor({ pactlineExecutable: executable, nodeVersion: '24.0.0' }) } catch (error) { observed = error }
      expect(observed).toBeInstanceOf(Error)
      expect((observed as Error).message).toContain('Bearer [REDACTED]')
      expect((observed as Error).message).not.toContain('definitely-not-a-real-credential')
    } finally {
      await rm(directory, { recursive: true, force: true })
    }
  })

  it('owns resident signal registration and graceful stop', async () => {
    const signals = new EventEmitter()
    let resolveStopped: (() => void) | undefined
    const stopped = new Promise<void>(resolvePromise => { resolveStopped = resolvePromise })
    let reloads = 0
    let stopReason: string | undefined
    const health = {
      serviceId: 'service-test',
      ready: true,
    } as FleetServiceHealth
    const service = {
      health,
      async start() { return { url: 'http://127.0.0.1:7331' } },
      async reload() { reloads += 1 },
      async stop(reason?: string) {
        stopReason = reason
        resolveStopped?.()
      },
      async waitUntilStopped() { await stopped },
    }
    let started: unknown
    const serving = runFleetServe({
      configPath: '/test/fleet.yml',
      service,
      signals,
      onStarted: result => { started = result },
    })
    await new Promise(resolvePromise => setImmediate(resolvePromise))

    signals.emit('SIGHUP')
    await new Promise(resolvePromise => setImmediate(resolvePromise))
    signals.emit('SIGTERM')
    await serving

    expect(started).toEqual({
      url: 'http://127.0.0.1:7331',
      serviceId: 'service-test',
      ready: true,
    })
    expect(reloads).toBe(1)
    expect(stopReason).toBe('received SIGTERM')
    expect(signals.listenerCount('SIGINT')).toBe(0)
    expect(signals.listenerCount('SIGTERM')).toBe(0)
    expect(signals.listenerCount('SIGHUP')).toBe(0)
  })

  it('runs one finite scheduler cycle and stops without installing signal handlers', async () => {
    const signals = new EventEmitter()
    const calls: string[] = []
    const service = {
      health: { serviceId: 'service-once', ready: true } as FleetServiceHealth,
      async start() { calls.push('start'); return { url: 'http://127.0.0.1:7331' } },
      async runOnce() { calls.push('once') },
      async reload() { calls.push('reload') },
      async stop(reason?: string) { calls.push(`stop:${String(reason)}`) },
      async waitUntilStopped() { throw new Error('finite mode must not wait for a signal') },
    }
    await runFleetServe({ configPath: '/test/fleet.yml', service, signals, once: true })
    expect(calls).toEqual(['start', 'once', 'stop:finite cycle completed'])
    expect(signals.eventNames()).toEqual([])
  })
})
