import { describe, expect, it } from 'vitest'
import {
  createExternalEffectIntent,
  decodeExternalEffect,
  effectUncertainty,
  observeExternalEffect,
  replayExternalEffectIntent,
} from '../../src/run/external-effect.js'

describe('External Effect domain model', () => {
  it('accepts only the closed set of Fleet effects', () => {
    expect(decodeExternalEffect({
      runId: 'run-1',
      kind: 'claim',
      idempotencyKey: 'run-1-claim',
      status: 'intended',
      intent: { taskNumber: 21, taskVersion: 3, stage: 'execution' },
      createdAt: '2026-08-17T09:00:00.000Z',
      updatedAt: '2026-08-17T09:00:00.000Z',
    })).toMatchObject({ kind: 'claim', status: 'intended' })
    expect(() => decodeExternalEffect({
      runId: 'run-1',
      kind: 'unknown_effect',
      idempotencyKey: 'run-1-unknown',
      status: 'intended',
      intent: {},
      createdAt: '2026-08-17T09:00:00.000Z',
      updatedAt: '2026-08-17T09:00:00.000Z',
    })).toThrow('Fleet External Effect kind is invalid')
  })

  it('makes an observed effect immutable while allowing idempotent replay', () => {
    const intended = decodeExternalEffect({
      runId: 'run-1',
      kind: 'adapter_session',
      idempotencyKey: 'run-1-session',
      status: 'intended',
      intent: { adapter: 'codex' },
      createdAt: '2026-08-17T09:00:00.000Z',
      updatedAt: '2026-08-17T09:00:00.000Z',
    })
    const observed = observeExternalEffect(
      intended,
      { runtimeSessionId: 'session-1' },
      '2026-08-17T09:01:00.000Z',
    )

    expect(observeExternalEffect(
      observed,
      { runtimeSessionId: 'session-1' },
      '2026-08-17T09:02:00.000Z',
    )).toEqual(observed)
    expect(() => observeExternalEffect(
      observed,
      { runtimeSessionId: 'session-2' },
      '2026-08-17T09:02:00.000Z',
    )).toThrow('Observed Fleet External Effect adapter_session is immutable')
  })

  it('validates the typed intent and observation payloads at persistence boundaries', () => {
    expect(() => decodeExternalEffect({
      runId: 'run-1', kind: 'claim', idempotencyKey: 'run-1-claim', status: 'intended', intent: {},
      createdAt: '2026-08-17T09:00:00.000Z', updatedAt: '2026-08-17T09:00:00.000Z',
    })).toThrow('Fleet External Effect claim intent is invalid')
    expect(() => decodeExternalEffect({
      runId: 'run-1', kind: 'adapter_session', idempotencyKey: 'run-1-session', status: 'observed',
      intent: { adapter: 'codex' }, observation: {},
      createdAt: '2026-08-17T09:00:00.000Z', updatedAt: '2026-08-17T09:00:00.000Z',
    })).toThrow('Fleet External Effect adapter_session observation is invalid')
    expect(() => decodeExternalEffect({
      runId: 'run-1', kind: 'pactline_settlement', idempotencyKey: 'run-1-settle', status: 'intended',
      intent: {},
      createdAt: '2026-08-17T09:00:00.000Z', updatedAt: '2026-08-17T09:00:00.000Z',
    })).toThrow('Fleet External Effect pactline_settlement intent is invalid')
  })

  it('persists the exact immutable settlement action for recovery replay', () => {
    const proposal = {
      schemaVersion: 1 as const,
      kind: 'execution' as const,
      runId: 'run-1',
      claimId: 'claim-1',
      taskNumber: 21,
      recommendation: 'unable_to_complete' as const,
      summary: 'Unable.',
      verification: [],
      criteria: [],
      limitations: [],
      changedPaths: [],
    }
    const intended = createExternalEffectIntent(
      'run-1',
      'pactline_settlement',
      'run-1-settle',
      { stage: 'execution', taskVersion: 4, proposal },
      '2026-08-17T09:00:00.000Z',
    )

    expect(intended.intent).toEqual({ stage: 'execution', taskVersion: 4, proposal })
    expect(() => replayExternalEffectIntent(
      intended,
      'run-1-settle',
      { stage: 'execution', taskVersion: 5, proposal },
    )).toThrow('Fleet External Effect pactline_settlement intent is immutable')
  })

  it('accepts an intent replay only when its typed identity and facts are unchanged', () => {
    const intended = createExternalEffectIntent(
      'run-1',
      'claim',
      'run-1-claim',
      { taskNumber: 21, taskVersion: 3, stage: 'execution' },
      '2026-08-17T09:00:00.000Z',
    )

    expect(replayExternalEffectIntent(
      intended,
      'run-1-claim',
      { stage: 'execution', taskVersion: 3, taskNumber: 21 },
    )).toBe(intended)
    expect(() => replayExternalEffectIntent(
      intended,
      'run-1-other',
      { taskNumber: 21, taskVersion: 3, stage: 'execution' },
    )).toThrow('Fleet External Effect claim intent is immutable')
    expect(() => replayExternalEffectIntent(
      intended,
      'run-1-claim',
      { taskNumber: 21, taskVersion: 4, stage: 'execution' },
    )).toThrow('Fleet External Effect claim intent is immutable')
  })

  it('owns uncertainty classification in the typed effect model', () => {
    expect(effectUncertainty('claim')).toBe('reconcilable')
    expect(effectUncertainty('workspace')).toBe('local')
    expect(effectUncertainty('git_push')).toBe('ambiguous_external')
    expect(effectUncertainty('pactline_settlement')).toBe('ambiguous_external')
  })
})
