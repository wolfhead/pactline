import { randomUUID } from 'node:crypto'
import Database from 'better-sqlite3'
import type { FleetConfigSnapshot } from '../config/types.js'
import {
  admitRun as admitDomainRun,
  assertRunDecisionEffect,
  decodeRunRecord,
  isLegacyRun,
  TERMINAL_RUN_STATES,
  transitionRun as transitionDomainRun,
} from '../run/run.js'
import type {
  FleetRun,
  FleetRunAdmission,
  FleetRunRecord,
  FleetRunStage,
  FleetRunState,
  FleetRunUpdate,
} from '../run/run.js'
import {
  createExternalEffectIntent,
  decodeExternalEffect,
  observeExternalEffect,
  replayExternalEffectIntent,
} from '../run/external-effect.js'
import type {
  FleetExternalEffectIntent,
  FleetExternalEffectKind,
  FleetExternalEffectObservation,
  FleetExternalEffectRecord,
} from '../run/external-effect.js'
import { preparePrivateFile } from '../service/state-directory.js'
import type { RunRecoveryDecision, RunRecoveryFacts } from '../run/recovery.js'
import { decodeFleetWorkspace } from '../repository/workspace.js'
import type { FleetWorkspace } from '../repository/workspace.js'

const SCHEMA_VERSION = 4

export type { FleetRun, FleetRunAdmission, FleetRunRecord, FleetRunStage, FleetRunState, FleetRunUpdate } from '../run/run.js'
export type {
  FleetExternalEffectIntent,
  FleetExternalEffectKind,
  FleetExternalEffectObservation,
  FleetExternalEffectRecord,
  FleetExternalEffectStatus,
} from '../run/external-effect.js'

export type FleetExternalEffectDecision = {
  readonly [K in FleetExternalEffectKind]:
    | {
        readonly type: 'intent'
        readonly kind: K
        readonly idempotencyKey: string
        readonly intent: FleetExternalEffectIntent<K>
      }
    | {
        readonly type: 'observation'
        readonly kind: K
        readonly observation: FleetExternalEffectObservation<K>
      }
}[FleetExternalEffectKind]

export interface FleetRunDecision {
  readonly expected: {
    readonly state: FleetRunState
    /** The persisted Run timestamp is its optimistic local version token. */
    readonly updatedAt: string
  }
  readonly transition?: {
    readonly state: FleetRunState
    readonly update?: FleetRunUpdate
  }
  readonly effect?: FleetExternalEffectDecision
}

export interface FleetRunDecisionResult {
  readonly run: FleetRunRecord
  readonly effect?: FleetExternalEffectRecord
}

export interface FleetRunEventRecord {
  readonly sequence: number
  readonly runId: string
  readonly eventType: string
  readonly payload: Readonly<Record<string, unknown>>
  readonly createdAt: string
}

export interface FleetRunListOptions {
  readonly fleetId?: string
  readonly state?: FleetRunState
  readonly stage?: FleetRunStage
  readonly limit?: number
  readonly before?: string
}

export type TaskRole = 'implementer' | 'reviewer'

export interface TaskRoleSessionBinding {
  readonly adapterId: string
  readonly runtimeSessionId: string
}

export interface FleetTaskRuntime {
  readonly projectNumber: number
  readonly taskNumber: number
  readonly workspace: FleetWorkspace
  readonly sessions: Partial<Record<TaskRole, TaskRoleSessionBinding>>
}

interface FleetRow {
  readonly fleet_id: string
  readonly project_number: number
  readonly config_revision: string
}

interface RunRow {
  readonly run_id: string
  readonly service_id: string
  readonly fleet_id: string
  readonly project_number: number
  readonly config_revision: string
  readonly state: FleetRunState
  readonly task_number: number | null
  readonly task_version: number | null
  readonly stage: FleetRunStage | null
  readonly frozen_policy_json: string | null
  readonly claim_id: string | null
  readonly claim_version: number | null
  readonly claim_task_version: number | null
  readonly runtime_session_id: string | null
  readonly workspace_json: string | null
  readonly checkpoint: string | null
  readonly disposition: string | null
  readonly error: string | null
  readonly created_at: string
  readonly updated_at: string
}

interface EffectRow {
  readonly run_id: string
  readonly kind: string
  readonly idempotency_key: string
  readonly status: string
  readonly intent_json: string
  readonly observation_json: string | null
  readonly created_at: string
  readonly updated_at: string
}

interface EventRow {
  readonly sequence: number
  readonly run_id: string
  readonly event_type: string
  readonly payload_json: string
  readonly created_at: string
}

interface TaskRuntimeRow {
  readonly project_number: number
  readonly task_number: number
  readonly workspace_json: string
}

