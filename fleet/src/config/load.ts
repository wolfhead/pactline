import { createHash } from 'node:crypto'
import { isAbsolute, relative, resolve } from 'node:path'
import { readFile } from 'node:fs/promises'
import { parse } from 'yaml'
import type {
  FleetConfigLoadOptions,
  FleetConfigSnapshot,
  FleetDefinitionConfig,
  FleetRouteConfig,
  FleetRoutingConfig,
  FleetServiceConfig,
} from './types.js'

const DEFAULT_POLL_INTERVAL_MS = 10_000
const DEFAULT_SHUTDOWN_DEADLINE_MS = 30_000
const DEFAULT_HTTP_PORT = 7_331
const DEFAULT_PROMPT_VERSION = 'v1'
const DEFAULT_RESULT_CONTRACT_VERSION = 1
const FLEET_ID = /^[a-z0-9][a-z0-9._-]{0,63}$/
const ENVIRONMENT_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/
const CREDENTIAL_REFERENCE = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/

function record(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${path} must be an object`)
  }
  return value as Record<string, unknown>
}

function knownKeys(value: Record<string, unknown>, allowed: readonly string[], path: string): void {
  const unexpected = Object.keys(value).filter(key => !allowed.includes(key))
  if (unexpected.length > 0) throw new Error(`${path} contains unknown field: ${unexpected.join(', ')}`)
}

function nonEmpty(value: unknown, path: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`${path} must be a non-empty string`)
  return value.trim()
}

function positiveInteger(value: unknown, path: string, fallback?: number): number {
  const candidate = value ?? fallback
  if (!Number.isSafeInteger(candidate) || Number(candidate) < 1) {
    throw new Error(`${path} must be a positive integer`)
  }
  return Number(candidate)
}

function booleanValue(value: unknown, path: string, fallback: boolean): boolean {
  const candidate = value ?? fallback
  if (typeof candidate !== 'boolean') throw new Error(`${path} must be a boolean`)
  return candidate
}

function durationMilliseconds(value: unknown, path: string, fallback: number): number {
  if (value === undefined) return fallback
  if (typeof value === 'number') return positiveInteger(value, path)
  if (typeof value !== 'string') throw new Error(`${path} must be a positive duration`)
  const match = /^([1-9][0-9]*)(ms|s|m|h)$/.exec(value.trim())
  if (match === null) throw new Error(`${path} must use ms, s, m, or h units`)
  const amount = Number(match[1])
  const multiplier = { ms: 1, s: 1_000, m: 60_000, h: 3_600_000 }[match[2] as 'ms' | 's' | 'm' | 'h']
  const result = amount * multiplier
  if (!Number.isSafeInteger(result) || result < 1) throw new Error(`${path} is outside the supported range`)
  return result
}

function absolutePath(value: unknown, path: string): string {
  const candidate = nonEmpty(value, path)
  if (!isAbsolute(candidate)) throw new Error(`${path} must be an absolute path`)
  return resolve(candidate)
}

function serverURL(value: unknown): string {
  const candidate = nonEmpty(value, 'service.pactline.server')
  let parsed: URL
  try {
    parsed = new URL(candidate)
  } catch {
    throw new Error('service.pactline.server must be an absolute HTTP URL')
  }
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username !== '' || parsed.password !== '') {
    throw new Error('service.pactline.server must be an absolute HTTP URL without credentials')
  }
  parsed.hash = ''
  parsed.search = ''
  return parsed.toString().replace(/\/$/, '')
}

function route(
  value: unknown,
  path: string,
  knownAdapterIds: ReadonlySet<string> | undefined,
): FleetRouteConfig {
  const input = record(value, path)
  knownKeys(input, ['adapter', 'model', 'reasoning', 'promptVersion', 'resultContractVersion'], path)
  const adapter = nonEmpty(input.adapter, `${path}.adapter`)
  if (knownAdapterIds !== undefined && !knownAdapterIds.has(adapter)) {
    throw new Error(`${path}.adapter is unavailable: ${adapter}`)
  }
  const reasoning = input.reasoning === undefined ? undefined : nonEmpty(input.reasoning, `${path}.reasoning`)
  return {
    adapter,
    model: nonEmpty(input.model, `${path}.model`),
    ...(reasoning === undefined ? {} : { reasoning }),
    promptVersion: input.promptVersion === undefined
      ? DEFAULT_PROMPT_VERSION
      : nonEmpty(input.promptVersion, `${path}.promptVersion`),
    resultContractVersion: positiveInteger(
      input.resultContractVersion,
      `${path}.resultContractVersion`,
      DEFAULT_RESULT_CONTRACT_VERSION,
    ),
  }
}

function routing(
  value: unknown,
  path: string,
  knownAdapterIds: ReadonlySet<string> | undefined,
): FleetRoutingConfig {
  const input = record(value, path)
  knownKeys(input, ['execution', 'review', 'correction', 'resolutionAnalysis'], path)
  for (const required of ['execution', 'review', 'correction', 'resolutionAnalysis']) {
    if (input[required] === undefined) throw new Error(`${path}.${required} is required`)
  }
  return {
    execution: route(input.execution, `${path}.execution`, knownAdapterIds),
    review: route(input.review, `${path}.review`, knownAdapterIds),
    correction: route(input.correction, `${path}.correction`, knownAdapterIds),
    resolution_analysis: route(input.resolutionAnalysis, `${path}.resolutionAnalysis`, knownAdapterIds),
  }
}

function credentials(value: unknown, path: string): FleetDefinitionConfig['credentials'] {
  if (value === undefined) return {}
  const input = record(value, path)
  knownKeys(input, ['git'], path)
  if (input.git === undefined) return {}
  const git = nonEmpty(input.git, `${path}.git`)
  if (!CREDENTIAL_REFERENCE.test(git)) throw new Error(`${path}.git is not a valid credential reference`)
  return { git }
}

function workPlugin(value: unknown, path: string): FleetDefinitionConfig['workPlugin'] {
  if (value === undefined) return undefined
  const input = record(value, path)
  knownKeys(input, ['executable', 'args', 'timeout'], path)
  const executable = absolutePath(input.executable, `${path}.executable`)
  const argsValue = input.args ?? []
  if (!Array.isArray(argsValue) || argsValue.length > 32 || argsValue.some(item => typeof item !== 'string')) {
    throw new Error(`${path}.args must be an array of at most 32 strings`)
  }
  return {
    executable,
    args: argsValue as string[],
    timeoutMs: durationMilliseconds(input.timeout, `${path}.timeout`, 120_000),
  }
}

function fleetDefinition(
  fleetId: string,
  value: unknown,
  globalConcurrency: number,
  knownAdapterIds: ReadonlySet<string> | undefined,
): FleetDefinitionConfig {
  if (!FLEET_ID.test(fleetId)) throw new Error(`fleets.${fleetId} is not a valid Fleet ID`)
  const path = `fleets.${fleetId}`
  const input = record(value, path)
  knownKeys(input, ['project', 'enabled', 'maxConcurrentRuns', 'workspaceRoot', 'routing', 'credentials', 'workPlugin'], path)
  const maxConcurrentRuns = positiveInteger(input.maxConcurrentRuns, `${path}.maxConcurrentRuns`, 1)
  if (maxConcurrentRuns > globalConcurrency) {
    throw new Error(`${path}.maxConcurrentRuns must not exceed service.maxConcurrentRuns`)
  }
  const plugin = workPlugin(input.workPlugin, `${path}.workPlugin`)
  return {
    id: fleetId,
    projectNumber: positiveInteger(input.project, `${path}.project`),
    enabled: booleanValue(input.enabled, `${path}.enabled`, true),
    maxConcurrentRuns,
    workspaceRoot: absolutePath(input.workspaceRoot, `${path}.workspaceRoot`),
    routing: routing(input.routing, `${path}.routing`, knownAdapterIds),
    credentials: credentials(input.credentials, `${path}.credentials`),
    ...(plugin === undefined ? {} : { workPlugin: plugin }),
  }
}

function pathsOverlap(first: string, second: string): boolean {
  const firstToSecond = relative(first, second)
  const secondToFirst = relative(second, first)
  return firstToSecond === '' || (!firstToSecond.startsWith('..') && !isAbsolute(firstToSecond))
    || (!secondToFirst.startsWith('..') && !isAbsolute(secondToFirst))
}

function parseConfig(value: unknown, options: FleetConfigLoadOptions): FleetServiceConfig {
  const root = record(value, 'configuration')
  knownKeys(root, ['version', 'service', 'fleets'], 'configuration')
  if (root.version !== 1) throw new Error('configuration.version must be 1')

  const service = record(root.service, 'service')
  knownKeys(service, ['pactline', 'stateDirectory', 'pollInterval', 'maxConcurrentRuns', 'shutdownDeadline', 'http'], 'service')
  const pactline = record(service.pactline, 'service.pactline')
  knownKeys(pactline, ['server', 'tokenEnv', 'executable'], 'service.pactline')
  const tokenEnv = pactline.tokenEnv === undefined ? 'PACTLINE_TOKEN' : nonEmpty(pactline.tokenEnv, 'service.pactline.tokenEnv')
  if (!ENVIRONMENT_NAME.test(tokenEnv)) throw new Error('service.pactline.tokenEnv must be an environment variable name')

  const http = service.http === undefined ? {} : record(service.http, 'service.http')
  knownKeys(http, ['address', 'port'], 'service.http')
  const address = http.address === undefined ? '127.0.0.1' : nonEmpty(http.address, 'service.http.address')
  if (!['127.0.0.1', '::1', 'localhost'].includes(address)) {
    throw new Error('service.http.address must be a loopback address')
  }
  const port = positiveInteger(http.port, 'service.http.port', DEFAULT_HTTP_PORT)
  if (port > 65_535) throw new Error('service.http.port must not exceed 65535')
  const maxConcurrentRuns = positiveInteger(service.maxConcurrentRuns, 'service.maxConcurrentRuns', 1)
  const knownAdapterIds = options.knownAdapterIds === undefined
    ? undefined
    : new Set(options.knownAdapterIds)
  const fleetInput = root.fleets === undefined ? {} : record(root.fleets, 'fleets')
  const fleets: Record<string, FleetDefinitionConfig> = {}
  for (const [fleetId, definition] of Object.entries(fleetInput)) {
    fleets[fleetId] = fleetDefinition(fleetId, definition, maxConcurrentRuns, knownAdapterIds)
  }

  const enabledProjects = new Map<number, string>()
  for (const fleet of Object.values(fleets)) {
    if (!fleet.enabled) continue
    const existing = enabledProjects.get(fleet.projectNumber)
    if (existing !== undefined) {
      throw new Error(`Project ${String(fleet.projectNumber)} is enabled by both ${existing} and ${fleet.id}`)
    }
    enabledProjects.set(fleet.projectNumber, fleet.id)
  }
  const definitions = Object.values(fleets)
  for (let index = 0; index < definitions.length; index += 1) {
    const first = definitions[index]!
    for (let otherIndex = index + 1; otherIndex < definitions.length; otherIndex += 1) {
      const second = definitions[otherIndex]!
      if (pathsOverlap(first.workspaceRoot, second.workspaceRoot)) {
        throw new Error(`Fleet workspace roots overlap: ${first.id} and ${second.id}`)
      }
    }
  }

  return {
    version: 1,
    service: {
      pactline: {
        server: serverURL(pactline.server),
        tokenEnv,
        executable: pactline.executable === undefined
          ? 'pactline'
          : nonEmpty(pactline.executable, 'service.pactline.executable'),
      },
      stateDirectory: absolutePath(service.stateDirectory, 'service.stateDirectory'),
      pollIntervalMs: durationMilliseconds(service.pollInterval, 'service.pollInterval', DEFAULT_POLL_INTERVAL_MS),
      maxConcurrentRuns,
      shutdownDeadlineMs: durationMilliseconds(
        service.shutdownDeadline,
        'service.shutdownDeadline',
        DEFAULT_SHUTDOWN_DEADLINE_MS,
      ),
      http: {
        address: address as FleetServiceConfig['service']['http']['address'],
        port,
      },
    },
    fleets,
  }
}

function canonical(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonical)
  if (typeof value !== 'object' || value === null) return value
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .sort(([first], [second]) => first.localeCompare(second))
      .map(([key, child]) => [key, canonical(child)]),
  )
}

export function fleetConfigRevision(config: FleetServiceConfig): string {
  return createHash('sha256').update(JSON.stringify(canonical(config))).digest('hex')
}

export function parseFleetConfig(
  source: string,
  sourcePath: string,
  options: FleetConfigLoadOptions = {},
): FleetConfigSnapshot {
  let decoded: unknown
  try {
    decoded = parse(source)
  } catch (error) {
    throw new Error('Fleet configuration is not valid YAML', { cause: error })
  }
  const config = parseConfig(decoded, options)
  return {
    sourcePath: resolve(sourcePath),
    revision: fleetConfigRevision(config),
    loadedAt: (options.now ?? (() => new Date()))().toISOString(),
    config,
  }
}

export async function loadFleetConfig(
  sourcePath: string,
  options: FleetConfigLoadOptions = {},
): Promise<FleetConfigSnapshot> {
  const absoluteSource = resolve(sourcePath)
  const source = await readFile(absoluteSource, 'utf8')
  return parseFleetConfig(source, absoluteSource, options)
}
