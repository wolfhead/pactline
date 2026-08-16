import type { HarnessRunResult } from './harness-result.js'

export type HarnessStage = 'execution' | 'review' | 'correction' | 'resolution_analysis'
export type HarnessSandbox = 'read_only' | 'workspace_write'

export interface HarnessProbeRequest {
  readonly requiredStages: readonly HarnessStage[]
  readonly requiredSandbox: HarnessSandbox
  readonly requireNativeTools: boolean
  readonly requireStructuredResult: boolean
  readonly requireEventStream: boolean
  readonly requireCancellation: boolean
  readonly requireSessionResume: boolean
}

export interface HarnessCapabilities {
  readonly nativeTools: boolean
  readonly structuredResult: boolean
  readonly eventStream: boolean
  readonly cancellation: boolean
  readonly sessionResume: boolean
  readonly sandboxModes: readonly HarnessSandbox[]
  readonly supportedStages: readonly HarnessStage[]
}

export interface HarnessRunPolicy {
  readonly model: string
  readonly reasoning?: string
  readonly promptVersion: string
  readonly systemInstructions: string
  readonly stageInstructions: string
  readonly resultContractVersion: number
}

export interface HarnessRunRequest {
  readonly runId: string
  readonly claimId: string
  readonly stage: HarnessStage
  readonly workspace: string
  readonly repositoryRevision: string
  readonly taskPacket: Readonly<Record<string, unknown>>
  readonly allowedPaths: readonly string[]
  readonly verificationCommands: readonly string[]
  readonly resultSchema: Readonly<Record<string, unknown>>
  readonly sandbox: HarnessSandbox
  readonly deadline: string
  readonly policy: HarnessRunPolicy
}

export interface HarnessRunEvent {
  readonly at: string
  readonly type: string
  readonly outcome?: string
  readonly tool?: string
}

export interface HarnessSessionReference {
  readonly runtimeSessionId: string
}

export interface HarnessRunObserver {
  onSessionStarted(reference: HarnessSessionReference): Promise<void> | void
  onEvent(event: HarnessRunEvent): Promise<void> | void
}

/** One Agent Harness implementation behind the Fleet-owned workflow boundary. */
export interface HarnessAdapter {
  readonly id: string
  readonly version: string

  probe(request: HarnessProbeRequest): Promise<HarnessCapabilities>
  run(request: HarnessRunRequest, observer: HarnessRunObserver, signal: AbortSignal): Promise<HarnessRunResult>
  resume?(
    runtimeSessionId: string,
    request: HarnessRunRequest,
    observer: HarnessRunObserver,
    signal: AbortSignal,
  ): Promise<HarnessRunResult>
  cancel?(runtimeSessionId: string, reason: string): Promise<void>
}
