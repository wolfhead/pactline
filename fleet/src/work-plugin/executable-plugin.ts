import { spawn } from 'node:child_process'
import { constants } from 'node:fs'
import { access, lstat } from 'node:fs/promises'
import { isAbsolute } from 'node:path'
import type { FleetDefinitionConfig, FleetRouteConfig } from '../config/types.js'
import type { FleetWorkDefinition } from '../core/work-definition.js'
import type { PactlineCLI } from '../pactline/client.js'
import type { RepositoryDelivery, RepositoryIdentity } from '../repository/delivery.js'
import { validateRepositoryDelivery } from '../repository/delivery.js'
import type { FleetRunRecord } from '../registry/fleet-registry.js'
import type { FleetWorkCandidate, ResolvedFleetWork, WorkDefinitionResolver } from '../scheduler/candidate.js'

const MAX_OUTPUT_BYTES = 2 * 1024 * 1024

export async function assertWorkPluginExecutable(path: string): Promise<void> {
  const info = await lstat(path)
  if (!info.isFile() || info.isSymbolicLink()) throw new Error('Fleet work plugin must be a regular non-symlink file')
  await access(path, constants.X_OK)
}

export interface FrozenWorkPluginPolicy {
  readonly definition: FleetWorkDefinition
  readonly route: FleetRouteConfig
  readonly plugin: NonNullable<FleetDefinitionConfig['workPlugin']>
  readonly workspaceRoot: string
  readonly gitCredentialReference?: string
}

interface PluginEnvelope {
  readonly ok: true
  readonly data: unknown
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function string(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`${name} must be a non-empty string`)
  return value
}

function positive(value: unknown, name: string): number {
  if (!Number.isSafeInteger(value) || Number(value) < 1) throw new Error(`${name} must be a positive integer`)
  return Number(value)
}

function stringList(value: unknown, name: string): readonly string[] {
  if (!Array.isArray(value) || value.length === 0 || value.length > 64
    || value.some(item => typeof item !== 'string' || item.trim() === '')) throw new Error(`${name} must be a non-empty bounded string array`)
  return value as string[]
}

function repositoryIdentity(value: unknown): RepositoryIdentity {
  const item = record(value, 'work definition repository')
  if (!['github', 'gitlab'].includes(String(item.provider))) throw new Error('work definition repository provider is invalid')
  return {
    provider: item.provider as RepositoryIdentity['provider'],
    host: string(item.host, 'repository.host'),
    owner: string(item.owner, 'repository.owner'),
    name: string(item.name, 'repository.name'),
  }
}

function revision(value: unknown, name: string): FleetWorkDefinition['base'] {
  const item = record(value, name)
  const sha = string(item.revision, `${name}.revision`)
  if (!/^[a-f0-9]{40}$/.test(sha)) throw new Error(`${name}.revision must be a lowercase Git SHA`)
  const source = string(item.source, `${name}.source`)
  try {
    const parsed = new URL(source)
    if (parsed.username !== '' || parsed.password !== '') throw new Error(`${name}.source must not contain credentials`)
  } catch (error) {
    if (error instanceof Error && error.message.includes('must not contain credentials')) throw error
    if (!isAbsolute(source)) throw new Error(`${name}.source must be a credential-free URL or absolute local path`)
  }
  return { source, ref: string(item.ref, `${name}.ref`), revision: sha }
}

