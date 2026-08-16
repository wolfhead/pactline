import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import type { FleetServiceHealth } from '../../src/health/model.js'
import { FleetObservationProjector } from '../../src/observation/projection.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []
afterEach(async () => { await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true }))) })

function health(revision: string): FleetServiceHealth {
  return {
    serviceId: 'service-observation', version: 'test', mode: 'ready', live: true, ready: true,
    startedAt: '2026-08-16T10:00:00.000Z', updatedAt: '2026-08-16T10:02:00.000Z',
    config: { revision, loadedAt: '2026-08-16T10:00:00.000Z' },
    registry: { status: 'ok', path: '/private/fleet.sqlite3', schemaVersion: 3, nonTerminalRuns: 0 },
    pactline: { status: 'ok', server: 'http://localhost:8080' },
    adapters: [{ id: 'codex', status: 'ok', version: 'test', capabilities: {
      nativeTools: true, structuredResult: true, eventStream: true, cancellation: true, sessionResume: true,
      sandboxModes: ['workspace_write'], supportedStages: ['execution'],
    } }],
    fleets: [{ id: 'first', projectNumber: 5, enabled: true, status: 'healthy', adapters: ['codex'], discovery: { status: 'ok', candidateCount: 1, checkedAt: '2026-08-16T10:02:00.000Z' } }],
  }
}

describe('FleetObservationProjector', () => {
  it('projects bounded causal Run evidence without raw Harness result or credentials', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-observation-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const configPath = join(parent, 'fleet.yml')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'workspace') })
    await writeFile(configPath, source)
    const snapshot = parseFleetConfig(source, configPath, { knownAdapterIds: ['codex'] })
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    registry.recordConfiguration(snapshot)
    let run = registry.admitRun('first', {
      taskNumber: 41, taskVersion: 7, stage: 'execution',
      frozenPolicy: { route: { adapter: 'codex', model: 'gpt-5.6-sol', reasoning: 'high' }, apiKey: 'must-not-project' },
    }, new Date('2026-08-16T10:01:00.000Z'))
    run = registry.transitionRun(run.runId, 'admitted', 'claiming', { checkpoint: 'claim_intent' }, new Date('2026-08-16T10:01:01.000Z'))
    registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
    registry.observeEffect(run.runId, 'harness_result', {
      terminalState: 'success', runtimeSessionId: 'session-41',
      result: { reasoning: 'must-not-project', prompt: 'must-not-project' },
    })
    registry.recordEffectIntent(run.runId, 'repository_delivery', `${run.runId}-delivery`, {})
    registry.observeEffect(run.runId, 'repository_delivery', {
      codeChangeUrl: 'https://actor:must-not-project@example.com/org/repo/pull/41?access_token=must-not-project#secret',
      revision: 'c'.repeat(40), branch: 'fleet/run-41',
    })
    const projector = new FleetObservationProjector(() => health(snapshot.revision), () => snapshot, registry, () => new Date('2026-08-16T10:02:00.000Z'))

    expect(projector.overview().data.activeRuns).toEqual([expect.objectContaining({
      taskNumber: 41, adapter: 'codex', checkpoint: 'claim_intent',
    })])
    const detail = projector.run(run.runId)!.data
    expect(detail.timeline.map(item => item.title)).toEqual(['Run admitted', 'Run entered claiming'])
    expect(detail.effects).toEqual(expect.arrayContaining([
      expect.objectContaining({ kind: 'harness_result', detail: { terminalState: 'success', runtimeSessionId: 'session-41' } }),
      expect.objectContaining({ kind: 'repository_delivery', detail: expect.objectContaining({ codeChangeUrl: 'https://example.com/org/repo/pull/41' }) }),
    ]))
    const serialized = JSON.stringify(detail)
    expect(serialized).not.toContain('must-not-project')
    expect(serialized).not.toContain('prompt')
    expect(projector.metrics()).toContain('pactline_fleet_runs{state="claiming"} 1')
    registry.close()
  })
})
