import { spawn } from 'node:child_process'
import type { L2V2CaseSpec, L2V2Spec } from './l2-v2-spec.js'

const MAX_OUTPUT = 2_000_000

export interface L2V2CommandResult { readonly stdout: string; readonly stderr: string }
export type L2V2CommandRunner = (executable: string, args: readonly string[]) => Promise<L2V2CommandResult>

export interface L2V2EffectInventory {
  readonly projectCreates: 1
  readonly repositoryBindings: 1
  readonly taskCreates: 6
  readonly criterionCreates: 12
  readonly readinessCommands: 6
  readonly staticRefCreates: readonly string[]
  readonly seededDraftPullRequests: readonly { readonly caseId: string; readonly base: string; readonly head: string }[]
  readonly maximumDeliveryDraftPullRequests: 5
  readonly forbiddenRefs: readonly ['refs/heads/main']
}

export interface L2V2RepositoryPreflight {
  readonly repository: string
  readonly viewerPermission: 'ADMIN' | 'MAINTAIN' | 'WRITE'
  readonly observedBaseRevision: string
  readonly verifiedSeedRefs: number
  readonly targetNamespaceEmpty: true
  readonly inventory: L2V2EffectInventory
}

function redact(value: string): string {
  return value
    .replace(/\bBearer\s+\S+/gi, 'Bearer [REDACTED]')
    .replace(/\b(?:sk|gh[opsu]|bb_pat)[-_][A-Za-z0-9._-]{8,}\b/g, '[REDACTED]')
    .slice(-4_096)
}

export function defaultL2V2CommandRunner(executable: string, args: readonly string[]): Promise<L2V2CommandResult> {
  return new Promise((resolve, reject) => {
    const detached = process.platform !== 'win32'
    const child = spawn(executable, [...args], {
      shell: false, stdio: ['ignore', 'pipe', 'pipe'],
      detached,
      env: {
        PATH: process.env.PATH, HOME: process.env.HOME,
        XDG_CONFIG_HOME: process.env.XDG_CONFIG_HOME, XDG_DATA_HOME: process.env.XDG_DATA_HOME,
        LANG: 'C.UTF-8', LC_ALL: 'C.UTF-8', GH_PAGER: '', PAGER: 'cat',
        GIT_TERMINAL_PROMPT: '0', GIT_ASKPASS: '/usr/bin/false',
        GIT_HTTP_LOW_SPEED_LIMIT: '1', GIT_HTTP_LOW_SPEED_TIME: '20',
      },
    })
    let stdout = ''; let stderr = ''; let bytes = 0; let settled = false
    let pendingFailure: Error | undefined
    let forceTimer: NodeJS.Timeout | undefined
    const terminate = (signal: NodeJS.Signals): void => {
      if (child.pid === undefined) return
      try {
        if (detached) process.kill(-child.pid, signal)
        else child.kill(signal)
      } catch (error: unknown) {
        if ((error as NodeJS.ErrnoException).code !== 'ESRCH') throw error
      }
    }
    const stop = (error: Error): void => {
      if (settled || pendingFailure !== undefined) return
      pendingFailure = error; terminate('SIGTERM')
      forceTimer = setTimeout(() => terminate('SIGKILL'), 2_000); forceTimer.unref()
    }
    const timer = setTimeout(() => { stop(new Error(`${executable} preflight timed out`)) }, 60_000)
    timer.unref()
    const finish = (error?: Error): void => {
      if (settled) return
      settled = true; clearTimeout(timer); if (forceTimer !== undefined) clearTimeout(forceTimer)
      if (error !== undefined) reject(error); else resolve({ stdout, stderr })
    }
    const collect = (target: 'stdout' | 'stderr', chunk: Buffer): void => {
      bytes += chunk.length
      if (bytes > MAX_OUTPUT) { stop(new Error(`${executable} preflight output exceeded its bound`)); return }
      if (target === 'stdout') stdout += chunk.toString('utf8'); else stderr += chunk.toString('utf8')
    }
    child.stdout.on('data', (chunk: Buffer) => { collect('stdout', chunk) })
    child.stderr.on('data', (chunk: Buffer) => { collect('stderr', chunk) })
    child.once('error', error => { finish(new Error(`${executable} preflight failed to start`, { cause: error })) })
    child.once('close', code => {
      if (pendingFailure !== undefined) finish(pendingFailure)
      else if (code === 0) finish()
      else finish(new Error(`${executable} preflight exited ${String(code)}: ${redact(stderr || stdout)}`))
    })
  })
}

