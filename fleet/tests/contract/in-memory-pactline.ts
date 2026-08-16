import type { ClaimStageClient } from '../../src/core/claim-stage.js'
import type { PactlineCallOptions } from '../../src/pactline/client.js'
import type {
  PactlineCheckOutcome,
  PactlineClaimMutationResult,
  PactlineClaimStage,
  PactlineCodeChangeMutationResult,
  PactlineIssueType,
  PactlineOperation,
} from '../../src/pactline/types.js'

interface ClaimState {
  id: string
  task_number: number
  stage: PactlineClaimStage
  status: string
  outcome?: string
  version: number
}

export interface MutationRecord {
  readonly operation: string
  readonly idempotencyKey?: string
  readonly taskVersion?: number
}

export class InMemoryPactlineClient implements ClaimStageClient {
  readonly mutations: MutationRecord[] = []
  readonly checks: Array<{ criterionId: string; outcome: PactlineCheckOutcome }> = []
  phase: string
  activity: string
  version: number
  private claimSequence = 0
  private readonly claims = new Map<string, ClaimState>()
  private activeIssue: { id: string; version: number } | undefined

  constructor(
    readonly taskNumber: number,
    readonly criteria: readonly { id: string; revision: number }[],
    initial: { phase?: string; activity?: string; version?: number } = {},
  ) {
    this.phase = initial.phase ?? 'ready'
    this.activity = initial.activity ?? 'available'
    this.version = initial.version ?? 1
  }

  showTask(taskNumber: number): Promise<PactlineOperation<Record<string, unknown>>> {
    if (taskNumber !== this.taskNumber) return Promise.reject(new Error('Task not found'))
    return Promise.resolve({ data: this.packet() })
  }

  claimTask(taskNumber: number, taskVersion: number, stage: PactlineClaimStage, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    this.assertTask(taskNumber, taskVersion)
    const expected = stage === 'execution' ? ['ready', 'in_progress'] : ['in_review']
    if (!expected.includes(this.phase) || this.activity !== 'available') return Promise.reject(new Error('Task not claimable'))
    this.record('claim', options, taskVersion)
    this.claimSequence += 1
    const claim: ClaimState = { id: `claim-${String(this.claimSequence)}`, task_number: taskNumber, stage, status: 'active', version: 1 }
    this.claims.set(claim.id, claim)
    this.version += 1
    if (stage === 'execution' && this.phase === 'ready') this.phase = 'in_progress'
    this.activity = 'working'
    return Promise.resolve({ data: this.mutation(claim) })
  }

  showClaim(claimId: string): Promise<PactlineOperation<Record<string, unknown>>> {
    const claim = this.claims.get(claimId)
    if (claim === undefined) return Promise.reject(new Error('Claim not found'))
    return Promise.resolve({ data: this.packet(claim) })
  }

  progressClaim(): Promise<PactlineOperation<unknown>> { return Promise.resolve({ data: {} }) }

  verifyClaim(
    claimId: string, criterionId: string, taskVersion: number, _criterionRevision: number,
    outcome: PactlineCheckOutcome, _evidence: string, options: PactlineCallOptions,
  ): Promise<PactlineOperation<unknown>> {
    this.assertActive(claimId, taskVersion)
    this.record('verify', options, taskVersion)
    this.checks.push({ criterionId, outcome })
    return Promise.resolve({ data: {} })
  }

