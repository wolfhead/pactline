import { execFile } from 'node:child_process'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { ReplayHarnessAdapter } from '../../src/adapters/replay/replay-adapter.js'
import { continueClaimStageAfterHarness, runCandidateImport, runClaimStage } from '../../src/core/claim-stage.js'
import type { ExecutionProposal, HarnessRunResult, ReviewProposal } from '../../src/core/harness-result.js'
import { StaticRuntimeRouter } from '../../src/core/runtime-router.js'
import type { RuntimeRoutes } from '../../src/core/runtime-router.js'
import type { FleetWorkDefinition } from '../../src/evaluation/corpus.js'
import type { RepositoryDelivery } from '../../src/repository/delivery.js'
import { resolveTypedIssue } from '../../src/pactline/settlement.js'
import type { ResolvedIssueAuthority } from '../../src/pactline/settlement.js'
import { prepareWorkspace, removeWorkspace } from '../../src/repository/workspace.js'
import type { FleetWorkspace, WorkspaceMode } from '../../src/repository/workspace.js'
import { InMemoryPactlineClient } from '../contract/in-memory-pactline.js'

const exec = promisify(execFile)
const criterion = { id: 'criterion-1', revision: 1 }
const delivery: RepositoryDelivery = {
  repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
  codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/100', revision: 'b'.repeat(40), branch: 'fleet/run/test',
}

function routes(): RuntimeRoutes {
  const route = { adapterId: 'replay', model: 'quality-max', reasoning: 'max', promptVersion: 'm1', resultContractVersion: 1 }
  return { execution: route, correction: route, review: route, resolution_analysis: route }
}

function result(proposal: ExecutionProposal | ReviewProposal, sessionId: string): HarnessRunResult {
  return {
    adapterId: 'replay', adapterVersion: '1.0.0', runtimeSessionId: sessionId,
    model: { provider: 'replay', model: 'quality-max', reasoning: 'max' }, terminalState: 'completed', proposal,
    usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
  }
}

function executionProposal(request: { runId: string; claimId: string }, recommendation: ExecutionProposal['recommendation'] = 'complete'): ExecutionProposal {
  return {
    schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId, taskNumber: 21,
    recommendation, summary: recommendation === 'complete' ? 'Implemented.' : 'Needs a decision.',
    changedPaths: recommendation === 'complete' ? ['README.md'] : [],
    verification: recommendation === 'complete' ? [{ command: 'true', outcome: 'passed', summary: 'passed' }] : [],
    criteria: [{ criterionId: criterion.id, criterionRevision: criterion.revision, outcome: recommendation === 'complete' ? 'passed' : 'unable', evidence: 'Replay evidence.' }],
    limitations: [],
    ...(recommendation === 'request_resolution' ? { resolutionRequest: { issueType: 'decision_required' as const, request: 'Choose the required behavior.' } } : {}),
  }
}

function reviewProposal(request: { runId: string; claimId: string }, recommendation: ReviewProposal['recommendation']): ReviewProposal {
  const changes = recommendation === 'request_changes'
  return {
    schemaVersion: 1, kind: 'review', runId: request.runId, claimId: request.claimId, taskNumber: 21,
    recommendation, summary: changes ? 'Correction required.' : 'Accepted.',
    findings: changes ? [{ path: 'README.md', line: 1, severity: 'high', category: 'correctness', evidence: 'Baseline text.', explanation: 'Expected corrected text.' }] : [],
    verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
    criteria: [{ criterionId: criterion.id, criterionRevision: criterion.revision, outcome: changes ? 'failed' : 'passed', evidence: 'Reviewed.' }],
    limitations: [],
  }
}

