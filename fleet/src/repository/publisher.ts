import { spawn } from 'node:child_process'
import type { FleetWorkspace } from './workspace.js'
import { verifyWorkspace } from './workspace.js'

const GIT_TIMEOUT_MS = 300_000
const GIT_CREDENTIAL_HELPER = '!f() { if [ "$1" = get ]; then printf "%s\\n" "username=pactline-fleet" "password=$PACTLINE_FLEET_GIT_PASSWORD"; fi; }; f'

export interface DeliveryAuthority {
  readonly remote: string
  readonly baseRef: string
  readonly baseRevision: string
  readonly deliveryRef: string
  readonly priorDeliveryRevision?: string
}

export interface PublishedRevision {
  readonly [key: string]: string
  readonly revision: string
  readonly branch: string
}

function safeEnvironment(source: NodeJS.ProcessEnv, credential?: string): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = {}
  for (const key of ['PATH', 'TMPDIR', 'TMP', 'TEMP', 'LANG', 'LC_ALL', 'TZ', 'SSL_CERT_FILE', 'SSL_CERT_DIR']) {
    if (source[key] !== undefined) environment[key] = source[key]
  }
  return {
    ...environment,
    GIT_CONFIG_NOSYSTEM: '1',
    GIT_CONFIG_GLOBAL: '/dev/null',
    GIT_TERMINAL_PROMPT: '0',
    GIT_ASKPASS: '/usr/bin/false',
    ...(credential === undefined ? {} : { PACTLINE_FLEET_GIT_PASSWORD: credential }),
  }
}

function git(
  args: readonly string[],
  cwd: string,
  environment: NodeJS.ProcessEnv,
  allowExitOne = false,
): Promise<{ readonly stdout: string; readonly code: number }> {
  return new Promise((resolve, reject) => {
    const child = spawn('git', ['-c', 'core.hooksPath=/dev/null', ...args], {
      cwd, env: environment, shell: false, stdio: ['ignore', 'pipe', 'pipe'],
    })
    const stdout: Buffer[] = []
    const stderr: Buffer[] = []
    const timer = setTimeout(() => {
      child.kill('SIGTERM')
      reject(new Error(`Fleet Git command timed out: git ${args[0] ?? ''}`))
    }, GIT_TIMEOUT_MS)
    timer.unref()
    child.stdout.on('data', (chunk: Buffer) => stdout.push(chunk))
    child.stderr.on('data', (chunk: Buffer) => stderr.push(chunk))
    child.once('error', reject)
    child.once('close', code => {
      clearTimeout(timer)
      const exitCode = code ?? 1
      if (exitCode !== 0 && !(allowExitOne && exitCode === 1)) {
        reject(new Error(`Fleet Git command failed: ${Buffer.concat(stderr).toString('utf8').trim()}`))
        return
      }
      resolve({ stdout: Buffer.concat(stdout).toString('utf8').trim(), code: exitCode })
    })
  })
}

function branchFromRef(ref: string): string {
  if (!ref.startsWith('refs/heads/') || ref.length <= 'refs/heads/'.length) {
    throw new Error('Fleet delivery requires a branch ref')
  }
  return ref.slice('refs/heads/'.length)
}

async function remoteRevision(
  remote: string,
  ref: string,
  cwd: string,
  environment: NodeJS.ProcessEnv,
): Promise<string | undefined> {
  const output = (await git([
    '-c', 'credential.helper=', '-c', `credential.helper=${GIT_CREDENTIAL_HELPER}`,
    'ls-remote', '--refs', remote, ref,
  ], cwd, environment)).stdout
  if (output === '') return undefined
  const rows = output.split('\n')
  if (rows.length !== 1) throw new Error(`Fleet remote ref is ambiguous: ${ref}`)
  const [revision, actualRef, ...extra] = rows[0]!.split(/\s+/)
  if (extra.length > 0 || actualRef !== ref || !/^[a-f0-9]{40}$/.test(revision ?? '')) {
    throw new Error(`Fleet remote ref observation is invalid: ${ref}`)
  }
  return revision
}

