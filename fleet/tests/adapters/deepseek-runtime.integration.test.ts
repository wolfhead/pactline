import { existsSync } from 'node:fs'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import type { HarnessRunRequest } from '../../src/core/harness-adapter.js'
import { proposalResultSchema } from '../../src/core/harness-result.js'
import { DeepSeekHarnessAdapter, deepSeekAdapterPolicy } from '../../src/adapters/deepseek/deepseek-adapter.js'

const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const runtimeRoot = join(fleetRoot, 'runtime/deepseek')
const runtimeBin = join(runtimeRoot, 'node_modules/.bin/dsh-jsonrpc-agent')
const replayConfig = join(runtimeRoot, 'cordis.replay.yml')

function request(stage: 'execution' | 'review', workspace: string): HarnessRunRequest {
  return {
    runId: 'run-1', claimId: 'claim-1', stage, workspace,
    repositoryRevision: 'a'.repeat(40), taskPacket: { task: { number: 7 } },
    allowedPaths: stage === 'execution' ? ['adapter-proof.txt'] : ['README.md'],
    verificationCommands: ['true'],
    resultSchema: proposalResultSchema({
      stage, runId: 'run-1', claimId: 'claim-1', taskNumber: 7,
      criteria: [], verificationCommands: ['true'],
    }),
    sandbox: stage === 'execution' ? 'workspace_write' : 'read_only',
    deadline: new Date(Date.now() + 30_000).toISOString(),
    policy: {
      model: deepSeekAdapterPolicy.model, reasoning: deepSeekAdapterPolicy.reasoning,
      promptVersion: 'fleet-m2-v1', resultContractVersion: 1,
      systemInstructions: 'Do not mutate remote systems.',
      stageInstructions: stage === 'execution' ? 'Implement the Task.' : 'Review the delivery.',
    },
  }
}

describe.runIf(existsSync(runtimeBin))('installed DeepSeek Harness runtime closure', () => {
  it.each([
    ['execution', 'execution-session.jsonl', 'complete'],
    ['review', 'review-session.jsonl', 'accept'],
  ] as const)('runs a real keyless DSH %s Session with native tools', async (stage, fixture, recommendation) => {
    const workspace = await mkdtemp(join(tmpdir(), `pactline-fleet-dsh-${stage}-`))
    await writeFile(join(workspace, 'README.md'), 'Frozen review fixture.\n')
    const adapter = new DeepSeekHarnessAdapter({
      runtimeConfig: replayConfig,
      environment: process.env,
      runtimeEnvironment: {
        DSH_SNAPSHOT_FILE: join(runtimeRoot, 'fixtures', fixture),
      },
      requestTimeoutMs: 10_000,
      terminateTimeoutMs: 2_000,
    })
    const eventTypes: string[] = []
    try {
      await expect(adapter.probe({
        requiredStages: [stage], requiredSandbox: stage === 'execution' ? 'workspace_write' : 'read_only',
        requireNativeTools: true, requireStructuredResult: true, requireEventStream: true,
        requireCancellation: true, requireSessionResume: false,
      })).resolves.toMatchObject({ nativeTools: true, structuredResult: true, eventStream: true })
      const result = await adapter.run(request(stage, workspace), {
        onSessionStarted: () => {},
        onEvent: event => { eventTypes.push(event.type) },
      }, new AbortController().signal)
      expect(result.terminalState).toBe('completed')
      expect(result.proposal.kind).toBe(stage)
      expect(result.proposal.recommendation).toBe(recommendation)
      expect(result.eventSummary.toolCalls).toHaveProperty(stage === 'execution' ? 'write' : 'read')
      expect(result.eventSummary.toolCalls).toHaveProperty('submit_fleet_result')
      expect(eventTypes).toContain('deepseek.turn.end')
      if (stage === 'execution') {
        await expect(readFile(join(workspace, 'adapter-proof.txt'), 'utf8'))
          .resolves.toBe('DeepSeek Adapter workspace-write proof.\n')
      } else {
        await expect(readFile(join(workspace, 'README.md'), 'utf8')).resolves.toBe('Frozen review fixture.\n')
        expect(existsSync(join(workspace, 'adapter-proof.txt'))).toBe(false)
      }
    } finally {
      await rm(workspace, { recursive: true, force: true })
    }
  }, 30_000)

  it.each([
    ['review', 'changes-request-session.jsonl', 'request_changes'],
    ['execution', 'typed-resolution-session.jsonl', 'request_resolution'],
  ] as const)('preserves frozen %s lifecycle proposal semantics: %s', async (stage, fixture, recommendation) => {
    const workspace = await mkdtemp(join(tmpdir(), `pactline-fleet-dsh-parity-${stage}-`))
    await writeFile(join(workspace, 'README.md'), 'Seeded parity fixture.\n')
    const adapter = new DeepSeekHarnessAdapter({
      runtimeConfig: replayConfig,
      environment: process.env,
      runtimeEnvironment: { DSH_SNAPSHOT_FILE: join(runtimeRoot, 'fixtures', fixture) },
      requestTimeoutMs: 10_000,
      terminateTimeoutMs: 2_000,
    })
    try {
      const result = await adapter.run(request(stage, workspace), {
        onSessionStarted: () => {}, onEvent: () => {},
      }, new AbortController().signal)
      expect(result.proposal.recommendation).toBe(recommendation)
      if (recommendation === 'request_resolution') {
        expect(existsSync(join(workspace, 'adapter-proof.txt'))).toBe(false)
        expect(result.proposal).toMatchObject({
          resolutionRequest: { issueType: 'decision_required' },
        })
      }
    } finally {
      await rm(workspace, { recursive: true, force: true })
    }
  }, 30_000)

  it.each([
    ['cancelled by integration test', false],
    ['DeepSeek Harness Run deadline elapsed', true],
  ] as const)('reaps a real hanging DSH runtime after %s', async (expected, useDeadline) => {
    const workspace = await mkdtemp(join(tmpdir(), 'pactline-fleet-dsh-interrupt-'))
    await writeFile(join(workspace, 'README.md'), 'Cancellation fixture.\n')
    const adapter = new DeepSeekHarnessAdapter({
      runtimeConfig: replayConfig,
      environment: process.env,
      runtimeEnvironment: {
        DSH_SNAPSHOT_FILE: join(runtimeRoot, 'fixtures/hang-session.jsonl'),
        DSH_SNAPSHOT_OVERRIDE: join(runtimeRoot, 'fixtures/hang.override.json'),
      },
      requestTimeoutMs: 10_000,
      terminateTimeoutMs: 2_000,
    })
    const controller = new AbortController()
    const interruptedRequest = request('review', workspace)
    const run = adapter.run({
      ...interruptedRequest,
      deadline: new Date(Date.now() + (useDeadline ? 250 : 10_000)).toISOString(),
    }, {
      onSessionStarted: () => {},
      onEvent: event => {
        if (!useDeadline && event.type === 'deepseek.session.running') {
          controller.abort(new Error(expected))
        }
      },
    }, controller.signal)
    try {
      await expect(run).rejects.toThrow(expected)
    } finally {
      await rm(workspace, { recursive: true, force: true })
    }
  }, 30_000)
})
