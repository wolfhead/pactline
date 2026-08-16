import type {
  HarnessAdapter,
  HarnessCapabilities,
  HarnessProbeRequest,
  HarnessRunEvent,
  HarnessRunObserver,
  HarnessRunRequest,
} from '../../core/harness-adapter.js'
import type { HarnessRunResult } from '../../core/harness-result.js'

export interface ReplayStep {
  readonly sessionId: string
  readonly events?: readonly HarnessRunEvent[]
  readonly effect?: (request: HarnessRunRequest) => Promise<void> | void
  readonly result: (request: HarnessRunRequest, sessionId: string) => HarnessRunResult
}

/** Deterministic finite Adapter used to prove Core workflows without a real Harness. */
export class ReplayHarnessAdapter implements HarnessAdapter {
  readonly id = 'replay'
  readonly version = '1.0.0'
  readonly requests: HarnessRunRequest[] = []
  private cursor = 0

  constructor(
    private readonly steps: readonly ReplayStep[],
    private readonly capabilities: HarnessCapabilities = {
      nativeTools: true,
      structuredResult: true,
      eventStream: true,
      cancellation: true,
      sessionResume: false,
      sandboxModes: ['read_only', 'workspace_write'],
      supportedStages: ['execution', 'review', 'correction', 'resolution_analysis'],
    },
  ) {}

  probe(_request: HarnessProbeRequest): Promise<HarnessCapabilities> {
    return Promise.resolve(this.capabilities)
  }

  async run(request: HarnessRunRequest, observer: HarnessRunObserver, signal: AbortSignal): Promise<HarnessRunResult> {
    if (signal.aborted) throw signal.reason
    const step = this.steps[this.cursor]
    if (step === undefined) throw new Error('Replay Adapter has no remaining step')
    this.cursor += 1
    this.requests.push(request)
    await observer.onSessionStarted({ runtimeSessionId: step.sessionId })
    await step.effect?.(request)
    for (const event of step.events ?? []) await observer.onEvent(event)
    return step.result(request, step.sessionId)
  }
}
