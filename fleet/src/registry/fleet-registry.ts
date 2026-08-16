import { randomUUID } from 'node:crypto'
import Database from 'better-sqlite3'
import type { FleetConfigSnapshot } from '../config/types.js'
import { preparePrivateFile } from '../service/state-directory.js'

const SCHEMA_VERSION = 3
const TERMINAL_RUN_STATES = ['completed', 'released', 'quarantined', 'failed'] as const

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

export type FleetRunStage = 'execution' | 'review' | 'correction'

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

export interface FleetRunRecord {
  readonly runId: string
  readonly serviceId: string
  readonly fleetId: string
  readonly projectNumber: number
  readonly configRevision: string
  readonly state: FleetRunState
  readonly taskNumber?: number
  readonly taskVersion?: number
  readonly stage?: FleetRunStage
  readonly frozenPolicy?: Readonly<Record<string, unknown>>
  readonly claimId?: string
  readonly claimVersion?: number
  readonly claimTaskVersion?: number
  readonly runtimeSessionId?: string
  readonly workspace?: Readonly<Record<string, unknown>>
  readonly checkpoint?: string
  readonly disposition?: string
  readonly error?: string
  readonly createdAt: string
  readonly updatedAt: string
}

export type FleetExternalEffectStatus = 'intended' | 'observed'

export interface FleetExternalEffectRecord {
  readonly runId: string
  readonly kind: string
  readonly idempotencyKey: string
  readonly status: FleetExternalEffectStatus
  readonly intent: Readonly<Record<string, unknown>>
  readonly observation?: Readonly<Record<string, unknown>>
  readonly createdAt: string
  readonly updatedAt: string
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
  readonly status: FleetExternalEffectStatus
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

function parseJSON(value: string | null, name: string): Readonly<Record<string, unknown>> | undefined {
  if (value === null) return undefined
  const parsed: unknown = JSON.parse(value)
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error(`Fleet registry ${name} is corrupt`)
  }
  return parsed as Readonly<Record<string, unknown>>
}

function runRecord(row: RunRow): FleetRunRecord {
  const frozenPolicy = parseJSON(row.frozen_policy_json, 'frozen policy')
  const workspace = parseJSON(row.workspace_json, 'workspace')
  return {
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
  }
}

function effectRecord(row: EffectRow): FleetExternalEffectRecord {
  const intent = parseJSON(row.intent_json, 'effect intent')!
  const observation = parseJSON(row.observation_json, 'effect observation')
  return {
    runId: row.run_id,
    kind: row.kind,
    idempotencyKey: row.idempotency_key,
    status: row.status,
    intent,
    ...(observation === undefined ? {} : { observation }),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  }
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
}

const allowedTransitions: Readonly<Record<FleetRunState, readonly FleetRunState[]>> = {
  admitted: ['claiming', 'releasing', 'released', 'quarantined', 'failed'],
  claiming: ['claimed', 'releasing', 'released', 'quarantined', 'failed'],
  claimed: ['preparing_workspace', 'releasing', 'quarantined', 'failed'],
  preparing_workspace: ['starting_harness', 'releasing', 'quarantined', 'failed'],
  starting_harness: ['running_harness', 'releasing', 'quarantined', 'failed'],
  running_harness: ['validating', 'releasing', 'quarantined', 'failed'],
  validating: ['delivering', 'settling', 'releasing', 'quarantined', 'failed'],
  delivering: ['settling', 'releasing', 'quarantined', 'failed'],
  settling: ['completed', 'released', 'quarantined', 'failed'],
  releasing: ['released', 'quarantined', 'failed'],
  completed: [], released: [], quarantined: [], failed: [],
}

