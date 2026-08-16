import { writeFile } from 'node:fs/promises'
import { describe, expect, it } from 'vitest'
import type { HarnessRunEvent, HarnessRunRequest } from '../../src/core/harness-adapter.js'
import { proposalResultSchema } from '../../src/core/harness-result.js'
import { CodexHarnessAdapter, codexAdapterPolicy, codexChildEnvironment, codexOutputSchema } from '../../src/adapters/codex/codex-adapter.js'
import type { CodexRuntimeLaunch, CodexWireEvent } from '../../src/adapters/codex/wire.js'

const executionProposal = {
  schemaVersion: 1, kind: 'execution', runId: 'run-1', claimId: 'claim-1', taskNumber: 7,
  recommendation: 'complete', summary: 'Implemented.', changedPaths: ['README.md'],
  verification: [{ command: 'true', outcome: 'passed', summary: 'Passed.' }], criteria: [], limitations: [],
}

const reviewProposal = {
  schemaVersion: 1, kind: 'review', runId: 'run-1', claimId: 'claim-1', taskNumber: 7,
  recommendation: 'accept', summary: 'Clean review.', findings: [],
  verification: [{ command: 'true', outcome: 'passed', summary: 'Passed.' }], criteria: [], limitations: [],
}

function request(stage: 'execution' | 'review', proposal: unknown): HarnessRunRequest {
  const resultSchema = proposalResultSchema({
    stage, runId: 'run-1', claimId: 'claim-1', taskNumber: 7,
    criteria: [], verificationCommands: ['true'],
  })
  return {
    runId: 'run-1', claimId: 'claim-1', stage,
    workspace: '/tmp/fleet-codex-test', repositoryRevision: 'a'.repeat(40),
    taskPacket: { task: { number: 7 }, expectedProposal: proposal },
    allowedPaths: ['README.md'], verificationCommands: ['true'], resultSchema,
    sandbox: stage === 'review' ? 'read_only' : 'workspace_write', deadline: '2099-08-15T00:00:00.000Z',
    policy: {
      model: codexAdapterPolicy.model, reasoning: codexAdapterPolicy.reasoning,
      promptVersion: 'fleet-m3-v1', resultContractVersion: 1,
      systemInstructions: 'Do not mutate remote systems.',
      stageInstructions: stage === 'review' ? 'Review the delivery.' : 'Implement the Task.',
    },
  }
}

class ScriptedRuntime {
  readonly processId = 123
  readonly events: CodexWireEvent[]
  terminated: string | undefined
  closed = false
  private waitingReject: ((error: Error) => void) | undefined

  constructor(private readonly launch: CodexRuntimeLaunch, proposal: unknown, private readonly hang = false) {
    this.events = [
      { type: 'thread.started', thread_id: 'codex-session-1' },
      { type: 'item.started', item: { type: 'command_execution', status: 'in_progress' } },
      { type: 'item.completed', item: { type: 'command_execution', status: 'completed' } },
      { type: 'turn.completed', usage: { input_tokens: 10, cached_input_tokens: 3, output_tokens: 5, reasoning_output_tokens: 2 } },
    ]
    this.proposal = proposal
  }

  private readonly proposal: unknown

  nextEvent(): Promise<CodexWireEvent> {
    const item = this.events.shift()
    if (!this.hang && item !== undefined) return Promise.resolve(item)
    return new Promise((_resolve, reject) => { this.waitingReject = reject })
  }

  async waitForSuccessfulExit(): Promise<void> {
    const outputIndex = this.launch.args.indexOf('--output-last-message')
    const outputPath = this.launch.args[outputIndex + 1]
    if (outputIndex < 0 || outputPath === undefined) throw new Error('missing output path')
    await writeFile(outputPath, JSON.stringify(this.proposal))
  }

  close(): Promise<void> { this.closed = true; return Promise.resolve() }
  terminate(reason: string): Promise<void> {
    this.terminated = reason; this.waitingReject?.(new Error(reason)); return Promise.resolve()
  }
}

