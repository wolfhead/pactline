import { describe, expect, it } from 'vitest'
import type { HarnessCapabilities } from '../../src/core/harness-adapter.js'
import type { HarnessRunResult } from '../../src/core/harness-result.js'
import { RuntimeAdmissionError, StaticRuntimeRouter } from '../../src/core/runtime-router.js'
import type { RuntimeRoutes } from '../../src/core/runtime-router.js'
import { FakeHarnessAdapter } from '../contract/fake-adapter.js'

const fullCapabilities: HarnessCapabilities = {
  nativeTools: true,
  structuredResult: true,
  eventStream: true,
  cancellation: true,
  sessionResume: false,
  sandboxModes: ['read_only', 'workspace_write'],
  supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'],
}

const result: HarnessRunResult = {
  adapterId: 'fake', adapterVersion: '1', runtimeSessionId: 'session-1',
  model: { provider: 'fake', model: 'deterministic' }, terminalState: 'completed',
  proposal: {
    schemaVersion: 1, kind: 'execution', runId: 'run-1', claimId: 'claim-1', taskNumber: 1,
    recommendation: 'complete', summary: 'done', changedPaths: ['README.md'], verification: [], criteria: [], limitations: [],
  },
  usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
}

function routes(adapterId = 'fake'): RuntimeRoutes {
  const route = { adapterId, model: 'quality-max', reasoning: 'max', promptVersion: 'm1', resultContractVersion: 1 }
  return { execution: route, review: route, correction: route, resolution_analysis: route }
}

describe('StaticRuntimeRouter', () => {
  it('admits the exact configured Adapter and stage capabilities', async () => {
    const adapter = new FakeHarnessAdapter(fullCapabilities, result)
    const admitted = await new StaticRuntimeRouter([adapter], routes()).admit('execution')
    expect(admitted.adapter).toBe(adapter)
    expect(admitted.probe).toEqual({
      requiredStages: ['execution'], requiredSandbox: 'workspace_write',
      requireNativeTools: true, requireStructuredResult: true, requireEventStream: true,
      requireCancellation: true, requireSessionResume: false,
    })
  })

  it('rejects missing capabilities before the caller can create a Claim', async () => {
    const probeOnly = new FakeHarnessAdapter({ ...fullCapabilities, sandboxModes: ['read_only'] }, result)
    await expect(new StaticRuntimeRouter([probeOnly], routes()).admit('execution')).rejects.toMatchObject({
      code: 'CAPABILITY_MISSING', adapterId: 'fake', capability: 'sandbox:workspace_write',
    })
  })

  it('never falls back to another Adapter when the configured provider fails', async () => {
    const fallback = new FakeHarnessAdapter(fullCapabilities, result)
    let probes = 0
    const failing = {
      id: 'selected',
      version: '1',
      probe: async () => {
        probes += 1
        throw new Error('provider unavailable')
      },
      run: fallback.run.bind(fallback),
    }
    const router = new StaticRuntimeRouter([failing, fallback], routes('selected'))
    await expect(router.admit('review')).rejects.toThrow('provider unavailable')
    expect(probes).toBe(1)
    expect(fallback.requests).toEqual([])
  })

  it('rejects an unavailable configured Adapter instead of selecting an installed one', async () => {
    const fallback = new FakeHarnessAdapter(fullCapabilities, result)
    await expect(new StaticRuntimeRouter([fallback], routes('missing')).admit('review')).rejects.toBeInstanceOf(RuntimeAdmissionError)
  })
})
