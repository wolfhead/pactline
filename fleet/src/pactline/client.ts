/** Strict subprocess adapter for the Pactline CLI machine contract. */

import { spawn } from 'node:child_process'
import type {
  PactlineCapabilities,
  PactlineCheckOutcome,
  PactlineClaimMutationResult,
  PactlineClaimStage,
  PactlineClaimSummary,
  PactlineClientErrorCode,
  PactlineCodeChangeMutationResult,
  PactlineDoctor,
  PactlineErrorBody,
  PactlineFailure,
  PactlineIssueType,
  PactlineOperation,
  PactlinePreflightResult,
  PactlineResponseMeta,
  PactlineSuccess,
  PactlineTaskSummary,
} from './types.js'

const DEFAULT_REQUIRED_FEATURES = [
  'bounded_work_packets',
  'claim_progress',
  'claim_release',
  'execution_claims',
  'execution_completion',
  'execution_verification',
  'issue_resolution',
  'repository_code_change_links',
  'repeatable_submission',
  'resolution_request',
  'review_acceptance',
  'review_claims',
  'review_request_changes',
  'success_metadata',
  'task_acceptance',
  'thread_collaboration',
] as const

export interface PactlineCLIConfig {
  readonly executable?: string
  readonly server?: string
  readonly clientKind?: string
  readonly timeoutMs?: number
  readonly maxOutputBytes?: number
  readonly rateLimitRetries?: number
}

export interface PactlinePreflightOptions {
  readonly protocol?: number
  readonly requiredFeatures?: readonly string[]
  readonly verifyAuthentication?: boolean
  readonly sessionId?: string
  readonly signal?: AbortSignal
}

export interface PactlineCLIRuntime {
  readonly environment?: NodeJS.ProcessEnv
}

export class PactlineClientError extends Error {
  readonly code: PactlineClientErrorCode
  readonly exitCode?: number
  readonly pactlineError?: PactlineErrorBody

  constructor(
    code: PactlineClientErrorCode,
    message: string,
    options: { cause?: unknown; exitCode?: number; pactlineError?: PactlineErrorBody } = {},
  ) {
    super(message, options.cause === undefined ? undefined : { cause: options.cause })
    this.name = 'PactlineClientError'
    this.code = code
    if (options.exitCode !== undefined) this.exitCode = options.exitCode
    if (options.pactlineError !== undefined) this.pactlineError = options.pactlineError
  }
}

interface InvocationOptions {
  readonly sessionId?: string | undefined
  readonly signal?: AbortSignal | undefined
  readonly idempotencyKey?: string | undefined
  readonly input?: string | undefined
}

interface ProcessResult {
  readonly exitCode: number
  readonly stdout: string
  readonly stderr: string
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(item => typeof item === 'string')
}

function safeErrorBody(value: unknown): PactlineErrorBody | undefined {
  if (!isRecord(value) || typeof value.code !== 'string' || typeof value.message !== 'string') return undefined
  return {
    code: value.code,
    message: value.message,
    ...(typeof value.hint === 'string' ? { hint: value.hint } : {}),
    ...(typeof value.request_id === 'string' ? { request_id: value.request_id } : {}),
    ...(Number.isSafeInteger(value.retry_after_seconds) && Number(value.retry_after_seconds) >= 0
      ? { retry_after_seconds: Number(value.retry_after_seconds) }
      : {}),
  }
}

function parseCapabilities(value: unknown): PactlineCapabilities {
  if (!isRecord(value)
    || typeof value.cli_version !== 'string'
    || !Number.isSafeInteger(value.protocol)
    || !isStringArray(value.features)) {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline capabilities data is invalid')
  }
  return { cli_version: value.cli_version, protocol: value.protocol as number, features: [...value.features] }
}

function parseDoctor(value: unknown): PactlineDoctor {
  if (!isRecord(value)
    || typeof value.server !== 'string'
    || typeof value.client_kind !== 'string'
    || typeof value.session_id !== 'string'
    || typeof value.token !== 'string'
    || !Object.hasOwn(value, 'principal')) {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline doctor data is invalid')
  }
  return {
    server: value.server,
    client_kind: value.client_kind,
    session_id: value.session_id,
    token: value.token,
    principal: value.principal,
  }
}

