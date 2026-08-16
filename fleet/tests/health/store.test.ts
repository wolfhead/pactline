import { describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import { FleetHealthStore, sanitizeHealthDiagnostic } from '../../src/health/store.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const capabilities = {
  nativeTools: true,
  structuredResult: true,
  eventStream: true,
  cancellation: true,
  sessionResume: true,
  sandboxModes: ['read_only', 'workspace_write'] as const,
  supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'] as const,
}

function store(): FleetHealthStore {
  const snapshot = parseFleetConfig(serviceConfigYAML({
    stateDirectory: '/tmp/fleet-health-state',
    firstWorkspace: '/tmp/fleet-health-work',
  }), '/tmp/fleet-health.yml', { knownAdapterIds: ['codex'] })
  return new FleetHealthStore(
    'service-test',
    'test',
    snapshot,
    '/tmp/fleet-health-state/fleet.sqlite3',
    '2026-08-15T10:00:00.000Z',
    () => new Date('2026-08-15T10:00:01Z'),
  )
}

describe('FleetHealthStore', () => {
  it('becomes ready only when registry, Pactline, and one Fleet route are healthy', () => {
    const health = store()
    health.setMode('checking')
    health.setRegistry('ok', 0)
    health.setPactline({ status: 'ok', cliVersion: '0.1.0', protocol: 2, featureCount: 16 })
    health.setAdapter({ id: 'codex', version: 'test', status: 'ok', capabilities })
    health.settleMode()

    expect(health.snapshot()).toMatchObject({
      mode: 'ready',
      live: true,
      ready: true,
      fleets: [{ id: 'first', projectNumber: 5, status: 'healthy' }],
    })
  })

  it('keeps service diagnostics available while a configured Adapter is degraded', () => {
    const health = store()
    health.setRegistry('ok', 0)
    health.setPactline({ status: 'ok' })
    health.setAdapter({ id: 'codex', status: 'error', message: 'runtime missing' })
    health.settleMode()

    expect(health.snapshot()).toMatchObject({
      mode: 'degraded',
      live: true,
      ready: false,
      fleets: [{ status: 'degraded', message: 'Unavailable Adapter: codex' }],
    })
  })

  it('redacts credential-shaped diagnostics', () => {
    expect(sanitizeHealthDiagnostic('Bearer abc token=secret-value')).toBe(
      'Bearer [REDACTED] token=[REDACTED]',
    )
  })
})
