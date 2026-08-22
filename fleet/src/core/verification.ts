import { spawn } from 'node:child_process'
import { readFile, stat } from 'node:fs/promises'
import { join } from 'node:path'
import type { ExecutionProposal, ReviewProposal, VerificationProposal } from './harness-result.js'

const DEFAULT_TIMEOUT_MS = 300_000
const DEFAULT_OUTPUT_BYTES = 256 * 1024

export interface CommandObservation {
  readonly command: string
  readonly outcome: 'passed' | 'failed'
  readonly exitCode: number
  readonly summary: string
}

export interface GitObservation {
  readonly head: string
  readonly changedPaths: readonly string[]
  readonly porcelain: string
}

export function decodeGitObservation(value: unknown): GitObservation {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('Git observation must be an object')
  }
  const item = value as Record<string, unknown>
  if (typeof item.head !== 'string' || !/^[a-f0-9]{40}$/.test(item.head)
    || !Array.isArray(item.changedPaths) || item.changedPaths.some(path => typeof path !== 'string')
    || typeof item.porcelain !== 'string') {
    throw new Error('Git observation is invalid')
  }
  return { head: item.head, changedPaths: item.changedPaths as string[], porcelain: item.porcelain }
}

export interface VerificationObservation {
  readonly git: GitObservation
  readonly commands: readonly CommandObservation[]
}

export interface VerificationRuntimeOptions {
  readonly environment?: NodeJS.ProcessEnv
  readonly timeoutMs?: number
  readonly maxOutputBytes?: number
}

interface ProcessResult {
  readonly code: number
  readonly stdout: string
  readonly stderr: string
}

function safeEnvironment(source: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = {}
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TMP', 'TEMP', 'LANG', 'LC_ALL', 'TZ', 'CI', 'GOPATH', 'GOMODCACHE', 'GOCACHE']) {
    if (source[key] !== undefined) environment[key] = source[key]
  }
  return environment
}

function run(
  executable: string,
  args: readonly string[],
  cwd: string,
  environment: NodeJS.ProcessEnv,
  timeoutMs: number,
  maxOutputBytes: number,
): Promise<ProcessResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, { cwd, env: environment, shell: false, stdio: ['ignore', 'pipe', 'pipe'] })
    const stdout: Buffer[] = []
    const stderr: Buffer[] = []
    let bytes = 0
    let terminalError: Error | undefined
    const stop = (error: Error): void => {
      if (terminalError !== undefined) return
      terminalError = error
      child.kill('SIGTERM')
    }
    const timer = setTimeout(() => stop(new Error(`Command exceeded ${String(timeoutMs)} ms`)), timeoutMs)
    timer.unref()
    const capture = (target: Buffer[]) => (chunk: Buffer): void => {
      bytes += chunk.length
      if (bytes > maxOutputBytes) {
        stop(new Error(`Command output exceeded ${String(maxOutputBytes)} bytes`))
        return
      }
      target.push(chunk)
    }
    child.stdout.on('data', capture(stdout))
    child.stderr.on('data', capture(stderr))
    child.once('error', stop)
    child.once('close', code => {
      clearTimeout(timer)
      if (terminalError !== undefined) { reject(terminalError); return }
      resolve({ code: code ?? 1, stdout: Buffer.concat(stdout).toString('utf8'), stderr: Buffer.concat(stderr).toString('utf8') })
    })
  })
}

function summary(result: ProcessResult): string {
  const output = [result.stdout.trim(), result.stderr.trim()].filter(Boolean).join('\n')
  const bounded = Buffer.from(output).subarray(0, 16_384).toString('utf8')
  return bounded === '' ? `Exited with status ${String(result.code)}.` : bounded
}

async function git(repositoryPath: string, args: readonly string[], environment: NodeJS.ProcessEnv): Promise<string> {
  const result = await run('git', args, repositoryPath, environment, 60_000, DEFAULT_OUTPUT_BYTES)
  if (result.code !== 0) throw new Error(`Git audit failed: ${summary(result)}`)
  return result.stdout
}

function statusPaths(porcelain: string): string[] {
  if (porcelain === '') return []
  return porcelain.split('\n').flatMap(line => {
    const value = line.slice(3)
    return value.includes(' -> ')
      ? value.split(' -> ').map(path => path.replace(/^"|"$/g, ''))
      : [value.replace(/^"|"$/g, '')]
  })
}

