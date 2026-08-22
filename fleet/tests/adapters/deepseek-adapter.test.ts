import { describe, expect, it } from 'vitest'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import type { HarnessRunEvent, HarnessRunRequest } from '../../src/core/harness-adapter.js'
import { DeepSeekHarnessAdapter, deepSeekAdapterPolicy, deepSeekChildEnvironment } from '../../src/adapters/deepseek/deepseek-adapter.js'
import type { DeepSeekWireNotification } from '../../src/adapters/deepseek/wire.js'

const executionProposal = {
  schemaVersion: 1, kind: 'execution', runId: 'run-1', claimId: 'claim-1', taskNumber: 7,
  recommendation: 'complete', summary: 'Implemented.', changedPaths: ['README.md'],
  verification: [{ command: 'true', outcome: 'passed', summary: 'Passed.' }],
  criteria: [], limitations: [],
}

const reviewProposal = {
  schemaVersion: 1, kind: 'review', runId: 'run-1', claimId: 'claim-1', taskNumber: 7,
  recommendation: 'accept', summary: 'Clean review.', findings: [],
  verification: [{ command: 'true', outcome: 'passed', summary: 'Passed.' }],
  criteria: [], limitations: [],
}

function request(stage: 'execution' | 'review', proposal: unknown, workspace = '/tmp/fleet-deepseek-test'): HarnessRunRequest {
  return {
    runId: 'run-1', claimId: 'claim-1', stage,
    workspace, repositoryRevision: 'a'.repeat(40),
    taskPacket: { task: { number: 7 }, expectedProposal: proposal },
    allowedPaths: ['README.md'], verificationCommands: ['true'], resultSchema: { type: 'object' },
    sandbox: stage === 'review' ? 'read_only' : 'workspace_write',
    deadline: '2099-08-15T00:00:00.000Z',
    policy: {
      model: deepSeekAdapterPolicy.model, reasoning: deepSeekAdapterPolicy.reasoning,
      promptVersion: 'fleet-m2-v1', resultContractVersion: 1,
      systemInstructions: 'Do not mutate remote systems.',
      stageInstructions: stage === 'review' ? 'Review the delivery.' : 'Implement the Task.',
    },
  }
}

class ScriptedRuntime {
  readonly notifications: DeepSeekWireNotification[]
  readonly requests: { method: string; params?: Record<string, unknown> }[] = []
  terminated: string | undefined
  closed = false
  private waitingReject: ((error: Error) => void) | undefined

  constructor(proposal: unknown, private readonly hang = false) {
    const resultArguments = JSON.stringify(proposal)
    this.notifications = [
      { method: 'session.status', params: { sessionId: 'ignored', status: 'running' } },
      { method: 'session.event', params: { sessionId: 'SESSION', event: { type: 'agent/inbox/spliced', time: 1, data: { inserted: [{ id: 'message-1' }] } } } },
      { method: 'session.status', params: { sessionId: 'SESSION', status: 'running' } },
      { method: 'session.event', params: { sessionId: 'SESSION', event: { type: 'tool/call', time: 2, data: { callId: 'result-1', name: 'submit_fleet_result', arguments: resultArguments } } } },
      { method: 'session.event', params: { sessionId: 'SESSION', event: { type: 'tool/result', time: 3, data: { message: { source: { callId: 'result-1' }, content: [{ isError: false }] } } } } },
      { method: 'session.event', params: { sessionId: 'SESSION', event: { type: 'assistant/message', time: 4, data: { usage: { inputTokens: 10, cacheReadTokens: 3, outputTokens: 5, reasoningTokens: 2 } } } } },
      { method: 'session.event', params: { sessionId: 'SESSION', event: { type: 'turn/end', time: 5, data: { reason: { kind: 'completed' } } } } },
      { method: 'session.status', params: { sessionId: 'SESSION', status: 'idle' } },
    ]
  }

  request(method: string, params?: Record<string, unknown>): Promise<unknown> {
    this.requests.push({ method, ...(params === undefined ? {} : { params }) })
    if (method === 'initialize') return Promise.resolve({ serverInfo: { name: 'deepseek-harness-sdk-runtime', version: '0.0.1' } })
    if (method === 'session/prompt') {
      const sessionId = String(params?.sessionId)
      for (const notification of this.notifications) {
        if (notification.params.sessionId === 'SESSION') notification.params.sessionId = sessionId
      }
      return Promise.resolve({ messageId: 'message-1' })
    }
    return Promise.resolve({})
  }

  nextNotification(): Promise<DeepSeekWireNotification> {
    const item = this.notifications.shift()
    if (!this.hang && item !== undefined) return Promise.resolve(item)
    return new Promise((_resolve, reject) => { this.waitingReject = reject })
  }

  close(): Promise<void> {
    this.closed = true
    return Promise.resolve()
  }

  terminate(reason: string): Promise<void> {
    this.terminated = reason
    this.waitingReject?.(new Error(reason))
    return Promise.resolve()
  }
}