function positiveInteger(value: number, name: string): void {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error(`${name} must be a positive integer`)
}

function nonEmpty(value: string, name: string): void {
  if (value.trim() === '') throw new Error(`${name} must be non-empty`)
}

function operationMeta(value: unknown): PactlineResponseMeta | undefined {
  if (value === undefined) return undefined
  if (!isRecord(value)) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline success metadata is invalid')
  for (const key of ['request_id', 'etag', 'idempotency_key']) {
    if (value[key] !== undefined && typeof value[key] !== 'string') {
      throw new PactlineClientError('INVALID_RESPONSE', 'Pactline success metadata is invalid')
    }
  }
  return {
    ...(typeof value.request_id === 'string' ? { request_id: value.request_id } : {}),
    ...(typeof value.etag === 'string' ? { etag: value.etag } : {}),
    ...(typeof value.idempotency_key === 'string' ? { idempotency_key: value.idempotency_key } : {}),
  }
}

function taskSummary(value: unknown): PactlineTaskSummary {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.number !== 'number'
    || typeof value.title !== 'string' || typeof value.version !== 'number'
    || typeof value.phase !== 'string' || (value.activity !== undefined && typeof value.activity !== 'string')) {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline Task summary is invalid')
  }
  positiveInteger(value.number, 'Task number')
  positiveInteger(value.version, 'Task version')
  return { ...value, activity: typeof value.activity === 'string' ? value.activity : '' } as unknown as PactlineTaskSummary
}

function packetTask(value: unknown): void {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.number !== 'number'
    || typeof value.title !== 'string' || typeof value.version !== 'number' || typeof value.phase !== 'string'
    || (value.activity !== undefined && typeof value.activity !== 'string')) {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline work packet Task is invalid')
  }
  positiveInteger(value.number, 'Task number')
  positiveInteger(value.version, 'Task version')
}

function claimSummary(value: unknown): PactlineClaimSummary {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.task_number !== 'number'
    || !['execution', 'review'].includes(String(value.stage)) || typeof value.status !== 'string'
    || typeof value.version !== 'number') {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline Claim summary is invalid')
  }
  positiveInteger(value.task_number, 'Claim Task number')
  positiveInteger(value.version, 'Claim version')
  return value as unknown as PactlineClaimSummary
}

function claimMutation(value: unknown): PactlineClaimMutationResult {
  if (!isRecord(value) || !isRecord(value.task) || typeof value.task.task_number !== 'number'
    || typeof value.task.version !== 'number' || typeof value.task.phase !== 'string'
    || (value.task.activity !== undefined && typeof value.task.activity !== 'string')) {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline Claim mutation result is invalid')
  }
  const claim = claimSummary(value.claim)
  positiveInteger(value.task.task_number, 'Task number')
  positiveInteger(value.task.version, 'Task version')
  return {
    ...value,
    task: {
      ...value.task,
      activity: typeof value.task.activity === 'string' ? value.task.activity : '',
    } as unknown as PactlineClaimMutationResult['task'],
    claim,
  }
}

function codeChangeMutation(value: unknown): PactlineCodeChangeMutationResult {
  if (!isRecord(value) || !isRecord(value.task) || typeof value.task.task_number !== 'number'
    || typeof value.task.version !== 'number' || typeof value.task.phase !== 'string'
    || (value.task.activity !== undefined && typeof value.task.activity !== 'string') || !isRecord(value.code_change)) {
    throw new PactlineClientError('INVALID_RESPONSE', 'Pactline code-change mutation result is invalid')
  }
  positiveInteger(value.task.task_number, 'Task number')
  positiveInteger(value.task.version, 'Task version')
  return {
    task: {
      ...value.task,
      activity: typeof value.task.activity === 'string' ? value.task.activity : '',
    } as unknown as PactlineCodeChangeMutationResult['task'],
    code_change: value.code_change,
  }
}

export interface PactlineCallOptions {
  readonly sessionId: string
  readonly idempotencyKey?: string
  readonly signal?: AbortSignal
}

/** Pactline product client restricted to declared machine-facing operations. */
export class PactlineCLI {
  readonly executable: string
  readonly server: string | undefined
  readonly clientKind: string
  readonly timeoutMs: number
  readonly maxOutputBytes: number
  readonly rateLimitRetries: number
  private readonly environment: NodeJS.ProcessEnv

