import { spawn } from 'node:child_process'

const MAX_OUTPUT_BYTES = 1_048_576
const DEFAULT_TIMEOUT_MS = 15_000

interface PactlineCapabilitiesEnvelope {
  readonly ok: true
  readonly data: {
    readonly cli_version: string
    readonly protocol: number
    readonly features: readonly string[]
  }
}

export interface FleetDoctorResult {
  readonly status: 'ok'
  readonly application: '@pactline/fleet'
  readonly node: {
    readonly version: string
    readonly supported: true
  }
  readonly pactline: {
    readonly executable: string
    readonly cliVersion: string
    readonly protocol: 2
    readonly featureCount: number
  }
  readonly adapters: {
    readonly configured: 0
    readonly note: 'No Harness is selected by this check; run deepseek-doctor for the optional DeepSeek runtime'
  }
}

export interface FleetDoctorOptions {
  readonly pactlineExecutable: string
  readonly nodeVersion?: string
  readonly timeoutMs?: number
  readonly runCapabilities?: (executable: string, timeoutMs: number) => Promise<unknown>
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function sanitizeDiagnostic(value: string): string {
  return value
    .replace(/\bBearer\s+\S+/gi, 'Bearer [REDACTED]')
    .replace(/\bsk-[A-Za-z0-9_-]{8,}\b/g, '[REDACTED]')
    .replace(/\b(token|api[_-]?key|secret|password)(\s*[=:]\s*)\S+/gi, '$1$2[REDACTED]')
}

export function isSupportedNodeVersion(version: string): boolean {
  const match = /^(\d+)\.(\d+)\.(\d+)/.exec(version)
  if (match === null) return false
  const major = Number(match[1]); const minor = Number(match[2])
  return (major === 22 && minor >= 19) || major >= 24
}

function parseCapabilities(value: unknown): PactlineCapabilitiesEnvelope {
  const envelope = record(value, 'Pactline capabilities response')
  if (envelope.ok !== true) throw new Error('Pactline capabilities response is not successful')
  const data = record(envelope.data, 'Pactline capabilities data')
  if (typeof data.cli_version !== 'string' || data.cli_version.trim() === '') throw new Error('Pactline CLI version is missing')
  if (data.protocol !== 2) throw new Error(`Pactline CLI protocol ${String(data.protocol)} is unsupported; expected 2`)
  if (!Array.isArray(data.features) || data.features.some(feature => typeof feature !== 'string')) {
    throw new Error('Pactline CLI features are invalid')
  }
  return {
    ok: true,
    data: { cli_version: data.cli_version, protocol: 2, features: data.features as string[] },
  }
}

async function defaultRunCapabilities(executable: string, timeoutMs: number): Promise<unknown> {
  return await new Promise<unknown>((resolve, reject) => {
    const child = spawn(executable, ['capabilities', '--json'], {
      stdio: ['ignore', 'pipe', 'pipe'], shell: false, env: process.env,
    })
    let stdout = ''; let stderr = ''; let outputBytes = 0; let settled = false; let timer: NodeJS.Timeout | undefined
    const finish = (error?: Error): void => {
      if (settled) return
      settled = true
      if (timer !== undefined) clearTimeout(timer)
      if (error !== undefined) reject(error)
      else {
        try { resolve(JSON.parse(stdout) as unknown) } catch { reject(new Error('Pactline capabilities output is not valid JSON')) }
      }
    }
    const collect = (source: 'stdout' | 'stderr', chunk: Buffer): void => {
      outputBytes += chunk.length
      if (outputBytes > MAX_OUTPUT_BYTES) {
        child.kill('SIGKILL'); finish(new Error('Pactline capabilities output exceeded the size limit')); return
      }
      if (source === 'stdout') stdout += chunk.toString('utf8')
      else stderr += chunk.toString('utf8')
    }
    child.stdout.on('data', (chunk: Buffer) => { collect('stdout', chunk) })
    child.stderr.on('data', (chunk: Buffer) => { collect('stderr', chunk) })
    child.on('error', (error: Error) => { finish(new Error(`Pactline capabilities failed to start: ${error.message}`)) })
    child.on('close', code => {
      if (code !== 0) finish(new Error(`Pactline capabilities exited with code ${String(code)}: ${sanitizeDiagnostic(stderr.trim().slice(-4096))}`))
      else finish()
    })
    timer = setTimeout(() => {
      child.kill('SIGKILL'); finish(new Error(`Pactline capabilities exceeded ${String(timeoutMs)}ms`))
    }, timeoutMs)
  })
}

export async function runFleetDoctor(options: FleetDoctorOptions): Promise<FleetDoctorResult> {
  const nodeVersion = options.nodeVersion ?? process.versions.node
  if (!isSupportedNodeVersion(nodeVersion)) throw new Error(`Node.js ${nodeVersion} is unsupported; expected ^22.19.0 or >=24`)
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1) throw new Error('Doctor timeout must be a positive integer')
  if (options.pactlineExecutable.trim() === '') throw new Error('Pactline executable is required')
  const capabilities = parseCapabilities(await (options.runCapabilities ?? defaultRunCapabilities)(options.pactlineExecutable, timeoutMs))
  return {
    status: 'ok', application: '@pactline/fleet',
    node: { version: nodeVersion, supported: true },
    pactline: {
      executable: options.pactlineExecutable,
      cliVersion: capabilities.data.cli_version,
      protocol: 2,
      featureCount: capabilities.data.features.length,
    },
    adapters: { configured: 0, note: 'No Harness is selected by this check; run deepseek-doctor for the optional DeepSeek runtime' },
  }
}
