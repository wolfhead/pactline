import type { HarnessStage } from './harness-adapter.js'
import type { PactlineCheckOutcome, PactlineIssueType } from '../pactline/types.js'

export type HarnessTerminalState = 'completed' | 'failed' | 'cancelled' | 'timed_out'
export type ExecutionRecommendation = 'complete' | 'request_resolution' | 'unable_to_complete'
export type ReviewRecommendation = 'accept' | 'request_changes' | 'request_resolution' | 'unable_to_complete'
export type HarnessRecommendation = ExecutionRecommendation | ReviewRecommendation

export interface CriterionIdentity {
  readonly id: string
  readonly revision: number
}

export interface CriterionProposal {
  readonly criterionId: string
  readonly criterionRevision: number
  readonly outcome: PactlineCheckOutcome
  readonly evidence: string
}

export interface VerificationProposal {
  readonly command: string
  readonly outcome: 'passed' | 'failed'
  readonly summary: string
}

export interface ResolutionRequestProposal {
  readonly issueType: PactlineIssueType
  readonly request: string
}

interface ProposalIdentity {
  readonly schemaVersion: 1
  readonly runId: string
  readonly claimId: string
  readonly taskNumber: number
}

interface ProposalBody extends ProposalIdentity {
  readonly summary: string
  readonly verification: readonly VerificationProposal[]
  readonly criteria: readonly CriterionProposal[]
  readonly limitations: readonly string[]
  readonly resolutionRequest?: ResolutionRequestProposal
}

export interface ExecutionProposal extends ProposalBody {
  readonly kind: 'execution'
  readonly recommendation: ExecutionRecommendation
  readonly changedPaths: readonly string[]
}

export interface ReviewFinding {
  readonly path: string
  readonly line: number
  readonly severity: 'low' | 'medium' | 'high'
  readonly category: string
  readonly evidence: string
  readonly explanation: string
}

export interface ReviewProposal extends ProposalBody {
  readonly kind: 'review'
  readonly recommendation: ReviewRecommendation
  readonly findings: readonly ReviewFinding[]
}

export interface ResolutionAnalysisProposal extends ProposalBody {
  readonly kind: 'resolution_analysis'
  readonly recommendation: 'request_resolution' | 'unable_to_complete'
  readonly resolutionRequest?: ResolutionRequestProposal
}

export type HarnessProposal = ExecutionProposal | ReviewProposal | ResolutionAnalysisProposal

export interface ModelProvenance {
  readonly provider: string
  readonly model: string
  readonly reasoning?: string
}

export interface HarnessTokenUsage {
  readonly inputTokens?: number
  readonly cachedInputTokens?: number
  readonly outputTokens?: number
  readonly reasoningTokens?: number
}

export interface HarnessEventSummary {
  readonly total: number
  readonly byType: Readonly<Record<string, number>>
  readonly toolCalls: Readonly<Record<string, number>>
  readonly toolErrors: Readonly<Record<string, number>>
}

export interface HarnessRunResult {
  readonly adapterId: string
  readonly adapterVersion: string
  readonly runtimeSessionId: string
  readonly model: ModelProvenance
  readonly terminalState: HarnessTerminalState
  readonly proposal: HarnessProposal
  readonly usage: HarnessTokenUsage
  readonly eventSummary: HarnessEventSummary
}

export interface ProposalValidationContext {
  readonly stage: HarnessStage
  readonly runId: string
  readonly claimId: string
  readonly taskNumber: number
  readonly criteria: readonly CriterionIdentity[]
  readonly verificationCommands: readonly string[]
  /** Criterion IDs explicitly superseded by a resolved Pactline Issue. */
  readonly waivedCriterionIds?: readonly string[]
}

const MAX_TEXT_BYTES = 16_384
const MAX_ITEMS = 64

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function text(value: unknown, name: string, maxBytes = MAX_TEXT_BYTES): string {
  if (typeof value !== 'string' || value.trim() === '' || Buffer.byteLength(value) > maxBytes) {
    throw new Error(`${name} must be non-empty and at most ${String(maxBytes)} bytes`)
  }
  return value
}

