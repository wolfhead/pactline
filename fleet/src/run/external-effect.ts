import type { FleetRunStage } from './run.js'
import type { ExecutionProposal, ReviewProposal } from '../core/harness-result.js'
import type { RepositoryDelivery } from '../repository/delivery.js'
import { validateRepositoryDelivery } from '../repository/delivery.js'

export const EXTERNAL_EFFECT_KINDS = [
  'claim',
  'workspace',
  'adapter_session',
  'harness_result',
  'git_commit',
  'git_push',
  'code_change_creation',
  'repository_delivery',
  'pactline_settlement',
  'claim_release',
] as const

export type FleetExternalEffectKind = (typeof EXTERNAL_EFFECT_KINDS)[number]
export type FleetExternalEffectStatus = 'intended' | 'observed'
export type FleetExternalEffectUncertainty = 'local' | 'reconcilable' | 'ambiguous_external'

interface RevisionBranch extends Readonly<Record<string, unknown>> {
  readonly revision: string
  readonly branch: string
}

export interface PactlineSettlementIntent extends Readonly<Record<string, unknown>> {
  readonly stage: 'execution' | 'review'
  readonly taskVersion: number
  readonly proposal: ExecutionProposal | ReviewProposal
  readonly delivery?: RepositoryDelivery
}

interface EffectDetails {
  readonly claim: {
    readonly intent: { readonly taskNumber: number; readonly taskVersion: number; readonly stage: FleetRunStage }
    readonly observation: { readonly claimId: string; readonly taskVersion: number; readonly claimVersion?: number }
  }
  readonly workspace: {
    readonly intent: { readonly mode: 'execution' | 'review' }
    readonly observation: Readonly<Record<string, unknown>>
  }
  readonly adapter_session: {
    readonly intent: { readonly adapter: string }
    readonly observation: { readonly runtimeSessionId: string }
  }
  readonly harness_result: {
    readonly intent: Readonly<Record<string, never>>
    readonly observation: {
      readonly terminalState: string
      readonly runtimeSessionId: string
      readonly result: unknown
    }
  }
  readonly git_commit: {
    readonly intent: Readonly<Record<string, never>>
    readonly observation: RevisionBranch
  }
  readonly git_push: {
    readonly intent: RevisionBranch
    readonly observation: RevisionBranch
  }
  readonly code_change_creation: {
    readonly intent: RevisionBranch
    readonly observation: RevisionBranch & { readonly codeChangeUrl: string }
  }
  readonly repository_delivery: {
    readonly intent: Readonly<Record<string, never>>
    readonly observation: RevisionBranch & { readonly codeChangeUrl: string }
  }
  readonly pactline_settlement: {
    readonly intent: PactlineSettlementIntent
    readonly observation: Readonly<Record<string, unknown>> & {
      readonly claimId: string
      readonly claimStatus: string
      readonly taskVersion: number
    }
  }
  readonly claim_release: {
    readonly intent: { readonly reason: string }
    readonly observation: { readonly claimStatus: string; readonly taskVersion: number }
  }
}

export type FleetExternalEffectIntent<K extends FleetExternalEffectKind> = EffectDetails[K]['intent']
export type FleetExternalEffectObservation<K extends FleetExternalEffectKind> = EffectDetails[K]['observation']

interface EffectRecordBase<K extends FleetExternalEffectKind> {
  readonly runId: string
  readonly kind: K
  readonly idempotencyKey: string
  readonly createdAt: string
  readonly updatedAt: string
}

export type FleetExternalEffectRecord<K extends FleetExternalEffectKind = FleetExternalEffectKind> = {
  readonly [P in K]: EffectRecordBase<P> & {
    readonly intent: FleetExternalEffectIntent<P>
  } & (
    | { readonly status: 'intended'; readonly observation?: never }
    | { readonly status: 'observed'; readonly observation: FleetExternalEffectObservation<P> }
  )
}[K]

function record(value: unknown, name: string): Readonly<Record<string, unknown>> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`Fleet External Effect ${name} is invalid`)
  }
  return value as Readonly<Record<string, unknown>>
}

function string(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`Fleet External Effect ${name} is invalid`)
  return value
}

function nonEmpty(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== ''
}

