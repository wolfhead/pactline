import { constants } from 'node:fs'
import { access, chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
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
import { CodexExecRuntime, type CodexRuntimeLaunch, type CodexWireEvent } from './wire.js'

const ADAPTER_ID = 'codex'
const ADAPTER_VERSION = '0.1.0-rc.1'
const PROVIDER = 'openai-codex'
const REQUIRED_MODEL = 'gpt-5.6-sol'
const REQUIRED_REASONING = 'high'
const RUNTIME_VERSION = '0.147.0'
const SENSITIVE_ENV = /(?:KEY|SECRET|TOKEN|PASSWORD|CREDENTIAL|AUTH|COOKIE)/i
const MAX_TIMER_DELAY_MS = 2_147_483_647
const adapterDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(adapterDirectory, '../../..')

interface RuntimePeer {
  readonly processId: number | undefined
  nextEvent(): Promise<CodexWireEvent>
  waitForSuccessfulExit(): Promise<void>
  close(): Promise<void>
  terminate(reason: string): Promise<void>
}

export interface CodexHarnessAdapterOptions {
  readonly runtimeCommand?: string
  readonly runtimeArgs?: readonly string[]
  readonly environment?: NodeJS.ProcessEnv
  readonly maxLineBytes?: number
  readonly maxOutputBytes?: number
  readonly maxStderrBytes?: number
  readonly exitGraceMs?: number
  readonly terminateGraceMs?: number
  readonly now?: () => Date
  readonly runtimeFactory?: (launch: CodexRuntimeLaunch) => RuntimePeer
  readonly probeVersion?: (command: string) => Promise<string>
  /** M4 may deliberately relax the native sandbox while Fleet Core keeps repository and settlement gates authoritative. */
  readonly workspaceSandbox?: 'workspace-write' | 'danger-full-access'
}

interface RunFacts {
  sessionId?: string
  completed: boolean
  failed: boolean
  failureDiagnostic?: string
  usage: HarnessTokenUsage
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function nonEmptyString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined
}

function integer(value: unknown): number | undefined {
  return Number.isSafeInteger(value) && Number(value) >= 0 ? Number(value) : undefined
}

/** Remove control-plane credentials; only the Codex provider credential may reach the Harness process. */
export function codexChildEnvironment(source: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const result: NodeJS.ProcessEnv = {}
  for (const [name, value] of Object.entries(source)) {
    if (value === undefined || SENSITIVE_ENV.test(name)) continue
    result[name] = value
  }
  for (const name of ['OPENAI_API_KEY', 'CODEX_API_KEY']) {
    if (source[name] !== undefined) result[name] = source[name]
  }
  return {
    ...result,
    GIT_TERMINAL_PROMPT: '0',
    GIT_ASKPASS: '/usr/bin/false',
  }
}

function runtimeCommand(options: CodexHarnessAdapterOptions): string {
  return options.runtimeCommand ?? resolve(fleetRoot, 'runtime/codex/node_modules/.bin/codex')
}

function configArgs(reasoning: string): string[] {
  return [
    '-c', `model_reasoning_effort=${JSON.stringify(reasoning)}`,
    '-c', 'approval_policy="never"',
    '-c', 'shell_environment_policy.inherit="none"',
    '-c', 'shell_environment_policy.include_only=["PATH","LANG","LC_ALL","TZ"]',
    '-c', 'features.multi_agent=false',
    '-c', 'web_search="disabled"',
    '-c', 'analytics.enabled=false',
  ]
}

function invocationArgs(
  request: HarnessRunRequest,
  schemaPath: string,
  resultPath: string,
  workspaceSandbox: 'workspace-write' | 'danger-full-access',
  resumeSessionId?: string,
): string[] {
  const common = [
    'exec', '--ignore-user-config', '--ignore-rules', '--json', '--color', 'never',
    '--model', request.policy.model,
    '--sandbox', request.sandbox === 'read_only' ? 'read-only' : workspaceSandbox,
    '-C', request.workspace,
    '--output-schema', schemaPath,
    '--output-last-message', resultPath,
    ...configArgs(request.policy.reasoning ?? ''),
  ]
  return resumeSessionId === undefined
    ? [...common, '-']
    : [...common, 'resume', resumeSessionId, '-']
}

function prompt(request: HarnessRunRequest, workspaceSandbox: 'workspace-write' | 'danger-full-access'): string {
  return [
    'You are the Codex Harness Adapter worker for Pactline Fleet.',
    request.policy.systemInstructions,
    request.policy.stageInstructions,
    `Workspace: ${request.workspace}`,
    `Admitted repository revision: ${request.repositoryRevision}`,
    `Allowed paths: ${JSON.stringify(request.allowedPaths)}`,
    `Fixed verification commands: ${JSON.stringify(request.verificationCommands)}`,
    `Sandbox policy: ${request.sandbox}`,
    `Native Codex sandbox: ${request.sandbox === 'read_only' ? 'read-only' : workspaceSandbox}`,
    'Use Codex native coding tools to inspect, edit, and test as the stage permits.',
    'Do not call Pactline, GitHub, GitLab, or any remote mutation command.',
    'Your final response must be only one JSON object matching the supplied output schema.',
    `Use this exact identity: runId=${request.runId}, claimId=${request.claimId}.`,
    'Authoritative sanitized Pactline work packet:',
    JSON.stringify(request.taskPacket, null, 2),
  ].join('\n\n')
}

/** Translate Fleet's provider-neutral schema into Codex strict structured-output form. */
export function codexOutputSchema(request: HarnessRunRequest): Readonly<Record<string, unknown>> {
  const root = structuredClone(request.resultSchema) as Record<string, unknown>
  const properties = record(root.properties)
  if (properties === undefined) throw new Error('Fleet result schema properties are missing')
  if (request.stage === 'review') delete properties.changedPaths
  else if (request.stage === 'execution' || request.stage === 'correction') delete properties.findings
  else { delete properties.changedPaths; delete properties.findings }
  const identityTypes: Readonly<Record<string, string>> = {
    schemaVersion: 'integer', kind: 'string', runId: 'string', claimId: 'string', taskNumber: 'integer',
  }
  for (const [name, type] of Object.entries(identityTypes)) {
    const property = record(properties[name])
    if (property !== undefined && property.type === undefined) property.type = type
  }
  // Codex strict structured output rejects some valid shell characters in
  // schema string literals. Core still requires every fixed command exactly
  // once, so the transport schema only needs to constrain this field to text.
  const verification = record(properties.verification)
  const verificationItems = record(verification?.items)
  const verificationProperties = record(verificationItems?.properties)
  const verificationCommand = record(verificationProperties?.command)
  if (verificationCommand !== undefined) delete verificationCommand.enum
  const resolution = properties.resolutionRequest
  if (resolution !== undefined) properties.resolutionRequest = { anyOf: [resolution, { type: 'null' }] }
  root.required = Object.keys(properties)
  return root
}

function eventTime(now: () => Date): string {
  return now().toISOString()
}

function itemTool(item: Record<string, unknown>): string | undefined {
  const type = nonEmptyString(item.type)
  if (type === 'command_execution') return 'shell'
  if (type === 'file_change') return 'file_change'
  if (type === 'mcp_tool_call') return nonEmptyString(item.tool) ?? 'mcp'
  if (type === 'web_search') return 'web_search'
  return undefined
}

function normalizeEvent(event: CodexWireEvent, facts: RunFacts, now: () => Date): HarnessRunEvent | undefined {
  const type = event.type
  if (type === 'thread.started') {
    const sessionId = nonEmptyString(event.thread_id)
    if (sessionId !== undefined) facts.sessionId = sessionId
    return { at: eventTime(now), type: 'codex.thread.started' }
  }
  if (type === 'turn.completed') {
    const usage = record(event.usage)
    const inputTokens = integer(usage?.input_tokens)
    const cachedInputTokens = integer(usage?.cached_input_tokens)
    const outputTokens = integer(usage?.output_tokens)
    const reasoningTokens = integer(usage?.reasoning_output_tokens)
    facts.completed = true
    facts.usage = {
      ...(inputTokens === undefined ? {} : { inputTokens }),
      ...(cachedInputTokens === undefined ? {} : { cachedInputTokens }),
      ...(outputTokens === undefined ? {} : { outputTokens }),
      ...(reasoningTokens === undefined ? {} : { reasoningTokens }),
    }
    return { at: eventTime(now), type: 'codex.turn.completed', outcome: 'ok' }
  }
  if (type === 'turn.failed') {
    facts.failed = true
    const error = record(event.error)
    const message = nonEmptyString(error?.message) ?? nonEmptyString(event.message)
    if (message !== undefined) facts.failureDiagnostic = message.slice(0, 4_096)
    return { at: eventTime(now), type: 'codex.turn.failed', outcome: 'error' }
  }
  if (type === 'error') return { at: eventTime(now), type: 'codex.transport.error', outcome: 'retryable' }
  if (type === 'item.started' || type === 'item.updated' || type === 'item.completed') {
    const item = record(event.item) ?? {}
    const tool = itemTool(item)
    const status = nonEmptyString(item.status)
    return {
      at: eventTime(now), type: `codex.${type}`,
      ...(tool === undefined ? {} : { tool }),
      ...(status === 'failed' ? { outcome: 'error' } : type === 'item.completed' ? { outcome: 'ok' } : {}),
    }
  }
  return { at: eventTime(now), type: `codex.${type}` }
}

function assertProposalShape(value: unknown, request: HarnessRunRequest): asserts value is HarnessProposal {
  const proposal = record(value)
  const task = record(request.taskPacket.task)
  const expectedKind = request.stage === 'correction' ? 'execution' : request.stage
  if (proposal?.schemaVersion !== 1 || proposal.kind !== expectedKind || proposal.runId !== request.runId
    || proposal.claimId !== request.claimId || proposal.taskNumber !== task?.number) {
    throw new Error('Codex proposal identity or stage does not match the active Run')
  }
}

async function defaultProbeVersion(command: string, environment: NodeJS.ProcessEnv): Promise<string> {
  const { execFile } = await import('node:child_process')
  return await new Promise<string>((resolvePromise, reject) => {
    execFile(command, ['--version'], {
      timeout: 10_000,
      maxBuffer: 64 * 1024,
      env: environment,
    }, (error, stdout) => {
      if (error !== null) reject(new Error('Codex runtime version probe failed', { cause: error }))
      else resolvePromise(stdout.trim())
    })
  })
}

/** Codex Agent CLI Adapter with pinned quality policy and bounded JSONL process ownership. */
export class CodexHarnessAdapter implements HarnessAdapter {
  readonly id = ADAPTER_ID
  readonly version = ADAPTER_VERSION
  private readonly active = new Map<string, RuntimePeer>()

  constructor(private readonly options: CodexHarnessAdapterOptions = {}) {}

  async probe(_request: HarnessProbeRequest): Promise<HarnessCapabilities> {
    const command = runtimeCommand(this.options)
    if (this.options.runtimeFactory === undefined) await access(command, constants.X_OK)
    const version = this.options.probeVersion === undefined
      ? await defaultProbeVersion(command, codexChildEnvironment(this.options.environment ?? process.env))
      : await this.options.probeVersion(command)
    if (!version.includes(`codex-cli ${RUNTIME_VERSION}`)) {
      throw new Error(`Codex Adapter requires codex-cli ${RUNTIME_VERSION}; observed ${version}`)
    }
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

  run(request: HarnessRunRequest, observer: HarnessRunObserver, signal: AbortSignal): Promise<HarnessRunResult> {
    return this.execute(request, observer, signal)
  }

  resume(runtimeSessionIdValue: string, request: HarnessRunRequest, observer: HarnessRunObserver, signal: AbortSignal): Promise<HarnessRunResult> {
    if (runtimeSessionIdValue.trim() === '') return Promise.reject(new Error('Codex runtime Session ID is required'))
    return this.execute(request, observer, signal, runtimeSessionIdValue)
  }

  async cancel(runtimeSessionIdValue: string, reason: string): Promise<void> {
    const runtime = this.active.get(runtimeSessionIdValue)
    if (runtime !== undefined) await runtime.terminate(`Codex Run cancelled: ${reason}`)
  }

  private async execute(
    request: HarnessRunRequest,
    observer: HarnessRunObserver,
    signal: AbortSignal,
    resumeSessionId?: string,
  ): Promise<HarnessRunResult> {
    if (signal.aborted) throw signal.reason
    if (request.policy.model !== REQUIRED_MODEL || request.policy.reasoning !== REQUIRED_REASONING) {
      throw new Error(`Codex Adapter requires ${REQUIRED_MODEL} with reasoning=${REQUIRED_REASONING} during M3`)
    }
    const deadline = Date.parse(request.deadline)
    if (!Number.isFinite(deadline) || deadline <= Date.now()) throw new Error('Codex Run deadline is invalid or elapsed')

    const privateRoot = await mkdtemp(join(tmpdir(), 'pactline-fleet-codex-'))
    await chmod(privateRoot, 0o700)
    const schemaPath = join(privateRoot, 'result-schema.json')
    const resultPath = join(privateRoot, 'result.json')
    await writeFile(schemaPath, `${JSON.stringify(codexOutputSchema(request))}\n`, { mode: 0o600 })
    const launch: CodexRuntimeLaunch = {
      command: runtimeCommand(this.options),
      args: [
        ...(this.options.runtimeArgs ?? []),
        ...invocationArgs(request, schemaPath, resultPath, this.options.workspaceSandbox ?? 'workspace-write', resumeSessionId),
      ],
      cwd: request.workspace,
      env: codexChildEnvironment(this.options.environment ?? process.env),
      input: prompt(request, this.options.workspaceSandbox ?? 'workspace-write'),
      maxLineBytes: this.options.maxLineBytes ?? 1_048_576,
      maxOutputBytes: this.options.maxOutputBytes ?? 16_777_216,
      maxStderrBytes: this.options.maxStderrBytes ?? 65_536,
      exitGraceMs: this.options.exitGraceMs ?? 1_000,
      terminateGraceMs: this.options.terminateGraceMs ?? 3_000,
    }
    const runtime = this.options.runtimeFactory?.(launch) ?? new CodexExecRuntime(launch)
    const facts: RunFacts = { completed: false, failed: false, usage: {} }
    const collector = new HarnessEventCollector()
    const now = this.options.now ?? (() => new Date())
    let activeSession = resumeSessionId
    if (activeSession !== undefined) {
      this.active.set(activeSession, runtime)
      await observer.onSessionStarted({ runtimeSessionId: activeSession })
    }
    let deadlineTimer: NodeJS.Timeout | undefined
    let abortHandler: (() => void) | undefined
    try {
      const interrupted = new Promise<never>((_resolve, reject) => {
        const terminate = (error: Error): void => {
          void runtime.terminate(error.message)
          reject(error)
        }
        abortHandler = () => terminate(signal.reason instanceof Error ? signal.reason : new Error('Codex Run cancelled'))
        signal.addEventListener('abort', abortHandler, { once: true })
        if (signal.aborted) { abortHandler(); return }
        const armDeadline = (): void => {
          const remaining = deadline - Date.now()
          if (remaining <= 0) { terminate(new Error('Codex Run deadline elapsed')); return }
          deadlineTimer = setTimeout(armDeadline, Math.min(remaining, MAX_TIMER_DELAY_MS)); deadlineTimer.unref()
        }
        armDeadline()
      })
      while (!facts.completed && !facts.failed) {
        const wireEvent = await Promise.race([runtime.nextEvent(), interrupted])
        const event = normalizeEvent(wireEvent, facts, now)
        if (facts.sessionId !== undefined && activeSession === undefined) {
          activeSession = facts.sessionId
          this.active.set(activeSession, runtime)
          await observer.onSessionStarted({ runtimeSessionId: activeSession })
        }
        if (event !== undefined) { await observer.onEvent(event); collector.accept(event) }
      }
      if (facts.failed) throw new Error(`Codex Run failed${facts.failureDiagnostic === undefined ? '' : `: ${facts.failureDiagnostic}`}`)
      await Promise.race([runtime.waitForSuccessfulExit(), interrupted])
      if (!facts.completed) throw new Error('Codex Run did not complete successfully')
      if (activeSession === undefined) throw new Error('Codex Run did not emit a runtime Session ID')
      await chmod(resultPath, 0o600)
      const proposal = JSON.parse(await readFile(resultPath, 'utf8')) as unknown
      const proposalRecord = record(proposal)
      if (proposalRecord?.resolutionRequest === null) delete proposalRecord.resolutionRequest
      assertProposalShape(proposal, request)
      return {
        adapterId: this.id,
        adapterVersion: this.version,
        runtimeSessionId: activeSession,
        model: { provider: PROVIDER, model: request.policy.model, reasoning: request.policy.reasoning },
        terminalState: 'completed',
        proposal,
        usage: facts.usage,
        eventSummary: collector.summary(),
      }
    } finally {
      if (deadlineTimer !== undefined) clearTimeout(deadlineTimer)
      if (abortHandler !== undefined) signal.removeEventListener('abort', abortHandler)
      if (activeSession !== undefined) this.active.delete(activeSession)
      try { await runtime.close() } finally { await rm(privateRoot, { recursive: true, force: true }) }
    }
  }
}

export const codexAdapterPolicy = Object.freeze({
  provider: PROVIDER,
  model: REQUIRED_MODEL,
  reasoning: REQUIRED_REASONING,
  runtimeVersion: RUNTIME_VERSION,
})
