import type { FleetExternalEffectRecord } from './external-effect.js'

export const TERMINAL_RUN_STATES = ['completed', 'released', 'quarantined', 'failed'] as const
export const RUN_STAGES = ['execution', 'review', 'correction'] as const

export type FleetRunState =
  | 'admitted'
  | 'claiming'
  | 'claimed'
  | 'preparing_workspace'
  | 'starting_harness'
  | 'running_harness'
  | 'validating'
  | 'delivering'
  | 'settling'
  | 'releasing'
  | (typeof TERMINAL_RUN_STATES)[number]

export type FleetRunStage = (typeof RUN_STAGES)[number]

export interface FleetRunIdentity {
  readonly runId: string
  readonly serviceId: string
  readonly fleetId: string
  readonly projectNumber: number
  readonly configRevision: string
}

export interface FleetRunAdmission {
  readonly taskNumber: number
  readonly taskVersion: number
  readonly stage: FleetRunStage
  /** Complete credential-free policy frozen before Claim creation. */
  readonly frozenPolicy: Readonly<Record<string, unknown>>
}

export interface FleetRunUpdate {
  readonly claimId?: string
  readonly claimVersion?: number
  readonly claimTaskVersion?: number
  readonly runtimeSessionId?: string
  readonly workspace?: Readonly<Record<string, unknown>>
  readonly checkpoint?: string
  readonly disposition?: string
  readonly error?: string
}

export interface FleetRunEffectChange {
  readonly type: 'intent' | 'observation'
  readonly kind: FleetExternalEffectRecord['kind']
}

export interface FleetRun extends FleetRunIdentity, FleetRunAdmission, FleetRunUpdate {
  readonly state: FleetRunState
  readonly createdAt: string
  readonly updatedAt: string
}

export type FleetTerminalDisposition =
  | { readonly kind: 'completed_settlement'; readonly outcome: string; readonly claimId: string }
  | { readonly kind: 'local_release'; readonly reason: string }
  | { readonly kind: 'claim_release'; readonly reason: string; readonly claimId: string }
  | { readonly kind: 'quarantine'; readonly reason: string; readonly claimId?: string }
  | { readonly kind: 'legacy_failed'; readonly reason?: string }

/** Schema-v1 compatibility projection. New application behavior cannot create one. */
export interface LegacyFleetRunRecord extends FleetRunIdentity, FleetRunUpdate {
  readonly legacy: true
  readonly state: FleetRunState
  readonly taskNumber?: never
  readonly taskVersion?: never
  readonly stage?: never
  readonly frozenPolicy?: never
  readonly createdAt: string
  readonly updatedAt: string
}

/** Compatibility name for observation and recovery callers. */
export type FleetRunRecord = FleetRun | LegacyFleetRunRecord

const allowedTransitions: Readonly<Record<FleetRunState, readonly FleetRunState[]>> = {
  admitted: ['claiming', 'releasing', 'released', 'quarantined'],
  claiming: ['claimed', 'releasing', 'released', 'quarantined'],
  claimed: ['preparing_workspace', 'releasing', 'quarantined'],
  preparing_workspace: ['starting_harness', 'releasing', 'quarantined'],
  starting_harness: ['running_harness', 'releasing', 'quarantined'],
  running_harness: ['validating', 'releasing', 'quarantined'],
  validating: ['delivering', 'settling', 'releasing', 'quarantined'],
  delivering: ['settling', 'releasing', 'quarantined'],
  settling: ['completed', 'released', 'quarantined'],
  releasing: ['released', 'quarantined'],
  completed: [],
  released: [],
  quarantined: [],
  failed: [],
}

function assertPositive(value: number, name: string): void {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error(`${name} must be positive`)
}

function assertAdmission(admission: FleetRunAdmission): void {
  assertPositive(admission.taskNumber, 'Task number')
  assertPositive(admission.taskVersion, 'Task version')
  if (!RUN_STAGES.includes(admission.stage)) throw new Error('Run stage is invalid')
}

function record(value: unknown, name: string): Readonly<Record<string, unknown>> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`Fleet Run ${name} is invalid`)
  return value as Readonly<Record<string, unknown>>
}

function requiredString(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`Fleet Run ${name} is invalid`)
  return value
}

function optionalString(value: unknown, name: string): string | undefined {
  return value === undefined ? undefined : requiredString(value, name)
}

function requiredPositive(value: unknown, name: string): number {
  if (typeof value !== 'number') throw new Error(`Fleet Run ${name} is invalid`)
  assertPositive(value, name)
  return value
}

function optionalPositive(value: unknown, name: string): number | undefined {
  return value === undefined ? undefined : requiredPositive(value, name)
}

