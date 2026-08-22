import { constants } from 'node:fs'
import { access, lstat, mkdir, mkdtemp, realpath, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { basename, dirname, isAbsolute, join, relative, resolve } from 'node:path'
import { spawn } from 'node:child_process'

const RUN_ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/

export type WorkspaceMode = 'execution' | 'review'

export interface RepositoryRevision {
  readonly source: string
  readonly ref: string
  readonly revision: string
}

export interface FleetWorkspace {
  readonly mode: WorkspaceMode
  readonly root: string
  readonly temporaryParent: string
  readonly repositoryPath: string
  readonly source: string
  readonly baseRevision: string
  readonly branch?: string
}

export interface PrepareWorkspaceOptions {
  readonly input: RepositoryRevision
  readonly candidate?: RepositoryRevision
  readonly mode: WorkspaceMode
  readonly runId: string
  readonly branchPrefix?: string
  readonly temporaryDirectory?: string
  readonly environment?: NodeJS.ProcessEnv
  readonly taskIdentity?: {
    readonly projectNumber: number
    readonly taskNumber: number
  }
}

export function decodeFleetWorkspace(value: unknown): FleetWorkspace {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('Task Workspace record must be an object')
  }
  const item = value as Record<string, unknown>
  const string = (key: string): string => {
    const field = item[key]
    if (typeof field !== 'string' || field.trim() === '') throw new Error(`Task Workspace ${key} is invalid`)
    return field
  }
  const mode = item.mode
  if (mode !== 'execution' && mode !== 'review') throw new Error('Task Workspace mode is invalid')
  const baseRevision = string('baseRevision')
  if (!/^[a-f0-9]{40}$/.test(baseRevision)) throw new Error('Task Workspace base revision is invalid')
  const branch = item.branch
  if (mode === 'execution' && (typeof branch !== 'string' || branch.trim() === '')) {
    throw new Error('Task Workspace execution branch is invalid')
  }
  if (branch !== undefined && typeof branch !== 'string') throw new Error('Task Workspace branch is invalid')
  return {
    mode,
    root: string('root'),
    temporaryParent: string('temporaryParent'),
    repositoryPath: string('repositoryPath'),
    source: string('source'),
    baseRevision,
    ...(typeof branch === 'string' ? { branch } : {}),
  }
}

function safeEnvironment(source: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const safe: NodeJS.ProcessEnv = {}
  for (const key of ['PATH', 'TMPDIR', 'TMP', 'TEMP', 'LANG', 'LC_ALL', 'TZ', 'SSL_CERT_FILE', 'SSL_CERT_DIR']) {
    if (source[key] !== undefined) safe[key] = source[key]
  }
  return {
    ...safe,
    GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null', GIT_TERMINAL_PROMPT: '0',
    GIT_ASKPASS: '/usr/bin/false', GIT_SSH_COMMAND: 'ssh -oBatchMode=yes',
    GIT_CONFIG_COUNT: '1', GIT_CONFIG_KEY_0: 'http.version', GIT_CONFIG_VALUE_0: 'HTTP/1.1',
  }
}

function runGit(args: readonly string[], cwd: string, environment: NodeJS.ProcessEnv, timeoutMs = 300_000): Promise<string> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn('git', args, { cwd, env: environment, shell: false, stdio: ['ignore', 'pipe', 'pipe'] })
    const stdout: Buffer[] = []
    const stderr: Buffer[] = []
    const timer = setTimeout(() => { child.kill('SIGTERM'); reject(new Error(`Git command timed out: git ${args.join(' ')}`)) }, timeoutMs)
    timer.unref()
    child.stdout.on('data', (chunk: Buffer) => stdout.push(chunk))
    child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk))
    child.once('error', reject)
    child.once('close', code => {
      clearTimeout(timer)
      if (code !== 0) { reject(new Error(`Git command failed: ${Buffer.concat(stderr).toString('utf8').trim()}`)); return }
      resolvePromise(Buffer.concat(stdout).toString('utf8').trim())
    })
  })
}

async function exists(path: string): Promise<boolean> {
  try { await access(path, constants.F_OK); return true } catch { return false }
}

async function assertContained(root: string, target: string): Promise<void> {
  const resolvedRoot = await realpath(root)
  const resolvedTarget = await realpath(target)
  const relation = relative(resolvedRoot, resolvedTarget)
  if (relation === '' || relation.startsWith('..') || isAbsolute(relation)) throw new Error('Repository escaped its Task Workspace root')
}

