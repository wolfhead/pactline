import type { HarnessAdapter, HarnessCapabilities } from '../core/harness-adapter.js'
import { DeepSeekHarnessAdapter, deepSeekAdapterPolicy } from '../adapters/deepseek/deepseek-adapter.js'

export interface DeepSeekDoctorOptions {
  readonly runtimeCommand?: string
  readonly runtimeConfig?: string
  readonly adapter?: Pick<HarnessAdapter, 'id' | 'version' | 'probe'>
}

export interface DeepSeekDoctorResult {
  readonly status: 'ok'
  readonly adapter: { readonly id: string; readonly version: string }
  readonly route: typeof deepSeekAdapterPolicy
  readonly capabilities: HarnessCapabilities
  readonly liveModelCall: false
}

/** Finite, keyless validation of the installed DeepSeek Adapter runtime closure. */
export async function runDeepSeekDoctor(options: DeepSeekDoctorOptions = {}): Promise<DeepSeekDoctorResult> {
  const adapter = options.adapter ?? new DeepSeekHarnessAdapter({
    ...(options.runtimeCommand === undefined ? {} : { runtimeCommand: options.runtimeCommand }),
    ...(options.runtimeConfig === undefined ? {} : { runtimeConfig: options.runtimeConfig }),
  })
  const capabilities = await adapter.probe({
    requiredStages: ['execution', 'review', 'correction', 'resolution_analysis'],
    requiredSandbox: 'workspace_write',
    requireNativeTools: true,
    requireStructuredResult: true,
    requireEventStream: true,
    requireCancellation: true,
    requireSessionResume: false,
  })
  return {
    status: 'ok',
    adapter: { id: adapter.id, version: adapter.version },
    route: deepSeekAdapterPolicy,
    capabilities,
    liveModelCall: false,
  }
}
