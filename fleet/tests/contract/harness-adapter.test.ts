import { describe, expect, it } from 'vitest'
import type { HarnessCapabilities, HarnessRunEvent, HarnessRunRequest } from '../../src/core/harness-adapter.js'
import type { HarnessRunResult } from '../../src/core/harness-result.js'
import { FakeHarnessAdapter } from './fake-adapter.js'

const capabilities: HarnessCapabilities = {
  nativeTools: true,
  structuredResult: true,
  eventStream: true,
  cancellation: true,
  sessionResume: false,
  sandboxModes: ['read_only', 'workspace_write'],
  supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'],
}

const result: HarnessRunResult = {
  adapterId: 'fake',
  adapterVersion: '1.0.0-test',
  runtimeSessionId: 'fake-session-1',
  model: { provider: 'fake', model: 'deterministic' },
  terminalState: 'completed',
  proposal: {
    schemaVersion: 1, kind: 'execution', runId: 'run-1', claimId: '00000000-0000-0000-0000-000000000001', taskNumber: 1,
    recommendation: 'complete',
    summary: 'Deterministic fake result.',
    changedPaths: ['README.md'], verification: [], criteria: [],
    limitations: [],
  },
  usage: {},
  eventSummary: { total: 1, byType: { completed: 1 }, toolCalls: {}, toolErrors: {} },
}

const request: HarnessRunRequest = {
  runId: 'run-1',
  claimId: '00000000-0000-0000-0000-000000000001',
  stage: 'execution',
  workspace: '/tmp/fleet-test-workspace',
  repositoryRevision: 'abc3599c863fbc2041e0cd463776d3d8ca8c7fb1',
  taskPacket: { task: { number: 1 } },
  allowedPaths: ['README.md'],
  verificationCommands: ['true'],
  resultSchema: { type: 'object' },
  sandbox: 'workspace_write',
  deadline: '2026-08-15T01:00:00.000Z',
  policy: {
    model: 'deterministic', promptVersion: '1', resultContractVersion: 1,
    systemInstructions: 'Return one bounded result.', stageInstructions: 'Implement the Task.',
  },
}

describe('HarnessAdapter contract scaffold', () => {
  it('probes and runs through only runtime-neutral inputs and outputs', async () => {
    const adapter = new FakeHarnessAdapter(capabilities, result)
    const events: HarnessRunEvent[] = []
    const sessions: string[] = []
    const controller = new AbortController()

    await expect(adapter.probe({
      requiredStages: ['execution'], requiredSandbox: 'workspace_write',
      requireNativeTools: true, requireStructuredResult: true, requireEventStream: true,
      requireCancellation: true, requireSessionResume: false,
    })).resolves.toEqual(capabilities)
    await expect(adapter.run(request, {
      onSessionStarted: reference => { sessions.push(reference.runtimeSessionId) },
      onEvent: event => { events.push(event) },
    }, controller.signal)).resolves.toEqual(result)

    expect(adapter.requests).toEqual([request])
    expect(sessions).toEqual(['fake-session-1'])
    expect(events).toEqual([{ at: '2026-08-15T00:00:00.000Z', type: 'fake.completed', outcome: 'ok' }])
  })

  it('honors cancellation before a Harness Run starts', async () => {
    const adapter = new FakeHarnessAdapter(capabilities, result)
    const controller = new AbortController(); controller.abort(new Error('cancelled by test'))

    await expect(adapter.run(request, {
      onSessionStarted: () => { throw new Error('cancelled Run must not create a Session') },
      onEvent: () => { throw new Error('cancelled Run must not emit events') },
    }, controller.signal)).rejects.toThrow('cancelled by test')
    expect(adapter.requests).toEqual([])
  })
})