export const RUN_STATES: readonly FleetRunState[] = [
  'admitted', 'claiming', 'claimed', 'preparing_workspace', 'starting_harness',
  'running_harness', 'validating', 'delivering', 'settling', 'releasing',
  ...TERMINAL_RUN_STATES,
]

const CLAIM_REQUIRED_STATES: readonly FleetRunState[] = [
  'claimed', 'preparing_workspace', 'starting_harness', 'running_harness',
  'validating', 'delivering', 'settling', 'completed',
]

const WORKSPACE_REQUIRED_STATES: readonly FleetRunState[] = [
  'starting_harness', 'running_harness', 'validating', 'delivering', 'settling', 'completed',
]

const SESSION_REQUIRED_STATES: readonly FleetRunState[] = [
  'running_harness', 'validating', 'delivering', 'settling', 'completed',
]

/** Decode persisted data into a domain-valid Run. */
export function decodeRun(value: unknown, effects: readonly FleetExternalEffectRecord[] = []): FleetRun {
  const item = record(value, 'record')
  const state = requiredString(item.state, 'state') as FleetRunState
  if (!RUN_STATES.includes(state)) throw new Error('Fleet Run state is invalid')
  const stage = requiredString(item.stage, 'stage') as FleetRunStage
  const claimId = optionalString(item.claimId, 'Claim identity')
  const claimVersion = optionalPositive(item.claimVersion, 'Claim version')
  const claimTaskVersion = optionalPositive(item.claimTaskVersion, 'post-Claim Task version')
  const runtimeSessionId = optionalString(item.runtimeSessionId, 'Adapter Session identity')
  const checkpoint = optionalString(item.checkpoint, 'checkpoint')
  const disposition = optionalString(item.disposition, 'disposition')
  const error = optionalString(item.error, 'error')
  const run: FleetRun = {
    runId: requiredString(item.runId, 'identity'),
    serviceId: requiredString(item.serviceId, 'service identity'),
    fleetId: requiredString(item.fleetId, 'Fleet identity'),
    projectNumber: requiredPositive(item.projectNumber, 'Project number'),
    configRevision: requiredString(item.configRevision, 'configuration revision'),
    taskNumber: requiredPositive(item.taskNumber, 'Task number'),
    taskVersion: requiredPositive(item.taskVersion, 'Task version'),
    stage,
    frozenPolicy: record(item.frozenPolicy, 'frozen policy'),
    state,
    ...(claimId === undefined ? {} : { claimId }),
    ...(claimVersion === undefined ? {} : { claimVersion }),
    ...(claimTaskVersion === undefined ? {} : { claimTaskVersion }),
    ...(runtimeSessionId === undefined ? {} : { runtimeSessionId }),
    ...(item.workspace === undefined ? {} : { workspace: record(item.workspace, 'workspace') }),
    ...(checkpoint === undefined ? {} : { checkpoint }),
    ...(disposition === undefined ? {} : { disposition }),
    ...(error === undefined ? {} : { error }),
    createdAt: requiredString(item.createdAt, 'creation timestamp'),
    updatedAt: requiredString(item.updatedAt, 'update timestamp'),
  }
  if (!RUN_STAGES.includes(run.stage)) throw new Error('Run stage is invalid')
  if (CLAIM_REQUIRED_STATES.includes(run.state)
    && (run.claimId === undefined || run.claimTaskVersion === undefined)) {
    throw new Error(`Fleet Run ${run.state} requires Claim identity and post-Claim Task version`)
  }
  if (WORKSPACE_REQUIRED_STATES.includes(run.state) && run.workspace === undefined) {
    throw new Error(`Fleet Run ${run.state} requires a persisted workspace`)
  }
  if (SESSION_REQUIRED_STATES.includes(run.state) && run.runtimeSessionId === undefined) {
    throw new Error(`Fleet Run ${run.state} requires an Adapter Session identity`)
  }
  if (run.claimId === undefined && (run.claimVersion !== undefined || run.claimTaskVersion !== undefined)) {
    throw new Error('Fleet Run Claim versions require Claim identity')
  }
  if (['completed', 'released', 'quarantined'].includes(run.state) && run.disposition === undefined) {
    throw new Error(`Fleet Run ${run.state} requires a terminal disposition`)
  }
  if (['validating', 'delivering', 'settling', 'completed'].includes(run.state)) {
    const harnessResult = effects.find(effect => effect.kind === 'harness_result')
    if (harnessResult?.status !== 'observed') {
      throw new Error(`Fleet Run ${run.state} requires an observed Harness result`)
    }
    if (harnessResult.observation.runtimeSessionId !== run.runtimeSessionId) {
      throw new Error('Fleet Run Harness result changed the Adapter Session identity')
    }
    if (run.state === 'delivering') {
      const result = record(harnessResult.observation.result, 'Harness result')
      const proposal = record(result.proposal, 'Harness proposal')
      if (proposal.kind !== 'execution' || proposal.recommendation !== 'complete') {
        throw new Error('Fleet Run delivering requires a complete execution proposal')
      }
    }
  }
  return run
}