  linkCodeChange(claimId: string, taskVersion: number, url: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineCodeChangeMutationResult>> {
    this.assertActive(claimId, taskVersion)
    this.record('link', options, taskVersion)
    this.version += 1
    return Promise.resolve({ data: { task: this.workflow(), code_change: { url } } })
  }

  submitClaim(claimId: string, taskVersion: number, _message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    const claim = this.assertActive(claimId, taskVersion)
    this.record('submit', options, taskVersion)
    return Promise.resolve({ data: this.mutation(claim) })
  }

  completeClaim(claimId: string, taskVersion: number, _message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    const claim = this.assertActive(claimId, taskVersion)
    this.record('complete', options, taskVersion)
    claim.status = 'completed'; claim.outcome = 'execution_completed'; claim.version += 1
    this.phase = 'in_review'; this.activity = 'available'; this.version += 1
    return Promise.resolve({ data: this.mutation(claim) })
  }

  releaseClaim(claimId: string, taskVersion: number, _message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    const claim = this.assertActive(claimId, taskVersion)
    this.record('release', options, taskVersion)
    claim.status = 'released'; claim.version += 1
    this.activity = 'available'; this.version += 1
    return Promise.resolve({ data: this.mutation(claim) })
  }

  requestChanges(claimId: string, taskVersion: number, _message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    const claim = this.assertActive(claimId, taskVersion)
    this.record('request_changes', options, taskVersion)
    claim.status = 'completed'; claim.outcome = 'changes_requested'; claim.version += 1
    this.phase = 'in_progress'; this.activity = 'available'; this.version += 1
    return Promise.resolve({ data: this.mutation(claim) })
  }

  acceptClaim(claimId: string, taskVersion: number, _message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    const claim = this.assertActive(claimId, taskVersion)
    this.record('accept', options, taskVersion)
    claim.status = 'completed'; claim.outcome = 'task_accepted'; claim.version += 1
    this.phase = 'done'; this.activity = ''; this.version += 1
    return Promise.resolve({ data: this.mutation(claim) })
  }

  requestResolution(
    claimId: string, taskVersion: number, _issueType: PactlineIssueType, _request: string, options: PactlineCallOptions,
  ): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    const claim = this.assertActive(claimId, taskVersion)
    this.record('request_resolution', options, taskVersion)
    claim.status = 'completed'; claim.outcome = 'needs_resolution'; claim.version += 1
    this.activity = 'needs_resolution'; this.version += 1
    this.activeIssue = { id: 'issue-1', version: 1 }
    return Promise.resolve({ data: this.mutation(claim) })
  }

  resolveIssue(
    taskNumber: number, issueThreadId: string, taskVersion: number, threadVersion: number,
    _conclusion: string, options: PactlineCallOptions,
  ): Promise<PactlineOperation<unknown>> {
    this.assertTask(taskNumber, taskVersion)
    if (this.activity !== 'needs_resolution' || this.activeIssue?.id !== issueThreadId || this.activeIssue.version !== threadVersion) {
      return Promise.reject(new Error('No matching Issue to resolve'))
    }
    this.record('resolve_issue', options, taskVersion)
    this.activity = 'available'
    this.version += 1
    this.activeIssue = undefined
    return Promise.resolve({ data: { task: this.workflow() } })
  }

  private packet(claim?: ClaimState): Record<string, unknown> {
    return {
      task: { id: `task-${String(this.taskNumber)}`, number: this.taskNumber, title: 'Fleet test Task', version: this.version, phase: this.phase, activity: this.activity },
      criteria: this.criteria.map(item => ({ ...item })),
      ...(claim === undefined ? {} : { claim: { ...claim } }),
    }
  }

  private workflow() {
    return { task_number: this.taskNumber, version: this.version, phase: this.phase, activity: this.activity }
  }

  private mutation(claim: ClaimState): PactlineClaimMutationResult {
    return { task: this.workflow(), claim: { ...claim } }
  }

  private assertTask(taskNumber: number, taskVersion: number): void {
    if (taskNumber !== this.taskNumber || taskVersion !== this.version) throw new Error('Stale Task version')
  }

  private assertActive(claimId: string, taskVersion: number): ClaimState {
    const claim = this.claims.get(claimId)
    if (claim === undefined || claim.status !== 'active') throw new Error('Claim is not active')
    this.assertTask(claim.task_number, taskVersion)
    return claim
  }

  private record(operation: string, options: PactlineCallOptions, taskVersion?: number): void {
    this.mutations.push({ operation, ...(options.idempotencyKey === undefined ? {} : { idempotencyKey: options.idempotencyKey }), ...(taskVersion === undefined ? {} : { taskVersion }) })
  }
}