function assertAdmission(admission: FleetRunAdmission): void {
  if (!Number.isSafeInteger(admission.taskNumber) || admission.taskNumber < 1) throw new Error('Task number must be positive')
  if (!Number.isSafeInteger(admission.taskVersion) || admission.taskVersion < 1) throw new Error('Task version must be positive')
  if (!['execution', 'review', 'correction'].includes(admission.stage)) throw new Error('Run stage is invalid')
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

  /** M5.1 compatibility helper for a policy-free placeholder Run. */
  createRun(fleetId: string, now: Date = new Date()): FleetRunRecord {
    return this.insertRun(fleetId, undefined, now)
  }

  admitRun(fleetId: string, admission: FleetRunAdmission, now: Date = new Date()): FleetRunRecord {
    assertAdmission(admission)
    return this.insertRun(fleetId, admission, now)
  }

  private insertRun(fleetId: string, admission: FleetRunAdmission | undefined, now: Date): FleetRunRecord {
    this.assertOpen()
    const fleet = this.database.prepare<[string], FleetRow>(`
      SELECT fleet_id, project_number, config_revision
      FROM fleet_definitions
      WHERE fleet_id = ? AND enabled = 1 AND retired_at IS NULL
    `).get(fleetId)
    if (fleet === undefined) throw new Error(`Enabled Fleet is not registered: ${fleetId}`)
    const runId = randomUUID()
    const timestamp = now.toISOString()
    this.database.prepare(`
      INSERT INTO runs(
        run_id, service_id, fleet_id, project_number, config_revision,
        state, task_number, task_version, stage, frozen_policy_json,
        checkpoint, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, 'admitted', ?, ?, ?, ?, ?, ?, ?)
    `).run(
      runId, this.serviceId, fleet.fleet_id, fleet.project_number, fleet.config_revision,
      admission?.taskNumber ?? null, admission?.taskVersion ?? null, admission?.stage ?? null,
      admission === undefined ? null : JSON.stringify(admission.frozenPolicy),
      admission === undefined ? null : 'run_admitted', timestamp, timestamp,
    )
    this.appendEvent(runId, 'run.admitted', admission ?? {}, timestamp)
    return this.getRun(runId)!
  }

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
    if (!allowedTransitions[current.state].includes(next)) throw new Error(`Invalid Fleet Run transition: ${current.state} -> ${next}`)
    if (update.claimId !== undefined && current.claimId !== undefined && update.claimId !== current.claimId) {
      throw new Error('Fleet Run Claim identity is immutable')
    }
    if (update.runtimeSessionId !== undefined && current.runtimeSessionId !== undefined
      && update.runtimeSessionId !== current.runtimeSessionId) throw new Error('Fleet Run Adapter Session identity is immutable')
    const timestamp = now.toISOString()
    const apply = this.database.transaction(() => {
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
        WHERE run_id = ? AND state = ?
      `).run(
        next,
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
      )
      if (result.changes !== 1) throw new Error('Fleet Run transition lost a concurrent update')
      this.appendEvent(runId, 'run.transitioned', { from: current.state, to: next, ...update }, timestamp)
    })
    apply()
    return this.getRun(runId)!
  }

  recordEffectIntent(
    runId: string,
    kind: string,
    idempotencyKey: string,
    intent: Readonly<Record<string, unknown>>,
    now: Date = new Date(),
  ): FleetExternalEffectRecord {
    this.assertOpen()
    if (kind.trim() === '' || idempotencyKey.trim() === '') throw new Error('External effect kind and idempotency key are required')
    const timestamp = now.toISOString()
    this.database.prepare(`
      INSERT INTO run_external_effects(
        run_id, kind, idempotency_key, status, intent_json, observation_json, created_at, updated_at
      ) VALUES (?, ?, ?, 'intended', ?, NULL, ?, ?)
      ON CONFLICT(run_id, kind) DO NOTHING
    `).run(runId, kind, idempotencyKey, JSON.stringify(intent), timestamp, timestamp)
    const effect = this.getEffect(runId, kind)!
    if (effect.idempotencyKey !== idempotencyKey || JSON.stringify(effect.intent) !== JSON.stringify(intent)) {
      throw new Error(`External effect intent changed for ${kind}`)
    }
    return effect
  }

  observeEffect(
    runId: string,
    kind: string,
    observation: Readonly<Record<string, unknown>>,
    now: Date = new Date(),
  ): FleetExternalEffectRecord {
    this.assertOpen()
    const timestamp = now.toISOString()
    const result = this.database.prepare(`
      UPDATE run_external_effects
      SET status = 'observed', observation_json = ?, updated_at = ?
      WHERE run_id = ? AND kind = ?
    `).run(JSON.stringify(observation), timestamp, runId, kind)
    if (result.changes !== 1) throw new Error(`External effect intent does not exist: ${kind}`)
    return this.getEffect(runId, kind)!
  }

  getEffect(runId: string, kind: string): FleetExternalEffectRecord | undefined {
    this.assertOpen()
    const row = this.database.prepare<[string, string], EffectRow>(`
      SELECT run_id, kind, idempotency_key, status, intent_json, observation_json, created_at, updated_at
      FROM run_external_effects WHERE run_id = ? AND kind = ?
    `).get(runId, kind)
    return row === undefined ? undefined : effectRecord(row)
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
    return row === undefined ? undefined : runRecord(row)
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
    `).all(...TERMINAL_RUN_STATES).map(runRecord)
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
    `).all(...parameters, limit).map(runRecord)
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

  private assertOpen(): void {
    if (this.closed) throw new Error('Fleet registry is closed')
  }
}