/** Decode current rows strictly while preserving fully policy-free schema-v1 rows for observation. */
export function decodeRunRecord(
  value: unknown,
  effects: readonly FleetExternalEffectRecord[] = [],
): FleetRunRecord {
  const item = record(value, 'record')
  const admissionValues = [item.taskNumber, item.taskVersion, item.stage, item.frozenPolicy]
  if (admissionValues.every(part => part === undefined)) {
    const state = requiredString(item.state, 'state') as FleetRunState
    if (!RUN_STATES.includes(state)) throw new Error('Fleet Run state is invalid')
    const claimId = optionalString(item.claimId, 'Claim identity')
    const claimVersion = optionalPositive(item.claimVersion, 'Claim version')
    const claimTaskVersion = optionalPositive(item.claimTaskVersion, 'post-Claim Task version')
    const runtimeSessionId = optionalString(item.runtimeSessionId, 'Adapter Session identity')
    const checkpoint = optionalString(item.checkpoint, 'checkpoint')
    const disposition = optionalString(item.disposition, 'disposition')
    const error = optionalString(item.error, 'error')
    return {
      legacy: true,
      runId: requiredString(item.runId, 'identity'),
      serviceId: requiredString(item.serviceId, 'service identity'),
      fleetId: requiredString(item.fleetId, 'Fleet identity'),
      projectNumber: requiredPositive(item.projectNumber, 'Project number'),
      configRevision: requiredString(item.configRevision, 'configuration revision'),
      state,
      ...(claimId === undefined ? {} : { claimId }),
      ...(claimVersion === undefined ? {} : { claimVersion }),
      ...(claimTaskVersion === undefined ? {} : { claimTaskVersion }),
      ...(runtimeSessionId === undefined ? {} : { runtimeSessionId }),
      ...(item.workspace === undefined ? {} : { workspace: record(item.workspace, 'workspace') }),
      ...(checkpoint === undefined ? {} : { checkpoint }),
      ...(disposition === undefined ? {} : { disposition }),
      ...(error === undefined ? {} : { error }),
      createdAt: requiredString(item.createdAt, 'creation timestamp'),
      updatedAt: requiredString(item.updatedAt, 'update timestamp'),
    }
  }
  if (admissionValues.some(part => part === undefined)) throw new Error('Fleet Run frozen admission is incomplete')
  return decodeRun(value, effects)
}

export function isLegacyRun(run: FleetRunRecord): run is LegacyFleetRunRecord {
  return 'legacy' in run && run.legacy
}

export function requireRun(run: FleetRunRecord | undefined): FleetRun {
  if (run === undefined) throw new Error('Fleet Run does not exist')
  if (isLegacyRun(run)) throw new Error(`Fleet Run ${run.runId} has no frozen admission`)
  return run
}

export type ClaimedFleetRun = FleetRun & {
  readonly claimId: string
  readonly claimTaskVersion: number
}

export type SessionFleetRun = ClaimedFleetRun & {
  readonly workspace: Readonly<Record<string, unknown>>
  readonly runtimeSessionId: string
}

export function requireClaimedRun(run: FleetRun): ClaimedFleetRun {
  if (run.claimId === undefined || run.claimTaskVersion === undefined) {
    throw new Error(`Fleet Run ${run.runId} has no complete Claim facts`)
  }
  return run as ClaimedFleetRun
}

export function requireSessionRun(run: FleetRun): SessionFleetRun {
  const claimed = requireClaimedRun(run)
  if (claimed.workspace === undefined || claimed.runtimeSessionId === undefined) {
    throw new Error(`Fleet Run ${run.runId} has no complete Adapter Session facts`)
  }
  return claimed as SessionFleetRun
}

export function admitRun(
  identity: FleetRunIdentity,
  admission: FleetRunAdmission,
  timestamp: string,
): FleetRun {
  assertAdmission(admission)
  assertPositive(identity.projectNumber, 'Project number')
  return decodeRun({
    ...identity,
    ...admission,
    state: 'admitted',
    checkpoint: 'run_admitted',
    createdAt: timestamp,
    updatedAt: timestamp,
  })
}