  constructor(config: PactlineCLIConfig = {}, runtime: PactlineCLIRuntime = {}) {
    this.executable = config.executable ?? 'pactline'
    this.server = config.server
    this.clientKind = config.clientKind ?? 'pactline-fleet'
    this.timeoutMs = config.timeoutMs ?? 30_000
    this.maxOutputBytes = config.maxOutputBytes ?? 1_048_576
    this.rateLimitRetries = config.rateLimitRetries ?? 3
    if (!Number.isSafeInteger(this.rateLimitRetries) || this.rateLimitRetries < 0 || this.rateLimitRetries > 10) {
      throw new Error('rateLimitRetries must be between 0 and 10')
    }
    this.environment = { ...(runtime.environment ?? process.env) }
  }

  async capabilities(signal?: AbortSignal): Promise<PactlineCapabilities> {
    return parseCapabilities(await this.invoke<unknown>(['capabilities'], { signal }))
  }

  async doctor(sessionId: string, signal?: AbortSignal): Promise<PactlineDoctor> {
    return parseDoctor(await this.invoke<unknown>(['doctor'], { sessionId, signal }))
  }

  async preflight(options: PactlinePreflightOptions = {}): Promise<PactlinePreflightResult> {
    const expectedProtocol = options.protocol ?? 2
    const requiredFeatures = options.requiredFeatures ?? DEFAULT_REQUIRED_FEATURES
    const actual = await this.capabilities(options.signal)
    if (actual.protocol !== expectedProtocol) {
      throw new PactlineClientError('PROTOCOL_MISMATCH', `Pactline CLI protocol ${String(actual.protocol)} is incompatible; expected ${String(expectedProtocol)}`)
    }
    const available = new Set(actual.features)
    const missing = requiredFeatures.filter(feature => !available.has(feature))
    if (missing.length > 0) {
      throw new PactlineClientError('MISSING_FEATURE', `Pactline CLI is missing required features: ${missing.join(', ')}`)
    }
    if (options.verifyAuthentication === false) return { capabilities: actual }
    return { capabilities: actual, doctor: await this.doctor(options.sessionId ?? 'pactline-fleet-preflight', options.signal) }
  }

  async listTasks(stage: PactlineClaimStage, projectNumber: number, limit: number, options: Pick<PactlineCallOptions, 'sessionId' | 'signal'>): Promise<PactlineOperation<readonly PactlineTaskSummary[]>> {
    positiveInteger(projectNumber, 'projectNumber')
    positiveInteger(limit, 'limit')
    if (limit > 200) throw new Error('limit must not exceed 200')
    const result = await this.invokeEnvelope<unknown>(['task', 'list', '--stage', stage, '--project', String(projectNumber), '--limit', String(limit)], options)
    if (!isRecord(result.data) || !Array.isArray(result.data.items)) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline Task list is invalid')
    return { data: result.data.items.map(taskSummary), ...(result.meta === undefined ? {} : { meta: result.meta }) }
  }

  async listActiveClaims(options: Pick<PactlineCallOptions, 'sessionId' | 'signal'>): Promise<PactlineOperation<readonly PactlineClaimSummary[]>> {
    const result = await this.invokeEnvelope<unknown>(['claim', 'list'], options)
    if (!isRecord(result.data) || !Array.isArray(result.data.items)) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline active Claim list is invalid')
    return { data: result.data.items.map(claimSummary), ...(result.meta === undefined ? {} : { meta: result.meta }) }
  }

  async showTask(taskNumber: number, threadItemsLimit: number, options: Pick<PactlineCallOptions, 'sessionId' | 'signal'>): Promise<PactlineOperation<Record<string, unknown>>> {
    positiveInteger(taskNumber, 'taskNumber')
    positiveInteger(threadItemsLimit, 'threadItemsLimit')
    if (threadItemsLimit > 100) throw new Error('threadItemsLimit must not exceed 100')
    const result = await this.invokeEnvelope<unknown>(['task', 'show', String(taskNumber), '--compact', '--thread-items-limit', String(threadItemsLimit)], options)
    if (!isRecord(result.data)) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline Task work packet is invalid')
    packetTask(result.data.task)
    return { data: result.data, ...(result.meta === undefined ? {} : { meta: result.meta }) }
  }