function stringList(value: unknown, name: string, allowEmpty = true): string[] {
  if (!Array.isArray(value) || value.length > MAX_ITEMS || (!allowEmpty && value.length === 0)) {
    throw new Error(`${name} must be a bounded array`)
  }
  const parsed = value.map((item, index) => text(item, `${name}[${String(index)}]`, 2_048))
  if (new Set(parsed).size !== parsed.length) throw new Error(`${name} must not contain duplicates`)
  return parsed
}

function repositoryPath(value: unknown, name: string): string {
  const parsed = text(value, name, 512)
  if (parsed.startsWith('/') || parsed.includes('\\') || parsed.split('/').some(part => part === '.' || part === '..')
    || parsed === '.git' || parsed.startsWith('.git/')) {
    throw new Error(`${name} must be a normalized repository-relative path`)
  }
  return parsed
}

function validateIdentity(root: Record<string, unknown>, context: ProposalValidationContext): void {
  if (root.schemaVersion !== 1 || root.runId !== context.runId || root.claimId !== context.claimId
    || root.taskNumber !== context.taskNumber) {
    throw new Error('Harness proposal identity does not match the active Run and Claim')
  }
}

function validateCriteria(value: unknown, context: ProposalValidationContext): CriterionProposal[] {
  if (!Array.isArray(value) || value.length !== context.criteria.length) {
    throw new Error('Harness proposal must cover every current criterion exactly once')
  }
  const expected = new Map(context.criteria.map(item => [item.id, item.revision]))
  const seen = new Set<string>()
  const waived = new Set(context.waivedCriterionIds ?? [])
  return value.map((raw, index) => {
    const item = record(raw, `criteria[${String(index)}]`)
    const criterionId = text(item.criterionId, `criteria[${String(index)}].criterionId`, 128)
    if (seen.has(criterionId) || expected.get(criterionId) !== item.criterionRevision) {
      throw new Error('Harness proposal criterion identity or revision is stale or duplicated')
    }
    seen.add(criterionId)
    if (!['passed', 'failed', 'unable', 'waived'].includes(String(item.outcome))) throw new Error('Harness proposal criterion outcome is invalid')
    if (item.outcome === 'waived' && !waived.has(criterionId)) {
      throw new Error('A criterion may be waived only after explicit typed resolution superseded it')
    }
    return {
      criterionId,
      criterionRevision: Number(item.criterionRevision),
      outcome: item.outcome as PactlineCheckOutcome,
      evidence: text(item.evidence, `criteria[${String(index)}].evidence`),
    }
  })
}

function validateVerification(value: unknown): VerificationProposal[] {
  if (!Array.isArray(value) || value.length > MAX_ITEMS) throw new Error('verification must be a bounded array')
  return value.map((raw, index) => {
    const item = record(raw, `verification[${String(index)}]`)
    if (!['passed', 'failed'].includes(String(item.outcome))) throw new Error('verification outcome is invalid')
    return {
      command: text(item.command, `verification[${String(index)}].command`, 2_048),
      outcome: item.outcome as 'passed' | 'failed',
      summary: text(item.summary, `verification[${String(index)}].summary`),
    }
  })
}

function requireFixedVerification(items: readonly VerificationProposal[], context: ProposalValidationContext): void {
  for (const command of context.verificationCommands) {
    const matches = items.filter(item => item.command === command)
    if (matches.length !== 1 || matches[0]?.outcome !== 'passed') {
      throw new Error(`Harness proposal must report the fixed verification command exactly once as passed: ${command}`)
    }
  }
}

