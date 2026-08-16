import { constants } from 'node:fs'
import { access, lstat, mkdtemp, realpath, rm } from 'node:fs/promises'
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
  readonly mode: WorkspaceMode
  readonly runId: string
  readonly branchPrefix?: string
  readonly temporaryDirectory?: string
  readonly environment?: NodeJS.ProcessEnv
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
  if (relation === '' || relation.startsWith('..') || isAbsolute(relation)) throw new Error('Repository escaped its disposable workspace root')
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

export async function prepareWorkspace(options: PrepareWorkspaceOptions): Promise<FleetWorkspace> {
  if (!RUN_ID_PATTERN.test(options.runId)) throw new Error('runId must be a lowercase branch-safe identifier')
  if (!/^[a-f0-9]{40}$/.test(options.input.revision)) throw new Error('Repository revision must be a lowercase 40-character Git SHA')
  if (options.input.source.trim() === '' || options.input.ref.trim() === '') throw new Error('Repository source and ref must be non-empty')
  try {
    const source = new URL(options.input.source)
    if (source.username !== '' || source.password !== '') throw new Error('Repository source must not contain credentials')
  } catch (error: unknown) {
    if (error instanceof Error && error.message === 'Repository source must not contain credentials') throw error
    if (!isAbsolute(options.input.source)) throw new Error('Repository source must be a credential-free URL or absolute local test path')
  }
  const parent = resolve(options.temporaryDirectory ?? tmpdir())
  const root = await mkdtemp(join(parent, 'pactline-fleet-'))
  const repositoryPath = join(root, 'repository')
  const environment = safeEnvironment(options.environment ?? process.env)
  try {
    await runGit(['init', '--quiet', repositoryPath], root, environment)
    await runGit(['remote', 'add', 'origin', options.input.source], repositoryPath, environment)
    await runGit(['fetch', '--quiet', '--no-tags', '--depth=1', 'origin', options.input.ref], repositoryPath, environment)
    const fetched = await runGit(['rev-parse', 'FETCH_HEAD'], repositoryPath, environment)
    if (fetched !== options.input.revision) throw new Error('Fetched revision does not match the admitted input')
    let workspace: FleetWorkspace
    if (options.mode === 'execution') {
      const prefix = options.branchPrefix ?? 'fleet/run/'
      if (!prefix.endsWith('/') || prefix.includes('..') || /[~^:?*[\\\s]/.test(prefix)) throw new Error('branchPrefix is unsafe')
      const branch = `${prefix}${options.runId}`
      await runGit(['checkout', '--quiet', '-b', branch, fetched], repositoryPath, environment)
      workspace = { mode: 'execution', root, temporaryParent: parent, repositoryPath, source: options.input.source, baseRevision: fetched, branch }
    } else {
      await runGit(['checkout', '--quiet', '--detach', fetched], repositoryPath, environment)
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