interface TaskRoleSessionRow {
  readonly project_number: number
  readonly task_number: number
  readonly role: TaskRole
  readonly adapter_id: string
  readonly runtime_session_id: string
}

function parseJSON(value: string | null, name: string): Readonly<Record<string, unknown>> | undefined {
  if (value === null) return undefined
  const parsed: unknown = JSON.parse(value)
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error(`Fleet registry ${name} is corrupt`)
  }
  return parsed as Readonly<Record<string, unknown>>
}

function runRecord(row: RunRow, effects: readonly FleetExternalEffectRecord[]): FleetRunRecord {
  const frozenPolicy = parseJSON(row.frozen_policy_json, 'frozen policy')
  const workspace = parseJSON(row.workspace_json, 'workspace')
  return decodeRunRecord({
    runId: row.run_id,
    serviceId: row.service_id,
    fleetId: row.fleet_id,
    projectNumber: row.project_number,
    configRevision: row.config_revision,
    state: row.state,
    ...(row.task_number === null ? {} : { taskNumber: row.task_number }),
    ...(row.task_version === null ? {} : { taskVersion: row.task_version }),
    ...(row.stage === null ? {} : { stage: row.stage }),
    ...(frozenPolicy === undefined ? {} : { frozenPolicy }),
    ...(row.claim_id === null ? {} : { claimId: row.claim_id }),
    ...(row.claim_version === null ? {} : { claimVersion: row.claim_version }),
    ...(row.claim_task_version === null ? {} : { claimTaskVersion: row.claim_task_version }),
    ...(row.runtime_session_id === null ? {} : { runtimeSessionId: row.runtime_session_id }),
    ...(workspace === undefined ? {} : { workspace }),
    ...(row.checkpoint === null ? {} : { checkpoint: row.checkpoint }),
    ...(row.disposition === null ? {} : { disposition: row.disposition }),
    ...(row.error === null ? {} : { error: row.error }),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  }, effects)
}

function effectRecord(row: EffectRow): FleetExternalEffectRecord {
  const intent = parseJSON(row.intent_json, 'effect intent')!
  const observation = parseJSON(row.observation_json, 'effect observation')
  return decodeExternalEffect({
    runId: row.run_id,
    kind: row.kind,
    idempotencyKey: row.idempotency_key,
    status: row.status,
    intent,
    ...(observation === undefined ? {} : { observation }),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  })
}

