import { describe, expect, it } from 'vitest'
import { decodeExternalEffect } from '../../src/run/external-effect.js'
import { admitRun, decodeRun, terminalDisposition, transitionRun } from '../../src/run/run.js'

const identity = {
  runId: 'run-1',
  serviceId: 'service-1',
  fleetId: 'fleet-1',
  projectNumber: 5,
  configRevision: 'config-1',
}

const admission = {
  taskNumber: 21,
  taskVersion: 3,
  stage: 'execution' as const,
  frozenPolicy: { route: { adapter: 'codex' } },
}

describe('Run domain model', () => {
  it('owns the allowed state transitions', () => {
    const admitted = admitRun(identity, admission, '2026-08-17T09:00:00.000Z')

    expect(transitionRun(admitted, 'claiming', { checkpoint: 'claim_intent' }, '2026-08-17T09:01:00.000Z'))
      .toMatchObject({ state: 'claiming', checkpoint: 'claim_intent' })
  })

  it.each(['running_harness', 'failed'] as const)(
    'rejects the invalid admitted -> %s transition',
    next => {
      const admitted = admitRun(identity, admission, '2026-08-17T09:00:00.000Z')
      expect(() => transitionRun(admitted, next, {}, '2026-08-17T09:01:00.000Z'))
        .toThrow(`Invalid Fleet Run transition: admitted -> ${next}`)
    },
  )

  it('rejects persisted states that are missing their required facts', () => {
    const admitted = admitRun(identity, admission, '2026-08-17T09:00:00.000Z')

    expect(() => decodeRun({ ...admitted, state: 'claimed' }))
      .toThrow('Fleet Run claimed requires Claim identity and post-Claim Task version')
    expect(() => decodeRun({
      ...admitted,
      state: 'running_harness',
      claimId: 'claim-1',
      claimVersion: 1,
      claimTaskVersion: 4,
    })).toThrow('Fleet Run running_harness requires a persisted workspace')
    expect(decodeRun({
      ...admitted,
      state: 'running_harness',
      claimId: 'claim-1',
      claimVersion: 1,
      claimTaskVersion: 4,
      workspace: { repositoryPath: '/tmp/work' },
      runtimeSessionId: 'session-1',
    })).toMatchObject({ state: 'running_harness', claimId: 'claim-1', runtimeSessionId: 'session-1' })
  })

  it('requires an observed complete Harness result for delivery', () => {
    const delivering = {
      ...admitRun(identity, admission, '2026-08-17T09:00:00.000Z'),
      state: 'delivering',
      claimId: 'claim-1',
      claimVersion: 1,
      claimTaskVersion: 4,
      workspace: { repositoryPath: '/tmp/work' },
      runtimeSessionId: 'session-1',
    }

    expect(() => decodeRun(delivering)).toThrow('Fleet Run delivering requires an observed Harness result')
    const harnessResult = decodeExternalEffect({
      runId: 'run-1', kind: 'harness_result', idempotencyKey: 'run-1-result', status: 'observed', intent: {},
      observation: {
        terminalState: 'completed', runtimeSessionId: 'session-1',
        result: { proposal: { kind: 'execution', recommendation: 'complete' } },
      },
      createdAt: '2026-08-17T09:01:00.000Z', updatedAt: '2026-08-17T09:02:00.000Z',
    })

    expect(decodeRun(delivering, [harnessResult])).toMatchObject({ state: 'delivering' })
  })

  it('represents terminal disposition by outcome kind', () => {
    const admitted = admitRun(identity, admission, '2026-08-17T09:00:00.000Z')
    expect(() => decodeRun({ ...admitted, state: 'released' }))
      .toThrow('Fleet Run released requires a terminal disposition')
    const released = transitionRun(
      admitted,
      'released',
      { disposition: 'candidate_changed' },
      '2026-08-17T09:01:00.000Z',
    )

    expect(terminalDisposition(released)).toEqual({ kind: 'local_release', reason: 'candidate_changed' })
  })

  it('keeps Claim, workspace, and Adapter Session facts immutable', () => {
    const running = decodeRun({
      ...admitRun(identity, admission, '2026-08-17T09:00:00.000Z'),
      state: 'running_harness', claimId: 'claim-1', claimVersion: 1, claimTaskVersion: 4,
      workspace: { repositoryPath: '/tmp/work' }, runtimeSessionId: 'session-1',
    })

    expect(() => transitionRun(running, 'releasing', { claimId: 'claim-2' }, '2026-08-17T09:01:00.000Z'))
      .toThrow('Fleet Run Claim identity is immutable')
    expect(() => transitionRun(running, 'releasing', { claimTaskVersion: 5 }, '2026-08-17T09:01:00.000Z'))
      .toThrow('Fleet Run post-Claim Task version is immutable')
    expect(() => transitionRun(running, 'releasing', { workspace: { repositoryPath: '/tmp/other' } }, '2026-08-17T09:01:00.000Z'))
      .toThrow('Fleet Run workspace is immutable')
    expect(() => transitionRun(running, 'releasing', { runtimeSessionId: 'session-2' }, '2026-08-17T09:01:00.000Z'))
      .toThrow('Fleet Run Adapter Session identity is immutable')
  })
})
