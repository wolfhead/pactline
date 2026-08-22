import { spawn } from 'node:child_process'
import { readFile, stat } from 'node:fs/promises'
import { join } from 'node:path'
import type { ExecutionProposal, ReviewProposal, VerificationProposal } from './harness-result.js'
import { sanitizeHealthDiagnostic } from '../health/store.js'

const DEFAULT_TIMEOUT_MS = 300_000
const DEFAULT_OUTPUT_BYTES = 256 * 1024
const MAX_RETAINED_OUTPUT_BYTES = 2_048
const MAX_RETAINED_PATHS = 64

export interface CommandObservation {
  readonly command: string
  readonly outcome: 'passed' | 'failed'
  readonly exitCode: number | null
  readonly summary: string
  readonly failureKind?: VerificationFailureKind
}

export type VerificationFailureKind =
  | 'test_failure'
  | 'command_unavailable'
  | 'timeout'
  | 'missing_prerequisite'
  | 'output_limit'

export type VerificationMismatchCategory = VerificationFailureKind | 'parse_mismatch' | 'result_mismatch' | 'changed_paths_mismatch'

export interface VerificationMismatchDetail {
  readonly category: VerificationMismatchCategory
  readonly command?: string
  readonly harness?: { readonly outcome: 'passed' | 'failed'; readonly summary: string }
  readonly fleet?: { readonly outcome: 'passed' | 'failed'; readonly exitCode: number | null; readonly summary: string }
  readonly harnessChangedPaths?: readonly string[]
  readonly fleetChangedPaths?: readonly string[]
  readonly harnessChangedPathsOmitted?: number
  readonly fleetChangedPathsOmitted?: number
}

export class VerificationMismatchError extends Error {
  constructor(readonly details: readonly VerificationMismatchDetail[]) {
    super(details.every(detail => detail.category === 'changed_paths_mismatch')
      ? 'Harness-reported changed paths do not match Fleet Git observation'
      : 'Harness-reported verification does not match Fleet observation')
    this.name = 'VerificationMismatchError'
  }
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
  readonly code: number | null
  readonly stdout: string
  readonly stderr: string
  readonly failureKind?: 'command_unavailable' | 'timeout' | 'output_limit'
  readonly failureMessage?: string
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
  return new Promise(resolve => {
    const child = spawn(executable, args, { cwd, env: environment, shell: false, stdio: ['ignore', 'pipe', 'pipe'] })
    const stdout: Buffer[] = []
    const stderr: Buffer[] = []
    let bytes = 0
    let terminalFailure: Pick<ProcessResult, 'failureKind' | 'failureMessage'> | undefined
    const stop = (failureKind: 'command_unavailable' | 'timeout' | 'output_limit', failureMessage: string): void => {
      if (terminalFailure !== undefined) return
      terminalFailure = { failureKind, failureMessage }
      child.kill('SIGTERM')
    }
    const timer = setTimeout(() => stop('timeout', `Command exceeded ${String(timeoutMs)} ms`), timeoutMs)
    timer.unref()
    const capture = (target: Buffer[]) => (chunk: Buffer): void => {
      bytes += chunk.length
      if (bytes > maxOutputBytes) {
        stop('output_limit', `Command output exceeded ${String(maxOutputBytes)} bytes`)
        return
      }
      target.push(chunk)
    }
    child.stdout.on('data', capture(stdout))
    child.stderr.on('data', capture(stderr))
    child.once('error', error => stop('command_unavailable', error.message))
    child.once('close', code => {
      clearTimeout(timer)
      resolve({
        code: terminalFailure === undefined ? (code ?? 1) : null,
        stdout: Buffer.concat(stdout).toString('utf8'),
        stderr: Buffer.concat(stderr).toString('utf8'),
        ...terminalFailure,
      })
    })
  })
}