function migrate(database: Database.Database): void {
  database.exec(`
    CREATE TABLE IF NOT EXISTS local_schema_migrations (
      version INTEGER PRIMARY KEY,
      applied_at TEXT NOT NULL
    );
  `)
  const applied = database.prepare<[], { version: number }>(
    'SELECT version FROM local_schema_migrations ORDER BY version',
  ).all().map(row => row.version)
  if (applied.some(version => version > SCHEMA_VERSION)) {
    throw new Error('Fleet registry schema is newer than this executable')
  }
  if (!applied.includes(1)) {
    database.transaction(() => {
      database.exec(`
        CREATE TABLE service_identity (
          singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
          service_id TEXT NOT NULL UNIQUE,
          created_at TEXT NOT NULL
        );

        CREATE TABLE configuration_revisions (
          revision TEXT PRIMARY KEY,
          source_path TEXT NOT NULL,
          loaded_at TEXT NOT NULL,
          config_json TEXT NOT NULL
        );

        CREATE TABLE fleet_definitions (
          fleet_id TEXT PRIMARY KEY,
          project_number INTEGER NOT NULL,
          enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
          config_revision TEXT NOT NULL REFERENCES configuration_revisions(revision),
          updated_at TEXT NOT NULL,
          retired_at TEXT
        );

        CREATE UNIQUE INDEX fleet_definitions_one_enabled_per_project
          ON fleet_definitions(project_number) WHERE enabled = 1 AND retired_at IS NULL;

        CREATE TABLE runs (
          run_id TEXT PRIMARY KEY,
          service_id TEXT NOT NULL,
          fleet_id TEXT NOT NULL,
          project_number INTEGER NOT NULL,
          config_revision TEXT NOT NULL REFERENCES configuration_revisions(revision),
          state TEXT NOT NULL CHECK (state IN (
            'admitted','claiming','claimed','preparing_workspace',
            'starting_harness','running_harness','validating','delivering',
            'settling','releasing','completed','released','quarantined','failed'
          )),
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );

        CREATE INDEX runs_non_terminal
          ON runs(updated_at, run_id)
          WHERE state NOT IN ('completed','released','quarantined','failed');
      `)
      database.prepare(
        'INSERT INTO local_schema_migrations(version, applied_at) VALUES (?, ?)',
      ).run(1, new Date().toISOString())
    })()
  }
  if (!applied.includes(2)) {
    database.transaction(() => {
      database.exec(`
        ALTER TABLE runs ADD COLUMN task_number INTEGER;
        ALTER TABLE runs ADD COLUMN task_version INTEGER;
        ALTER TABLE runs ADD COLUMN stage TEXT CHECK (stage IN ('execution','review','correction'));
        ALTER TABLE runs ADD COLUMN frozen_policy_json TEXT;
        ALTER TABLE runs ADD COLUMN claim_id TEXT;
        ALTER TABLE runs ADD COLUMN claim_version INTEGER;
        ALTER TABLE runs ADD COLUMN runtime_session_id TEXT;
        ALTER TABLE runs ADD COLUMN workspace_json TEXT;
        ALTER TABLE runs ADD COLUMN checkpoint TEXT;
        ALTER TABLE runs ADD COLUMN disposition TEXT;
        ALTER TABLE runs ADD COLUMN error TEXT;

        CREATE UNIQUE INDEX runs_one_local_active_task_stage
          ON runs(fleet_id, task_number, stage)
          WHERE task_number IS NOT NULL
            AND state NOT IN ('completed','released','quarantined','failed');

        CREATE TABLE run_external_effects (
          run_id TEXT NOT NULL REFERENCES runs(run_id),
          kind TEXT NOT NULL,
          idempotency_key TEXT NOT NULL,
          status TEXT NOT NULL CHECK (status IN ('intended','observed')),
          intent_json TEXT NOT NULL,
          observation_json TEXT,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          PRIMARY KEY(run_id, kind),
          UNIQUE(idempotency_key)
        );

        CREATE TABLE run_events (
          sequence INTEGER PRIMARY KEY AUTOINCREMENT,
          run_id TEXT NOT NULL REFERENCES runs(run_id),
          event_type TEXT NOT NULL,
          payload_json TEXT NOT NULL,
          created_at TEXT NOT NULL
        );
        CREATE INDEX run_events_by_run ON run_events(run_id, sequence);
      `)
      database.prepare(
        'INSERT INTO local_schema_migrations(version, applied_at) VALUES (?, ?)',
      ).run(2, new Date().toISOString())
    })()
  }
  if (!applied.includes(3)) {
    database.transaction(() => {
      database.exec('ALTER TABLE runs ADD COLUMN claim_task_version INTEGER;')
      database.prepare(
        'INSERT INTO local_schema_migrations(version, applied_at) VALUES (?, ?)',
      ).run(3, new Date().toISOString())
    })()
  }
  if (!applied.includes(4)) {
    database.transaction(() => {
      database.exec(`
        CREATE TABLE task_runtimes (
          project_number INTEGER NOT NULL,
          task_number INTEGER NOT NULL,
          workspace_root TEXT NOT NULL UNIQUE,
          workspace_json TEXT NOT NULL,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          PRIMARY KEY(project_number, task_number)
        );

        CREATE TABLE task_role_sessions (
          project_number INTEGER NOT NULL,
          task_number INTEGER NOT NULL,
          role TEXT NOT NULL CHECK (role IN ('implementer', 'reviewer')),
          adapter_id TEXT NOT NULL,
          runtime_session_id TEXT NOT NULL,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          PRIMARY KEY(project_number, task_number, role),
          UNIQUE(adapter_id, runtime_session_id),
          FOREIGN KEY(project_number, task_number)
            REFERENCES task_runtimes(project_number, task_number) ON DELETE CASCADE
        );
      `)
      database.prepare(
        'INSERT INTO local_schema_migrations(version, applied_at) VALUES (?, ?)',
      ).run(4, new Date().toISOString())
    })()
  }
}

/** Local durable operational ledger. Pactline remains authoritative for workflow state. */
export class FleetRegistry {
  readonly path: string
  readonly serviceId: string
  private closed = false

  private constructor(
    path: string,
    private readonly database: Database.Database,
    serviceId: string,
  ) {
    this.path = path
    this.serviceId = serviceId
  }

  static async open(path: string, now: () => Date = () => new Date()): Promise<FleetRegistry> {
    const target = await preparePrivateFile(path)
    const database = new Database(target, { timeout: 5_000 })
    try {
      database.pragma('foreign_keys = ON')
      database.pragma('journal_mode = DELETE')
      database.pragma('synchronous = FULL')
      database.pragma('busy_timeout = 5000')
      migrate(database)
      let identity = database.prepare<[], { service_id: string }>(
        'SELECT service_id FROM service_identity WHERE singleton = 1',
      ).get()
      if (identity === undefined) {
        const serviceId = randomUUID()
        database.prepare(
          'INSERT INTO service_identity(singleton, service_id, created_at) VALUES (1, ?, ?)',
        ).run(serviceId, now().toISOString())
        identity = { service_id: serviceId }
      }
      return new FleetRegistry(target, database, identity.service_id)
    } catch (error) {
      database.close()
      throw error
    }
  }

