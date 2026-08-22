/** Machine-facing JSON contracts consumed from the Pactline CLI. */

export interface PactlineResponseMeta {
  readonly request_id?: string
  readonly etag?: string
  readonly idempotency_key?: string
}

export interface PactlineSuccess<T> {
  readonly ok: true
  readonly data: T
  readonly meta?: PactlineResponseMeta
}

export interface PactlineErrorBody {
  readonly code: string
  readonly message: string
  readonly hint?: string
  readonly request_id?: string
  readonly retry_after_seconds?: number
}

export interface PactlineFailure {
  readonly ok: false
  readonly error: PactlineErrorBody
}

export interface PactlineCapabilities {
  readonly cli_version: string
  readonly protocol: number
  readonly features: readonly string[]
}

export interface PactlineDoctor {
  readonly server: string
  readonly client_kind: string
  readonly session_id: string
  readonly token: string
  readonly principal: unknown
}

export type PactlineClientErrorCode =
  | 'ABORTED'
  | 'CLI_ERROR'
  | 'INVALID_RESPONSE'
  | 'MISSING_FEATURE'
  | 'OUTPUT_LIMIT'
  | 'PROTOCOL_MISMATCH'
  | 'SPAWN_FAILED'
  | 'TIMEOUT'

export interface PactlinePreflightResult {
  readonly capabilities: PactlineCapabilities
  readonly doctor?: PactlineDoctor
}

export interface PactlineOperation<T> {
  readonly data: T
  readonly meta?: PactlineResponseMeta
}

export type PactlineClaimStage = 'execution' | 'review'
export type PactlineCheckOutcome = 'passed' | 'failed' | 'unable' | 'waived'
export type PactlineIssueType = 'decision_required' | 'dependency_required'

/** Private wire DTO. Domain callers receive a Fleet candidate projection from the Adapter. */
export interface PactlineTaskSummaryDTO {
  readonly id: string
  readonly number: number
  readonly title: string
  readonly version: number
  readonly phase: string
  readonly activity: string
}

export interface PactlineClaimSummary {
  readonly id: string
  readonly task_number: number
  readonly stage: PactlineClaimStage
  readonly status: string
  readonly outcome?: string
  readonly version: number
}

export interface PactlineWorkflowSummary {
  readonly task_number: number
  readonly version: number
  readonly phase: string
  readonly activity: string
}

export interface PactlineClaimMutationResult {
  readonly task: PactlineWorkflowSummary
  readonly claim: PactlineClaimSummary
  readonly [key: string]: unknown
}

export interface PactlineCodeChangeMutationResult {
  readonly task: PactlineWorkflowSummary
  readonly code_change: Record<string, unknown>
  readonly changed: boolean
}
