import { describe, expect, it } from 'vitest'
import type { PactlineCallOptions } from '../../src/pactline/client.js'
import { PactlineClientError } from '../../src/pactline/client.js'
import { settleReview } from '../../src/pactline/settlement.js'
import type { ReviewProposal } from '../../src/core/harness-result.js'
import type { PactlineClaimMutationResult, PactlineOperation } from '../../src/pactline/types.js'
import { InMemoryPactlineClient } from '../contract/in-memory-pactline.js'

function proposal(claimId: string): ReviewProposal {
  return {
    schemaVersion: 1, kind: 'review', runId: 'run-1', claimId, taskNumber: 31,
    recommendation: 'accept', summary: 'Accepted.', findings: [], verification: [], criteria: [], limitations: [],
  }
}

class UncertainAfterCommitClient extends InMemoryPactlineClient {
  acceptCalls = 0
  showCalls = 0

  override showClaim(claimId: string) {
    this.showCalls += 1
    return super.showClaim(claimId)
  }

  override async acceptClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    this.acceptCalls += 1
    const committed = await super.acceptClaim(claimId, taskVersion, message, options)
    if (this.acceptCalls === 1) throw new PactlineClientError('TIMEOUT', 'response lost after commit')
    return committed
  }
}

class UncertainBeforeCommitClient extends InMemoryPactlineClient {
  acceptCalls = 0
  showCalls = 0

  override showClaim(claimId: string) {
    this.showCalls += 1
    return super.showClaim(claimId)
  }

  override acceptClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    this.acceptCalls += 1
    if (this.acceptCalls === 1) return Promise.reject(new PactlineClientError('TIMEOUT', 'request outcome unknown'))
    return super.acceptClaim(claimId, taskVersion, message, options)
  }
}

async function activeReview(client: InMemoryPactlineClient) {
  return (await client.claimTask(31, client.version, 'review', { sessionId: 'fleet', idempotencyKey: 'claim' })).data.claim
}

describe('Pactline settlement reconciliation', () => {
  it('re-reads authority and does not retry a terminal mutation already committed', async () => {
    const client = new UncertainAfterCommitClient(31, [], { phase: 'in_review', activity: 'available' })
    const claim = await activeReview(client)
    const taskVersion = client.version
    const settled = await settleReview(client, proposal(claim.id), {
      taskNumber: 31, claimId: claim.id, taskVersion, stage: 'review', sessionId: 'fleet', idempotencyKey: 'settle',
    })
    expect(settled).toMatchObject({ task: { phase: 'done' }, claim: { outcome: 'task_accepted' } })
    expect(client.acceptCalls).toBe(1)
    expect(client.showCalls).toBe(2)
  })

  it('re-reads exact active authority before retrying with the same idempotency key', async () => {
    const client = new UncertainBeforeCommitClient(31, [], { phase: 'in_review', activity: 'available' })
    const claim = await activeReview(client)
    const taskVersion = client.version
    await settleReview(client, proposal(claim.id), {
      taskNumber: 31, claimId: claim.id, taskVersion, stage: 'review', sessionId: 'fleet', idempotencyKey: 'settle',
    })
    expect(client.acceptCalls).toBe(2)
    expect(client.showCalls).toBe(2)
    const accepts = client.mutations.filter(item => item.operation === 'accept')
    expect(accepts).toHaveLength(1)
    expect(accepts[0]?.idempotencyKey).toBe('settle-accept')
  })
})