  recordConfiguration(snapshot: FleetConfigSnapshot): void {
    this.assertOpen()
    const apply = this.database.transaction(() => {
      this.database.prepare(`
        INSERT INTO configuration_revisions(revision, source_path, loaded_at, config_json)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(revision) DO NOTHING
      `).run(snapshot.revision, snapshot.sourcePath, snapshot.loadedAt, JSON.stringify(snapshot.config))
      this.database.prepare(
        'UPDATE fleet_definitions SET enabled = 0, retired_at = ? WHERE retired_at IS NULL',
      ).run(snapshot.loadedAt)
      for (const fleet of Object.values(snapshot.config.fleets)) {
        this.database.prepare(`
          INSERT INTO fleet_definitions(
            fleet_id, project_number, enabled, config_revision, updated_at, retired_at
          ) VALUES (?, ?, ?, ?, ?, NULL)
          ON CONFLICT(fleet_id) DO UPDATE SET
            project_number = excluded.project_number,
            enabled = excluded.enabled,
            config_revision = excluded.config_revision,
            updated_at = excluded.updated_at,
            retired_at = NULL
        `).run(fleet.id, fleet.projectNumber, fleet.enabled ? 1 : 0, snapshot.revision, snapshot.loadedAt)
      }
    })
    apply()
  }

  admitRun(fleetId: string, admission: FleetRunAdmission, now: Date = new Date()): FleetRun {
    return this.insertRun(fleetId, admission, now)
  }

  private insertRun(fleetId: string, admission: FleetRunAdmission, now: Date): FleetRun {
    this.assertOpen()
    const fleet = this.database.prepare<[string], FleetRow>(`
      SELECT fleet_id, project_number, config_revision
      FROM fleet_definitions
      WHERE fleet_id = ? AND enabled = 1 AND retired_at IS NULL
    `).get(fleetId)
    if (fleet === undefined) throw new Error(`Enabled Fleet is not registered: ${fleetId}`)
    const runId = randomUUID()
    const timestamp = now.toISOString()
    const admitted = admitDomainRun({
      runId,
      serviceId: this.serviceId,
      fleetId: fleet.fleet_id,
      projectNumber: fleet.project_number,
      configRevision: fleet.config_revision,
    }, admission, timestamp)
    this.database.prepare(`
      INSERT INTO runs(
        run_id, service_id, fleet_id, project_number, config_revision,
        state, task_number, task_version, stage, frozen_policy_json,
        checkpoint, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, 'admitted', ?, ?, ?, ?, ?, ?, ?)
    `).run(
      runId, this.serviceId, fleet.fleet_id, fleet.project_number, fleet.config_revision,
      admitted.taskNumber, admitted.taskVersion, admitted.stage, JSON.stringify(admitted.frozenPolicy),
      admitted.checkpoint, timestamp, timestamp,
    )
    this.appendEvent(runId, 'run.admitted', admission, timestamp)
    const created = this.getRun(runId)!
    if (isLegacyRun(created)) throw new Error('Fleet registry created a legacy Run')
    return created
  }