describe('DeepSeekHarnessAdapter', () => {
  it.each([
    ['workspace-write execution', 'execution', executionProposal],
    ['read-only review', 'review', reviewProposal],
  ] as const)('runs the shared Adapter contract for %s', async (_name, stage, proposal) => {
    let runtime: ScriptedRuntime | undefined
    const adapter = new DeepSeekHarnessAdapter({
      runtimeFactory: () => runtime = new ScriptedRuntime(proposal),
      now: () => new Date('2026-08-15T00:00:00.000Z'),
    })
    const events: HarnessRunEvent[] = []
    const sessions: string[] = []
    const result = await adapter.run(request(stage, proposal), {
      onSessionStarted: value => { sessions.push(value.runtimeSessionId) },
      onEvent: event => { events.push(event) },
    }, new AbortController().signal)

    expect(result.terminalState).toBe('completed')
    expect(result.proposal).toEqual(proposal)
    expect(result.model).toEqual({ provider: 'deepseek-official', model: 'deepseek-v4-pro', reasoning: 'max' })
    expect(result.usage).toEqual({ inputTokens: 10, cachedInputTokens: 3, outputTokens: 5, reasoningTokens: 2 })
    expect(result.eventSummary.total).toBe(events.length)
    expect(result.eventSummary.toolCalls).toEqual({ submit_fleet_result: 2 })
    expect(sessions).toHaveLength(1)
    expect(runtime?.requests[0]).toMatchObject({ method: 'initialize', params: { model: 'deepseek-v4-pro', maxTokens: 32768 } })
    expect(runtime?.closed).toBe(true)
  })

  it('terminates its owned runtime when the Fleet signal is cancelled', async () => {
    let runtime: ScriptedRuntime | undefined
    const adapter = new DeepSeekHarnessAdapter({ runtimeFactory: () => runtime = new ScriptedRuntime(executionProposal, true) })
    const controller = new AbortController()
    const run = adapter.run(request('execution', executionProposal), { onSessionStarted: () => {}, onEvent: () => {} }, controller.signal)
    await new Promise(resolve => setImmediate(resolve))
    controller.abort(new Error('cancelled by coordinator'))

    await expect(run).rejects.toThrow('cancelled by coordinator')
    expect(runtime?.terminated).toBe('cancelled by coordinator')
    expect(runtime?.closed).toBe(true)
  })

  it('resumes the same native Session from Task-owned durable storage', async () => {
    const taskRoot = await mkdtemp(join(tmpdir(), 'pactline-fleet-deepseek-resume-'))
    const workspace = join(taskRoot, 'repository')
    const launches: Array<{ env: NodeJS.ProcessEnv }> = []
    const adapter = new DeepSeekHarnessAdapter({
      runtimeFactory: launch => {
        launches.push({ env: launch.env })
        return new ScriptedRuntime(executionProposal)
      },
    })
    const observer = { onSessionStarted: () => {}, onEvent: () => {} }
    try {
      const first = await adapter.run(
        request('execution', executionProposal, workspace), observer, new AbortController().signal,
      )
      const resumed = await adapter.resume!(
        first.runtimeSessionId,
        request('execution', executionProposal, workspace),
        observer,
        new AbortController().signal,
      )

      expect(resumed.runtimeSessionId).toBe(first.runtimeSessionId)
      expect(launches[0]?.env.DSH_SESSION_ROOT).toBe(join(taskRoot, '.deepseek-sessions'))
      expect(launches[1]?.env.DSH_SESSION_ROOT).toBe(launches[0]?.env.DSH_SESSION_ROOT)
      await expect(adapter.probe({
        requiredStages: ['execution'], requiredSandbox: 'workspace_write', requireNativeTools: true,
        requireStructuredResult: true, requireEventStream: true, requireCancellation: true,
        requireSessionResume: true,
      })).resolves.toMatchObject({ sessionResume: true })
    } finally {
      await rm(taskRoot, { recursive: true, force: true })
    }
  })

  it('refuses a weaker model route during the Pro/max evaluation phase', async () => {
    const adapter = new DeepSeekHarnessAdapter({ runtimeFactory: () => new ScriptedRuntime(executionProposal) })
    const weak = request('execution', executionProposal)
    await expect(adapter.run({ ...weak, policy: { ...weak.policy, model: 'deepseek-v4-flash' } }, {
      onSessionStarted: () => {}, onEvent: () => {},
    }, new AbortController().signal)).rejects.toThrow('requires deepseek-v4-pro with reasoning=max')
  })

  it('scrubs ambient service credentials while forwarding only the DeepSeek credential', () => {
    const env = deepSeekChildEnvironment({
      PATH: '/bin', GITHUB_TOKEN: 'github-secret', PACTLINE_TOKEN: 'pactline-secret',
      DEEPSEEK_API_KEY: 'deepseek-secret', DSH_HOME: '/private/harness-home',
      SSH_AUTH_SOCK: '/tmp/agent.sock', GIT_SSH_COMMAND: '/tmp/credential-wrapper',
      GIT_CONFIG_PARAMETERS: 'credential.helper=evil', BUILD_FLAG: '1',
    }, { DSH_CWD: '/tmp/work' })
    expect(env).toMatchObject({
      PATH: '/bin', BUILD_FLAG: '1', DEEPSEEK_API_KEY: 'deepseek-secret', DSH_CWD: '/tmp/work',
      GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null', GIT_TERMINAL_PROMPT: '0',
    })
    expect(env).not.toHaveProperty('GITHUB_TOKEN')
    expect(env).not.toHaveProperty('PACTLINE_TOKEN')
    expect(env).not.toHaveProperty('DSH_HOME')
    expect(env).not.toHaveProperty('SSH_AUTH_SOCK')
    expect(env.GIT_SSH_COMMAND).toBeUndefined()
    expect(env.GIT_CONFIG_PARAMETERS).toBeUndefined()
  })
})