describe('CodexHarnessAdapter', () => {
  it('translates the common result contract to Codex strict structured output without leaking provider rules into Core', () => {
    const base = request('review', reviewProposal)
    const schema = codexOutputSchema({
      ...base,
      resultSchema: {
        type: 'object', additionalProperties: false,
        required: ['schemaVersion', 'kind'],
        properties: {
          schemaVersion: { const: 1 }, kind: { const: 'review' }, findings: { type: 'array' },
          changedPaths: { type: 'array' }, resolutionRequest: { type: 'object' },
        },
      },
    })
    expect(schema).toMatchObject({
      required: ['schemaVersion', 'kind', 'findings', 'resolutionRequest'],
      properties: {
        schemaVersion: { type: 'integer', const: 1 }, kind: { type: 'string', const: 'review' },
        resolutionRequest: { anyOf: [{ type: 'object' }, { type: 'null' }] },
      },
    })
    expect(recordForTest(schema.properties)).not.toHaveProperty('changedPaths')
  })

  it.each([
    ['workspace-write execution', 'execution', executionProposal],
    ['read-only review', 'review', reviewProposal],
  ] as const)('runs the shared Adapter contract for %s', async (_name, stage, proposal) => {
    let launch: CodexRuntimeLaunch | undefined
    let runtime: ScriptedRuntime | undefined
    const adapter = new CodexHarnessAdapter({
      probeVersion: async () => 'codex-cli 0.147.0',
      runtimeFactory: value => { launch = value; return runtime = new ScriptedRuntime(value, proposal) },
      now: () => new Date('2026-08-15T00:00:00.000Z'),
    })
    const events: HarnessRunEvent[] = []; const sessions: string[] = []
    const result = await adapter.run(request(stage, proposal), {
      onSessionStarted: value => { sessions.push(value.runtimeSessionId) },
      onEvent: event => { events.push(event) },
    }, new AbortController().signal)

    expect(result.proposal).toEqual(proposal)
    expect(result.model).toEqual({ provider: 'openai-codex', model: 'gpt-5.6-sol', reasoning: 'high' })
    expect(result.usage).toEqual({ inputTokens: 10, cachedInputTokens: 3, outputTokens: 5, reasoningTokens: 2 })
    expect(result.eventSummary.toolCalls).toEqual({ shell: 2 })
    expect(sessions).toEqual(['codex-session-1'])
    expect(launch?.args).toContain('--ignore-user-config')
    expect(launch?.args).toContain('--ignore-rules')
    expect(launch?.args).toContain(stage === 'review' ? 'read-only' : 'workspace-write')
    expect(launch?.args.join(' ')).toContain('model_reasoning_effort="high"')
    expect(runtime?.closed).toBe(true)
  })

  it('terminates its process tree when Fleet cancels the Run', async () => {
    let runtime: ScriptedRuntime | undefined
    const adapter = new CodexHarnessAdapter({
      probeVersion: async () => 'codex-cli 0.147.0',
      runtimeFactory: launch => runtime = new ScriptedRuntime(launch, executionProposal, true),
    })
    const controller = new AbortController()
    const run = adapter.run(request('execution', executionProposal), { onSessionStarted: () => {}, onEvent: () => {} }, controller.signal)
    await new Promise(resolve => setImmediate(resolve)); controller.abort(new Error('cancelled by coordinator'))
    await expect(run).rejects.toThrow('cancelled by coordinator')
    expect(runtime?.terminated).toBe('cancelled by coordinator')
    expect(runtime?.closed).toBe(true)
  })

  it('can explicitly relax the native execution sandbox without changing the Fleet capability contract', async () => {
    let launch: CodexRuntimeLaunch | undefined
    const adapter = new CodexHarnessAdapter({
      workspaceSandbox: 'danger-full-access',
      runtimeFactory: value => { launch = value; return new ScriptedRuntime(value, executionProposal) },
    })
    await adapter.run(request('execution', executionProposal), {
      onSessionStarted: () => {}, onEvent: () => {},
    }, new AbortController().signal)
    const sandboxIndex = launch?.args.indexOf('--sandbox') ?? -1
    expect(launch?.args[sandboxIndex + 1]).toBe('danger-full-access')
    expect(launch?.input).toContain('Native Codex sandbox: danger-full-access')
  })

  it('resumes only the explicitly recorded Codex Session', async () => {
    let launch: CodexRuntimeLaunch | undefined
    const adapter = new CodexHarnessAdapter({
      runtimeFactory: value => { launch = value; return new ScriptedRuntime(value, reviewProposal) },
    })
    const sessions: string[] = []
    const result = await adapter.resume?.('codex-session-1', request('review', reviewProposal), {
      onSessionStarted: value => { sessions.push(value.runtimeSessionId) }, onEvent: () => {},
    }, new AbortController().signal)
    expect(result?.runtimeSessionId).toBe('codex-session-1')
    expect(sessions).toEqual(['codex-session-1'])
    expect(launch?.args.slice(-3)).toEqual(['resume', 'codex-session-1', '-'])
  })

  it('rejects an elapsed deadline before starting a runtime', async () => {
    const adapter = new CodexHarnessAdapter({ runtimeFactory: launch => new ScriptedRuntime(launch, executionProposal) })
    const expired = { ...request('execution', executionProposal), deadline: '2000-01-01T00:00:00.000Z' }
    await expect(adapter.run(expired, { onSessionStarted: () => {}, onEvent: () => {} }, new AbortController().signal))
      .rejects.toThrow('deadline is invalid or elapsed')
  })

  it('rejects a weaker or more expensive route during M3', async () => {
    const adapter = new CodexHarnessAdapter({ runtimeFactory: launch => new ScriptedRuntime(launch, executionProposal) })
    const base = request('execution', executionProposal)
    for (const policy of [{ ...base.policy, reasoning: 'max' }, { ...base.policy, model: 'gpt-5.5-codex' }]) {
      await expect(adapter.run({ ...base, policy }, { onSessionStarted: () => {}, onEvent: () => {} }, new AbortController().signal))
        .rejects.toThrow('requires gpt-5.6-sol with reasoning=high')
    }
  })

  it('scrubs control-plane credentials while preserving provider authentication', () => {
    const env = codexChildEnvironment({
      PATH: '/bin', HOME: '/home/test', GITHUB_TOKEN: 'github-secret', PACTLINE_TOKEN: 'pactline-secret',
      OPENAI_API_KEY: 'openai-secret', BUILD_FLAG: '1',
    })
    expect(env).toMatchObject({ PATH: '/bin', HOME: '/home/test', BUILD_FLAG: '1', OPENAI_API_KEY: 'openai-secret' })
    expect(env).not.toHaveProperty('GITHUB_TOKEN')
    expect(env).not.toHaveProperty('PACTLINE_TOKEN')
  })
})

function recordForTest(value: unknown): Record<string, unknown> {
  return value as Record<string, unknown>
}