function validateDefinition(value: unknown, candidate: FleetWorkCandidate): FleetWorkDefinition {
  const item = record(value, 'work definition')
  const criteriaValue = item.criteria
  if (!Array.isArray(criteriaValue)) throw new Error('work definition criteria must be an array')
  const criteria = criteriaValue.map((raw, index) => {
    const criterion = record(raw, `criteria[${String(index)}]`)
    return { id: string(criterion.id, 'criterion.id'), revision: positive(criterion.revision, 'criterion.revision') }
  })
  const taskNumber = positive(item.taskNumber, 'work definition taskNumber')
  const taskVersion = positive(item.taskVersion, 'work definition taskVersion')
  if (taskNumber !== candidate.task.number || taskVersion !== candidate.task.version) {
    throw new Error('work definition changed the discovered Task identity')
  }
  const repository = repositoryIdentity(item.repository)
  const base = revision(item.base, 'work definition base')
  let deliveryCandidate: FleetWorkDefinition['candidate']
  if (item.candidate !== undefined) {
    const raw = record(item.candidate, 'work definition candidate')
    deliveryCandidate = {
      ...validateRepositoryDelivery({
        repository,
        codeChangeUrl: string(raw.codeChangeUrl, 'candidate.codeChangeUrl'),
        revision: string(raw.revision, 'candidate.revision'),
        branch: string(raw.branch, 'candidate.branch'),
      }),
      ref: string(raw.ref, 'candidate.ref'),
    }
  }
  if (candidate.stage === 'review' && deliveryCandidate === undefined) throw new Error('review work definition requires a frozen delivery candidate')
  const allowedPaths = stringList(item.allowedPaths, 'work definition allowedPaths')
  if (allowedPaths.some(path => path.startsWith('/') || path.includes('..') || path === '.git' || path.startsWith('.git/'))) {
    throw new Error('work definition allowedPaths must be safe repository-relative paths')
  }
  return {
    caseId: string(item.caseId, 'work definition caseId'),
    taskNumber,
    taskVersion,
    base,
    repository,
    allowedPaths,
    verificationCommands: stringList(item.verificationCommands, 'work definition verificationCommands'),
    criteria,
    ...(deliveryCandidate === undefined ? {} : { candidate: deliveryCandidate }),
  }
}

export function workPluginEnvironment(
  source: NodeJS.ProcessEnv,
  pactlineTokenEnv: string,
  gitCredentialReference?: string,
): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = {}
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TMP', 'TEMP', 'LANG', 'LC_ALL', 'TZ', 'SSH_AUTH_SOCK', 'SSL_CERT_FILE', 'SSL_CERT_DIR']) {
    if (source[key] !== undefined) environment[key] = source[key]
  }
  if (gitCredentialReference !== undefined && /^[A-Za-z_][A-Za-z0-9_]*$/.test(gitCredentialReference)
    && source[gitCredentialReference] !== undefined) environment[gitCredentialReference] = source[gitCredentialReference]
  for (const key of [pactlineTokenEnv, 'PACTLINE_TOKEN', 'DEEPSEEK_API_KEY', 'OPENAI_API_KEY']) delete environment[key]
  return environment
}

export class ExecutableFleetWorkPlugin {
  constructor(
    readonly config: NonNullable<FleetDefinitionConfig['workPlugin']>,
    private readonly environment: NodeJS.ProcessEnv,
  ) {}

  invoke(
    operation: 'resolve' | 'commit' | 'push' | 'open-code-change',
    input: Readonly<Record<string, unknown>>,
    signal: AbortSignal,
  ): Promise<unknown> {
    return new Promise((resolvePromise, reject) => {
      const child = spawn(this.config.executable, [...this.config.args, operation], {
        env: this.environment, shell: false, stdio: ['pipe', 'pipe', 'pipe'], signal,
      })
      const stdout: Buffer[] = []
      const stderr: Buffer[] = []
      let size = 0
      const timer = setTimeout(() => child.kill('SIGTERM'), this.config.timeoutMs)
      timer.unref()
      child.stdout.on('data', (chunk: Buffer) => {
        size += chunk.length
        if (size > MAX_OUTPUT_BYTES) child.kill('SIGTERM')
        else stdout.push(chunk)
      })
      child.stderr.on('data', (chunk: Buffer) => {
        if (Buffer.concat(stderr).length < 64 * 1024) stderr.push(chunk)
      })
      child.once('error', reject)
      child.once('close', code => {
        clearTimeout(timer)
        if (size > MAX_OUTPUT_BYTES) { reject(new Error('Fleet work plugin output exceeded the limit')); return }
        if (code !== 0) {
          const diagnostic = Buffer.concat(stderr).toString('utf8').trim().slice(0, 2_000)
          reject(new Error(`Fleet work plugin failed${diagnostic === '' ? '' : `: ${diagnostic}`}`))
          return
        }
        try {
          const envelope = record(JSON.parse(Buffer.concat(stdout).toString('utf8')) as unknown, 'work plugin response') as unknown as PluginEnvelope
          if (envelope.ok !== true) throw new Error('work plugin response is not successful')
          resolvePromise(envelope.data)
        } catch (error) {
          reject(new Error('Fleet work plugin returned invalid JSON', { cause: error }))
        }
      })
      child.stdin.end(JSON.stringify(input))
    })
  }
}

