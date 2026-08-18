import { PactlineClientError } from './client.js'
import type { PactlineCLI, PactlineCallOptions } from './client.js'
import type { ExecutionProposal, ReviewProposal } from '../core/harness-result.js'
import type { PactlineClaimMutationResult, PactlineOperation } from './types.js'
import type { RepositoryDelivery } from '../repository/delivery.js'
import { validateRepositoryDelivery } from '../repository/delivery.js'
import type { PactlineSettlementIntent } from '../run/external-effect.js'

const RESOLVED_ISSUE_AUTHORITY = Symbol('resolved-issue-authority')

export interface SettlementClient {
  readonly showClaim: PactlineCLI['showClaim']
  readonly verifyClaim: PactlineCLI['verifyClaim']
  readonly linkCodeChange: PactlineCLI['linkCodeChange']
  readonly submitClaim: PactlineCLI['submitClaim']
  readonly completeClaim: PactlineCLI['completeClaim']
  readonly releaseClaim: PactlineCLI['releaseClaim']
  readonly requestResolution: PactlineCLI['requestResolution']
  readonly requestChanges: PactlineCLI['requestChanges']
  readonly acceptClaim: PactlineCLI['acceptClaim']
}

export interface SettlementContext extends PactlineCallOptions {
  readonly taskNumber: number
  readonly claimId: string
  readonly taskVersion: number
  readonly stage: 'execution' | 'review'
}

export interface ImportedCriterion {
  readonly criterionId: string
  readonly criterionRevision: number
  readonly outcome: 'passed' | 'failed' | 'unable' | 'waived'
  readonly evidence: string
}

export interface ResolvedIssueAuthority {
  readonly taskNumber: number
  readonly resolvedAtTaskVersion: number
  readonly issueThreadId: string
  readonly waivedCriterionIds: readonly string[]
  readonly [RESOLVED_ISSUE_AUTHORITY]: true
}

export interface ResolveTypedIssueOptions extends PactlineCallOptions {
  readonly taskNumber: number
  readonly taskVersion: number
  readonly issueThreadId: string
  readonly threadVersion: number
  readonly conclusion: string
  readonly waivedCriterionIds: readonly string[]
}

