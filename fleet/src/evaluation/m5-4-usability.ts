import { lstat, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { defaultL2V2CommandRunner, type L2V2CommandRunner } from './l2-v2-preflight.js'

export interface M54UsabilityPreflightOptions {
  readonly server: string
  readonly pactlineExecutable: string
  readonly fleetBinPath: string
  readonly repository: string
  readonly evidenceRoot: string
  readonly runId: string
  readonly run?: L2V2CommandRunner
  readonly fetchURL?: typeof fetch
}

export interface M54UsabilityPreflightResult {
  readonly ready: true
  readonly server: string
  readonly pactline: {
    readonly cliVersion: string
    readonly protocol: 2
    readonly featureCount: number
  }
  readonly fleet: { readonly version: string }
  readonly repository: {
    readonly path: string
    readonly baseRevision: string
    readonly workingTreeDirty: boolean
  }
  readonly evidence: { readonly path: string; readonly available: true }
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} response is invalid`)
  return value as Record<string, unknown>
}

function successData(stdout: string, name: string): Record<string, unknown> {
  let parsed: unknown
  try { parsed = JSON.parse(stdout) as unknown } catch { throw new Error(`${name} did not return JSON`) }
  const envelope = record(parsed, name)
  if (envelope.ok !== true) throw new Error(`${name} did not return a successful response`)
  return record(envelope.data, `${name} data`)
}

async function pathExists(path: string): Promise<boolean> {
  try { await lstat(path); return true } catch (error: unknown) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return false
    throw error
  }
}

/** Verify public local boundaries before M5.4 creates any acceptance resource. */
export async function preflightM54Usability(options: M54UsabilityPreflightOptions): Promise<M54UsabilityPreflightResult> {
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(options.runId)) throw new Error('M5.4 run ID is invalid')
  const run = options.run ?? defaultL2V2CommandRunner
  const fetchURL = options.fetchURL ?? fetch
  const server = options.server.replace(/\/$/, '')

  const ready = await fetchURL(new URL('/readyz', server), { signal: AbortSignal.timeout(10_000) })
  if (!ready.ok) throw new Error(`Local Pactline is not ready at ${server} (HTTP ${String(ready.status)})`)

  const pactline = successData((await run(options.pactlineExecutable, ['capabilities', '--json'])).stdout, 'Pactline capabilities')
  if (pactline.protocol !== 2 || typeof pactline.cli_version !== 'string' || !Array.isArray(pactline.features)
    || !pactline.features.every(value => typeof value === 'string')) {
    throw new Error('M5.4 requires Pactline CLI protocol 2')
  }

  const fleet = successData((await run(process.execPath, [options.fleetBinPath, 'version', '--json'])).stdout, 'Fleet version')
  if (typeof fleet.version !== 'string' || fleet.version.trim() === '') throw new Error('Fleet version is invalid')

  const repositoryStat = await stat(options.repository)
  if (!repositoryStat.isDirectory()) throw new Error('M5.4 repository must be a directory')
  const baseRevision = (await run('git', ['-C', options.repository, 'rev-parse', 'HEAD'])).stdout.trim()
  if (!/^[a-f0-9]{40}$/.test(baseRevision)) throw new Error('M5.4 repository HEAD is not an exact commit')
  const workingTreeDirty = (await run('git', ['-C', options.repository, 'status', '--porcelain=v1'])).stdout.trim() !== ''

  const evidenceRootStat = await stat(options.evidenceRoot)
  if (!evidenceRootStat.isDirectory()) throw new Error('M5.4 evidence root must be a directory')
  const evidencePath = join(options.evidenceRoot, options.runId)
  if (await pathExists(evidencePath)) throw new Error(`M5.4 evidence namespace is already occupied: ${options.runId}`)

  return {
    ready: true,
    server,
    pactline: {
      cliVersion: pactline.cli_version,
      protocol: 2,
      featureCount: pactline.features.length,
    },
    fleet: { version: fleet.version },
    repository: { path: options.repository, baseRevision, workingTreeDirty },
    evidence: { path: evidencePath, available: true },
  }
}