function githubRef(value: unknown): { readonly ref: string; readonly revision: string } {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('GitHub returned malformed ref evidence')
  const item = value as Record<string, unknown>
  const object = item.object
  if (typeof item.ref !== 'string' || !item.ref.startsWith('refs/heads/')
    || typeof object !== 'object' || object === null || Array.isArray(object)
    || !/^[a-f0-9]{40}$/.test(String((object as Record<string, unknown>).sha))) {
    throw new Error('GitHub returned malformed ref evidence')
  }
  return { ref: item.ref, revision: String((object as Record<string, unknown>).sha) }
}

function targetRef(spec: L2V2Spec, suffix: string): string {
  return `refs/heads/${spec.repository.branchPrefix}${suffix}`
}

function caseSuffix(item: L2V2CaseSpec): string {
  return item.caseId.toLowerCase()
}

export function l2V2EffectInventory(spec: L2V2Spec): L2V2EffectInventory {
  const staticRefCreates = [
    targetRef(spec, 'source'),
    ...spec.cases.map(item => targetRef(spec, `base/${caseSuffix(item)}`)),
    ...spec.cases.filter(item => item.candidate !== undefined).map(item => targetRef(spec, `candidate/${caseSuffix(item)}`)),
  ]
  return {
    projectCreates: 1, repositoryBindings: 1, taskCreates: 6, criterionCreates: 12, readinessCommands: 6,
    staticRefCreates,
    seededDraftPullRequests: spec.cases.filter(item => item.candidate !== undefined).map(item => ({
      caseId: item.caseId,
      base: targetRef(spec, `base/${caseSuffix(item)}`).replace('refs/heads/', ''),
      head: targetRef(spec, `candidate/${caseSuffix(item)}`).replace('refs/heads/', ''),
    })),
    maximumDeliveryDraftPullRequests: 5,
    forbiddenRefs: ['refs/heads/main'],
  }
}

/** Verify frozen source refs and an empty target namespace without external writes. */
export async function preflightL2V2Repository(
  spec: L2V2Spec,
  run: L2V2CommandRunner = defaultL2V2CommandRunner,
): Promise<L2V2RepositoryPreflight> {
  const sourceRefs = new Map<string, string>([[spec.repository.baseRef, spec.repository.baseRevision]])
  for (const item of spec.cases) {
    const existing = sourceRefs.get(item.seedRef)
    if (existing !== undefined && existing !== item.baseRevision) throw new Error('L2 v2 spec assigns conflicting revisions to one seed ref')
    sourceRefs.set(item.seedRef, item.baseRevision)
    if (item.candidate !== undefined) sourceRefs.set(item.candidate.seedRef, item.candidate.revision)
  }
  const repositoryPath = new URL(spec.repository.url).pathname.replace(/^\//, '').replace(/\/$/, '')
  if (new URL(spec.repository.url).hostname !== 'github.com' || repositoryPath.split('/').length !== 2) {
    throw new Error('M4 L2 v2 repository preflight requires one exact github.com repository')
  }
  const viewed = JSON.parse((await run('gh', ['repo', 'view', repositoryPath, '--json', 'nameWithOwner,viewerPermission'])).stdout) as unknown
  if (typeof viewed !== 'object' || viewed === null || Array.isArray(viewed)) throw new Error('GitHub repository preflight is invalid')
  const evidence = viewed as Record<string, unknown>
  if (evidence.nameWithOwner !== repositoryPath || !['ADMIN', 'MAINTAIN', 'WRITE'].includes(String(evidence.viewerPermission))) {
    throw new Error('GitHub authentication lacks write permission for the exact evaluation repository')
  }

  const remote = new Map<string, string>()
  for (const ref of sourceRefs.keys()) {
    const response = await run('gh', ['api', `repos/${repositoryPath}/git/ref/${ref.replace('refs/', '')}`])
    const parsed = githubRef(JSON.parse(response.stdout) as unknown)
    remote.set(parsed.ref, parsed.revision)
  }
  for (const [ref, revision] of sourceRefs) {
    if (remote.get(ref) !== revision) throw new Error(`Frozen source ref drifted or is missing: ${ref}`)
  }
  const targetResponse = JSON.parse((await run('gh', [
    'api', `repos/${repositoryPath}/git/matching-refs/heads/${spec.repository.branchPrefix.replace(/\/$/, '')}`,
  ])).stdout) as unknown
  if (!Array.isArray(targetResponse)) throw new Error('GitHub returned malformed target namespace evidence')
  const targets = targetResponse.map(githubRef)
  if (targets.length !== 0) throw new Error(`L2 v2 target namespace is not empty: ${targets.map(item => item.ref).sort().join(', ')}`)
  return {
    repository: spec.repository.url,
    viewerPermission: evidence.viewerPermission as L2V2RepositoryPreflight['viewerPermission'],
    observedBaseRevision: remote.get(spec.repository.baseRef)!, verifiedSeedRefs: sourceRefs.size,
    targetNamespaceEmpty: true, inventory: l2V2EffectInventory(spec),
  }
}