function positive(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

function revisionBranch(value: Readonly<Record<string, unknown>>): boolean {
  return typeof value.revision === 'string' && /^[a-f0-9]{40}$/.test(value.revision) && nonEmpty(value.branch)
}

function validSettlementIntent(value: Readonly<Record<string, unknown>>): boolean {
  if (!positive(value.taskVersion) || (value.stage !== 'execution' && value.stage !== 'review')) return false
  if (typeof value.proposal !== 'object' || value.proposal === null || Array.isArray(value.proposal)) return false
  const proposal = value.proposal as Readonly<Record<string, unknown>>
  if (proposal.kind !== value.stage) return false
  const requiresDelivery = value.stage === 'execution' && proposal.recommendation === 'complete'
  if (requiresDelivery !== (value.delivery !== undefined)) return false
  if (value.delivery !== undefined) {
    try {
      validateRepositoryDelivery(value.delivery as RepositoryDelivery)
    } catch {
      return false
    }
  }
  return true
}

function validIntent(kind: FleetExternalEffectKind, value: Readonly<Record<string, unknown>>): boolean {
  switch (kind) {
    case 'claim':
      return positive(value.taskNumber) && positive(value.taskVersion)
        && ['execution', 'review', 'correction'].includes(String(value.stage))
    case 'workspace': return value.mode === 'execution' || value.mode === 'review'
    case 'adapter_session': return nonEmpty(value.adapter)
    case 'harness_result':
    case 'git_commit':
    case 'repository_delivery': return Object.keys(value).length === 0
    case 'git_push':
    case 'code_change_creation': return revisionBranch(value)
    case 'pactline_settlement': return validSettlementIntent(value)
    case 'claim_release': return nonEmpty(value.reason)
  }
}

function validObservation(kind: FleetExternalEffectKind, value: Readonly<Record<string, unknown>>): boolean {
  switch (kind) {
    case 'claim':
      return nonEmpty(value.claimId) && positive(value.taskVersion)
        && (value.claimVersion === undefined || positive(value.claimVersion))
    case 'workspace':
      return (value.mode === 'execution' || value.mode === 'review')
        && ['root', 'temporaryParent', 'repositoryPath', 'source', 'baseRevision'].every(key => nonEmpty(value[key]))
    case 'adapter_session': return nonEmpty(value.runtimeSessionId)
    case 'harness_result':
      return nonEmpty(value.terminalState) && nonEmpty(value.runtimeSessionId) && value.result !== undefined
    case 'git_commit':
    case 'git_push': return revisionBranch(value)
    case 'code_change_creation':
    case 'repository_delivery': return revisionBranch(value) && nonEmpty(value.codeChangeUrl)
    case 'pactline_settlement':
      return nonEmpty(value.claimId) && nonEmpty(value.claimStatus) && positive(value.taskVersion)
    case 'claim_release': return nonEmpty(value.claimStatus) && positive(value.taskVersion)
  }
}

function normalized(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(normalized).join(',')}]`
  if (typeof value === 'object' && value !== null) {
    const item = value as Readonly<Record<string, unknown>>
    return `{${Object.keys(item).sort().map(key => `${JSON.stringify(key)}:${normalized(item[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

/** Decode a persisted effect through the closed Fleet effect vocabulary. */
export function decodeExternalEffect(value: unknown): FleetExternalEffectRecord {
  const item = record(value, 'record')
  const kind = string(item.kind, 'kind') as FleetExternalEffectKind
  if (!EXTERNAL_EFFECT_KINDS.includes(kind)) throw new Error('Fleet External Effect kind is invalid')
  const status = string(item.status, 'status')
  if (status !== 'intended' && status !== 'observed') throw new Error('Fleet External Effect status is invalid')
  const intent = record(item.intent, 'intent')
  if (!validIntent(kind, intent)) throw new Error(`Fleet External Effect ${kind} intent is invalid`)
  const base = {
    runId: string(item.runId, 'Run identity'),
    kind,
    idempotencyKey: string(item.idempotencyKey, 'idempotency key'),
    intent,
    createdAt: string(item.createdAt, 'creation timestamp'),
    updatedAt: string(item.updatedAt, 'update timestamp'),
  }
  if (status === 'intended') {
    if (item.observation !== undefined) throw new Error('Fleet External Effect intent cannot have an observation')
    return { ...base, status } as FleetExternalEffectRecord
  }
  if (item.observation === undefined) throw new Error('Observed Fleet External Effect requires an observation')
  const observation = record(item.observation, 'observation')
  if (!validObservation(kind, observation)) throw new Error(`Fleet External Effect ${kind} observation is invalid`)
  return { ...base, status, observation } as FleetExternalEffectRecord
}

/** Create a typed intent at the boundary where an external action becomes authorized. */
export function createExternalEffectIntent<K extends FleetExternalEffectKind>(
  runId: string,
  kind: K,
  idempotencyKey: string,
  intent: FleetExternalEffectIntent<K>,
  timestamp: string,
): FleetExternalEffectRecord<K> {
  return decodeExternalEffect({
    runId,
    kind,
    idempotencyKey,
    status: 'intended',
    intent,
    createdAt: timestamp,
    updatedAt: timestamp,
  }) as FleetExternalEffectRecord<K>
}

/** An intent replay is idempotent only when both its identity and typed facts match. */
export function replayExternalEffectIntent<K extends FleetExternalEffectKind>(
  current: FleetExternalEffectRecord<K>,
  idempotencyKey: string,
  intent: FleetExternalEffectIntent<K>,
): FleetExternalEffectRecord<K> {
  if (current.idempotencyKey !== idempotencyKey || normalized(current.intent) !== normalized(intent)) {
    throw new Error(`Fleet External Effect ${current.kind} intent is immutable`)
  }
  return current
}

/** Record one observation exactly once; identical recovery replay is idempotent. */
export function observeExternalEffect<K extends FleetExternalEffectKind>(
  current: FleetExternalEffectRecord<K>,
  observation: FleetExternalEffectObservation<K>,
  timestamp: string,
): FleetExternalEffectRecord<K> {
  if (current.status === 'observed') {
    if (normalized(current.observation) !== normalized(observation)) {
      throw new Error(`Observed Fleet External Effect ${current.kind} is immutable`)
    }
    return current
  }
  return decodeExternalEffect({
    ...current,
    status: 'observed',
    observation,
    updatedAt: timestamp,
  }) as FleetExternalEffectRecord<K>
}

/** Classify an unobserved intent by the recovery certainty available to Fleet. */
export function effectUncertainty(kind: FleetExternalEffectKind): FleetExternalEffectUncertainty {
  switch (kind) {
    case 'workspace': return 'local'
    case 'claim':
    case 'adapter_session':
    case 'harness_result': return 'reconcilable'
    case 'git_commit':
    case 'git_push':
    case 'code_change_creation':
    case 'repository_delivery':
    case 'pactline_settlement':
    case 'claim_release': return 'ambiguous_external'
  }
}

export function hasAmbiguousExternalEffect(effects: readonly FleetExternalEffectRecord[]): boolean {
  return effects.some(effect => effect.status === 'intended' && effectUncertainty(effect.kind) === 'ambiguous_external')
}
