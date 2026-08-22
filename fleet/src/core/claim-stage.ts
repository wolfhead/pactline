import type { HarnessRunEvent, HarnessRunObserver, HarnessRunRequest, HarnessStage } from './harness-adapter.js'
import type {
  ExecutionProposal,
  HarnessRunResult,
  ProposalValidationContext,
  ReviewProposal,
} from './harness-result.js'
import { proposalResultSchema, validateHarnessProposal } from './harness-result.js'
import { HarnessEventCollector } from './events.js'
import { promptPolicy } from './prompt-policy.js'
import type { AdmittedRuntime, StaticRuntimeRouter } from './runtime-router.js'
import { requiredSandbox } from './runtime-router.js'
import {
  assertAllowedPaths,
  assertProposalMatchesObservation,
  assertReviewFindingsExist,
  observeGit,
  runFixedVerification,
} from './verification.js'
import type { VerificationObservation } from './verification.js'
import type { GitObservation } from './verification.js'
import type { FleetWorkDefinition } from './work-definition.js'
import type { PactlineCLI } from '../pactline/client.js'
import { settleExecution, settleImportedDelivery, settleReview, waivedCriteriaFromAuthority } from '../pactline/settlement.js'
import type { ImportedCriterion, ResolvedIssueAuthority, SettlementClient } from '../pactline/settlement.js'
import type { PactlineClaimMutationResult } from '../pactline/types.js'
import type { RepositoryDelivery } from '../repository/delivery.js'
import type { FleetWorkspace } from '../repository/workspace.js'

export type ClaimWorkflowStage = Exclude<HarnessStage, 'resolution_analysis'>

export interface ClaimStageClient extends SettlementClient {
  readonly showTask: PactlineCLI['showTask']
  readonly claimTask: PactlineCLI['claimTask']
}

export interface ClaimStageDispatch {
  readonly definition: FleetWorkDefinition
  readonly stage: ClaimWorkflowStage
  readonly claimId: string
  readonly taskVersion: number
  readonly packet: Record<string, unknown>
  readonly request: HarnessRunRequest
  readonly runtime: AdmittedRuntime
}

export interface ClaimStageOptions {
  readonly client: ClaimStageClient
  readonly router: StaticRuntimeRouter
  readonly definition: FleetWorkDefinition
  readonly stage: ClaimWorkflowStage
  readonly taskVersion: number
  readonly runId: string
  readonly clientSessionId: string
  readonly idempotencyKey: string
  /** A factory lets the resident coordinator allocate a workspace only after Claim persistence. */
  readonly workspace: FleetWorkspace | ((claimId: string, taskVersion: number) => Promise<FleetWorkspace>)
  readonly deadline: string
  readonly resolutionAuthority?: ResolvedIssueAuthority
  readonly existingClaimId?: string
  /** Resume a failed Harness turn against its retained workspace and active Claim. */
  readonly resumeRuntimeSessionId?: string
  readonly signal?: AbortSignal
  readonly onRuntimeSession?: (runtimeSessionId: string, dispatch: ClaimStageDispatch) => Promise<void> | void
  readonly onClaimed?: (claimId: string, taskVersion: number, claimVersion?: number) => Promise<void> | void
  readonly onEvent?: (event: HarnessRunEvent) => Promise<void> | void
  readonly onHarnessResult?: (
    result: HarnessRunResult,
    dispatch: ClaimStageDispatch,
    baseline: GitObservation,
  ) => Promise<void> | void
  /** Run coordinator-owned hidden or policy checks after Harness exit and before any settlement or publish effect. */
  readonly validateObservation?: (
    dispatch: ClaimStageDispatch,
    proposal: ExecutionProposal | ReviewProposal,
    observation: VerificationObservation,
  ) => Promise<void> | void
  readonly publishDelivery?: (
    dispatch: ClaimStageDispatch,
    proposal: ExecutionProposal,
    observation: VerificationObservation,
  ) => Promise<RepositoryDelivery>
  readonly onBeforeSettlement?: (
    dispatch: ClaimStageDispatch,
    proposal: ExecutionProposal | ReviewProposal,
  ) => Promise<void> | void
  readonly onSettled?: (settlement: PactlineClaimMutationResult, dispatch: ClaimStageDispatch) => Promise<void> | void
}