export class ExecutableWorkDefinitionResolver implements WorkDefinitionResolver {
  constructor(
    private readonly client: Pick<PactlineCLI, 'showTask'>,
    private readonly snapshot: () => { readonly config: { readonly service: { readonly pactline: { readonly tokenEnv: string } }; readonly fleets: Readonly<Record<string, FleetDefinitionConfig>> } },
    private readonly environment: NodeJS.ProcessEnv,
    private readonly sessionId: string,
  ) {}

  enabled(fleet: FleetDefinitionConfig): boolean { return fleet.workPlugin !== undefined }

  async resolve(candidate: FleetWorkCandidate, fleet: FleetDefinitionConfig, signal: AbortSignal): Promise<ResolvedFleetWork | undefined> {
    if (fleet.workPlugin === undefined) return undefined
    const packet = await this.client.showTask(candidate.task.number, 20, { sessionId: this.sessionId, signal })
    const current = this.snapshot().config.fleets[fleet.id]
    if (current === undefined || !current.enabled || current.projectNumber !== candidate.projectNumber
      || current.workPlugin === undefined) return undefined
    const route = current.routing[candidate.stage]
    const plugin = new ExecutableFleetWorkPlugin(
      current.workPlugin,
      workPluginEnvironment(
        this.environment,
        this.snapshot().config.service.pactline.tokenEnv,
        current.credentials.git,
      ),
    )
    const definition = validateDefinition(await plugin.invoke('resolve', {
      schemaVersion: 1,
      operation: 'resolve',
      candidate,
      taskPacket: packet.data,
      projectNumber: candidate.projectNumber,
      gitCredentialReference: current.credentials.git,
    }, signal), candidate)
    const policy: FrozenWorkPluginPolicy = {
      definition,
      route,
      plugin: current.workPlugin,
      workspaceRoot: current.workspaceRoot,
      ...(current.credentials.git === undefined ? {} : { gitCredentialReference: current.credentials.git }),
    }
    return {
      admission: {
        taskNumber: candidate.task.number,
        taskVersion: candidate.task.version,
        stage: candidate.stage,
        frozenPolicy: policy as unknown as Readonly<Record<string, unknown>>,
      },
    }
  }
}

export function frozenPluginPolicy(run: FleetRunRecord): FrozenWorkPluginPolicy {
  const policy = record(run.frozenPolicy, 'frozen Run policy')
  const route = record(policy.route, 'frozen route')
  const plugin = record(policy.plugin, 'frozen work plugin')
  const candidate: FleetWorkCandidate = {
    fleetId: run.fleetId,
    projectNumber: run.projectNumber,
    stage: run.stage === 'review' ? 'review' : 'execution',
    task: {
      id: 'frozen', number: run.taskNumber!, title: 'frozen', version: run.taskVersion!, phase: 'frozen', activity: 'frozen',
    },
  }
  return {
    definition: validateDefinition(policy.definition, candidate),
    route: {
      adapter: string(route.adapter, 'route.adapter'),
      model: string(route.model, 'route.model'),
      ...(route.reasoning === undefined ? {} : { reasoning: string(route.reasoning, 'route.reasoning') }),
      promptVersion: string(route.promptVersion, 'route.promptVersion'),
      resultContractVersion: positive(route.resultContractVersion, 'route.resultContractVersion'),
    },
    plugin: {
      executable: string(plugin.executable, 'plugin.executable'),
      args: Array.isArray(plugin.args) && plugin.args.every(value => typeof value === 'string') ? plugin.args as string[] : [],
      timeoutMs: positive(plugin.timeoutMs, 'plugin.timeoutMs'),
    },
    workspaceRoot: string(policy.workspaceRoot, 'workspaceRoot'),
    ...(policy.gitCredentialReference === undefined ? {} : { gitCredentialReference: string(policy.gitCredentialReference, 'gitCredentialReference') }),
  }
}

export function validatePluginDelivery(value: unknown): RepositoryDelivery {
  const item = record(value, 'work plugin delivery')
  return validateRepositoryDelivery({
    repository: repositoryIdentity(item.repository),
    codeChangeUrl: string(item.codeChangeUrl, 'delivery.codeChangeUrl'),
    revision: string(item.revision, 'delivery.revision'),
    branch: string(item.branch, 'delivery.branch'),
  })
}