export async function observeGit(repositoryPath: string, baseRevision: string, environment: NodeJS.ProcessEnv = process.env): Promise<GitObservation> {
  if (!/^[a-f0-9]{40}$/.test(baseRevision)) throw new Error('baseRevision must be a lowercase 40-character Git SHA')
  const safe = safeEnvironment(environment)
  const head = (await git(repositoryPath, ['rev-parse', 'HEAD'], safe)).trim()
  const porcelain = (await git(repositoryPath, ['status', '--porcelain=v1', '--untracked-files=all'], safe)).trimEnd()
  const committed = (await git(repositoryPath, ['diff', '--name-only', '--diff-filter=ACDMRTUXB', `${baseRevision}..${head}`], safe)).trim()
  const changedPaths = [...new Set([
    ...committed.split('\n').filter(Boolean),
    ...statusPaths(porcelain),
  ])].sort()
  return { head, changedPaths, porcelain }
}

export async function runFixedVerification(
  repositoryPath: string,
  commands: readonly string[],
  options: VerificationRuntimeOptions = {},
): Promise<readonly CommandObservation[]> {
  if (commands.length === 0 || commands.length > 32) throw new Error('verification commands must be a non-empty bounded list')
  if (new Set(commands).size !== commands.length || commands.some(command => command.trim() === '')) {
    throw new Error('verification commands must be unique and non-empty')
  }
  const environment = safeEnvironment(options.environment ?? process.env)
  const observations: CommandObservation[] = []
  for (const command of commands) {
    // Verification runs in a minimal inherited environment. A login shell can
    // source user profiles, replace the supplied PATH, and make results depend
    // on the coordinator's home directory.
    const result = await run('/bin/sh', ['-c', command], repositoryPath, environment, options.timeoutMs ?? DEFAULT_TIMEOUT_MS, options.maxOutputBytes ?? DEFAULT_OUTPUT_BYTES)
    observations.push({ command, outcome: result.code === 0 ? 'passed' : 'failed', exitCode: result.code, summary: summary(result) })
  }
  return observations
}

export function assertAllowedPaths(changedPaths: readonly string[], allowedPaths: readonly string[]): void {
  const outside = changedPaths.filter(path => !allowedPaths.some(allowed => path === allowed || path.startsWith(`${allowed.replace(/\/$/, '')}/`)))
  if (outside.length > 0) throw new Error(`Workspace changed paths outside the allowlist: ${outside.join(', ')}`)
}

function verificationEqual(proposed: readonly VerificationProposal[], observed: readonly CommandObservation[]): boolean {
  if (proposed.length !== observed.length) return false
  return observed.every(actual => proposed.some(item => item.command === actual.command && item.outcome === actual.outcome))
}

/** Reject model-reported repository or verification facts that Fleet did not observe. */
export function assertProposalMatchesObservation(
  proposal: ExecutionProposal | ReviewProposal,
  observation: VerificationObservation,
  options: {
    readonly baseHead: string
    readonly allowedPaths: readonly string[]
    readonly reviewBaseline?: GitObservation
  },
): void {
  if (!verificationEqual(proposal.verification, observation.commands)) {
    throw new Error('Harness-reported verification does not match Fleet observation')
  }
  if (proposal.kind === 'execution') {
    if (observation.git.head !== options.baseHead) {
      throw new Error('Harness must leave commits to Fleet delivery authority')
    }
    assertAllowedPaths(observation.git.changedPaths, options.allowedPaths)
    const reported = [...proposal.changedPaths].sort()
    if (JSON.stringify(reported) !== JSON.stringify(observation.git.changedPaths)) {
      throw new Error('Harness-reported changed paths do not match Fleet Git observation')
    }
    return
  }
  const baseline = options.reviewBaseline ?? { head: options.baseHead, changedPaths: [], porcelain: '' }
  if (observation.git.head !== baseline.head
    || JSON.stringify(observation.git.changedPaths) !== JSON.stringify(baseline.changedPaths)
    || observation.git.porcelain !== baseline.porcelain) {
    throw new Error('Review workspace mutation blocks settlement')
  }
}

export async function assertReviewFindingsExist(repositoryPath: string, proposal: ReviewProposal): Promise<void> {
  for (const finding of proposal.findings) {
    const file = join(repositoryPath, finding.path)
    const info = await stat(file).catch(() => undefined)
    if (info === undefined || !info.isFile()) throw new Error(`Review finding path does not exist: ${finding.path}`)
    const lines = (await readFile(file, 'utf8')).split('\n')
    if (finding.line > lines.length) throw new Error(`Review finding line is outside the file: ${finding.path}:${String(finding.line)}`)
  }
}
