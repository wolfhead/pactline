import { describe, expect, it } from 'vitest'
import type { PactlineCallOptions } from '../../src/pactline/client.js'
import { PactlineClientError } from '../../src/pactline/client.js'
import { replaySettlement, settleExecution, settleReview } from '../../src/pactline/settlement.js'
import type { ExecutionProposal, ReviewProposal } from '../../src/core/harness-result.js'
import type { PactlineClaimMutationResult, PactlineCodeChangeMutationResult, PactlineOperation } from '../../src/pactline/types.js'
import { InMemoryPactlineClient } from '../contract/in-memory-pactline.js'

function proposal(claimId: string): ReviewProposal {
  return {
    schemaVersion: 1, kind: 'review', runId: 'run-1', claimId, taskNumber: 31,
    recommendation: 'accept', summary: 'Accepted.', findings: [], verification: [], criteria: [], limitations: [],
  }
}

function executionProposal(claimId: string): ExecutionProposal {
  return {
    schemaVersion: 1, kind: 'execution', runId: 'run-execution', claimId, taskNumber: 31,
    recommendation: 'complete', summary: 'Delivered.', changedPaths: ['README.md'],
    verification: [], criteria: [], limitations: [],
  }
}

const delivery = {
  repository: { provider: 'github' as const, host: 'github.com', owner: 'wolfhead', name: 'pactline' },
  codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/77',
  revision: 'a'.repeat(40), branch: 'fleet/project-5/task-31',
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

class UncertainLinkAfterCommitClient extends InMemoryPactlineClient {
  linkCalls = 0

  override async linkCodeChange(claimId: string, taskVersion: number, url: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineCodeChangeMutationResult>> {
    this.linkCalls += 1
    const committed = await super.linkCodeChange(claimId, taskVersion, url, options)
    if (this.linkCalls === 1) throw new PactlineClientError('TIMEOUT', 'link response lost after commit')
    return committed
  }
}

class UncertainSubmissionAfterCommitClient extends InMemoryPactlineClient {
  submitCalls = 0

  override async submitClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    this.submitCalls += 1
    const committed = await super.submitClaim(claimId, taskVersion, message, options)
    if (this.submitCalls === 1) throw new PactlineClientError('TIMEOUT', 'submission response lost after commit')
    return committed
  }
}

async function activeReview(client: InMemoryPactlineClient) {
  return (await client.claimTask(31, client.version, 'review', { sessionId: 'fleet', idempotencyKey: 'claim' })).data.claim
}

async function activeExecution(client: InMemoryPactlineClient) {
  return (await client.claimTask(31, client.version, 'execution', { sessionId: 'fleet', idempotencyKey: 'claim' })).data.claim
}

describe('Pactline settlement reconciliation', () => {
  it('replays a persisted settlement after the delivery link already advanced the Task', async () => {
    const client = new InMemoryPactlineClient(31, [], { phase: 'ready', activity: 'available' })
    const claim = await activeExecution(client)
    const admittedVersion = client.version
    await client.linkCodeChange(claim.id, admittedVersion, delivery.codeChangeUrl, {
      sessionId: 'fleet', idempotencyKey: 'lost-link',
    })

    const settled = await replaySettlement(client, {
      stage: 'execution', taskVersion: admittedVersion,
      proposal: executionProposal(claim.id), delivery,
    }, {
      taskNumber: 31, claimId: claim.id, taskVersion: admittedVersion,
      stage: 'execution', sessionId: 'fleet', idempotencyKey: 'persisted-settlement',
    })

    expect(settled.task.phase).toBe('in_review')
    expect(client.codeChanges).toEqual(new Set([delivery.codeChangeUrl]))
    expect(client.mainThreadItems).toHaveLength(1)
  })

  it('reuses one delivery link through request changes, correction, and second review', async () => {
    const client = new InMemoryPactlineClient(31, [], { phase: 'ready', activity: 'available' })
    const execution = await activeExecution(client)
    await settleExecution(client, executionProposal(execution.id), {
      taskNumber: 31, claimId: execution.id, taskVersion: client.version,
      stage: 'execution', sessionId: 'fleet', idempotencyKey: 'first-execution',
    }, delivery)

    const firstReview = await activeReview(client)
    await settleReview(client, {
      ...proposal(firstReview.id), recommendation: 'request_changes', summary: 'Correction required.',
      findings: [{ path: 'README.md', line: 1, severity: 'major', explanation: 'Update the delivery.' }],
    }, {
      taskNumber: 31, claimId: firstReview.id, taskVersion: client.version,
      stage: 'review', sessionId: 'fleet', idempotencyKey: 'first-review',
    })

    const correction = await activeExecution(client)
    await settleExecution(client, executionProposal(correction.id), {
      taskNumber: 31, claimId: correction.id, taskVersion: client.version,
      stage: 'execution', sessionId: 'fleet', idempotencyKey: 'correction',
    }, delivery)
    const secondReview = await activeReview(client)
    const accepted = await settleReview(client, proposal(secondReview.id), {
      taskNumber: 31, claimId: secondReview.id, taskVersion: client.version,
      stage: 'review', sessionId: 'fleet', idempotencyKey: 'second-review',
    })

    expect(accepted.task.phase).toBe('done')
    expect(client.codeChanges).toEqual(new Set([delivery.codeChangeUrl]))
    expect(client.mainThreadItems).toHaveLength(2)
    expect(client.mutations.filter(item => item.operation === 'link')).toHaveLength(2)
  })

  it('reconciles an uncertain code-change link through the same active Claim', async () => {
    const client = new UncertainLinkAfterCommitClient(31, [], { phase: 'ready', activity: 'available' })
    const claim = await activeExecution(client)
    const settled = await settleExecution(client, executionProposal(claim.id), {
      taskNumber: 31, claimId: claim.id, taskVersion: client.version,
      stage: 'execution', sessionId: 'fleet', idempotencyKey: 'settle',
    }, delivery)

    expect(settled).toMatchObject({ task: { phase: 'in_review' }, claim: { outcome: 'execution_completed' } })
    expect(client.linkCalls).toBe(2)
    expect(client.codeChanges).toEqual(new Set([delivery.codeChangeUrl]))
    expect(client.mainThreadItems).toHaveLength(1)
  })

  it('does not duplicate a work submission whose response was lost', async () => {
    const client = new UncertainSubmissionAfterCommitClient(31, [], { phase: 'ready', activity: 'available' })
    const claim = await activeExecution(client)
    const settled = await settleExecution(client, executionProposal(claim.id), {
      taskNumber: 31, claimId: claim.id, taskVersion: client.version,
      stage: 'execution', sessionId: 'fleet', idempotencyKey: 'settle',
    }, delivery)

    expect(settled).toMatchObject({ task: { phase: 'in_review' } })
    expect(client.submitCalls).toBe(1)
    expect(client.mainThreadItems).toHaveLength(1)
  })

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
