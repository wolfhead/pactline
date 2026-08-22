import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import Database from 'better-sqlite3'
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
  it('bounds persisted verification differences and records the omitted count', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-verification-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const configPath = join(parent, 'fleet.yml')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    registry.recordConfiguration(parseFleetConfig(source, configPath, { knownAdapterIds: ['codex'] }))
    const run = registry.admitRun('first', {
      taskNumber: 22, taskVersion: 1, stage: 'execution', frozenPolicy: {},
    })
    registry.recordVerificationMismatch(run.runId, {
      stage: 'execution', role: 'implementer',
      details: Array.from({ length: 70 }, (_, index) => ({
        category: 'parse_mismatch' as const, command: `command-${String(index)}`,
      })),
    })

    const event = registry.listRunEvents(run.runId).find(item => item.eventType === 'run.verification_mismatch')
    expect(event?.payload.details).toHaveLength(64)
    expect(event?.payload.detailsOmitted).toBe(6)
    registry.close()
  })

  it('persists one isolated Workspace and native Session per Task role', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-task-runtime-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const registry = await FleetRegistry.open(path)
    const workspace = {
      mode: 'execution' as const,
      root: join(parent, 'project-5-task-19'),
      temporaryParent: parent,
      repositoryPath: join(parent, 'project-5-task-19', 'repository'),
      source: 'https://github.com/wolfhead/pactline',
      baseRevision: 'a'.repeat(40),
      branch: 'fleet/task/19',
    }

    registry.bindTaskWorkspace(5, 19, workspace)
    registry.bindTaskRoleSession(5, 19, 'implementer', {
      adapterId: 'codex', runtimeSessionId: 'codex-task-19',
    })
    registry.bindTaskRoleSession(5, 19, 'reviewer', {
      adapterId: 'deepseek', runtimeSessionId: 'deepseek-task-19',
    })
    expect(() => registry.bindTaskWorkspace(5, 20, workspace)).toThrow('already belongs to another Task')
    registry.close()

    const reopened = await FleetRegistry.open(path)
    expect(reopened.getTaskRuntime(5, 19)).toEqual({
      projectNumber: 5,
      taskNumber: 19,
      workspace,
      sessions: {
        implementer: { adapterId: 'codex', runtimeSessionId: 'codex-task-19' },
        reviewer: { adapterId: 'deepseek', runtimeSessionId: 'deepseek-task-19' },
      },
    })
    expect(reopened.getTaskRuntime(5, 20)).toBeUndefined()
    reopened.retireTaskRuntime(5, 19)
    expect(reopened.getTaskRuntime(5, 19)).toBeUndefined()
    reopened.close()
  })

  it('rejects an invalid Task Workspace before it reaches persisted runtime state', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-invalid-runtime-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    expect(() => registry.bindTaskWorkspace(5, 19, {
      mode: 'execution', root: '', temporaryParent: '/tmp', repositoryPath: '/tmp/repository',
      source: '/tmp/origin.git', baseRevision: 'not-a-revision', branch: '',
    })).toThrow('Task Workspace')
    expect(registry.getTaskRuntime(5, 19)).toBeUndefined()
    registry.bindTaskWorkspace(5, 19, {
      mode: 'execution', root: join(parent, 'pactline-fleet-project-5-task-19'),
      temporaryParent: parent, repositoryPath: join(parent, 'pactline-fleet-project-5-task-19', 'repository'),
      source: '/tmp/origin.git', baseRevision: 'a'.repeat(40), branch: 'fleet/project-5/task-19',
    })
    const database = new Database(registry.path)
    database.prepare('UPDATE task_runtimes SET workspace_json = ? WHERE project_number = 5 AND task_number = 19')
      .run('{"mode":"execution"}')
    database.close()
    expect(() => registry.getTaskRuntime(5, 19)).toThrow('Task Workspace')
    registry.close()
  })

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
    const run = registry.admitRun('first', {
      taskNumber: 1, taskVersion: 1, stage: 'execution', frozenPolicy: { test: 'configuration-freeze' },
    }, new Date('2026-08-15T10:00:00Z'))

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
    expect((await readFile(path)).includes(Buffer.from('LOCAL_TEST_GIT'))).toBe(true)
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

    expect(registry.admitRun('replacement', {
      taskNumber: 1, taskVersion: 1, stage: 'execution', frozenPolicy: { test: 'replacement' },
    })).toMatchObject({
      fleetId: 'replacement',
      projectNumber: 5,
    })
    expect(() => registry.admitRun('first', {
      taskNumber: 2, taskVersion: 1, stage: 'execution', frozenPolicy: { test: 'retired' },
    })).toThrow('Enabled Fleet is not registered')
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
    registry.observeEffect(run.runId, 'claim', { claimId: 'claim-41', claimVersion: 1, taskVersion: 8 })
    expect(() => registry.observeEffect(run.runId, 'claim', {
      claimId: 'claim-other', claimVersion: 1, taskVersion: 8,
    })).toThrow('Observed Fleet External Effect claim is immutable')
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: 'claim-41', claimVersion: 1, claimTaskVersion: 8, checkpoint: 'claim_observed',
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
      observation: { claimId: 'claim-41', claimVersion: 1, taskVersion: 8 },
    })])
    reopened.close()
  })

  it('commits a Run transition, Effect fact, and audit event as one decision', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-decision-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const admitted = registry.admitRun('first', {
      taskNumber: 42, taskVersion: 3, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    }, new Date('2026-08-17T09:00:00.000Z'))

    const claiming = registry.commitRunDecision(admitted.runId, {
      expected: { state: admitted.state, updatedAt: admitted.updatedAt },
      transition: { state: 'claiming', update: { checkpoint: 'claim_intent' } },
      effect: {
        type: 'intent', kind: 'claim', idempotencyKey: `${admitted.runId}-claim`,
        intent: { taskNumber: 42, taskVersion: 3, stage: 'execution' },
      },
    }, new Date('2026-08-17T09:01:00.000Z'))

    expect(claiming.run).toMatchObject({ state: 'claiming', checkpoint: 'claim_intent' })
    expect(claiming.effect).toMatchObject({ kind: 'claim', status: 'intended' })
    expect(registry.listRunEvents(admitted.runId)).toEqual([
      expect.objectContaining({ eventType: 'run.admitted' }),
      expect.objectContaining({
        eventType: 'run.transitioned',
        payload: expect.objectContaining({ from: 'admitted', to: 'claiming', effect: 'claim' }),
      }),
    ])

    registry.close()
    const reopened = await FleetRegistry.open(path)
    expect(reopened.getRun(admitted.runId)).toMatchObject({ state: 'claiming' })
    expect(reopened.getEffect(admitted.runId, 'claim')).toMatchObject({ status: 'intended' })
    reopened.close()
  })

  it('rolls back Run state and Effect intent when the audit event cannot be appended', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-decision-rollback-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    let registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const admitted = registry.admitRun('first', {
      taskNumber: 43, taskVersion: 1, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })
    registry.close()
    const database = new Database(path)
    database.exec(`
      CREATE TRIGGER reject_transition_event
      BEFORE INSERT ON run_events
      WHEN NEW.event_type = 'run.transitioned'
      BEGIN
        SELECT RAISE(ABORT, 'injected event failure');
      END;
    `)
    database.close()
    registry = await FleetRegistry.open(path)

    expect(() => registry.commitRunDecision(admitted.runId, {
      expected: { state: admitted.state, updatedAt: admitted.updatedAt },
      transition: { state: 'claiming' },
      effect: {
        type: 'intent', kind: 'claim', idempotencyKey: `${admitted.runId}-claim`,
        intent: { taskNumber: 43, taskVersion: 1, stage: 'execution' },
      },
    })).toThrow('injected event failure')
    expect(registry.getRun(admitted.runId)).toMatchObject({ state: 'admitted' })
    expect(registry.getEffect(admitted.runId, 'claim')).toBeUndefined()
    expect(registry.listRunEvents(admitted.runId)).toHaveLength(1)
    registry.close()
  })

  it('does not let an external-boundary state claim an absent Effect fact', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-missing-effect-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const admitted = registry.admitRun('first', {
      taskNumber: 45, taskVersion: 1, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })

    expect(() => registry.commitRunDecision(admitted.runId, {
      expected: { state: admitted.state, updatedAt: admitted.updatedAt },
      transition: { state: 'claiming' },
    })).toThrow('requires its atomic External Effect fact')
    expect(registry.getRun(admitted.runId)).toMatchObject({ state: 'admitted' })
    expect(registry.listRunEvents(admitted.runId)).toHaveLength(1)
    registry.close()
  })

  it('rejects conflicting observations without advancing the Run or its audit history', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-observation-conflict-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const admitted = registry.admitRun('first', {
      taskNumber: 44, taskVersion: 2, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })
    const claiming = registry.commitRunDecision(admitted.runId, {
      expected: { state: admitted.state, updatedAt: admitted.updatedAt },
      transition: { state: 'claiming' },
      effect: {
        type: 'intent', kind: 'claim', idempotencyKey: `${admitted.runId}-claim`,
        intent: { taskNumber: 44, taskVersion: 2, stage: 'execution' },
      },
    }).run
    const claimed = registry.commitRunDecision(admitted.runId, {
      expected: { state: claiming.state, updatedAt: claiming.updatedAt },
      transition: {
        state: 'claimed',
        update: { claimId: 'claim-44', claimTaskVersion: 3 },
      },
      effect: {
        type: 'observation', kind: 'claim', observation: { claimId: 'claim-44', taskVersion: 3 },
      },
    }).run
    expect(claimed).toMatchObject({ state: 'claimed', claimId: 'claim-44' })
    expect(registry.getEffect(admitted.runId, 'claim')).toMatchObject({ status: 'observed' })
    const eventCount = registry.listRunEvents(admitted.runId).length

    expect(() => registry.commitRunDecision(admitted.runId, {
      expected: { state: claimed.state, updatedAt: claimed.updatedAt },
      effect: {
        type: 'observation', kind: 'claim', observation: { claimId: 'claim-other', taskVersion: 3 },
      },
    })).toThrow('Observed Fleet External Effect claim is immutable')
    expect(registry.getRun(admitted.runId)).toMatchObject({ state: 'claimed', claimId: 'claim-44' })
    expect(registry.listRunEvents(admitted.runId)).toHaveLength(eventCount)
    registry.close()
  })

  it('rejects a corrupt persisted Run through the domain decoder', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-corrupt-run-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const run = registry.admitRun('first', {
      taskNumber: 51, taskVersion: 2, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })
    registry.transitionRun(run.runId, 'admitted', 'claiming')
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: 'claim-51', claimVersion: 1, claimTaskVersion: 3,
    })
    registry.close()

    const database = new Database(path)
    database.prepare('UPDATE runs SET claim_id = NULL WHERE run_id = ?').run(run.runId)
    database.close()

    const reopened = await FleetRegistry.open(path)
    expect(() => reopened.getRun(run.runId))
      .toThrow('Fleet Run claimed requires Claim identity and post-Claim Task version')
    reopened.close()
  })

  it('rejects a corrupt persisted External Effect through the domain decoder', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-corrupt-effect-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const run = registry.admitRun('first', {
      taskNumber: 52, taskVersion: 1, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })
    registry.recordEffectIntent(run.runId, 'claim', `${run.runId}-claim`, {
      taskNumber: 52, taskVersion: 1, stage: 'execution',
    })
    registry.close()

    const database = new Database(path)
    database.prepare("UPDATE run_external_effects SET intent_json = '{}' WHERE run_id = ? AND kind = 'claim'").run(run.runId)
    database.close()

    const reopened = await FleetRegistry.open(path)
    expect(() => reopened.listEffects(run.runId)).toThrow('Fleet External Effect claim intent is invalid')
    reopened.close()
  })

  it('round-trips every supported new Run state across registry reopen', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-state-roundtrip-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    let registry = await FleetRegistry.open(path)
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const reopenAt = async (runId: string, expected: string): Promise<void> => {
      registry.close()
      registry = await FleetRegistry.open(path)
      expect(registry.getRun(runId)).toMatchObject({ state: expected })
    }

    const run = registry.admitRun('first', {
      taskNumber: 61, taskVersion: 1, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })
    await reopenAt(run.runId, 'admitted')
    registry.transitionRun(run.runId, 'admitted', 'claiming', { checkpoint: 'claim_intent' })
    await reopenAt(run.runId, 'claiming')
    registry.transitionRun(run.runId, 'claiming', 'claimed', {
      claimId: 'claim-61', claimVersion: 1, claimTaskVersion: 2,
    })
    await reopenAt(run.runId, 'claimed')
    registry.transitionRun(run.runId, 'claimed', 'preparing_workspace')
    await reopenAt(run.runId, 'preparing_workspace')
    registry.transitionRun(run.runId, 'preparing_workspace', 'starting_harness', {
      workspace: { repositoryPath: '/tmp/fleet-61' },
    })
    await reopenAt(run.runId, 'starting_harness')
    registry.transitionRun(run.runId, 'starting_harness', 'running_harness', {
      runtimeSessionId: 'session-61',
    })
    await reopenAt(run.runId, 'running_harness')
    registry.recordEffectIntent(run.runId, 'harness_result', `${run.runId}-result`, {})
    registry.observeEffect(run.runId, 'harness_result', {
      terminalState: 'completed', runtimeSessionId: 'session-61',
      result: { proposal: { kind: 'execution', recommendation: 'complete' } },
      baseline: { head: 'a'.repeat(40), changedPaths: [], porcelain: '' },
    })
    registry.transitionRun(run.runId, 'running_harness', 'validating')
    await reopenAt(run.runId, 'validating')
    registry.transitionRun(run.runId, 'validating', 'delivering')
    await reopenAt(run.runId, 'delivering')
    registry.transitionRun(run.runId, 'delivering', 'settling')
    await reopenAt(run.runId, 'settling')
    registry.transitionRun(run.runId, 'settling', 'completed', { disposition: 'execution_completed' })
    await reopenAt(run.runId, 'completed')

    const released = registry.admitRun('first', {
      taskNumber: 62, taskVersion: 1, stage: 'execution', frozenPolicy: { route: { adapter: 'codex' } },
    })
    registry.transitionRun(released.runId, 'admitted', 'releasing')
    await reopenAt(released.runId, 'releasing')
    registry.transitionRun(released.runId, 'releasing', 'released', { disposition: 'recovery_release' })
    await reopenAt(released.runId, 'released')

    const quarantined = registry.admitRun('first', {
      taskNumber: 63, taskVersion: 1, stage: 'review', frozenPolicy: { route: { adapter: 'codex' } },
    })
    registry.transitionRun(quarantined.runId, 'admitted', 'quarantined', { disposition: 'ambiguous_effect' })
    await reopenAt(quarantined.runId, 'quarantined')
    registry.close()
  })

  it('preserves policy-free and failed rows for legacy observation without allowing new creation', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-legacy-run-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const path = join(state, 'fleet.sqlite3')
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work') })
    const snapshot = parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] })
    const registry = await FleetRegistry.open(path)
    registry.recordConfiguration(snapshot)
    const serviceId = registry.serviceId
    registry.close()

    const database = new Database(path)
    database.prepare(`
      INSERT INTO runs(
        run_id, service_id, fleet_id, project_number, config_revision, state, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, 'failed', ?, ?)
    `).run('legacy-run', serviceId, 'first', 5, snapshot.revision, snapshot.loadedAt, snapshot.loadedAt)
    database.close()

    const reopened = await FleetRegistry.open(path)
    expect(reopened.getRun('legacy-run')).toMatchObject({ runId: 'legacy-run', state: 'failed', legacy: true })
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

  it('lists bounded Run history and causal events for observation', async () => {
    const parent = await mkdtemp(join(tmpdir(), 'fleet-registry-history-'))
    directories.push(parent)
    const state = await ensurePrivateDirectory(join(parent, 'state'))
    const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
    const source = serviceConfigYAML({ stateDirectory: state, firstWorkspace: join(parent, 'work', 'first') })
    registry.recordConfiguration(parseFleetConfig(source, join(parent, 'fleet.yml'), { knownAdapterIds: ['codex'] }))
    const first = registry.admitRun('first', { taskNumber: 2, taskVersion: 1, stage: 'execution', frozenPolicy: {} }, new Date('2026-08-16T10:00:00Z'))
    registry.transitionRun(first.runId, 'admitted', 'released', {
      checkpoint: 'candidate_changed', disposition: 'candidate_changed',
    }, new Date('2026-08-16T10:01:00Z'))
    registry.admitRun('first', { taskNumber: 3, taskVersion: 1, stage: 'review', frozenPolicy: {} }, new Date('2026-08-16T10:02:00Z'))

    expect(registry.listRuns({ limit: 1 })).toEqual([expect.objectContaining({ taskNumber: 3 })])
    expect(registry.listRuns({ state: 'released' })).toEqual([expect.objectContaining({ taskNumber: 2 })])
    expect(registry.listRunEvents(first.runId)).toEqual([
      expect.objectContaining({ eventType: 'run.admitted' }),
      expect.objectContaining({ eventType: 'run.transitioned', payload: expect.objectContaining({ to: 'released' }) }),
    ])
    registry.close()
  })
})