/** Commit only the verified allowlisted Workspace changes under Fleet-owned authorship. */
export async function commitDelivery(
  workspace: FleetWorkspace,
  allowedPaths: readonly string[],
  taskNumber: number,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<PublishedRevision> {
  await verifyWorkspace(workspace, environment)
  if (workspace.mode !== 'execution' || workspace.branch === undefined) throw new Error('Fleet delivery requires an execution Workspace')
  const safe = safeEnvironment(environment)
  await git(['add', '-A', '--', ...allowedPaths], workspace.repositoryPath, safe)
  const staged = await git(['diff', '--cached', '--quiet'], workspace.repositoryPath, safe, true)
  if (staged.code === 0) throw new Error(`Fleet Task ${String(taskNumber)} has no verified changes to commit`)
  await git([
    '-c', 'user.name=Pactline Fleet', '-c', 'user.email=fleet@example.invalid',
    'commit', '--quiet', '-m', `fleet: deliver Task ${String(taskNumber)}`,
  ], workspace.repositoryPath, safe)
  const revision = (await git(['rev-parse', 'HEAD'], workspace.repositoryPath, safe)).stdout
  const branch = (await git(['branch', '--show-current'], workspace.repositoryPath, safe)).stdout
  if (!/^[a-f0-9]{40}$/.test(revision) || branch !== workspace.branch) {
    throw new Error('Fleet committed revision does not match the Task delivery branch')
  }
  return { revision, branch }
}

function verifyDeliveryAuthority(workspace: FleetWorkspace, commit: PublishedRevision, authority: DeliveryAuthority): void {
  branchFromRef(authority.baseRef)
  const branch = branchFromRef(authority.deliveryRef)
  if (authority.remote !== workspace.source || authority.baseRevision !== workspace.baseRevision) {
    throw new Error('Fleet delivery authority does not match the Task Workspace repository revision')
  }
  if (authority.baseRef === authority.deliveryRef || branch !== workspace.branch || commit.branch !== branch) {
    throw new Error('Fleet delivery ref must be the stable non-base Task branch')
  }
}

/** Push one stable Task delivery ref without force and verify the remote refs afterwards. */
export async function pushDelivery(
  workspace: FleetWorkspace,
  commit: PublishedRevision,
  authority: DeliveryAuthority,
  credential: string | undefined,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<PublishedRevision> {
  await verifyWorkspace(workspace, environment)
  verifyDeliveryAuthority(workspace, commit, authority)
  const safe = safeEnvironment(environment, credential)
  const [baseBefore, deliveryBefore] = await Promise.all([
    remoteRevision(authority.remote, authority.baseRef, workspace.repositoryPath, safe),
    remoteRevision(authority.remote, authority.deliveryRef, workspace.repositoryPath, safe),
  ])
  if (baseBefore === undefined) throw new Error('Fleet base ref is missing before delivery push')
  if (deliveryBefore !== authority.priorDeliveryRevision) {
    throw new Error('Fleet delivery ref drifted before delivery push')
  }
  await git([
    '-c', 'credential.helper=', '-c', `credential.helper=${GIT_CREDENTIAL_HELPER}`,
    'push', '--porcelain', authority.remote, `${commit.revision}:${authority.deliveryRef}`,
  ], workspace.repositoryPath, safe)
  return await verifyPublishedDelivery(workspace, commit, authority, credential, environment)
}

/** Reconcile a possibly completed push using the same authoritative remote checks as a fresh delivery. */
export async function verifyPublishedDelivery(
  workspace: FleetWorkspace,
  commit: PublishedRevision,
  authority: DeliveryAuthority,
  credential: string | undefined,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<PublishedRevision> {
  await verifyWorkspace(workspace, environment)
  verifyDeliveryAuthority(workspace, commit, authority)
  const safe = safeEnvironment(environment, credential)
  const [baseAfter, deliveryAfter] = await Promise.all([
    remoteRevision(authority.remote, authority.baseRef, workspace.repositoryPath, safe),
    remoteRevision(authority.remote, authority.deliveryRef, workspace.repositoryPath, safe),
  ])
  if (baseAfter === undefined) throw new Error('Fleet base ref is missing after delivery push')
  if (deliveryAfter !== commit.revision) throw new Error('Fleet delivery ref does not match the committed revision')
  return commit
}
