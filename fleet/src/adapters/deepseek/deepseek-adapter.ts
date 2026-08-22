import { access, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { constants } from 'node:fs'
import { createHash } from 'node:crypto'
import { basename, dirname, join, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { fileURLToPath } from 'node:url'
import type {
  HarnessAdapter,
  HarnessCapabilities,
  HarnessProbeRequest,
  HarnessRunEvent,
  HarnessRunObserver,
  HarnessRunRequest,
} from '../../core/harness-adapter.js'
import { HarnessEventCollector } from '../../core/events.js'
import type { HarnessProposal, HarnessRunResult, HarnessTokenUsage } from '../../core/harness-result.js'
import {
  DeepSeekJsonRpcRuntime,
  type DeepSeekRuntimeLaunch,
  type DeepSeekWireNotification,
} from './wire.js'
import { resolveDeepSeekCredential } from './credential.js'

const ADAPTER_ID = 'deepseek'
const ADAPTER_VERSION = '0.1.0-rc.6'
const PROVIDER = 'deepseek-official'
const REQUIRED_MODEL = 'deepseek-v4-pro'
const REQUIRED_REASONING = 'max'
const RESULT_TOOL = 'submit_fleet_result'
const SENSITIVE_ENV = /(?:KEY|SECRET|TOKEN|PASSWORD|CREDENTIAL|AUTH|COOKIE)/i
const MAX_TIMER_DELAY_MS = 2_147_483_647
const adapterDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(adapterDirectory, '../../..')

interface RuntimePeer {
  request(method: string, params?: Record<string, unknown>, timeoutMs?: number): Promise<unknown>
  nextNotification(): Promise<DeepSeekWireNotification>
  close(): Promise<void>
  terminate(reason: string): Promise<void>
}

export interface DeepSeekHarnessAdapterOptions {
  readonly runtimeCommand?: string
  readonly runtimeArgs?: readonly string[]
  readonly runtimeCwd?: string
  readonly runtimeConfig?: string
  /** Explicit Adapter-owned runtime variables; ambient DSH_* variables are never inherited. */
  readonly runtimeEnvironment?: NodeJS.ProcessEnv
  readonly environment?: NodeJS.ProcessEnv
  readonly requestTimeoutMs?: number
  readonly shutdownTimeoutMs?: number
  readonly terminateTimeoutMs?: number
  readonly maxStderrBytes?: number
  readonly maxTokens?: number
  readonly now?: () => Date
  readonly runtimeFactory?: (launch: DeepSeekRuntimeLaunch) => RuntimePeer
}

interface SessionFacts {
  proposal: unknown
  proposalCallId?: string
  acceptedProposalCallId?: string
  receivedPrompt: boolean
  completedTurn: boolean
  usage: MutableUsage
  readonly calls: Map<string, string>
}

interface MutableUsage {
  inputTokens: number
  cachedInputTokens: number
  outputTokens: number
  reasoningTokens: number
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function nonEmptyString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined
}

function positiveInteger(value: unknown): number | undefined {
  return Number.isSafeInteger(value) && Number(value) >= 0 ? Number(value) : undefined
}

function runtimeSessionId(runId: string): string {
  const digest = createHash('sha256').update(runId).digest('hex').slice(0, 24)
  return `pactline-fleet-${digest}`
}

function taskSessionRoot(workspace: string): string {
  const parent = dirname(workspace)
  const taskRoot = basename(workspace) === 'repository' && basename(parent).startsWith('pactline-fleet-')
    ? parent
    : workspace
  return join(taskRoot, '.deepseek-sessions')
}

/** Preserve ordinary build/runtime variables but remove ambient Harness state and credentials except the selected model key. */
export function deepSeekChildEnvironment(
  source: NodeJS.ProcessEnv,
  additions: NodeJS.ProcessEnv,
): NodeJS.ProcessEnv {
  const result: NodeJS.ProcessEnv = {}
  for (const [name, value] of Object.entries(source)) {
    if (value === undefined || SENSITIVE_ENV.test(name) || name.startsWith('DSH_')) continue
    result[name] = value
  }
  for (const name of ['DEEPSEEK_API_KEY', 'DEEPSEEK_BASE_URL']) {
    if (source[name] !== undefined) result[name] = source[name]
  }
  return {
    ...result,
    ...additions,
    GIT_TERMINAL_PROMPT: '0',
    GIT_ASKPASS: '/usr/bin/false',
  }
}

function prompt(request: HarnessRunRequest): string {
  return [
    request.policy.stageInstructions,
    `Workspace: ${request.workspace}`,
    `Admitted repository revision: ${request.repositoryRevision}`,
    `Allowed paths: ${JSON.stringify(request.allowedPaths)}`,
    `Fixed verification commands: ${JSON.stringify(request.verificationCommands)}`,
    `Sandbox policy: ${request.sandbox}`,
    'Use the available native coding tools to inspect, implement, or review as required.',
    `Finish only by calling ${RESULT_TOOL} exactly once with a result matching its schema. Plain prose is not a result.`,
    `Use this exact identity: runId=${request.runId}, claimId=${request.claimId}.`,
    'Authoritative sanitized Pactline work packet:',
    JSON.stringify(request.taskPacket, null, 2),
  ].join('\n\n')
}

function systemPrompt(request: HarnessRunRequest): string {
  return [
    'You are the DeepSeek Harness Adapter worker for Pactline Fleet.',
    request.policy.systemInstructions,
    'Follow the current DSH file sandbox. Do not request wider permissions.',
    `The only accepted terminal result is a successful ${RESULT_TOOL} tool call.`,
  ].join('\n')
}

function eventTime(event: Record<string, unknown>, now: () => Date): string {
  const time = typeof event.time === 'number' ? event.time : Number.NaN
  return Number.isFinite(time) && time >= 0 ? new Date(time).toISOString() : now().toISOString()
}

function toolResultError(data: Record<string, unknown>): boolean {
  if (data.error !== undefined) return true
  const message = record(data.message)
  const content = Array.isArray(message?.content) ? message.content : []
  return content.some(item => record(item)?.isError === true)
}

function addUsage(target: MutableUsage, event: Record<string, unknown>): void {
  if (event.type !== 'assistant/message') return
  const data = record(event.data)
  const usage = record(data?.usage)
  if (usage === undefined) return
  target.inputTokens += positiveInteger(usage.inputTokens) ?? 0
  target.cachedInputTokens += positiveInteger(usage.cacheReadTokens) ?? 0
  target.outputTokens += positiveInteger(usage.outputTokens) ?? 0
  target.reasoningTokens += positiveInteger(usage.reasoningTokens) ?? 0
}

function normalizeEvent(
  notification: DeepSeekWireNotification,
  facts: SessionFacts,
  now: () => Date,
): HarnessRunEvent | undefined {
  if (notification.method === 'session.status') {
    const status = nonEmptyString(notification.params.status)
    return status === undefined ? undefined : { at: now().toISOString(), type: `deepseek.session.${status}` }
  }
  if (notification.method !== 'session.event') return undefined
  const event = record(notification.params.event)
  const type = nonEmptyString(event?.type)
  if (event === undefined || type === undefined || type === 'assistant/chunk') return undefined
  const data = record(event.data) ?? {}
  addUsage(facts.usage, event)
  if (type === 'tool/call') {
    const callId = nonEmptyString(data.callId)
    const tool = nonEmptyString(data.name)
    if (callId !== undefined && tool !== undefined) facts.calls.set(callId, tool)
    if (tool === RESULT_TOOL && callId !== undefined && typeof data.arguments === 'string') {
      facts.proposal = JSON.parse(data.arguments) as unknown
      facts.proposalCallId = callId
    }
    return { at: eventTime(event, now), type: 'deepseek.tool.call', ...(tool === undefined ? {} : { tool }) }
  }
  if (type === 'tool/result') {
    const message = record(data.message)
    const source = record(message?.source)
    const callId = nonEmptyString(source?.callId)
    const tool = callId === undefined ? undefined : facts.calls.get(callId)
    const failed = toolResultError(data)
    if (!failed && callId !== undefined && callId === facts.proposalCallId) facts.acceptedProposalCallId = callId
    return {
      at: eventTime(event, now), type: 'deepseek.tool.result', outcome: failed ? 'error' : 'ok',
      ...(tool === undefined ? {} : { tool }),
    }
  }
  if (type === 'turn/end') {
    const reason = record(data.reason)
    facts.completedTurn = reason?.kind === 'completed'
    return {
      at: eventTime(event, now), type: 'deepseek.turn.end',
      outcome: facts.completedTurn ? 'ok' : nonEmptyString(reason?.kind) ?? 'error',
    }
  }
  return { at: eventTime(event, now), type: `deepseek.${type}` }
}

function promptReceipt(notification: DeepSeekWireNotification, sessionId: string, messageId: string): boolean {
  if (notification.method !== 'session.event' || notification.params.sessionId !== sessionId) return false
  const event = record(notification.params.event)
  const data = event?.type === 'agent/inbox/spliced' ? record(event.data) : undefined
  const inserted = Array.isArray(data?.inserted) ? data.inserted : []
  return inserted.some(item => record(item)?.id === messageId)
}

function promptMessageId(value: unknown): string {
  const messageId = nonEmptyString(record(value)?.messageId)
  if (messageId === undefined) throw new Error('DeepSeek Harness returned an invalid prompt receipt')
  return messageId
}

function assertInitializeResult(value: unknown): void {
  const info = record(record(value)?.serverInfo)
  if (info?.name !== 'deepseek-harness-sdk-runtime') {
    throw new Error('DeepSeek Harness returned an incompatible SDK server identity')
  }
}

function assertProposalShape(value: unknown, request: HarnessRunRequest): asserts value is HarnessProposal {
  const proposal = record(value)
  const expectedKind = request.stage === 'correction' ? 'execution' : request.stage
  const task = record(request.taskPacket.task)
  if (proposal?.schemaVersion !== 1 || proposal.kind !== expectedKind || proposal.runId !== request.runId
    || proposal.claimId !== request.claimId || proposal.taskNumber !== task?.number) {
    throw new Error('DeepSeek Harness proposal identity or stage does not match the active Run')
  }
}

function usageResult(usage: MutableUsage): HarnessTokenUsage {
  return {
    ...(usage.inputTokens === 0 ? {} : { inputTokens: usage.inputTokens }),
    ...(usage.cachedInputTokens === 0 ? {} : { cachedInputTokens: usage.cachedInputTokens }),
    ...(usage.outputTokens === 0 ? {} : { outputTokens: usage.outputTokens }),
    ...(usage.reasoningTokens === 0 ? {} : { reasoningTokens: usage.reasoningTokens }),
  }
}

function defaultRuntimePaths(options: DeepSeekHarnessAdapterOptions): { command: string; args: readonly string[]; cwd: string; config: string } {
  const runtimeRoot = resolve(fleetRoot, 'runtime/deepseek')
  const command = options.runtimeCommand ?? join(runtimeRoot, 'node_modules/.bin/dsh-jsonrpc-agent')
  const config = options.runtimeConfig ?? join(runtimeRoot, 'cordis.yml')
  return {
    command,
    args: options.runtimeArgs ?? [config],
    cwd: options.runtimeCwd ?? runtimeRoot,
    config,
  }
}

/** DeepSeek Harness process Adapter over its public stdio JSON-RPC protocol. */
export class DeepSeekHarnessAdapter implements HarnessAdapter {
  readonly id = ADAPTER_ID
  readonly version = ADAPTER_VERSION
  private readonly active = new Map<string, RuntimePeer>()
  private probeTask: Promise<void> | undefined

  constructor(private readonly options: DeepSeekHarnessAdapterOptions = {}) {}

  async probe(request: HarnessProbeRequest): Promise<HarnessCapabilities> {
    const paths = defaultRuntimePaths(this.options)
    if (this.options.runtimeFactory === undefined) {
      await access(paths.command, constants.X_OK).catch((error: unknown) => {
        throw new Error('DeepSeek Harness Adapter runtime is not installed; run npm --prefix fleet/runtime/deepseek ci', { cause: error })
      })
      await access(paths.config, constants.R_OK).catch((error: unknown) => {
        throw new Error('DeepSeek Harness Adapter Cordis profile is unavailable', { cause: error })
      })
    }
    this.probeTask ??= this.probeRuntime(paths, request.requiredSandbox).catch((error: unknown) => {
      this.probeTask = undefined
      throw error
    })
    await this.probeTask
    return {
      nativeTools: true,
      structuredResult: true,
      eventStream: true,
      cancellation: true,
      sessionResume: true,
      sandboxModes: ['read_only', 'workspace_write'],
      supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'],
    }
  }

  private async probeRuntime(
    paths: { command: string; args: readonly string[]; cwd: string; config: string },
    sandbox: HarnessProbeRequest['requiredSandbox'],
  ): Promise<void> {
    const privateRoot = await mkdtemp(join(tmpdir(), 'pactline-fleet-deepseek-probe-'))
    const schemaPath = join(privateRoot, 'result-schema.json')
    await writeFile(schemaPath, '{"type":"object"}\n', { mode: 0o600 })
    const launch: DeepSeekRuntimeLaunch = {
      command: paths.command,
      args: paths.args,
      cwd: paths.cwd,
      env: deepSeekChildEnvironment(this.options.environment ?? process.env, {
        ...this.options.runtimeEnvironment,
        DSH_CORDIS_CONFIG: paths.config,
        DSH_CWD: paths.cwd,
        DSH_PERMISSION_MODE: sandbox === 'read_only' ? 'read-only' : 'workspace-write',
        DSH_SESSION_ROOT: join(privateRoot, 'sessions'),
        DSH_SYSTEM_PROMPT: 'Pactline Fleet DeepSeek Adapter keyless preflight.',
        PACTLINE_FLEET_RESULT_SCHEMA_PATH: schemaPath,
      }),
      requestTimeoutMs: this.options.requestTimeoutMs ?? 30_000,
      shutdownTimeoutMs: this.options.shutdownTimeoutMs ?? 1_000,
      terminateTimeoutMs: this.options.terminateTimeoutMs ?? 3_000,
      maxStderrBytes: this.options.maxStderrBytes ?? 65_536,
    }
    const runtime = this.options.runtimeFactory?.(launch) ?? new DeepSeekJsonRpcRuntime(launch)
    try {
      assertInitializeResult(await runtime.request('initialize', {
        cwd: paths.cwd,
        provider: PROVIDER,
        model: REQUIRED_MODEL,
        maxTokens: 1,
      }))
    } finally {
      try {
        await runtime.close()
      } finally {
        await rm(privateRoot, { recursive: true, force: true })
      }
    }
  }

  run(request: HarnessRunRequest, observer: HarnessRunObserver, signal: AbortSignal): Promise<HarnessRunResult> {
    return this.execute(request, observer, signal)
  }

  resume(
    runtimeSessionIdValue: string,
    request: HarnessRunRequest,
    observer: HarnessRunObserver,
    signal: AbortSignal,
  ): Promise<HarnessRunResult> {
    if (runtimeSessionIdValue.trim() === '') return Promise.reject(new Error('DeepSeek runtime Session ID is required'))
    return this.execute(request, observer, signal, runtimeSessionIdValue)
  }

  private async execute(
    request: HarnessRunRequest,
    observer: HarnessRunObserver,
    signal: AbortSignal,
    resumeSessionId?: string,
  ): Promise<HarnessRunResult> {
    if (signal.aborted) throw signal.reason
    if (request.policy.model !== REQUIRED_MODEL || request.policy.reasoning !== REQUIRED_REASONING) {
      throw new Error(`DeepSeek Harness Adapter requires ${REQUIRED_MODEL} with reasoning=max during M2`)
    }
    const deadline = Date.parse(request.deadline)
    if (!Number.isFinite(deadline) || deadline <= Date.now()) throw new Error('DeepSeek Harness Run deadline is invalid or elapsed')

    const paths = defaultRuntimePaths(this.options)
    const sourceEnvironment = this.options.environment ?? process.env
    const credential = await resolveDeepSeekCredential(sourceEnvironment)
    const privateRoot = await mkdtemp(join(tmpdir(), 'pactline-fleet-deepseek-'))
    const schemaPath = join(privateRoot, 'result-schema.json')
    await writeFile(schemaPath, `${JSON.stringify(request.resultSchema)}\n`, { mode: 0o600 })
    const sessionRoot = taskSessionRoot(request.workspace)
    await mkdir(sessionRoot, { recursive: true, mode: 0o700 })
    const sessionId = resumeSessionId ?? runtimeSessionId(request.runId)
    const launch: DeepSeekRuntimeLaunch = {
      command: paths.command,
      args: paths.args,
      cwd: paths.cwd,
      env: deepSeekChildEnvironment(sourceEnvironment, {
        ...this.options.runtimeEnvironment,
        DSH_CORDIS_CONFIG: paths.config,
        DSH_CWD: request.workspace,
        DSH_PERMISSION_MODE: request.sandbox === 'read_only' ? 'read-only' : 'workspace-write',
        DSH_SESSION_ROOT: sessionRoot,
        DSH_SYSTEM_PROMPT: systemPrompt(request),
        PACTLINE_FLEET_RESULT_SCHEMA_PATH: schemaPath,
        ...(credential === undefined ? {} : { DEEPSEEK_API_KEY: credential }),
      }),
      requestTimeoutMs: this.options.requestTimeoutMs ?? 30_000,
      shutdownTimeoutMs: this.options.shutdownTimeoutMs ?? 1_000,
      terminateTimeoutMs: this.options.terminateTimeoutMs ?? 3_000,
      maxStderrBytes: this.options.maxStderrBytes ?? 65_536,
    }
    const runtime = this.options.runtimeFactory?.(launch) ?? new DeepSeekJsonRpcRuntime(launch)
    this.active.set(sessionId, runtime)
    const collector = new HarnessEventCollector()
    const facts: SessionFacts = {
      proposal: undefined,
      receivedPrompt: false,
      completedTurn: false,
      usage: { inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningTokens: 0 },
      calls: new Map(),
    }
    const now = this.options.now ?? (() => new Date())
    let deadlineTimer: NodeJS.Timeout | undefined
    let abortHandler: (() => void) | undefined
    try {
      assertInitializeResult(await runtime.request('initialize', {
        cwd: request.workspace,
        provider: PROVIDER,
        model: request.policy.model,
        maxTokens: this.options.maxTokens ?? 32_768,
      }))
      await observer.onSessionStarted({ runtimeSessionId: sessionId })
      const promptResult = await runtime.request('session/prompt', {
        sessionId,
        contentBlocks: [{ type: 'text', text: prompt(request) }],
      })
      const messageId = promptMessageId(promptResult)
      const interrupted = new Promise<never>((_resolve, reject) => {
        abortHandler = () => {
          const reason = signal.reason instanceof Error ? signal.reason.message : 'DeepSeek Harness Run cancelled'
          void runtime.terminate(reason)
          reject(signal.reason instanceof Error ? signal.reason : new Error(reason))
        }
        signal.addEventListener('abort', abortHandler, { once: true })
        if (signal.aborted) {
          abortHandler()
          return
        }
        const armDeadline = (): void => {
          const remaining = deadline - Date.now()
          if (remaining <= 0) {
            const error = new Error('DeepSeek Harness Run deadline elapsed')
            void runtime.terminate(error.message)
            reject(error)
            return
          }
          deadlineTimer = setTimeout(armDeadline, Math.min(remaining, MAX_TIMER_DELAY_MS))
          deadlineTimer.unref()
        }
        armDeadline()
      })
      while (true) {
        const notification = await Promise.race([runtime.nextNotification(), interrupted])
        if (notification.params.sessionId !== sessionId) continue
        if (!facts.receivedPrompt && promptReceipt(notification, sessionId, messageId)) facts.receivedPrompt = true
        const event = normalizeEvent(notification, facts, now)
        if (event !== undefined) {
          await observer.onEvent(event)
          collector.accept(event)
        }
        if (facts.receivedPrompt && notification.method === 'session.status' && notification.params.status === 'idle') break
      }
      if (!facts.completedTurn) throw new Error('DeepSeek Harness Run did not end with a completed turn')
      if (facts.proposalCallId === undefined || facts.acceptedProposalCallId !== facts.proposalCallId) {
        throw new Error(`DeepSeek Harness Run did not commit ${RESULT_TOOL}`)
      }
      assertProposalShape(facts.proposal, request)
      return {
        adapterId: this.id,
        adapterVersion: this.version,
        runtimeSessionId: sessionId,
        model: { provider: PROVIDER, model: request.policy.model, reasoning: request.policy.reasoning },
        terminalState: 'completed',
        proposal: facts.proposal,
        usage: usageResult(facts.usage),
        eventSummary: collector.summary(),
      }
    } finally {
      if (deadlineTimer !== undefined) clearTimeout(deadlineTimer)
      if (abortHandler !== undefined) signal.removeEventListener('abort', abortHandler)
      this.active.delete(sessionId)
      try {
        await runtime.close()
      } finally {
        await rm(privateRoot, { recursive: true, force: true })
      }
    }
  }

  async cancel(runtimeSessionIdValue: string, reason: string): Promise<void> {
    const runtime = this.active.get(runtimeSessionIdValue)
    if (runtime !== undefined) await runtime.terminate(`DeepSeek Harness Run cancelled: ${reason}`)
  }
}

export const deepSeekAdapterPolicy = Object.freeze({
  provider: PROVIDER,
  model: REQUIRED_MODEL,
  reasoning: REQUIRED_REASONING,
  resultTool: RESULT_TOOL,
})
