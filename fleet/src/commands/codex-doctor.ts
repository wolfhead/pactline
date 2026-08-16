import type { HarnessAdapter, HarnessCapabilities } from '../core/harness-adapter.js'
import { CodexHarnessAdapter, codexAdapterPolicy } from '../adapters/codex/codex-adapter.js'

export interface CodexDoctorOptions {
  readonly runtimeCommand?: string
  readonly adapter?: Pick<HarnessAdapter, 'id' | 'version' | 'probe'>
}

export interface CodexDoctorResult {
  readonly status: 'ok'
  readonly adapter: { readonly id: string; readonly version: string }
  readonly route: typeof codexAdapterPolicy
  readonly capabilities: HarnessCapabilities
  readonly liveModelCall: false
}

/** Finite, keyless validation of the pinned Codex runtime and Adapter contract. */
export async function runCodexDoctor(options: CodexDoctorOptions = {}): Promise<CodexDoctorResult> {
  const adapter = options.adapter ?? new CodexHarnessAdapter({
    ...(options.runtimeCommand === undefined ? {} : { runtimeCommand: options.runtimeCommand }),
  })
  const capabilities = await adapter.probe({
    requiredStages: ['execution', 'review', 'correction', 'resolution_analysis'],
    requiredSandbox: 'workspace_write', requireNativeTools: true, requireStructuredResult: true,
    requireEventStream: true, requireCancellation: true, requireSessionResume: true,
  })
  return {
    status: 'ok', adapter: { id: adapter.id, version: adapter.version },
    route: codexAdapterPolicy, capabilities, liveModelCall: false,
  }
}