  async claimTask(taskNumber: number, taskVersion: number, stage: PactlineClaimStage, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    positiveInteger(taskNumber, 'taskNumber')
    positiveInteger(taskVersion, 'taskVersion')
    return this.mutation(['task', 'claim', String(taskNumber), '--stage', stage, '--task-version', String(taskVersion)], options, claimMutation)
  }

  async showClaim(claimId: string, threadItemsLimit: number, options: Pick<PactlineCallOptions, 'sessionId' | 'signal'>): Promise<PactlineOperation<Record<string, unknown>>> {
    nonEmpty(claimId, 'claimId')
    positiveInteger(threadItemsLimit, 'threadItemsLimit')
    if (threadItemsLimit > 100) throw new Error('threadItemsLimit must not exceed 100')
    const result = await this.invokeEnvelope<unknown>(['claim', 'show', claimId, '--compact', '--thread-items-limit', String(threadItemsLimit)], options)
    if (!isRecord(result.data)) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline Claim work packet is invalid')
    claimSummary(result.data.claim)
    packetTask(result.data.task)
    return { data: result.data, ...(result.meta === undefined ? {} : { meta: result.meta }) }
  }

  async progressClaim(claimId: string, message: string, options: PactlineCallOptions): Promise<PactlineOperation<unknown>> {
    return this.textMutation(['claim', 'progress', claimId], '--file', message, options)
  }

  async verifyClaim(claimId: string, criterionId: string, taskVersion: number, criterionRevision: number, outcome: PactlineCheckOutcome, evidence: string, options: PactlineCallOptions): Promise<PactlineOperation<unknown>> {
    nonEmpty(claimId, 'claimId')
    nonEmpty(criterionId, 'criterionId')
    positiveInteger(taskVersion, 'taskVersion')
    positiveInteger(criterionRevision, 'criterionRevision')
    return this.mutation(['claim', 'verify', claimId, criterionId, '--task-version', String(taskVersion), '--criterion-revision', String(criterionRevision), '--outcome', outcome, '--evidence-file', '-'], { ...options, input: evidence }, value => value)
  }

  async linkCodeChange(claimId: string, taskVersion: number, url: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineCodeChangeMutationResult>> {
    nonEmpty(claimId, 'claimId')
    positiveInteger(taskVersion, 'taskVersion')
    nonEmpty(url, 'url')
    return this.mutation(['claim', 'change', 'link', claimId, '--url', url, '--task-version', String(taskVersion)], options, codeChangeMutation)
  }

  async submitClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    return this.claimBodyMutation('submit', claimId, taskVersion, message, options)
  }