  /** Compatibility helper for local transitions; external boundaries use commitRunDecision. */
  transitionRun(
    runId: string,
    expected: FleetRunState | readonly FleetRunState[],
    next: FleetRunState,
    update: FleetRunUpdate = {},
    now: Date = new Date(),
  ): FleetRunRecord {
    this.assertOpen()
    const expectedStates = typeof expected === 'string' ? [expected] : [...expected]
    const current = this.getRun(runId)
    if (current === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
    if (!expectedStates.includes(current.state)) throw new Error(`Fleet Run ${runId} is ${current.state}; expected ${expectedStates.join(' or ')}`)
    return this.commitRunDecisionInternal(runId, {
      expected: { state: current.state, updatedAt: current.updatedAt },
      transition: { state: next, update },
    }, now, false).run
  }

  /** Commit one aggregate decision: Effect fact, Run state/version, and audit event. */
  commitRunDecision(
    runId: string,
    decision: FleetRunDecision,
    now: Date = new Date(),
  ): FleetRunDecisionResult {
    return this.commitRunDecisionInternal(runId, decision, now, true)
  }

  private commitRunDecisionInternal(
    runId: string,
    decision: FleetRunDecision,
    now: Date,
    enforceAtomicEffect: boolean,
  ): FleetRunDecisionResult {
    this.assertOpen()
    if (decision.transition === undefined && decision.effect === undefined) {
      throw new Error('Fleet Run decision has no transition or Effect fact')
    }
    const apply = this.database.transaction((): FleetRunDecisionResult => {
      const current = this.getRun(runId)
      if (current === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
      if (current.state !== decision.expected.state || current.updatedAt !== decision.expected.updatedAt) {
        throw new Error(`Fleet Run ${runId} changed after the decision was made`)
      }
      const timestamp = this.nextRunTimestamp(current.updatedAt, now)
      const effects = [...this.listEffects(runId)]
      const effectDecision = decision.effect
      let decidedEffect: FleetExternalEffectRecord | undefined
      let effectChanged = false
      if (effectDecision?.type === 'intent') {
        const existing = effects.find(effect => effect.kind === effectDecision.kind)
        if (existing === undefined) {
          decidedEffect = createExternalEffectIntent(
            runId,
            effectDecision.kind,
            effectDecision.idempotencyKey,
            effectDecision.intent,
            timestamp,
          )
          effects.push(decidedEffect)
          effectChanged = true
        } else {
          decidedEffect = replayExternalEffectIntent(
            existing,
            effectDecision.idempotencyKey,
            effectDecision.intent,
          )
        }
      } else if (effectDecision?.type === 'observation') {
        const index = effects.findIndex(effect => effect.kind === effectDecision.kind)
        if (index < 0) throw new Error(`External effect intent does not exist: ${effectDecision.kind}`)
        const currentEffect = effects[index]!
        decidedEffect = observeExternalEffect(currentEffect, effectDecision.observation, timestamp)
        effectChanged = decidedEffect !== currentEffect
        effects[index] = decidedEffect
      }

      const update = decision.transition?.update ?? {}
      if (decision.transition !== undefined) {
        if (enforceAtomicEffect) assertRunDecisionEffect(current, decision.transition.state, effectDecision)
        if (isLegacyRun(current)) transitionDomainRun(current, decision.transition.state, update, timestamp, effects)
        else transitionDomainRun(current, decision.transition.state, update, timestamp, effects)
      }
      if (decision.transition === undefined && !effectChanged) {
        return { run: current, ...(decidedEffect === undefined ? {} : { effect: decidedEffect }) }
      }

      if (effectChanged && decidedEffect !== undefined) this.persistEffect(decidedEffect)
      const nextState = decision.transition?.state ?? current.state
      const result = this.database.prepare(`
        UPDATE runs SET
          state = ?,
          claim_id = COALESCE(?, claim_id),
          claim_version = COALESCE(?, claim_version),
          claim_task_version = COALESCE(?, claim_task_version),
          runtime_session_id = COALESCE(?, runtime_session_id),
          workspace_json = COALESCE(?, workspace_json),
          checkpoint = COALESCE(?, checkpoint),
          disposition = COALESCE(?, disposition),
          error = COALESCE(?, error),
          updated_at = ?
        WHERE run_id = ? AND state = ? AND updated_at = ?
      `).run(
        nextState,
        update.claimId ?? null,
        update.claimVersion ?? null,
        update.claimTaskVersion ?? null,
        update.runtimeSessionId ?? null,
        update.workspace === undefined ? null : JSON.stringify(update.workspace),
        update.checkpoint ?? null,
        update.disposition ?? null,
        update.error ?? null,
        timestamp,
        runId,
        current.state,
        current.updatedAt,
      )
      if (result.changes !== 1) throw new Error('Fleet Run decision lost a concurrent update')
      const eventType = decision.transition === undefined
        ? effectDecision?.type === 'intent' ? 'run.effect_intended' : 'run.effect_observed'
        : 'run.transitioned'
      this.appendEvent(runId, eventType, {
        ...(decision.transition === undefined ? {} : { from: current.state, to: nextState, ...update }),
        ...(effectDecision === undefined ? {} : {
          effect: effectDecision.kind,
          effectStatus: effectDecision.type === 'intent' ? 'intended' : 'observed',
        }),
      }, timestamp)
      const run = this.getRun(runId)!
      return { run, ...(decidedEffect === undefined ? {} : { effect: this.getEffect(runId, decidedEffect.kind)! }) }
    })
    return apply()
  }

  recordEffectIntent<K extends FleetExternalEffectKind>(
    runId: string,
    kind: K,
    idempotencyKey: string,
    intent: FleetExternalEffectIntent<K>,
    now: Date = new Date(),
  ): FleetExternalEffectRecord<K> {
    const run = this.getRun(runId)
    if (run === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
    return this.commitRunDecision(runId, {
      expected: { state: run.state, updatedAt: run.updatedAt },
      effect: { type: 'intent', kind, idempotencyKey, intent } as FleetExternalEffectDecision,
    }, now).effect as FleetExternalEffectRecord<K>
  }

  observeEffect<K extends FleetExternalEffectKind>(
    runId: string,
    kind: K,
    observation: FleetExternalEffectObservation<K>,
    now: Date = new Date(),
  ): FleetExternalEffectRecord<K> {
    const run = this.getRun(runId)
    if (run === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
    return this.commitRunDecision(runId, {
      expected: { state: run.state, updatedAt: run.updatedAt },
      effect: { type: 'observation', kind, observation } as FleetExternalEffectDecision,
    }, now).effect as FleetExternalEffectRecord<K>
  }

  getEffect<K extends FleetExternalEffectKind>(runId: string, kind: K): FleetExternalEffectRecord<K> | undefined {
    this.assertOpen()
    const row = this.database.prepare<[string, string], EffectRow>(`
      SELECT run_id, kind, idempotency_key, status, intent_json, observation_json, created_at, updated_at
      FROM run_external_effects WHERE run_id = ? AND kind = ?
    `).get(runId, kind)
    return row === undefined ? undefined : effectRecord(row) as FleetExternalEffectRecord<K>
  }

  listEffects(runId: string): readonly FleetExternalEffectRecord[] {
    this.assertOpen()
    return this.database.prepare<[string], EffectRow>(`
      SELECT run_id, kind, idempotency_key, status, intent_json, observation_json, created_at, updated_at
      FROM run_external_effects WHERE run_id = ? ORDER BY created_at, kind
    `).all(runId).map(effectRecord)
  }

  listRunEvents(runId: string, limit = 200): readonly FleetRunEventRecord[] {
    this.assertOpen()
    const bounded = Math.min(Math.max(Math.trunc(limit), 1), 500)
    return this.database.prepare<[string, number], EventRow>(`
      SELECT sequence, run_id, event_type, payload_json, created_at
      FROM run_events WHERE run_id = ? ORDER BY sequence DESC LIMIT ?
    `).all(runId, bounded).reverse().map(row => ({
      sequence: row.sequence,
      runId: row.run_id,
      eventType: row.event_type,
      payload: parseJSON(row.payload_json, 'event payload')!,
      createdAt: row.created_at,
    }))
  }

  /** Append the bounded, secret-free facts behind one deterministic recovery choice. */
  recordRecoveryDecision(
    runId: string,
    facts: RunRecoveryFacts,
    decision: RunRecoveryDecision,
    now: Date = new Date(),
  ): void {
    this.assertOpen()
    if (this.getRun(runId) === undefined) throw new Error(`Fleet Run does not exist: ${runId}`)
    this.appendEvent(runId, 'run.recovery_decided', {
      state: facts.state,
      claimAuthority: facts.claimAuthority.kind,
      ...(facts.claimAuthority.kind === 'active' || facts.claimAuthority.kind === 'terminal'
        ? { claimIdentityMatches: facts.claimAuthority.identityMatches }
        : {}),
      ...(facts.claimAuthority.kind === 'terminal' ? { claimStatus: facts.claimAuthority.status } : {}),
      sessionResumable: facts.sessionResumable,
      hasSettlementIntent: facts.hasSettlementIntent,
      decision: decision.kind,
      ...('reason' in decision ? { reason: decision.reason } : {}),
      ...('terminal' in decision ? { terminal: decision.terminal } : {}),
    }, now.toISOString())
  }

  getRun(runId: string): FleetRunRecord | undefined {
    this.assertOpen()
    const row = this.database.prepare<[string], RunRow>(`
      SELECT run_id, service_id, fleet_id, project_number, config_revision,
             state, task_number, task_version, stage, frozen_policy_json,
             claim_id, claim_version, runtime_session_id, workspace_json,
             claim_task_version,
             checkpoint, disposition, error, created_at, updated_at
      FROM runs WHERE run_id = ?
    `).get(runId)
    return row === undefined ? undefined : runRecord(row, this.listEffects(row.run_id))
  }

  hasNonTerminalRun(fleetId: string, taskNumber: number, stage: FleetRunStage): boolean {
    this.assertOpen()
    return this.database.prepare<[string, number, FleetRunStage], { present: number }>(`
      SELECT 1 AS present FROM runs
      WHERE fleet_id = ? AND task_number = ? AND stage = ?
        AND state NOT IN ('completed','released','quarantined','failed')
      LIMIT 1
    `).get(fleetId, taskNumber, stage)?.present === 1
  }

  hasRegisteredClaim(claimId: string): boolean {
    this.assertOpen()
    return this.database.prepare<[string], { present: number }>(`
      SELECT 1 AS present FROM runs WHERE claim_id = ? LIMIT 1
    `).get(claimId)?.present === 1
  }

  listNonTerminalRuns(): readonly FleetRunRecord[] {
    this.assertOpen()
    const terminal = TERMINAL_RUN_STATES.map(() => '?').join(', ')
    return this.database.prepare<string[], RunRow>(`
      SELECT run_id, service_id, fleet_id, project_number, config_revision,
             state, task_number, task_version, stage, frozen_policy_json,
             claim_id, claim_version, runtime_session_id, workspace_json,
             claim_task_version,
             checkpoint, disposition, error, created_at, updated_at
      FROM runs
      WHERE state NOT IN (${terminal})
      ORDER BY created_at, run_id
    `).all(...TERMINAL_RUN_STATES).map(row => runRecord(row, this.listEffects(row.run_id)))
  }

  listRuns(options: FleetRunListOptions = {}): readonly FleetRunRecord[] {
    this.assertOpen()
    const limit = Math.min(Math.max(Math.trunc(options.limit ?? 50), 1), 200)
    const clauses: string[] = []
    const parameters: Array<string | number> = []
    if (options.fleetId !== undefined) {
      clauses.push('fleet_id = ?')
      parameters.push(options.fleetId)
    }
    if (options.state !== undefined) {
      clauses.push('state = ?')
      parameters.push(options.state)
    }
    if (options.stage !== undefined) {
      clauses.push('stage = ?')
      parameters.push(options.stage)
    }
    if (options.before !== undefined) {
      clauses.push('updated_at < ?')
      parameters.push(options.before)
    }
    const where = clauses.length === 0 ? '' : `WHERE ${clauses.join(' AND ')}`
    return this.database.prepare<Array<string | number>, RunRow>(`
      SELECT run_id, service_id, fleet_id, project_number, config_revision,
             state, task_number, task_version, stage, frozen_policy_json,
             claim_id, claim_version, runtime_session_id, workspace_json,
             claim_task_version,
             checkpoint, disposition, error, created_at, updated_at
      FROM runs ${where}
      ORDER BY updated_at DESC, run_id DESC
      LIMIT ?
    `).all(...parameters, limit).map(row => runRecord(row, this.listEffects(row.run_id)))
  }

  bindTaskWorkspace(
    projectNumber: number,
    taskNumber: number,
    workspace: FleetWorkspace,
    now: Date = new Date(),
  ): FleetTaskRuntime {
    this.assertOpen()
    const decodedWorkspace = decodeFleetWorkspace(workspace)
    const timestamp = now.toISOString()
    const encoded = JSON.stringify(decodedWorkspace)
    this.database.transaction(() => {
      const current = this.database.prepare<[number, number], TaskRuntimeRow>(`
        SELECT project_number, task_number, workspace_json
        FROM task_runtimes WHERE project_number = ? AND task_number = ?
      `).get(projectNumber, taskNumber)
      if (current !== undefined) {
        if (current.workspace_json !== encoded) throw new Error('Task Workspace binding is immutable')
        return
      }
      const owner = this.database.prepare<[string], { project_number: number; task_number: number }>(`
        SELECT project_number, task_number FROM task_runtimes WHERE workspace_root = ?
      `).get(decodedWorkspace.root)
      if (owner !== undefined) throw new Error('Task Workspace already belongs to another Task')
      this.database.prepare(`
        INSERT INTO task_runtimes(
          project_number, task_number, workspace_root, workspace_json, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?)
      `).run(projectNumber, taskNumber, decodedWorkspace.root, encoded, timestamp, timestamp)
    })()
    return this.getTaskRuntime(projectNumber, taskNumber)!
  }

  bindTaskRoleSession(
    projectNumber: number,
    taskNumber: number,
    role: TaskRole,
    binding: TaskRoleSessionBinding,
    now: Date = new Date(),
  ): FleetTaskRuntime {
    this.assertOpen()
    if (binding.adapterId.trim() === '' || binding.runtimeSessionId.trim() === '') {
      throw new Error('Task role Session binding is invalid')
    }
    const current = this.database.prepare<[number, number, TaskRole], TaskRoleSessionRow>(`
      SELECT role, adapter_id, runtime_session_id FROM task_role_sessions
      WHERE project_number = ? AND task_number = ? AND role = ?
    `).get(projectNumber, taskNumber, role)
    if (current !== undefined) {
      if (current.adapter_id !== binding.adapterId || current.runtime_session_id !== binding.runtimeSessionId) {
        throw new Error('Task role Session binding is immutable')
      }
      return this.getTaskRuntime(projectNumber, taskNumber)!
    }
    if (this.getTaskRuntime(projectNumber, taskNumber) === undefined) {
      throw new Error('Task Workspace must be bound before its role Sessions')
    }
    const timestamp = now.toISOString()
    this.database.prepare(`
      INSERT INTO task_role_sessions(
        project_number, task_number, role, adapter_id, runtime_session_id, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `).run(projectNumber, taskNumber, role, binding.adapterId, binding.runtimeSessionId, timestamp, timestamp)
    return this.getTaskRuntime(projectNumber, taskNumber)!
  }

  getTaskRuntime(projectNumber: number, taskNumber: number): FleetTaskRuntime | undefined {
    this.assertOpen()
    const row = this.database.prepare<[number, number], TaskRuntimeRow>(`
      SELECT project_number, task_number, workspace_json
      FROM task_runtimes WHERE project_number = ? AND task_number = ?
    `).get(projectNumber, taskNumber)
    if (row === undefined) return undefined
    const workspace = decodeFleetWorkspace(parseJSON(row.workspace_json, 'Task Workspace'))
    const sessions: Partial<Record<TaskRole, TaskRoleSessionBinding>> = {}
    for (const session of this.database.prepare<[number, number], TaskRoleSessionRow>(`
      SELECT project_number, task_number, role, adapter_id, runtime_session_id FROM task_role_sessions
      WHERE project_number = ? AND task_number = ? ORDER BY role
    `).all(projectNumber, taskNumber)) {
      sessions[session.role] = {
        adapterId: session.adapter_id,
        runtimeSessionId: session.runtime_session_id,
      }
    }
    return {
      projectNumber: row.project_number,
      taskNumber: row.task_number,
      workspace,
      sessions,
    }
  }

  listTaskRuntimes(): readonly FleetTaskRuntime[] {
    this.assertOpen()
    const rows = this.database.prepare<[], TaskRuntimeRow>(`
      SELECT project_number, task_number, workspace_json
      FROM task_runtimes ORDER BY project_number, task_number
    `).all()
    const sessions = new Map<string, Partial<Record<TaskRole, TaskRoleSessionBinding>>>()
    for (const session of this.database.prepare<[], TaskRoleSessionRow>(`
      SELECT project_number, task_number, role, adapter_id, runtime_session_id
      FROM task_role_sessions ORDER BY project_number, task_number, role
    `).all()) {
      const key = `${String(session.project_number)}:${String(session.task_number)}`
      const bindings = sessions.get(key) ?? {}
      bindings[session.role] = { adapterId: session.adapter_id, runtimeSessionId: session.runtime_session_id }
      sessions.set(key, bindings)
    }
    return rows.map(row => ({
      projectNumber: row.project_number,
      taskNumber: row.task_number,
      workspace: decodeFleetWorkspace(parseJSON(row.workspace_json, 'Task Workspace')),
      sessions: sessions.get(`${String(row.project_number)}:${String(row.task_number)}`) ?? {},
    }))
  }

  retireTaskRuntime(projectNumber: number, taskNumber: number): void {
    this.assertOpen()
    this.database.prepare(
      'DELETE FROM task_runtimes WHERE project_number = ? AND task_number = ?',
    ).run(projectNumber, taskNumber)
  }

  healthCheck(): boolean {
    this.assertOpen()
    return this.database.prepare<[], { ok: number }>('SELECT 1 AS ok').get()?.ok === 1
  }

  close(): void {
    if (this.closed) return
    this.closed = true
    this.database.close()
  }

  private appendEvent(runId: string, eventType: string, payload: unknown, timestamp: string): void {
    this.database.prepare(`
      INSERT INTO run_events(run_id, event_type, payload_json, created_at)
      VALUES (?, ?, ?, ?)
    `).run(runId, eventType, JSON.stringify(payload), timestamp)
  }

  private persistEffect(effect: FleetExternalEffectRecord): void {
    if (effect.status === 'intended') {
      this.database.prepare(`
        INSERT INTO run_external_effects(
          run_id, kind, idempotency_key, status, intent_json, observation_json, created_at, updated_at
        ) VALUES (?, ?, ?, 'intended', ?, NULL, ?, ?)
      `).run(
        effect.runId,
        effect.kind,
        effect.idempotencyKey,
        JSON.stringify(effect.intent),
        effect.createdAt,
        effect.updatedAt,
      )
      return
    }
    const result = this.database.prepare(`
      UPDATE run_external_effects
      SET status = 'observed', observation_json = ?, updated_at = ?
      WHERE run_id = ? AND kind = ? AND status = 'intended'
    `).run(JSON.stringify(effect.observation), effect.updatedAt, effect.runId, effect.kind)
    if (result.changes !== 1) throw new Error(`External effect intent does not exist: ${effect.kind}`)
  }

  private nextRunTimestamp(current: string, now: Date): string {
    const requested = now.toISOString()
    if (requested > current) return requested
    return new Date(Date.parse(current) + 1).toISOString()
  }

  private assertOpen(): void {
    if (this.closed) throw new Error('Fleet registry is closed')
  }
}