function summary(result: ProcessResult): string {
  const output = [result.stdout.trim(), result.stderr.trim(), result.failureMessage].filter(Boolean).join('\n')
  const redacted = sanitizeHealthDiagnostic(output === '' ? `Exited with status ${String(result.code)}.` : output)
  return Buffer.from(redacted).subarray(0, MAX_RETAINED_OUTPUT_BYTES).toString('utf8')
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
    const failureKind = result.failureKind
      ?? ([126, 127].includes(result.code ?? -1) ? 'command_unavailable'
        : result.code !== 0 && (/^\s*test\s+-[a-z]+\s+/i.test(command) || /no such file|not found|cannot find/i.test(`${result.stdout}\n${result.stderr}`))
          ? 'missing_prerequisite'
          : result.code === 0 ? undefined : 'test_failure')
    observations.push({
      command,
      outcome: result.code === 0 ? 'passed' : 'failed',
      exitCode: result.code,
      summary: summary(result),
      ...(failureKind === undefined ? {} : { failureKind }),
    })
  }
  return observations
}

export function assertAllowedPaths(changedPaths: readonly string[], allowedPaths: readonly string[]): void {
  const outside = changedPaths.filter(path => !allowedPaths.some(allowed => path === allowed || path.startsWith(`${allowed.replace(/\/$/, '')}/`)))
  if (outside.length > 0) throw new Error(`Workspace changed paths outside the allowlist: ${outside.join(', ')}`)
}

function verificationDifferences(
  proposed: readonly VerificationProposal[],
  observed: readonly CommandObservation[],
): VerificationMismatchDetail[] {
  const differences: VerificationMismatchDetail[] = []
  for (const actual of observed) {
    const reports = proposed.filter(item => item.command === actual.command)
    const reported = reports[0]
    if (reports.length !== 1 || reported?.outcome !== actual.outcome) {
      differences.push({
        category: reports.length !== 1 ? 'parse_mismatch' : actual.failureKind ?? 'result_mismatch',
        command: sanitizeHealthDiagnostic(actual.command),
        ...(reported === undefined ? {} : { harness: { outcome: reported.outcome, summary: sanitizeHealthDiagnostic(reported.summary) } }),
        fleet: { outcome: actual.outcome, exitCode: actual.exitCode, summary: actual.summary },
      })
    }
  }
  for (const reported of proposed) {
    if (!observed.some(actual => actual.command === reported.command)) {
      differences.push({
        category: 'parse_mismatch', command: sanitizeHealthDiagnostic(reported.command),
        harness: { outcome: reported.outcome, summary: sanitizeHealthDiagnostic(reported.summary) },
      })
    }
  }
  return differences
}

function retainedPaths(paths: readonly string[]): {
  readonly values: readonly string[]
  readonly omitted: number
} {
  return {
    values: paths.slice(0, MAX_RETAINED_PATHS).map(path => sanitizeHealthDiagnostic(path)),
    omitted: Math.max(0, paths.length - MAX_RETAINED_PATHS),
  }
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
  const commandDifferences = verificationDifferences(proposal.verification, observation.commands)
  if (commandDifferences.length > 0) throw new VerificationMismatchError(commandDifferences)
  if (proposal.kind === 'execution') {
    if (observation.git.head !== options.baseHead) {
      throw new Error('Harness must leave commits to Fleet delivery authority')
    }
    assertAllowedPaths(observation.git.changedPaths, options.allowedPaths)
    const reported = [...proposal.changedPaths].sort()
    if (JSON.stringify(reported) !== JSON.stringify(observation.git.changedPaths)) {
      const harnessPaths = retainedPaths(reported)
      const fleetPaths = retainedPaths(observation.git.changedPaths)
      throw new VerificationMismatchError([{
        category: 'changed_paths_mismatch',
        harnessChangedPaths: harnessPaths.values,
        fleetChangedPaths: fleetPaths.values,
        ...(harnessPaths.omitted === 0 ? {} : { harnessChangedPathsOmitted: harnessPaths.omitted }),
        ...(fleetPaths.omitted === 0 ? {} : { fleetChangedPathsOmitted: fleetPaths.omitted }),
      }])
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