function validateResolution(value: unknown, required: boolean): ResolutionRequestProposal | undefined {
  if (value === undefined) {
    if (required) throw new Error('resolutionRequest is required for request_resolution')
    return undefined
  }
  if (!required) throw new Error('resolutionRequest is allowed only for request_resolution')
  const item = record(value, 'resolutionRequest')
  if (!['decision_required', 'dependency_required'].includes(String(item.issueType))) throw new Error('resolutionRequest.issueType is invalid')
  return { issueType: item.issueType as PactlineIssueType, request: text(item.request, 'resolutionRequest.request') }
}

function body(root: Record<string, unknown>, context: ProposalValidationContext, needsResolution: boolean): Omit<ProposalBody, keyof ProposalIdentity> {
  const resolutionRequest = validateResolution(root.resolutionRequest, needsResolution)
  return {
    summary: text(root.summary, 'summary'),
    verification: validateVerification(root.verification),
    criteria: validateCriteria(root.criteria, context),
    limitations: stringList(root.limitations, 'limitations'),
    ...(resolutionRequest === undefined ? {} : { resolutionRequest }),
  }
}

export function validateHarnessProposal(value: unknown, context: ProposalValidationContext): HarnessProposal {
  const root = record(value, 'Harness proposal')
  validateIdentity(root, context)
  const expectedKind = context.stage === 'correction' ? 'execution' : context.stage
  if (root.kind !== expectedKind) throw new Error(`Harness proposal kind must be ${expectedKind}`)

  if (expectedKind === 'execution') {
    if (!['complete', 'request_resolution', 'unable_to_complete'].includes(String(root.recommendation))) throw new Error('execution recommendation is invalid')
    const recommendation = root.recommendation as ExecutionRecommendation
    const result: ExecutionProposal = {
      schemaVersion: 1, kind: 'execution', runId: context.runId, claimId: context.claimId, taskNumber: context.taskNumber,
      recommendation, changedPaths: stringList(root.changedPaths, 'changedPaths').map((item, index) => repositoryPath(item, `changedPaths[${String(index)}]`)),
      ...body(root, context, recommendation === 'request_resolution'),
    }
    if (recommendation === 'complete') {
      if (result.changedPaths.length === 0 || result.verification.some(item => item.outcome !== 'passed')
        || result.criteria.some(item => !['passed', 'waived'].includes(item.outcome))) {
        throw new Error('complete requires changed paths and passing verification for every criterion')
      }
      requireFixedVerification(result.verification, context)
    }
    return result
  }

  if (expectedKind === 'review') {
    if (!['accept', 'request_changes', 'request_resolution', 'unable_to_complete'].includes(String(root.recommendation))) throw new Error('review recommendation is invalid')
    const recommendation = root.recommendation as ReviewRecommendation
    if (!Array.isArray(root.findings) || root.findings.length > MAX_ITEMS) throw new Error('findings must be a bounded array')
    const findings = root.findings.map((raw, index): ReviewFinding => {
      const item = record(raw, `findings[${String(index)}]`)
      if (!Number.isSafeInteger(item.line) || Number(item.line) < 1 || !['low', 'medium', 'high'].includes(String(item.severity))) {
        throw new Error(`findings[${String(index)}] location or severity is invalid`)
      }
      return {
        path: repositoryPath(item.path, `findings[${String(index)}].path`),
        line: Number(item.line),
        severity: item.severity as ReviewFinding['severity'],
        category: text(item.category, `findings[${String(index)}].category`, 128),
        evidence: text(item.evidence, `findings[${String(index)}].evidence`),
        explanation: text(item.explanation, `findings[${String(index)}].explanation`),
      }
    })
    const result: ReviewProposal = {
      schemaVersion: 1, kind: 'review', runId: context.runId, claimId: context.claimId, taskNumber: context.taskNumber,
      recommendation, findings, ...body(root, context, recommendation === 'request_resolution'),
    }
    if (recommendation === 'accept') {
      if (findings.some(item => item.severity === 'high') || result.verification.some(item => item.outcome !== 'passed')
        || result.criteria.some(item => !['passed', 'waived'].includes(item.outcome))) {
        throw new Error('accept requires passing verification and criteria with no high-severity finding')
      }
      requireFixedVerification(result.verification, context)
    }
    if (recommendation === 'request_changes' && findings.length === 0) throw new Error('request_changes requires at least one finding')
    return result
  }

  if (!['request_resolution', 'unable_to_complete'].includes(String(root.recommendation))) throw new Error('resolution analysis recommendation is invalid')
  const recommendation = root.recommendation as ResolutionAnalysisProposal['recommendation']
  return {
    schemaVersion: 1, kind: 'resolution_analysis', runId: context.runId, claimId: context.claimId, taskNumber: context.taskNumber,
    recommendation, ...body(root, context, recommendation === 'request_resolution'),
  }
}

