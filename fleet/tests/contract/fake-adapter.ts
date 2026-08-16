import type {
  HarnessAdapter, HarnessCapabilities, HarnessProbeRequest, HarnessRunObserver, HarnessRunRequest,
} from '../../src/core/harness-adapter.js'
import type { HarnessRunResult } from '../../src/core/harness-result.js'

export class FakeHarnessAdapter implements HarnessAdapter {
  readonly id = 'fake'
  readonly version = '1.0.0-test'
  readonly requests: HarnessRunRequest[] = []

  constructor(
    private readonly capabilities: HarnessCapabilities,
    private readonly result: HarnessRunResult,
  ) {}

  probe(_request: HarnessProbeRequest): Promise<HarnessCapabilities> {
    return Promise.resolve(this.capabilities)
  }

  async run(request: HarnessRunRequest, observer: HarnessRunObserver, signal: AbortSignal): Promise<HarnessRunResult> {
    if (signal.aborted) throw signal.reason
    this.requests.push(request)
    await observer.onSessionStarted({ runtimeSessionId: this.result.runtimeSessionId })
    await observer.onEvent({ at: '2026-08-15T00:00:00.000Z', type: 'fake.completed', outcome: 'ok' })
    return this.result
  }
}