export async function verifyWorkspace(workspace: FleetWorkspace, environment: NodeJS.ProcessEnv = process.env): Promise<void> {
  const rootInfo = await lstat(workspace.root)
  const repositoryInfo = await lstat(workspace.repositoryPath)
  if (!rootInfo.isDirectory() || rootInfo.isSymbolicLink() || !repositoryInfo.isDirectory() || repositoryInfo.isSymbolicLink()) {
    throw new Error('Fleet workspace must use real directories without symlink roots')
  }
  if (!basename(workspace.root).startsWith('pactline-fleet-')
    || await realpath(dirname(workspace.root)) !== await realpath(workspace.temporaryParent)) {
    throw new Error('Fleet workspace root is outside its recorded temporary parent')
  }
  await assertContained(workspace.root, workspace.repositoryPath)
  const safe = safeEnvironment(environment)
  const top = await realpath(resolve(await runGit(['rev-parse', '--show-toplevel'], workspace.repositoryPath, safe)))
  if (top !== await realpath(workspace.repositoryPath)) throw new Error('Git top-level does not match the admitted repository path')
  const origin = (await runGit(['config', '--get', 'remote.origin.url'], workspace.repositoryPath, safe)).replace(/\/$/, '').replace(/\.git$/, '')
  if (origin !== workspace.source.replace(/\/$/, '').replace(/\.git$/, '')) throw new Error('Workspace origin does not match the admitted source')
  if (await exists(join(workspace.repositoryPath, '.git', 'objects', 'info', 'alternates'))) throw new Error('Workspace uses an alternate Git object store')
  if (await exists(join(workspace.repositoryPath, '.gitmodules'))) throw new Error('Workspace contains unsupported submodules')
  const branch = await runGit(['symbolic-ref', '--quiet', '--short', 'HEAD'], workspace.repositoryPath, safe).catch(() => '')
  if (workspace.mode === 'review' && branch !== '') throw new Error('Review workspace must use detached HEAD')
  if (workspace.mode === 'execution' && branch !== workspace.branch) throw new Error('Execution workspace is not on its admitted branch')
}

