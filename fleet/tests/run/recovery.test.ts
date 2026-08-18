import { describe, expect, it } from 'vitest'
import { decideRunRecovery } from '../../src/run/recovery.js'
import type { RunRecoveryFacts } from '../../src/run/recovery.js'

function facts(overrides: Partial<RunRecoveryFacts>): RunRecoveryFacts {
  return {
    state: 'admitted',
    claimAuthority: { kind: 'not_read' },
    sessionResumable: false,
    hasSettlementIntent: false,
    ...overrides,
  }
}

describe('Run recovery policy', () => {
  it.each([
    [facts({ state: 'admitted' }), { kind: 'release_local', reason: 'recovered_before_claim' }],
    [facts({ state: 'claiming' }), { kind: 'reconcile_claim' }],
    [facts({
      state: 'claimed', claimAuthority: { kind: 'active', identityMatches: true },
    }), { kind: 'restore_workspace' }],
    [facts({
      state: 'preparing_workspace', claimAuthority: { kind: 'active', identityMatches: true },
    }), { kind: 'restore_workspace' }],
    [facts({
      state: 'starting_harness', claimAuthority: { kind: 'active', identityMatches: true },
    }), { kind: 'quarantine', reason: 'adapter_session_intent_cannot_be_reconciled' }],
    [facts({
      state: 'running_harness', claimAuthority: { kind: 'active', identityMatches: true }, sessionResumable: true,
    }), { kind: 'resume_harness' }],
    [facts({
      state: 'running_harness', claimAuthority: { kind: 'active', identityMatches: true }, sessionResumable: false,
    }), { kind: 'release_claim', reason: 'adapter_session_not_resumable' }],
    [facts({
      state: 'validating', claimAuthority: { kind: 'active', identityMatches: true }, sessionResumable: true,
    }), { kind: 'revalidate_result' }],
    [facts({
      state: 'delivering', claimAuthority: { kind: 'active', identityMatches: true }, sessionResumable: true,
    }), { kind: 'reconcile_delivery' }],
    [facts({
      state: 'settling', claimAuthority: { kind: 'active', identityMatches: true }, hasSettlementIntent: true,
    }), { kind: 'replay_settlement' }],
    [facts({
      state: 'releasing', claimAuthority: { kind: 'active', identityMatches: true },
    }), { kind: 'reconcile_release' }],
    [facts({ state: 'completed' }), { kind: 'no_action' }],
  ] as const)('selects a deterministic action for %#', (input, expected) => {
    expect(decideRunRecovery(input)).toEqual(expected)
  })

  it('finishes from matching terminal authority before considering local continuation', () => {
    expect(decideRunRecovery(facts({
      state: 'settling',
      claimAuthority: { kind: 'terminal', identityMatches: true, status: 'completed' },
      hasSettlementIntent: true,
    }))).toEqual({ kind: 'finish_terminal', terminal: 'completed', reason: 'claim_reconciled_completed' })
  })

  it.each([
    { kind: 'unavailable' } as const,
    { kind: 'active', identityMatches: false } as const,
    { kind: 'terminal', identityMatches: false, status: 'released' } as const,
  ])('quarantines uncertain or contradictory Claim authority %#', claimAuthority => {
    expect(decideRunRecovery(facts({ state: 'validating', claimAuthority }))).toMatchObject({ kind: 'quarantine' })
  })

  it('requires the exact persisted settlement intent before replay', () => {
    expect(decideRunRecovery(facts({
      state: 'settling',
      claimAuthority: { kind: 'active', identityMatches: true },
      hasSettlementIntent: false,
    }))).toEqual({ kind: 'quarantine', reason: 'settlement_intent_missing' })
  })
})