export function transitionRun(
  current: FleetRun,
  next: FleetRunState,
  update: FleetRunUpdate,
  timestamp: string,
  effects?: readonly FleetExternalEffectRecord[],
): FleetRun
export function transitionRun(
  current: LegacyFleetRunRecord,
  next: FleetRunState,
  update: FleetRunUpdate,
  timestamp: string,
  effects?: readonly FleetExternalEffectRecord[],
): LegacyFleetRunRecord
export function transitionRun(
  current: FleetRunRecord,
  next: FleetRunState,
  update: FleetRunUpdate,
  timestamp: string,
  effects: readonly FleetExternalEffectRecord[] = [],
): FleetRunRecord {
  if (!allowedTransitions[current.state].includes(next)) {
    throw new Error(`Invalid Fleet Run transition: ${current.state} -> ${next}`)
  }
  if (update.claimId !== undefined && current.claimId !== undefined && update.claimId !== current.claimId) {
    throw new Error('Fleet Run Claim identity is immutable')
  }
  if (update.claimTaskVersion !== undefined && current.claimTaskVersion !== undefined
    && update.claimTaskVersion !== current.claimTaskVersion) {
    throw new Error('Fleet Run post-Claim Task version is immutable')
  }
  if (update.workspace !== undefined && current.workspace !== undefined
    && JSON.stringify(update.workspace) !== JSON.stringify(current.workspace)) {
    throw new Error('Fleet Run workspace is immutable')
  }
  if (update.runtimeSessionId !== undefined && current.runtimeSessionId !== undefined
    && update.runtimeSessionId !== current.runtimeSessionId) {
    throw new Error('Fleet Run Adapter Session identity is immutable')
  }
  const changed = { ...current, ...update, state: next, updatedAt: timestamp }
  return isLegacyRun(current) ? decodeRunRecord(changed, effects) : decodeRun(changed, effects)
}

/** Require the external-effect fact that gives a boundary transition its meaning. */
export function assertRunDecisionEffect(
  current: FleetRunRecord,
  next: FleetRunState,
  effect: FleetRunEffectChange | undefined,
): void {
  let expected: FleetRunEffectChange | readonly FleetRunEffectChange[] | undefined
  if (current.state === 'admitted' && next === 'claiming') expected = { type: 'intent', kind: 'claim' }
  else if (current.state === 'claiming' && next === 'claimed') expected = { type: 'observation', kind: 'claim' }
  else if (current.state === 'claimed' && next === 'preparing_workspace') expected = { type: 'intent', kind: 'workspace' }
  else if (current.state === 'preparing_workspace' && next === 'starting_harness') expected = { type: 'observation', kind: 'workspace' }
  else if (current.state === 'starting_harness' && next === 'running_harness') expected = { type: 'observation', kind: 'adapter_session' }
  else if (current.state === 'running_harness' && next === 'validating') expected = { type: 'observation', kind: 'harness_result' }
  else if (current.state === 'validating' && next === 'delivering') {
    expected = [
      { type: 'intent', kind: 'repository_delivery' },
      { type: 'intent', kind: 'git_commit' },
    ]
  } else if ((current.state === 'validating' || current.state === 'delivering') && next === 'settling') {
    expected = { type: 'intent', kind: 'pactline_settlement' }
  } else if (current.state === 'settling' && (next === 'completed' || next === 'released')) {
    expected = { type: 'observation', kind: 'pactline_settlement' }
  } else if (current.state === 'releasing' && next === 'released') {
    expected = { type: 'observation', kind: 'claim_release' }
  } else if (next === 'releasing' && current.claimId !== undefined) {
    expected = { type: 'intent', kind: 'claim_release' }
  }
  if (expected === undefined) return
  const accepted = Array.isArray(expected) ? expected : [expected]
  if (effect === undefined || !accepted.some(value => value.type === effect.type && value.kind === effect.kind)) {
    throw new Error(`Fleet Run ${current.state} -> ${next} requires its atomic External Effect fact`)
  }
}

export function isTerminalRunState(state: FleetRunState): boolean {
  return (TERMINAL_RUN_STATES as readonly FleetRunState[]).includes(state)
}

export function terminalDisposition(run: FleetRun): FleetTerminalDisposition {
  const disposition = run.disposition
  switch (run.state) {
    case 'completed':
      if (disposition === undefined || run.claimId === undefined) throw new Error('Completed Fleet Run has no settlement disposition')
      return { kind: 'completed_settlement', outcome: disposition, claimId: run.claimId }
    case 'released':
      if (disposition === undefined) throw new Error('Released Fleet Run has no disposition')
      return run.claimId === undefined
        ? { kind: 'local_release', reason: disposition }
        : { kind: 'claim_release', reason: disposition, claimId: run.claimId }
    case 'quarantined':
      if (disposition === undefined) throw new Error('Quarantined Fleet Run has no disposition')
      return { kind: 'quarantine', reason: disposition, ...(run.claimId === undefined ? {} : { claimId: run.claimId }) }
    case 'failed': return { kind: 'legacy_failed', ...(disposition === undefined ? {} : { reason: disposition }) }
    default: throw new Error(`Fleet Run ${run.runId} is not terminal`)
  }
}