export function proposalResultSchema(context: ProposalValidationContext): Readonly<Record<string, unknown>> {
  const kind = context.stage === 'correction' ? 'execution' : context.stage
  const commonRequired = ['schemaVersion', 'kind', 'runId', 'claimId', 'taskNumber', 'recommendation', 'summary', 'verification', 'criteria', 'limitations']
  const recommendation = kind === 'execution'
    ? ['complete', 'request_resolution', 'unable_to_complete']
    : kind === 'review'
      ? ['accept', 'request_changes', 'request_resolution', 'unable_to_complete']
      : ['request_resolution', 'unable_to_complete']
  return {
    type: 'object',
    title: `${context.stage} proposal contract v1`,
    required: [...commonRequired, ...(kind === 'execution' ? ['changedPaths'] : []), ...(kind === 'review' ? ['findings'] : [])],
    additionalProperties: false,
    properties: {
      schemaVersion: { const: 1 },
      kind: { const: kind },
      runId: { const: context.runId },
      claimId: { const: context.claimId },
      taskNumber: { const: context.taskNumber },
      recommendation: { type: 'string', enum: recommendation },
      summary: { type: 'string', minLength: 1 },
      changedPaths: { type: 'array', maxItems: MAX_ITEMS, items: { type: 'string' } },
      findings: {
        type: 'array', maxItems: MAX_ITEMS, items: {
          type: 'object', additionalProperties: false,
          required: ['path', 'line', 'severity', 'category', 'evidence', 'explanation'],
          properties: {
            path: { type: 'string' }, line: { type: 'integer', minimum: 1 },
            severity: { type: 'string', enum: ['low', 'medium', 'high'] },
            category: { type: 'string' }, evidence: { type: 'string' }, explanation: { type: 'string' },
          },
        },
      },
      verification: {
        type: 'array', minItems: context.verificationCommands.length, maxItems: context.verificationCommands.length, items: {
          type: 'object', additionalProperties: false, required: ['command', 'outcome', 'summary'],
          properties: {
            command: { type: 'string', enum: [...context.verificationCommands] },
            outcome: { type: 'string', enum: ['passed', 'failed'] }, summary: { type: 'string' },
          },
        },
      },
      criteria: {
        type: 'array', minItems: context.criteria.length, maxItems: context.criteria.length,
        items: {
          type: 'object', additionalProperties: false,
          required: ['criterionId', 'criterionRevision', 'outcome', 'evidence'],
          properties: {
            criterionId: { type: 'string', enum: context.criteria.map(item => item.id) },
            criterionRevision: { type: 'integer', enum: [...new Set(context.criteria.map(item => item.revision))] },
            outcome: { type: 'string', enum: ['passed', 'failed', 'unable', 'waived'] }, evidence: { type: 'string' },
          },
        },
      },
      limitations: { type: 'array', maxItems: MAX_ITEMS, items: { type: 'string' } },
      resolutionRequest: {
        type: 'object', additionalProperties: false, required: ['issueType', 'request'],
        properties: { issueType: { type: 'string', enum: ['decision_required', 'dependency_required'] }, request: { type: 'string' } },
      },
    },
  }
}