  async completeClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    return this.claimBodyMutation('complete', claimId, taskVersion, message, options)
  }

  async releaseClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    return this.claimBodyMutation('release', claimId, taskVersion, message, options)
  }

  async requestChanges(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    return this.claimBodyMutation('request-changes', claimId, taskVersion, message, options)
  }

  async acceptClaim(claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    return this.claimBodyMutation('accept', claimId, taskVersion, message, options)
  }

  async requestResolution(claimId: string, taskVersion: number, issueType: PactlineIssueType, request: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    nonEmpty(claimId, 'claimId')
    positiveInteger(taskVersion, 'taskVersion')
    nonEmpty(request, 'request')
    return this.mutation(['claim', 'request-resolution', claimId, '--task-version', String(taskVersion), '--issue-type', issueType, '--file', '-'], { ...options, input: request }, claimMutation)
  }

  async resolveIssue(taskNumber: number, issueThreadId: string, taskVersion: number, threadVersion: number, conclusion: string, options: PactlineCallOptions): Promise<PactlineOperation<unknown>> {
    positiveInteger(taskNumber, 'taskNumber')
    nonEmpty(issueThreadId, 'issueThreadId')
    positiveInteger(taskVersion, 'taskVersion')
    positiveInteger(threadVersion, 'threadVersion')
    nonEmpty(conclusion, 'conclusion')
    return this.mutation(['issue', 'resolve', String(taskNumber), issueThreadId, '--task-version', String(taskVersion), '--thread-version', String(threadVersion), '--file', '-'], { ...options, input: conclusion }, value => value)
  }

  private async claimBodyMutation(command: string, claimId: string, taskVersion: number, message: string, options: PactlineCallOptions): Promise<PactlineOperation<PactlineClaimMutationResult>> {
    nonEmpty(claimId, 'claimId')
    positiveInteger(taskVersion, 'taskVersion')
    nonEmpty(message, 'message')
    return this.mutation(['claim', command, claimId, '--task-version', String(taskVersion), '--file', '-'], { ...options, input: message }, claimMutation)
  }

  private async textMutation(args: readonly string[], fileFlag: string, value: string, options: PactlineCallOptions): Promise<PactlineOperation<unknown>> {
    nonEmpty(String(args.at(-1) ?? ''), 'claimId')
    nonEmpty(value, 'text')
    return this.mutation([...args, fileFlag, '-'], { ...options, input: value }, item => item)
  }

  private async mutation<T>(args: readonly string[], options: PactlineCallOptions & { readonly input?: string }, validate: (value: unknown) => T): Promise<PactlineOperation<T>> {
    nonEmpty(options.sessionId, 'sessionId')
    nonEmpty(options.idempotencyKey ?? '', 'idempotencyKey')
    const result = await this.invokeEnvelope<unknown>(args, options)
    return { data: validate(result.data), ...(result.meta === undefined ? {} : { meta: result.meta }) }
  }

  private async invoke<T>(args: readonly string[], options: InvocationOptions): Promise<T> {
    return (await this.invokeEnvelope<T>(args, options)).data
  }

  private async invokeEnvelope<T>(args: readonly string[], options: InvocationOptions): Promise<PactlineOperation<T>> {
    for (let attempt = 0; ; attempt += 1) {
      const result = await this.runProcess(args, options)
      const parsed = this.parseEnvelope<T>(result.stdout)
      if (parsed.ok === false) {
        const pactlineError: PactlineErrorBody = {
          ...parsed.error,
          message: this.safeDiagnostic(parsed.error.message),
          ...(parsed.error.hint === undefined ? {} : { hint: this.safeDiagnostic(parsed.error.hint) }),
        }
        if (pactlineError.code === 'RATE_LIMITED'
          && pactlineError.retry_after_seconds !== undefined
          && attempt < this.rateLimitRetries) {
          await this.waitForRateLimit(Math.min(30, pactlineError.retry_after_seconds) * 1_000, options.signal)
          continue
        }
        throw new PactlineClientError('CLI_ERROR', `Pactline CLI rejected ${args[0] ?? 'operation'}: ${pactlineError.code}`, {
          exitCode: result.exitCode,
          pactlineError,
        })
      }
      if (result.exitCode !== 0) {
        throw new PactlineClientError('CLI_ERROR', `Pactline CLI exited with status ${String(result.exitCode)}: ${this.safeDiagnostic(result.stderr)}`, { exitCode: result.exitCode })
      }
      const meta = operationMeta(parsed.meta)
      return { data: parsed.data, ...(meta === undefined ? {} : { meta }) }
    }
  }

  private waitForRateLimit(delayMs: number, signal?: AbortSignal): Promise<void> {
    if (signal?.aborted === true) return Promise.reject(new PactlineClientError('ABORTED', 'Pactline CLI retry was aborted'))
    return new Promise((resolvePromise, reject) => {
      const timer = setTimeout(() => {
        signal?.removeEventListener('abort', onAbort)
        resolvePromise()
      }, delayMs)
      const onAbort = (): void => {
        clearTimeout(timer)
        reject(new PactlineClientError('ABORTED', 'Pactline CLI retry was aborted'))
      }
      signal?.addEventListener('abort', onAbort, { once: true })
    })
  }

  private parseEnvelope<T>(stdout: string): PactlineSuccess<T> | PactlineFailure {
    let parsed: unknown
    try {
      parsed = JSON.parse(stdout)
    } catch (error: unknown) {
      throw new PactlineClientError('INVALID_RESPONSE', 'Pactline CLI did not emit exactly one JSON document', { cause: error })
    }
    if (!isRecord(parsed) || typeof parsed.ok !== 'boolean') throw new PactlineClientError('INVALID_RESPONSE', 'Pactline CLI JSON envelope is invalid')
    if (parsed.ok === false) {
      const error = safeErrorBody(parsed.error)
      if (error === undefined) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline CLI error envelope is invalid')
      return { ok: false, error }
    }
    if (!Object.hasOwn(parsed, 'data')) throw new PactlineClientError('INVALID_RESPONSE', 'Pactline CLI success envelope has no data')
    const meta = operationMeta(parsed.meta)
    return { ok: true, data: parsed.data as T, ...(meta === undefined ? {} : { meta }) }
  }

  private runProcess(args: readonly string[], options: InvocationOptions): Promise<ProcessResult> {
    if (options.signal?.aborted === true) return Promise.reject(new PactlineClientError('ABORTED', 'Pactline CLI invocation was aborted'))
    const argv = ['--json', ...(options.idempotencyKey === undefined ? [] : ['--idempotency-key', options.idempotencyKey]), ...args]
    const environment: NodeJS.ProcessEnv = {
      ...this.environment,
      PACTLINE_CLIENT_KIND: this.clientKind,
      ...(this.server === undefined ? {} : { PACTLINE_SERVER: this.server }),
      ...(options.sessionId === undefined ? {} : { PACTLINE_SESSION_ID: options.sessionId }),
    }

    return new Promise((resolvePromise, reject) => {
      const child = spawn(this.executable, argv, {
        env: environment,
        shell: false,
        stdio: [options.input === undefined ? 'ignore' : 'pipe', 'pipe', 'pipe'],
      })
      const stdout: Buffer[] = []
      const stderr: Buffer[] = []
      let stdoutBytes = 0
      let stderrBytes = 0
      let terminalError: PactlineClientError | undefined
      let forceKill: NodeJS.Timeout | undefined

      const stop = (error: PactlineClientError): void => {
        if (terminalError !== undefined) return
        terminalError = error
        child.kill('SIGTERM')
        forceKill = setTimeout(() => child.kill('SIGKILL'), 250)
        forceKill.unref()
      }
      const timer = setTimeout(() => stop(new PactlineClientError('TIMEOUT', `Pactline CLI exceeded ${String(this.timeoutMs)} ms`)), this.timeoutMs)
      timer.unref()
      const onAbort = (): void => { stop(new PactlineClientError('ABORTED', 'Pactline CLI invocation was aborted')) }
      options.signal?.addEventListener('abort', onAbort, { once: true })
      if (options.input !== undefined) child.stdin!.end(options.input)

      child.stdout!.on('data', (chunk: Buffer) => {
        stdoutBytes += chunk.length
        if (stdoutBytes > this.maxOutputBytes) {
          stop(new PactlineClientError('OUTPUT_LIMIT', 'Pactline CLI stdout exceeded the configured limit'))
          return
        }
        stdout.push(chunk)
      })
      child.stderr!.on('data', (chunk: Buffer) => {
        stderrBytes += chunk.length
        if (stderrBytes > this.maxOutputBytes) {
          stop(new PactlineClientError('OUTPUT_LIMIT', 'Pactline CLI stderr exceeded the configured limit'))
          return
        }
        stderr.push(chunk)
      })
      child.once('error', (error: Error) => stop(new PactlineClientError('SPAWN_FAILED', `Failed to start Pactline CLI: ${error.message}`, { cause: error })))
      child.once('close', (code: number | null) => {
        clearTimeout(timer)
        if (forceKill !== undefined) clearTimeout(forceKill)
        options.signal?.removeEventListener('abort', onAbort)
        if (terminalError !== undefined) {
          reject(terminalError)
          return
        }
        resolvePromise({
          exitCode: code ?? 1,
          stdout: Buffer.concat(stdout).toString('utf8'),
          stderr: Buffer.concat(stderr).toString('utf8'),
        })
      })
    })
  }

  private safeDiagnostic(output: string): string {
    let safe = output.trim().slice(0, 2_048)
    const token = this.environment.PACTLINE_TOKEN
    if (token !== undefined && token.length > 0) safe = safe.replaceAll(token, '[REDACTED]')
    safe = safe
      .replace(/(Bearer\s+)[^\s]+/gi, '$1[REDACTED]')
      .replace(/(PACTLINE_TOKEN\s*=\s*)[^\s]+/gi, '$1[REDACTED]')
    return safe.length === 0 ? 'no diagnostic output' : safe
  }
}

export const REQUIRED_PACTLINE_FEATURES: readonly string[] = DEFAULT_REQUIRED_FEATURES
