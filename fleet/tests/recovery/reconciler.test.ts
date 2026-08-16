import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import { FleetRunReconciler } from '../../src/recovery/reconciler.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

describe('FleetRunReconciler', () => {
  it('records but never adopts or releases an unfamiliar same-principal active Claim', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'fleet-reconciler-test-'))
    directories.push(directory)
    const state = await ensurePrivateDirectory(join(directory, 'state'))
    const snapshot = parseFleetConfig(serviceConfigYAML({
      stateDirectory: state, firstWorkspace: join(directory, 'work'),
    }), join(directory, 'fleet.yml'), { knownAdapterIds: ['codex'] })
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    registry.recordConfiguration(snapshot)
    let mutations = 0
    const reconciler = new FleetRunReconciler(
      registry,
      {
        execute: () => Promise.reject(new Error('not used')),
        recover: () => Promise.reject(new Error('not used')),
      },
      {
        listActiveClaims: async () => ({ data: [{
          id: 'claim-from-other-service', task_number: 41, stage: 'execution', status: 'active', version: 3,
        }] }),
        showClaim: async () => ({ data: {
          claim: { id: 'claim-from-other-service' },
          task: { number: 41, version: 9, project: { number: 5 } },
        } }),
      } as never,
      () => snapshot,
      'recovery-test',
    )

    const results = await reconciler.reconcile()
    expect(results).toEqual([expect.objectContaining({
      outcome: { kind: 'quarantined', reason: 'unfamiliar_same_principal_claim' },
    })])
    expect(registry.listNonTerminalRuns()).toEqual([])
    expect(registry.hasRegisteredClaim('claim-from-other-service')).toBe(true)
    expect(mutations).toBe(0)
    registry.close()
  })
})