/** Read the local delivery authority needed to reconcile an unobserved commit intent. */
export async function observeWorkspaceRevision(
  workspace: FleetWorkspace,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<{ readonly revision: string; readonly branch: string; readonly clean: boolean }> {
  await verifyWorkspace(workspace, environment)
  const safe = safeEnvironment(environment)
  const revision = (await runGit(['rev-parse', 'HEAD'], workspace.repositoryPath, safe)).trim()
  const branch = (await runGit(['symbolic-ref', '--quiet', '--short', 'HEAD'], workspace.repositoryPath, safe)).trim()
  const porcelain = (await runGit(['status', '--porcelain=v1', '--untracked-files=all'], workspace.repositoryPath, safe)).trimEnd()
  if (!/^[a-f0-9]{40}$/.test(revision) || branch === '') throw new Error('Execution workspace revision authority is invalid')
  return { revision, branch, clean: porcelain === '' }
}

/** Read the remote branch without mutating it; absence is authoritative, transport failure is not. */
export async function observeRemoteRevision(
  workspace: FleetWorkspace,
  branch: string,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<string | undefined> {
  await verifyWorkspace(workspace, environment)
  if (branch.trim() === '' || branch.includes('..') || /[~^:?*[\\\s]/.test(branch)) {
    throw new Error('Remote branch authority is invalid')
  }
  const output = await runGit(
    ['ls-remote', '--heads', 'origin', `refs/heads/${branch}`],
    workspace.repositoryPath,
    safeEnvironment(environment),
  )
  if (output.trim() === '') return undefined
  const [revision, ref, ...extra] = output.trim().split(/\s+/)
  if (extra.length > 0 || ref !== `refs/heads/${branch}` || !/^[a-f0-9]{40}$/.test(revision ?? '')) {
    throw new Error('Remote branch revision authority is invalid')
  }
  return revision
}

export async function prepareWorkspace(options: PrepareWorkspaceOptions): Promise<FleetWorkspace> {
  if (!RUN_ID_PATTERN.test(options.runId)) throw new Error('runId must be a lowercase branch-safe identifier')
  if (!/^[a-f0-9]{40}$/.test(options.input.revision)) throw new Error('Repository revision must be a lowercase 40-character Git SHA')
  if (options.input.source.trim() === '' || options.input.ref.trim() === '') throw new Error('Repository source and ref must be non-empty')
  if (options.candidate !== undefined) {
    if (!/^[a-f0-9]{40}$/.test(options.candidate.revision)
      || options.candidate.ref.trim() === '' || options.candidate.source !== options.input.source) {
      throw new Error('Candidate revision must belong to the admitted repository')
    }
  }
  try {
    const source = new URL(options.input.source)
    if (source.username !== '' || source.password !== '') throw new Error('Repository source must not contain credentials')
  } catch (error: unknown) {
    if (error instanceof Error && error.message === 'Repository source must not contain credentials') throw error
    if (!isAbsolute(options.input.source)) throw new Error('Repository source must be a credential-free URL or absolute local test path')
  }
  const parent = resolve(options.temporaryDirectory ?? tmpdir())
  if (options.taskIdentity !== undefined
    && (!Number.isSafeInteger(options.taskIdentity.projectNumber) || options.taskIdentity.projectNumber < 1
      || !Number.isSafeInteger(options.taskIdentity.taskNumber) || options.taskIdentity.taskNumber < 1)) {
    throw new Error('Task Workspace identity must use positive Project and Task numbers')
  }
  const root = options.taskIdentity === undefined
    ? await mkdtemp(join(parent, 'pactline-fleet-'))
    : join(parent, `pactline-fleet-project-${String(options.taskIdentity.projectNumber)}-task-${String(options.taskIdentity.taskNumber)}`)
  const repositoryPath = join(root, 'repository')
  const environment = safeEnvironment(options.environment ?? process.env)
  const taskBranch = options.taskIdentity === undefined
    ? undefined
    : `fleet/project-${String(options.taskIdentity.projectNumber)}/task-${String(options.taskIdentity.taskNumber)}`
  if (options.taskIdentity !== undefined && await exists(root)) {
    const workspace: FleetWorkspace = options.mode === 'execution'
      ? {
          mode: 'execution', root, temporaryParent: parent, repositoryPath,
          source: options.input.source, baseRevision: options.input.revision, branch: taskBranch!,
        }
      : {
          mode: 'review', root, temporaryParent: parent, repositoryPath,
          source: options.input.source, baseRevision: options.input.revision,
        }
    await verifyWorkspace(workspace, options.environment)
    return workspace
  }
  try {
    if (options.taskIdentity !== undefined) await mkdir(root, { mode: 0o700 })
    await runGit(['init', '--quiet', repositoryPath], root, environment)
    await runGit(['remote', 'add', 'origin', options.input.source], repositoryPath, environment)
    await runGit(['fetch', '--quiet', '--no-tags', '--depth=1', 'origin', options.input.ref], repositoryPath, environment)
    const fetched = await runGit(['rev-parse', 'FETCH_HEAD'], repositoryPath, environment)
    if (fetched !== options.input.revision) throw new Error('Fetched revision does not match the admitted input')
    let checkoutRevision = fetched
    if (options.candidate !== undefined) {
      await runGit(['fetch', '--quiet', '--no-tags', '--depth=1', 'origin', options.candidate.ref], repositoryPath, environment)
      checkoutRevision = await runGit(['rev-parse', 'FETCH_HEAD'], repositoryPath, environment)
      if (checkoutRevision !== options.candidate.revision) throw new Error('Fetched revision does not match the admitted candidate')
    }
    let workspace: FleetWorkspace
    if (options.mode === 'execution') {
      const prefix = options.branchPrefix ?? 'fleet/run/'
      if (!prefix.endsWith('/') || prefix.includes('..') || /[~^:?*[\\\s]/.test(prefix)) throw new Error('branchPrefix is unsafe')
      const branch = taskBranch ?? `${prefix}${options.runId}`
      await runGit(['checkout', '--quiet', '-b', branch, checkoutRevision], repositoryPath, environment)
      workspace = { mode: 'execution', root, temporaryParent: parent, repositoryPath, source: options.input.source, baseRevision: fetched, branch }
    } else {
      await runGit(['checkout', '--quiet', '--detach', checkoutRevision], repositoryPath, environment)
      workspace = { mode: 'review', root, temporaryParent: parent, repositoryPath, source: options.input.source, baseRevision: fetched }
    }
    await verifyWorkspace(workspace, options.environment)
    return workspace
  } catch (error) {
    await rm(root, { recursive: true, force: true }).catch(() => undefined)
    throw error
  }
}

export async function removeWorkspace(workspace: FleetWorkspace): Promise<void> {
  await verifyWorkspace(workspace)
  await rm(workspace.root, { recursive: true, force: false })
}