describe('Harness-neutral Claim-stage workflows', () => {
  let directory: string
  let origin: string
  let revision: string
  let definition: FleetWorkDefinition
  const workspaces: FleetWorkspace[] = []

  beforeEach(async () => {
    directory = await mkdtemp(join(tmpdir(), 'pactline-fleet-stage-test-'))
    origin = join(directory, 'origin.git')
    const seed = join(directory, 'seed')
    await mkdir(seed)
    await exec('git', ['init', '--quiet', '--bare', origin])
    await exec('git', ['init', '--quiet', seed])
    await writeFile(join(seed, 'README.md'), 'baseline\n')
    await exec('git', ['-C', seed, 'add', 'README.md'])
    await exec('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'baseline'])
    revision = (await exec('git', ['-C', seed, 'rev-parse', 'HEAD'])).stdout.trim()
    await exec('git', ['-C', seed, 'branch', '-M', 'main'])
    await exec('git', ['-C', seed, 'remote', 'add', 'origin', origin])
    await exec('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])
    definition = {
      caseId: 'M1-direct', taskNumber: 21, taskVersion: 1,
      base: { source: origin, ref: 'refs/heads/main', revision },
      repository: delivery.repository, allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [criterion],
    }
  })

  afterEach(async () => {
    for (const workspace of workspaces) await removeWorkspace(workspace).catch(() => undefined)
    await rm(directory, { recursive: true, force: true })
  })

  async function workspace(mode: WorkspaceMode, runId: string): Promise<FleetWorkspace> {
    const value = await prepareWorkspace({ input: definition.base, mode, runId, temporaryDirectory: directory })
    workspaces.push(value)
    return value
  }

  async function execute(client: InMemoryPactlineClient, stage: 'execution' | 'correction', runId: string, resolutionAuthority?: ResolvedIssueAuthority) {
    const adapter = new ReplayHarnessAdapter([{
      sessionId: `${runId}-session`,
      effect: async request => { await writeFile(join(request.workspace, 'README.md'), `${runId}\n`) },
      result: request => result(executionProposal(request), `${runId}-session`),
    }])
    return runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage, taskVersion: client.version,
      runId, clientSessionId: 'fleet-test', idempotencyKey: runId, workspace: await workspace('execution', runId),
      deadline: '2026-08-16T00:00:00Z', publishDelivery: async () => delivery,
      ...(resolutionAuthority === undefined ? {} : { resolutionAuthority }),
    })
  }

  async function review(
    client: InMemoryPactlineClient,
    runId: string,
    recommendation: ReviewProposal['recommendation'],
    resolutionAuthority?: ResolvedIssueAuthority,
  ) {
    const adapter = new ReplayHarnessAdapter([{
      sessionId: `${runId}-session`,
      result: request => {
        const proposal = reviewProposal(request, recommendation)
        return result(resolutionAuthority !== undefined ? {
          ...proposal,
          criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'waived', evidence: 'Explicitly superseded.' }],
        } : proposal, `${runId}-session`)
      },
    }])
    return runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'review', taskVersion: client.version,
      runId, clientSessionId: 'fleet-test', idempotencyKey: runId, workspace: await workspace('review', runId),
      deadline: '2026-08-16T00:00:00Z', ...(resolutionAuthority === undefined ? {} : { resolutionAuthority }),
    })
  }

  it('drives direct execution and independent review to done', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    await execute(client, 'execution', 'direct-exec')
    expect(client.phase).toBe('in_review')
    await review(client, 'direct-review', 'accept')
    expect(client.phase).toBe('done')
    expect(client.mutations.map(item => item.operation)).toEqual(['claim', 'verify', 'link', 'submit', 'complete', 'claim', 'verify', 'accept'])
  })

  it('publishes a complete execution delivery before signaling settlement intent', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const operations: string[] = []
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'delivery-order-session',
      effect: async request => { await writeFile(join(request.workspace, 'README.md'), 'implemented\n') },
      result: request => result(executionProposal(request), 'delivery-order-session'),
    }])

    await runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'delivery-order', clientSessionId: 'fleet-test', idempotencyKey: 'delivery-order',
      workspace: await workspace('execution', 'delivery-order'), deadline: '2026-08-16T00:00:00Z',
      publishDelivery: async () => { operations.push('delivery'); return delivery },
      onBeforeSettlement: () => { operations.push('settlement') },
    })

    expect(operations).toEqual(['delivery', 'settlement'])
  })

  it('settles an unable execution without publishing a delivery', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const operations: string[] = []
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'unable-session',
      result: request => result(executionProposal(request, 'unable_to_complete'), 'unable-session'),
    }])

    await runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'unable', clientSessionId: 'fleet-test', idempotencyKey: 'unable',
      workspace: await workspace('execution', 'unable'), deadline: '2026-08-16T00:00:00Z',
      publishDelivery: async () => { operations.push('delivery'); return delivery },
      onBeforeSettlement: () => { operations.push('settlement') },
    })

    expect(operations).toEqual(['settlement'])
    expect(client.activity).toBe('available')
    expect(client.mutations.map(item => item.operation)).toEqual(['claim', 'release'])
  })

  it('imports a frozen candidate before review-first acceptance', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    await runCandidateImport({
      client, definition, taskVersion: client.version, clientSessionId: 'fleet-test', idempotencyKey: 'import',
      summary: 'Imported candidate.', criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'Coordinator evidence.' }], delivery,
    })
    expect(client.phase).toBe('in_review')
    await review(client, 'import-review', 'accept')
    expect(client.phase).toBe('done')
  })

  it('creates a later correction Claim and a new Review after request changes', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    await runCandidateImport({
      client, definition, taskVersion: client.version, clientSessionId: 'fleet-test', idempotencyKey: 'correction-import',
      summary: 'Imported candidate.', criteria: [{ criterionId: criterion.id, criterionRevision: 1, outcome: 'passed', evidence: 'Candidate.' }], delivery,
    })
    const firstReview = await review(client, 'changes-review', 'request_changes')
    expect(client.phase).toBe('in_progress')
    const correction = await execute(client, 'correction', 'correction-exec')
    expect(correction.claimId).not.toBe(firstReview.claimId)
    const secondReview = await review(client, 'correction-review', 'accept')
    expect(secondReview.claimId).not.toBe(firstReview.claimId)
    expect(client.phase).toBe('done')
  })

  it('requires unchanged state for typed resolution, then permits only the explicitly waived criterion', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'resolution-session',
      result: request => result(executionProposal(request, 'request_resolution'), 'resolution-session'),
    }])
    const resolutionWorkspace = await workspace('execution', 'resolution-request')
    const request = await runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'resolution-request', clientSessionId: 'fleet-test', idempotencyKey: 'resolution-request', workspace: resolutionWorkspace,
      deadline: '2026-08-16T00:00:00Z',
    })
    expect(request.observation.git).toMatchObject({ head: revision, changedPaths: [], porcelain: '' })
    expect(client.activity).toBe('needs_resolution')
    const authority = await resolveTypedIssue(client, {
      taskNumber: 21, issueThreadId: 'issue-1', taskVersion: client.version, threadVersion: 1,
      conclusion: 'The criterion is superseded by the explicit decision.', waivedCriterionIds: [criterion.id],
      sessionId: 'fleet-test', idempotencyKey: 'resolve-issue',
    })
    const execution = await execute(client, 'correction', 'post-resolution', authority)
    expect(execution.claimId).not.toBe(request.claimId)
    await review(client, 'post-resolution-review', 'accept', authority)
    expect(client.phase).toBe('done')
    expect(client.checks.some(item => item.outcome === 'waived')).toBe(true)
  })

  it('rejects stale admission and missing capabilities before Claim creation', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const adapter = new ReplayHarnessAdapter([], {
      nativeTools: true, structuredResult: false, eventStream: true, cancellation: true, sessionResume: false,
      sandboxModes: ['workspace_write'], supportedStages: ['execution'],
    })
    await expect(runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'missing-capability', clientSessionId: 'fleet-test', idempotencyKey: 'missing-capability',
      workspace: await workspace('execution', 'missing-capability'), deadline: '2026-08-16T00:00:00Z',
    })).rejects.toMatchObject({ code: 'CAPABILITY_MISSING' })
    expect(client.mutations).toEqual([])

    const capable = new ReplayHarnessAdapter([])
    await expect(runClaimStage({
      client, router: new StaticRuntimeRouter([capable], routes()), definition, stage: 'execution', taskVersion: 99,
      runId: 'stale-task', clientSessionId: 'fleet-test', idempotencyKey: 'stale-task',
      workspace: await workspace('execution', 'stale-task'), deadline: '2026-08-16T00:00:00Z',
    })).rejects.toThrow('no longer eligible')
    expect(client.mutations).toEqual([])
  })

  it('blocks typed resolution when the Harness mutates workspace or HEAD', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'bad-resolution-session',
      effect: async request => { await writeFile(join(request.workspace, 'README.md'), 'mutated\n') },
      result: request => result(executionProposal(request, 'request_resolution'), 'bad-resolution-session'),
    }])
    await expect(runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'bad-resolution', clientSessionId: 'fleet-test', idempotencyKey: 'bad-resolution',
      workspace: await workspace('execution', 'bad-resolution'), deadline: '2026-08-16T00:00:00Z',
    })).rejects.toThrow('requires unchanged workspace and HEAD')
    expect(client.mutations.map(item => item.operation)).toEqual(['claim'])
  })

  it('blocks a persisted resolution result when the Harness mutated its pre-run workspace', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const claimed = await client.claimTask(21, client.version, 'execution', {
      sessionId: 'fleet-test', idempotencyKey: 'persisted-resolution-claim',
    })
    const retained = await workspace('execution', 'persisted-resolution')
    const baseline = { head: revision, changedPaths: [], porcelain: '' }
    await writeFile(join(retained.repositoryPath, 'README.md'), 'mutated before crash\n')
    const proposal = executionProposal({ runId: 'persisted-resolution', claimId: claimed.data.claim.id }, 'request_resolution')

    await expect(continueClaimStageAfterHarness({
      client, router: new StaticRuntimeRouter([new ReplayHarnessAdapter([])], routes()), definition,
      stage: 'execution', taskVersion: claimed.data.task.version, runId: 'persisted-resolution',
      clientSessionId: 'fleet-test', idempotencyKey: 'persisted-resolution', workspace: retained,
      deadline: '2026-08-16T00:00:00Z', existingClaimId: claimed.data.claim.id,
      resumeRuntimeSessionId: 'persisted-resolution-session',
      harnessResult: result(proposal, 'persisted-resolution-session'),
      baseline,
    })).rejects.toThrow('requires unchanged workspace and HEAD')
    expect(client.mutations.map(item => item.operation)).toEqual(['claim'])
  })

  it('preserves exact Claim identity and prevents an invalid proposal from reaching settlement', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'wrong-identity-session',
      result: request => result({ ...executionProposal(request), claimId: 'another-claim' }, 'wrong-identity-session'),
    }])
    await expect(runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'wrong-identity', clientSessionId: 'fleet-test', idempotencyKey: 'wrong-identity',
      workspace: await workspace('execution', 'wrong-identity'), deadline: '2026-08-16T00:00:00Z',
      publishDelivery: async () => delivery,
    })).rejects.toThrow('identity does not match')
    expect(client.mutations.map(item => item.operation)).toEqual(['claim'])
  })

  it('runs coordinator-owned validation before publish or settlement', async () => {
    const client = new InMemoryPactlineClient(21, [criterion])
    let published = false
    const adapter = new ReplayHarnessAdapter([{
      sessionId: 'hidden-gate-session',
      effect: async request => { await writeFile(join(request.workspace, 'README.md'), 'changed\n') },
      result: request => result(executionProposal(request), 'hidden-gate-session'),
    }])
    await expect(runClaimStage({
      client, router: new StaticRuntimeRouter([adapter], routes()), definition, stage: 'execution', taskVersion: client.version,
      runId: 'hidden-gate', clientSessionId: 'fleet-test', idempotencyKey: 'hidden-gate',
      workspace: await workspace('execution', 'hidden-gate'), deadline: '2026-08-16T00:00:00Z',
      validateObservation: () => { throw new Error('hidden verification failed') },
      publishDelivery: async () => { published = true; return delivery },
    })).rejects.toThrow('hidden verification failed')
    expect(published).toBe(false)
    expect(client.mutations.map(item => item.operation)).toEqual(['claim'])
  })
})