interface ResolutionClient {
  readonly resolveIssue: PactlineCLI['resolveIssue']
  readonly showTask: PactlineCLI['showTask']
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

async function assertCurrentClaim(client: SettlementClient, context: SettlementContext): Promise<void> {
  const packet = await client.showClaim(context.claimId, 20, context)
  const claim = record(packet.data.claim, 'Claim')
  const task = record(packet.data.task, 'Task')
  if (claim.id !== context.claimId || claim.stage !== context.stage || claim.status !== 'active'
    || task.number !== context.taskNumber || task.version !== context.taskVersion) {
    throw new Error('Pactline Claim or Task changed after Harness dispatch')
  }
}

function key(context: SettlementContext, operation: string, index?: number): PactlineCallOptions {
  if (context.idempotencyKey === undefined || context.idempotencyKey.trim() === '') throw new Error('Settlement requires an idempotency-key prefix')
  return {
    sessionId: context.sessionId,
    ...(context.signal === undefined ? {} : { signal: context.signal }),
    idempotencyKey: `${context.idempotencyKey}-${operation}${index === undefined ? '' : `-${String(index)}`}`,
  }
}

function isUncertain(error: unknown): boolean {
  return error instanceof PactlineClientError && ['TIMEOUT', 'OUTPUT_LIMIT', 'INVALID_RESPONSE', 'SPAWN_FAILED'].includes(error.code)
}

/** Resolve one typed Pactline Issue and mint the only Core authority that permits later waived checks. */
export async function resolveTypedIssue(
  client: ResolutionClient,
  options: ResolveTypedIssueOptions,
): Promise<ResolvedIssueAuthority> {
  if (options.issueThreadId.trim() === '' || options.conclusion.trim() === '') throw new Error('Typed Issue resolution requires an Issue and conclusion')
  if (options.idempotencyKey === undefined || options.idempotencyKey.trim() === '') throw new Error('Typed Issue resolution requires an idempotency key')
  if (options.waivedCriterionIds.length === 0 || new Set(options.waivedCriterionIds).size !== options.waivedCriterionIds.length
    || options.waivedCriterionIds.some(id => id.trim() === '')) {
    throw new Error('Typed Issue resolution must identify unique superseded criteria')
  }
  await client.resolveIssue(
    options.taskNumber, options.issueThreadId, options.taskVersion, options.threadVersion, options.conclusion, options,
  )
  const current = await client.showTask(options.taskNumber, 20, options)
  const task = record(current.data.task, 'resolved Task')
  if (task.number !== options.taskNumber || typeof task.version !== 'number' || task.version <= options.taskVersion
    || task.activity !== 'available') {
    throw new Error('Pactline did not confirm the typed Issue resolution')
  }
  return {
    taskNumber: options.taskNumber,
    resolvedAtTaskVersion: task.version,
    issueThreadId: options.issueThreadId,
    waivedCriterionIds: [...options.waivedCriterionIds],
    [RESOLVED_ISSUE_AUTHORITY]: true,
  }
}

export function waivedCriteriaFromAuthority(
  authority: ResolvedIssueAuthority | undefined,
  taskNumber: number,
  taskVersion: number,
): readonly string[] | undefined {
  if (authority === undefined) return undefined
  if (authority[RESOLVED_ISSUE_AUTHORITY] !== true || authority.taskNumber !== taskNumber || taskVersion < authority.resolvedAtTaskVersion) {
    throw new Error('Resolved Issue authority does not match the current Task lineage')
  }
  return authority.waivedCriterionIds
}

function packetMutation(packet: Record<string, unknown>): PactlineClaimMutationResult {
  const task = record(packet.task, 'reconciled Task')
  const claim = record(packet.claim, 'reconciled Claim')
  if (typeof task.number !== 'number' || typeof task.version !== 'number' || typeof task.phase !== 'string'
    || typeof claim.id !== 'string' || typeof claim.task_number !== 'number' || typeof claim.stage !== 'string'
    || typeof claim.status !== 'string' || typeof claim.version !== 'number') {
    throw new Error('Pactline reconciliation packet is invalid')
  }
  return {
    task: {
      task_number: task.number,
      version: task.version,
      phase: task.phase,
      activity: typeof task.activity === 'string' ? task.activity : '',
    },
    claim: claim as unknown as PactlineClaimMutationResult['claim'],
  }
}

async function terminalMutation(
  client: SettlementClient,
  context: SettlementContext,
  expected: (packet: Record<string, unknown>) => boolean,
  mutate: () => Promise<PactlineOperation<PactlineClaimMutationResult>>,
): Promise<PactlineClaimMutationResult> {
  try {
    return (await mutate()).data
  } catch (error: unknown) {
    if (!isUncertain(error)) throw error
    const reconciled = await client.showClaim(context.claimId, 20, context)
    if (expected(reconciled.data)) return packetMutation(reconciled.data)
    const claim = record(reconciled.data.claim, 'reconciled Claim')
    const task = record(reconciled.data.task, 'reconciled Task')
    if (claim.id !== context.claimId || claim.status !== 'active' || claim.stage !== context.stage
      || task.number !== context.taskNumber || task.version !== context.taskVersion) {
      throw new Error('Uncertain Pactline mutation could not be reconciled safely', { cause: error })
    }
    return (await mutate()).data
  }
}

function terminalExpected(status: string, outcome?: string): (packet: Record<string, unknown>) => boolean {
  return packet => {
    const claim = record(packet.claim, 'Claim')
    return claim.status === status && (outcome === undefined || claim.outcome === outcome)
  }
}

async function recordCriteria(
  client: SettlementClient,
  criteria: readonly ImportedCriterion[],
  context: SettlementContext,
): Promise<void> {
  for (const [index, criterion] of criteria.entries()) {
    await client.verifyClaim(
      context.claimId, criterion.criterionId, context.taskVersion, criterion.criterionRevision,
      criterion.outcome, criterion.evidence, key(context, 'verify', index),
    )
  }
}

async function publishExecution(
  client: SettlementClient,
  summary: string,
  criteria: readonly ImportedCriterion[],
  delivery: RepositoryDelivery,
  context: SettlementContext,
): Promise<PactlineClaimMutationResult> {
  validateRepositoryDelivery(delivery)
  await recordCriteria(client, criteria, context)
  const linked = await client.linkCodeChange(context.claimId, context.taskVersion, delivery.codeChangeUrl, key(context, 'link'))
  const linkedVersion = linked.data.task.version
  const submission = [summary, `Revision: ${delivery.revision}`, `Branch: ${delivery.branch}`, `Code change: ${delivery.codeChangeUrl}`].join('\n')
  await client.submitClaim(context.claimId, linkedVersion, submission, key(context, 'submit'))
  const terminalContext = { ...context, taskVersion: linkedVersion }
  return terminalMutation(client, terminalContext, terminalExpected('completed'), () => (
    client.completeClaim(context.claimId, linkedVersion, summary, key(context, 'complete'))
  ))
}

export async function settleExecution(
  client: SettlementClient,
  proposal: ExecutionProposal,
  context: SettlementContext,
  delivery?: RepositoryDelivery,
): Promise<PactlineClaimMutationResult> {
  if (context.stage !== 'execution' || proposal.claimId !== context.claimId || proposal.taskNumber !== context.taskNumber) {
    throw new Error('Execution proposal does not match the settlement context')
  }
  await assertCurrentClaim(client, context)
  if (proposal.recommendation === 'unable_to_complete') {
    return terminalMutation(client, context, terminalExpected('released'), () => (
      client.releaseClaim(context.claimId, context.taskVersion, proposal.summary, key(context, 'release'))
    ))
  }
  if (proposal.recommendation === 'request_resolution') {
    const request = proposal.resolutionRequest
    if (request === undefined) throw new Error('Validated resolution recommendation has no request')
    return terminalMutation(client, context, terminalExpected('completed', 'needs_resolution'), () => (
      client.requestResolution(context.claimId, context.taskVersion, request.issueType, request.request, key(context, 'resolution'))
    ))
  }
  if (delivery === undefined) throw new Error('Complete requires coordinator-verified repository delivery')
  return publishExecution(client, proposal.summary, proposal.criteria, delivery, context)
}

export async function settleImportedDelivery(
  client: SettlementClient,
  criteria: readonly ImportedCriterion[],
  summary: string,
  delivery: RepositoryDelivery,
  context: SettlementContext,
): Promise<PactlineClaimMutationResult> {
  if (context.stage !== 'execution' || summary.trim() === '' || criteria.length === 0) throw new Error('Candidate import settlement is invalid')
  await assertCurrentClaim(client, context)
  return publishExecution(client, summary, criteria, delivery, context)
}

export async function settleReview(
  client: SettlementClient,
  proposal: ReviewProposal,
  context: SettlementContext,
): Promise<PactlineClaimMutationResult> {
  if (context.stage !== 'review' || proposal.claimId !== context.claimId || proposal.taskNumber !== context.taskNumber) {
    throw new Error('Review proposal does not match the settlement context')
  }
  await assertCurrentClaim(client, context)
  await recordCriteria(client, proposal.criteria, context)
  if (proposal.recommendation === 'accept') {
    return terminalMutation(client, context, terminalExpected('completed', 'task_accepted'), () => (
      client.acceptClaim(context.claimId, context.taskVersion, proposal.summary, key(context, 'accept'))
    ))
  }
  if (proposal.recommendation === 'request_changes') {
    const findings = proposal.findings.map(item => `${item.path}:${String(item.line)} [${item.severity}] ${item.explanation}`).join('\n')
    return terminalMutation(client, context, terminalExpected('completed', 'changes_requested'), () => (
      client.requestChanges(context.claimId, context.taskVersion, `${proposal.summary}\n${findings}`, key(context, 'changes'))
    ))
  }
  if (proposal.recommendation === 'request_resolution') {
    const request = proposal.resolutionRequest
    if (request === undefined) throw new Error('Validated resolution recommendation has no request')
    return terminalMutation(client, context, terminalExpected('completed', 'needs_resolution'), () => (
      client.requestResolution(context.claimId, context.taskVersion, request.issueType, request.request, key(context, 'resolution'))
    ))
  }
  return terminalMutation(client, context, terminalExpected('released'), () => (
    client.releaseClaim(context.claimId, context.taskVersion, proposal.summary, key(context, 'release'))
  ))
}

/** Replay only the immutable settlement action persisted before the original external call. */
export async function replaySettlement(
  client: SettlementClient,
  intent: PactlineSettlementIntent,
  context: SettlementContext,
): Promise<PactlineClaimMutationResult> {
  if (intent.stage !== context.stage || intent.taskVersion !== context.taskVersion) {
    throw new Error('Persisted settlement intent does not match the active Claim context')
  }
  return intent.stage === 'execution'
    ? settleExecution(client, intent.proposal as ExecutionProposal, context, intent.delivery)
    : settleReview(client, intent.proposal as ReviewProposal, context)
}