export interface ClaimStageResult {
  readonly claimId: string
  readonly dispatchedTaskVersion: number
  readonly runtimeSessionId: string
  readonly harnessResult: HarnessRunResult
  readonly proposal: ExecutionProposal | ReviewProposal
  readonly observation: VerificationObservation
  readonly settlement: PactlineClaimMutationResult
}

export interface PersistedResultContinuationOptions extends Omit<
  ClaimStageOptions,
  'workspace' | 'existingClaimId' | 'resumeRuntimeSessionId' | 'onRuntimeSession' | 'onClaimed' | 'onEvent' | 'onHarnessResult'
> {
  readonly workspace: FleetWorkspace
  readonly existingClaimId: string
  readonly resumeRuntimeSessionId: string
  readonly harnessResult: HarnessRunResult
  readonly baseline: GitObservation
}

export interface CandidateImportOptions {
  readonly client: ClaimStageClient
  readonly definition: FleetWorkDefinition
  readonly taskVersion: number
  readonly clientSessionId: string
  readonly idempotencyKey: string
  readonly summary: string
  readonly criteria: readonly ImportedCriterion[]
  readonly delivery: RepositoryDelivery
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function pactlineStage(stage: ClaimWorkflowStage): 'execution' | 'review' {
  return stage === 'review' ? 'review' : 'execution'
}

function assertTaskAdmission(
  packet: Record<string, unknown>,
  definition: FleetWorkDefinition,
  stage: ClaimWorkflowStage,
  taskVersion: number,
): void {
  const task = record(packet.task, 'Task packet Task')
  const expectedPhase = pactlineStage(stage) === 'execution' ? ['ready', 'in_progress'] : ['in_review']
  if (task.number !== definition.taskNumber || task.version !== taskVersion || !expectedPhase.includes(String(task.phase))) {
    throw new Error('Pactline Task is no longer eligible for the requested Fleet stage')
  }
  if (!Array.isArray(packet.criteria)) throw new Error('Pactline compact packet criteria are invalid')
  const actual = packet.criteria.map((raw, index) => {
    const criterion = record(raw, `criteria[${String(index)}]`)
    return `${String(criterion.id)}:${String(criterion.revision)}`
  }).sort()
  const expected = definition.criteria.map(item => `${item.id}:${String(item.revision)}`).sort()
  if (JSON.stringify(actual) !== JSON.stringify(expected)) throw new Error('Pactline criterion identities or revisions drifted from the admitted work definition')
}

function assertInitialWorkspace(options: ClaimStageOptions, workspace: FleetWorkspace, head: string, changedPaths: readonly string[], porcelain: string): void {
  const expectedHead = options.stage === 'review'
    ? options.definition.candidate?.revision ?? workspace.baseRevision
    : options.resumeRuntimeSessionId === undefined ? workspace.baseRevision : head
  if (head !== expectedHead) throw new Error('Harness workspace is not at the admitted stage revision')
  if (workspace.mode !== 'execution' && options.stage !== 'review') {
    throw new Error('Harness execution requires a writable Task Workspace')
  }
  if (options.stage === 'review') {
    if (porcelain !== '') throw new Error('Review requires a clean retained Task Workspace')
    return
  }
  if (options.resumeRuntimeSessionId !== undefined) {
    assertAllowedPaths(changedPaths, options.definition.allowedPaths)
    return
  }
  if (changedPaths.length > 0 || porcelain !== '') {
    throw new Error('Harness workspace must start at the admitted clean revision')
  }
}

function assertRunResult(result: HarnessRunResult, runtime: AdmittedRuntime, runtimeSessionId: string): void {
  if (result.adapterId !== runtime.adapter.id || result.adapterVersion !== runtime.adapter.version
    || result.runtimeSessionId !== runtimeSessionId) {
    throw new Error('Harness result provenance does not match the admitted Adapter Session')
  }
  if (result.terminalState !== 'completed') throw new Error(`Harness Run did not complete: ${result.terminalState}`)
  if (result.model.model !== runtime.route.model || result.model.reasoning !== runtime.route.reasoning) {
    throw new Error('Harness model provenance does not match the frozen Runtime Route')
  }
}

async function dispatchHarness(
  options: ClaimStageOptions,
  dispatch: ClaimStageDispatch,
): Promise<{ result: HarnessRunResult; runtimeSessionId: string }> {
  const collector = new HarnessEventCollector()
  let runtimeSessionId: string | undefined
  const observer: HarnessRunObserver = {
    onSessionStarted: async reference => {
      if (runtimeSessionId !== undefined || reference.runtimeSessionId.trim() === '') throw new Error('Harness emitted an invalid or duplicate Session identity')
      if (options.resumeRuntimeSessionId !== undefined && reference.runtimeSessionId !== options.resumeRuntimeSessionId) {
        throw new Error('Harness resumed a different Session identity')
      }
      runtimeSessionId = reference.runtimeSessionId
      await options.onRuntimeSession?.(reference.runtimeSessionId, dispatch)
    },
    onEvent: async event => {
      if (runtimeSessionId === undefined) throw new Error('Harness emitted an event before its Session identity')
      collector.accept(event)
      await options.onEvent?.(event)
    },
  }
  const signal = options.signal ?? new AbortController().signal
  let result: HarnessRunResult
  if (options.resumeRuntimeSessionId === undefined) {
    result = await dispatch.runtime.adapter.run(dispatch.request, observer, signal)
  } else {
    if (!dispatch.runtime.capabilities.sessionResume || dispatch.runtime.adapter.resume === undefined) {
      throw new Error('Harness Adapter does not support Session resume')
    }
    result = await dispatch.runtime.adapter.resume(options.resumeRuntimeSessionId, dispatch.request, observer, signal)
  }
  if (runtimeSessionId === undefined) throw new Error('Harness did not emit a Session identity before completion')
  assertRunResult(result, dispatch.runtime, runtimeSessionId)
  const stable = (value: typeof result.eventSummary): string => JSON.stringify({
    total: value.total,
    byType: Object.fromEntries(Object.entries(value.byType).sort()),
    toolCalls: Object.fromEntries(Object.entries(value.toolCalls).sort()),
    toolErrors: Object.fromEntries(Object.entries(value.toolErrors).sort()),
  })
  if (stable(result.eventSummary) !== stable(collector.summary())) {
    throw new Error('Harness result event summary does not match observed events')
  }
  return { result, runtimeSessionId }
}

function buildDispatch(
  options: ClaimStageOptions,
  runtime: AdmittedRuntime,
  claimId: string,
  taskVersion: number,
  packet: Record<string, unknown>,
  workspace: FleetWorkspace,
): { readonly dispatch: ClaimStageDispatch; readonly validationContext: ProposalValidationContext } {
  const waivedCriterionIds = waivedCriteriaFromAuthority(
    options.resolutionAuthority, options.definition.taskNumber, options.taskVersion,
  )
  const validationContext: ProposalValidationContext = {
    stage: options.stage,
    runId: options.runId,
    claimId,
    taskNumber: options.definition.taskNumber,
    criteria: options.definition.criteria,
    verificationCommands: options.definition.verificationCommands,
    ...(waivedCriterionIds === undefined ? {} : { waivedCriterionIds }),
  }
  const policy = promptPolicy(options.stage, runtime.route.promptVersion)
  const request: HarnessRunRequest = {
    runId: options.runId,
    claimId,
    stage: options.stage,
    workspace: workspace.repositoryPath,
    repositoryRevision: workspace.baseRevision,
    taskPacket: packet,
    allowedPaths: options.definition.allowedPaths,
    verificationCommands: options.definition.verificationCommands,
    resultSchema: proposalResultSchema(validationContext),
    sandbox: requiredSandbox(options.stage),
    deadline: options.deadline,
    policy: {
      model: runtime.route.model,
      ...(runtime.route.reasoning === undefined ? {} : { reasoning: runtime.route.reasoning }),
      promptVersion: runtime.route.promptVersion,
      systemInstructions: policy.system,
      stageInstructions: policy.stageInstructions,
      resultContractVersion: runtime.route.resultContractVersion,
    },
  }
  return {
    validationContext,
    dispatch: {
      definition: options.definition,
      stage: options.stage,
      claimId,
      taskVersion,
      packet,
      request,
      runtime,
    },
  }
}

async function finishClaimStage(
  options: ClaimStageOptions,
  dispatch: ClaimStageDispatch,
  workspace: FleetWorkspace,
  harnessResult: HarnessRunResult,
  validationContext: ProposalValidationContext,
  baseline: GitObservation,
): Promise<ClaimStageResult> {
  const proposal = validateHarnessProposal(harnessResult.proposal, validationContext)
  if (proposal.kind === 'resolution_analysis') throw new Error('Resolution analysis cannot directly settle a Pactline Claim')

  const afterAgent = await observeGit(workspace.repositoryPath, workspace.baseRevision)
  const requiresUnchanged = proposal.recommendation === 'request_resolution' || proposal.recommendation === 'unable_to_complete'
  if (requiresUnchanged && (afterAgent.head !== baseline.head
    || JSON.stringify(afterAgent.changedPaths) !== JSON.stringify(baseline.changedPaths)
    || afterAgent.porcelain !== baseline.porcelain)) {
    throw new Error('Resolution or release proposal requires unchanged workspace and HEAD')
  }
  const commands = requiresUnchanged ? [] : await runFixedVerification(workspace.repositoryPath, options.definition.verificationCommands)
  const git = await observeGit(workspace.repositoryPath, workspace.baseRevision)
  const observation = { git, commands }
  if (!requiresUnchanged) {
    assertProposalMatchesObservation(proposal, observation, {
      baseHead: baseline.head,
      allowedPaths: options.definition.allowedPaths,
      ...(proposal.kind === 'review' ? { reviewBaseline: baseline } : {}),
    })
  }
  if (proposal.kind === 'review') await assertReviewFindingsExist(workspace.repositoryPath, proposal)
  await options.validateObservation?.(dispatch, proposal, observation)

  const settlementContext = {
    taskNumber: options.definition.taskNumber,
    claimId: dispatch.claimId,
    taskVersion: dispatch.taskVersion,
    stage: pactlineStage(options.stage),
    sessionId: options.clientSessionId,
    idempotencyKey: `${options.idempotencyKey}-settle`,
    ...(options.signal === undefined ? {} : { signal: options.signal }),
  } as const
  const delivery = proposal.kind === 'execution' && proposal.recommendation === 'complete'
    ? await options.publishDelivery?.(dispatch, proposal, observation)
    : undefined
  await options.onBeforeSettlement?.(dispatch, proposal)
  const settlement = proposal.kind === 'execution'
    ? await settleExecution(options.client, proposal, settlementContext, delivery)
    : await settleReview(options.client, proposal, settlementContext)
  await options.onSettled?.(settlement, dispatch)
  return {
    claimId: dispatch.claimId,
    dispatchedTaskVersion: dispatch.taskVersion,
    runtimeSessionId: harnessResult.runtimeSessionId,
    harnessResult,
    proposal,
    observation,
    settlement,
  }
}

/** Admit capability, Claim exact state, run one Harness Session, observe it, and settle its proposal. */
export async function runClaimStage(options: ClaimStageOptions): Promise<ClaimStageResult> {
  if ((options.stage as HarnessStage) === 'resolution_analysis') {
    throw new Error('Resolution analysis is advisory and cannot create a Pactline Claim')
  }
  const runtime = await options.router.admit(options.stage)
  const readOptions = { sessionId: options.clientSessionId, ...(options.signal === undefined ? {} : { signal: options.signal }) }
  const inspected = await options.client.showTask(options.definition.taskNumber, 20, readOptions)
  assertTaskAdmission(inspected.data, options.definition, options.stage, options.taskVersion)
  const stage = pactlineStage(options.stage)
  let claimId: string
  let dispatchedTaskVersion: number
  let claimVersion: number | undefined
  if (options.existingClaimId === undefined) {
    const claimed = await options.client.claimTask(options.definition.taskNumber, options.taskVersion, stage, {
      sessionId: options.clientSessionId, idempotencyKey: `${options.idempotencyKey}-claim`,
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    })
    claimId = claimed.data.claim.id
    claimVersion = claimed.data.claim.version
    dispatchedTaskVersion = claimed.data.task.version
    if (claimed.data.claim.stage !== stage || claimed.data.claim.status !== 'active') throw new Error('Pactline returned an invalid active Claim')
  } else {
    claimId = options.existingClaimId
    dispatchedTaskVersion = options.taskVersion
  }
  await options.onClaimed?.(claimId, dispatchedTaskVersion, claimVersion)
  const work = await options.client.showClaim(claimId, 20, readOptions)
  const task = record(work.data.task, 'Claim work packet Task')
  const claim = record(work.data.claim, 'Claim work packet Claim')
  if (task.number !== options.definition.taskNumber || task.version !== dispatchedTaskVersion || claim.id !== claimId
    || claim.stage !== stage || claim.status !== 'active') throw new Error('Pactline Claim changed before Harness dispatch')
  assertTaskAdmission(work.data, options.definition, options.stage, dispatchedTaskVersion)
  const workspace = typeof options.workspace === 'function'
    ? await options.workspace(claimId, dispatchedTaskVersion)
    : options.workspace

  const { dispatch, validationContext } = buildDispatch(
    options, runtime, claimId, dispatchedTaskVersion, work.data, workspace,
  )
  const before = await observeGit(workspace.repositoryPath, workspace.baseRevision)
  assertInitialWorkspace(options, workspace, before.head, before.changedPaths, before.porcelain)
  const run = await dispatchHarness(options, dispatch)
  await options.onHarnessResult?.(run.result, dispatch, before)
  return await finishClaimStage(options, dispatch, workspace, run.result, validationContext, before)
}

/** Continue only the post-Harness half from an observed result; never runs or resumes an Adapter. */
export async function continueClaimStageAfterHarness(
  options: PersistedResultContinuationOptions,
): Promise<ClaimStageResult> {
  if ((options.stage as HarnessStage) === 'resolution_analysis') {
    throw new Error('Resolution analysis is advisory and cannot create a Pactline Claim')
  }
  const runtime = await options.router.admit(options.stage)
  const readOptions = { sessionId: options.clientSessionId, ...(options.signal === undefined ? {} : { signal: options.signal }) }
  const work = await options.client.showClaim(options.existingClaimId, 20, readOptions)
  const task = record(work.data.task, 'Claim work packet Task')
  const claim = record(work.data.claim, 'Claim work packet Claim')
  const stage = pactlineStage(options.stage)
  if (task.number !== options.definition.taskNumber || task.version !== options.taskVersion
    || claim.id !== options.existingClaimId || claim.stage !== stage || claim.status !== 'active') {
    throw new Error('Pactline Claim changed before persisted result continuation')
  }
  assertTaskAdmission(work.data, options.definition, options.stage, options.taskVersion)
  const { dispatch, validationContext } = buildDispatch(
    options, runtime, options.existingClaimId, options.taskVersion, work.data, options.workspace,
  )
  assertRunResult(options.harnessResult, runtime, options.resumeRuntimeSessionId)
  return await finishClaimStage(
    options,
    dispatch,
    options.workspace,
    options.harnessResult,
    validationContext,
    options.baseline,
  )
}

/** Import a pre-existing frozen candidate through an honest Execution Claim before independent review. */
export async function runCandidateImport(options: CandidateImportOptions): Promise<PactlineClaimMutationResult> {
  const inspected = await options.client.showTask(options.definition.taskNumber, 20, { sessionId: options.clientSessionId })
  assertTaskAdmission(inspected.data, options.definition, 'execution', options.taskVersion)
  const claimed = await options.client.claimTask(options.definition.taskNumber, options.taskVersion, 'execution', {
    sessionId: options.clientSessionId, idempotencyKey: `${options.idempotencyKey}-claim`,
  })
  if (claimed.data.claim.stage !== 'execution' || claimed.data.claim.status !== 'active') throw new Error('Pactline returned an invalid candidate-import Claim')
  const context = {
    taskNumber: options.definition.taskNumber,
    claimId: claimed.data.claim.id,
    taskVersion: claimed.data.task.version,
    stage: 'execution',
    sessionId: options.clientSessionId,
    idempotencyKey: `${options.idempotencyKey}-settle`,
  } as const
  return settleImportedDelivery(options.client, options.criteria, options.summary, options.delivery, context)
}
