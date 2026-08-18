import { isTerminalRunState } from './run.js'
import type { FleetRunState } from './run.js'

export type RecoveryClaimAuthority =
  | { readonly kind: 'not_read' }
  | { readonly kind: 'unavailable' }
  | { readonly kind: 'active'; readonly identityMatches: boolean }
  | {
      readonly kind: 'terminal'
      readonly identityMatches: boolean
      readonly status: 'completed' | 'released'
    }

export interface RunRecoveryFacts {
  readonly state: FleetRunState
  readonly claimAuthority: RecoveryClaimAuthority
  readonly sessionResumable: boolean
  readonly hasSettlementIntent: boolean
}

export type RunRecoveryDecision =
  | { readonly kind: 'no_action' }
  | { readonly kind: 'release_local'; readonly reason: string }
  | { readonly kind: 'reconcile_claim' }
  | { readonly kind: 'restore_workspace' }
  | { readonly kind: 'resume_harness' }
  | { readonly kind: 'release_claim'; readonly reason: string }
  | { readonly kind: 'revalidate_result' }
  | { readonly kind: 'reconcile_delivery' }
  | { readonly kind: 'replay_settlement' }
  | { readonly kind: 'reconcile_release' }
  | { readonly kind: 'finish_terminal'; readonly terminal: 'completed' | 'released'; readonly reason: string }
  | { readonly kind: 'quarantine'; readonly reason: string }

/** Select one recovery instruction without performing I/O or changing the Run. */
export function decideRunRecovery(facts: RunRecoveryFacts): RunRecoveryDecision {
  if (isTerminalRunState(facts.state)) return { kind: 'no_action' }
  if (facts.state === 'admitted') return { kind: 'release_local', reason: 'recovered_before_claim' }
  if (facts.state === 'claiming') return { kind: 'reconcile_claim' }

  const authority = facts.claimAuthority
  if (authority.kind === 'not_read') return { kind: 'quarantine', reason: 'claim_authority_not_read' }
  if (authority.kind === 'unavailable') return { kind: 'quarantine', reason: 'claim_authority_unavailable' }
  if (!authority.identityMatches) return { kind: 'quarantine', reason: 'claim_authority_contradiction' }
  if (authority.kind === 'terminal') {
    return {
      kind: 'finish_terminal',
      terminal: authority.status,
      reason: `claim_reconciled_${authority.status}`,
    }
  }

  switch (facts.state) {
    case 'claimed':
    case 'preparing_workspace': return { kind: 'restore_workspace' }
    case 'starting_harness': return { kind: 'quarantine', reason: 'adapter_session_intent_cannot_be_reconciled' }
    case 'running_harness': return facts.sessionResumable
      ? { kind: 'resume_harness' }
      : { kind: 'release_claim', reason: 'adapter_session_not_resumable' }
    case 'validating': return { kind: 'revalidate_result' }
    case 'delivering': return { kind: 'reconcile_delivery' }
    case 'settling': return facts.hasSettlementIntent
      ? { kind: 'replay_settlement' }
      : { kind: 'quarantine', reason: 'settlement_intent_missing' }
    case 'releasing': return { kind: 'reconcile_release' }
    case 'completed':
    case 'released':
    case 'quarantined':
    case 'failed': throw new Error(`Fleet recovery policy did not terminate for ${facts.state}`)
  }
}
