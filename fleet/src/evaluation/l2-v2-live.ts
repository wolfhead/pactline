import { execFile } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { chmod, lstat, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { promisify } from 'node:util'
import { fileURLToPath } from 'node:url'
import { CodexHarnessAdapter, codexAdapterPolicy } from '../adapters/codex/codex-adapter.js'
import { DeepSeekHarnessAdapter, deepSeekAdapterPolicy } from '../adapters/deepseek/deepseek-adapter.js'
import { runCandidateImport, runClaimStage, type ClaimStageResult, type ClaimWorkflowStage } from '../core/claim-stage.js'
import type { HarnessRunEvent } from '../core/harness-adapter.js'
import type { ExecutionProposal, ReviewProposal } from '../core/harness-result.js'
import { StaticRuntimeRouter, type RuntimeRoutes } from '../core/runtime-router.js'
import { assertAllowedPaths, observeGit, runFixedVerification } from '../core/verification.js'
import type { FleetWorkDefinition } from '../core/work-definition.js'
import { PactlineCLI } from '../pactline/client.js'
import { resolveTypedIssue, type ResolvedIssueAuthority } from '../pactline/settlement.js'
import type { RepositoryDelivery, RepositoryIdentity } from '../repository/delivery.js'
import {
  prepareWorkspace,
  removeWorkspace,
  verifyWorkspace,
  type FleetWorkspace,
  type RepositoryRevision,
} from '../repository/workspace.js'
import { runL2V2HiddenVerification, type HiddenVerificationEvidence } from './l2-v2-hidden.js'
import { loadL2V2Spec, type L2V2CaseSpec, type L2V2Spec } from './l2-v2-spec.js'
import { LocalDevelopmentAPI, type L2V2ProvisionManifest, type ProvisionedCase } from './l2-v2-provision.js'

const exec = promisify(execFile)
const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')
const repositoryRoot = resolve(fleetRoot, '..')
const AUTHORITY_FILE = '.recovery-authority.json'

interface RecoveryAuthorityFile {
  readonly schemaVersion: 1
  readonly cohortId: string
  readonly server: string
  readonly tokenId: string
  readonly token: string
}

interface StageRun {
  readonly result: ClaimStageResult
  readonly delivery?: RepositoryDelivery
  readonly workspaceBranch?: string
  readonly evidenceDirectory: string
  readonly hidden?: HiddenVerificationEvidence
}

export interface L2V2CodexCaseResult {
  readonly caseId: string
  readonly taskNumber: number
  readonly outcome: string
  readonly reviewCycle: number
  readonly delivery?: RepositoryDelivery
  readonly evidenceDirectory: string
}

export interface L2V2LiveOptions {
  readonly caseId: string
  readonly server?: string
  readonly manifestPath?: string
  readonly mirrorPath?: string
  readonly provisionedCase?: ProvisionedCase
  readonly routeSelection?: L2V2RouteSelection
  readonly runPrefix?: string
  readonly pactlineExecutable?: string
  readonly resultRoot?: string
  readonly environment?: NodeJS.ProcessEnv
  readonly log?: (message: string) => void
}

export interface L2V2RouteSelection {
  readonly execution: 'codex' | 'deepseek'
  readonly review: 'codex' | 'deepseek'
}

export interface L2V2ReviewResumeOptions extends L2V2LiveOptions {
  readonly runDirectory: string
  readonly existingClaimId?: string
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function integer(value: unknown, name: string): number {
  if (!Number.isSafeInteger(value) || Number(value) < 1) throw new Error(`${name} must be a positive integer`)
  return Number(value)
}

function text(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`${name} must be non-empty text`)
  return value
}

function safeError(error: unknown): string {
  return (error instanceof Error ? error.message : String(error))
    .replace(/\b(?:sk|bb_pat|gh[opsu])[-_][A-Za-z0-9._-]+\b/g, '[REDACTED]')
    .replace(/Bearer\s+\S+/gi, 'Bearer [REDACTED]')
    .slice(0, 4_096)
}

async function writePrivateJSON(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 })
  await chmod(dirname(path), 0o700)
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
  await chmod(path, 0o600)
}

async function loadProvisionManifest(path: string): Promise<L2V2ProvisionManifest> {
  const info = await lstat(path)
  if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) throw new Error('L2 v2 manifest must be a private regular file')
  const value = JSON.parse(await readFile(path, 'utf8')) as L2V2ProvisionManifest
  if (value.schemaVersion !== 1 || value.status !== 'provisioned' || value.pactline === undefined || value.cases.length !== 6) {
    throw new Error('L2 v2 corpus is not fully provisioned')
  }
  return value
}

async function validateMirror(path: string, manifest: L2V2ProvisionManifest): Promise<void> {
  const info = await lstat(path)
  if (!info.isDirectory() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) {
    throw new Error('L2 v2 repository mirror must be a private real directory')
  }
  for (const [ref, revision] of Object.entries(manifest.repository.createdRefs)) {
    const actual = await command('git', ['rev-parse', ref], path, process.env)
    if (actual !== revision) throw new Error(`L2 v2 repository mirror drifted at ${ref}`)
  }
}

function repositoryIdentity(spec: L2V2Spec): RepositoryIdentity {
  const parsed = new URL(spec.repository.url)
  const [owner, name] = parsed.pathname.replace(/^\//, '').replace(/\/$/, '').split('/')
  if (parsed.hostname !== 'github.com' || owner === undefined || name === undefined) throw new Error('L2 v2 requires the frozen GitHub repository')
  return { provider: 'github', host: parsed.hostname, owner, name }
}

function definition(spec: L2V2Spec, item: L2V2CaseSpec, provisioned: ProvisionedCase): FleetWorkDefinition {
  const repository = repositoryIdentity(spec)
  return {
    caseId: item.caseId, taskNumber: provisioned.taskNumber, taskVersion: provisioned.taskVersion,
    base: { source: spec.repository.url, ref: provisioned.baseRef, revision: provisioned.baseRevision },
    repository, allowedPaths: item.allowedPaths, verificationCommands: item.verificationCommands,
    criteria: provisioned.criteria.map(value => ({ id: value.id, revision: value.revision })),
    ...(provisioned.candidateRef === undefined || provisioned.candidateRevision === undefined || provisioned.seededDraftPullRequest === undefined ? {} : {
      candidate: {
        repository, ref: provisioned.candidateRef, revision: provisioned.candidateRevision,
        branch: provisioned.candidateRef.replace('refs/heads/', ''), codeChangeUrl: provisioned.seededDraftPullRequest,
      },
    }),
  }
}

function routes(selection: L2V2RouteSelection = { execution: 'codex', review: 'codex' }): RuntimeRoutes {
  const route = (adapterId: 'codex' | 'deepseek') => ({
    adapterId,
    model: adapterId === 'codex' ? codexAdapterPolicy.model : deepSeekAdapterPolicy.model,
    reasoning: adapterId === 'codex' ? codexAdapterPolicy.reasoning : deepSeekAdapterPolicy.reasoning,
    promptVersion: 'fleet-m4-l2-v2', resultContractVersion: 1,
  })
  return {
    execution: route(selection.execution), correction: route(selection.execution),
    review: route(selection.review), resolution_analysis: route(selection.execution),
  }
}

function runtimeRouter(environment: NodeJS.ProcessEnv, selection: L2V2RouteSelection): StaticRuntimeRouter {
  const adapterIds = new Set([selection.execution, selection.review])
  return new StaticRuntimeRouter([
    ...(adapterIds.has('codex') ? [new CodexHarnessAdapter({ environment, workspaceSandbox: 'danger-full-access' })] : []),
    ...(adapterIds.has('deepseek') ? [new DeepSeekHarnessAdapter({ environment, maxTokens: 32_768 })] : []),
  ], routes(selection))
}

async function ensureAuthority(
  api: LocalDevelopmentAPI,
  path: string,
  cohortId: string,
  server: string,
): Promise<RecoveryAuthorityFile> {
  try {
    const info = await lstat(path)
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) throw new Error('M4 recovery authority must be private')
    const value = JSON.parse(await readFile(path, 'utf8')) as RecoveryAuthorityFile
    if (value.schemaVersion !== 1 || value.cohortId !== cohortId || value.server !== server
      || typeof value.tokenId !== 'string' || typeof value.token !== 'string') throw new Error('M4 recovery authority is invalid')
    return value
  } catch (error: unknown) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error
  }
  const issued = record((await api.request('/api/account/tokens', {
    method: 'POST', body: JSON.stringify({ name: `pactline-fleet-m4:${cohortId}`, scopes: ['work:execute'], expires_in_days: 30 }),
  })).value, 'Issued Token')
  const authority: RecoveryAuthorityFile = {
    schemaVersion: 1, cohortId, server,
    tokenId: text(issued.id, 'Token ID'), token: text(issued.token, 'Token value'),
  }
  await writePrivateJSON(path, authority)
  return authority
}

async function admitReady(api: LocalDevelopmentAPI, item: ProvisionedCase): Promise<number> {
  const task = record((await api.request(`/api/v1/tasks/${String(item.taskNumber)}`)).value, 'Pactline Task')
  if (task.phase === 'ready' && (task.activity == null || task.activity === '')) return integer(task.version, 'Ready Task version')
  if (task.phase !== 'backlog' || task.activity != null) throw new Error(`${item.caseId} is not an admissible Backlog Task`)
  const version = integer(task.version, 'Backlog Task version')
  const admitted = record((await api.request(`/api/v1/tasks/${String(item.taskNumber)}/commands/mark-ready`, {
    method: 'POST', headers: { 'If-Match': `"${String(version)}"` },
  }, `fleet-m4-${item.caseId.toLowerCase()}-ready`)).value, 'Ready command')
  if (admitted.phase !== 'ready' || (admitted.activity != null && admitted.activity !== '')) throw new Error('Pactline did not admit the Task as ready')
  return integer(admitted.version, 'Admitted Task version')
}

async function command(executable: string, args: readonly string[], cwd: string, environment: NodeJS.ProcessEnv, timeout = 300_000): Promise<string> {
  try {
    const result = await exec(executable, [...args], { cwd, env: environment, timeout, maxBuffer: 2 * 1024 * 1024 })
    return result.stdout.trim()
  } catch (error: unknown) {
    const item = error as { stderr?: string; stdout?: string }
    throw new Error(`${executable} failed: ${safeError(item.stderr || item.stdout || error)}`)
  }
}

async function publishDelivery(
  workspace: FleetWorkspace,
  spec: L2V2Spec,
  item: L2V2CaseSpec,
  runId: string,
  mirrorPath: string,
  environment: NodeJS.ProcessEnv,
): Promise<RepositoryDelivery> {
  const branch = text(workspace.branch, 'Execution branch')
  if (!branch.startsWith(spec.repository.branchPrefix + 'run/')) throw new Error('Delivery branch escaped the L2 v2 namespace')
  const status = await command('git', ['status', '--porcelain=v1', '--untracked-files=all'], workspace.repositoryPath, environment)
  if (status !== '') {
    await command('git', ['add', '-A', '--', ...item.allowedPaths], workspace.repositoryPath, environment)
    await command('git', ['-c', 'user.name=Pactline Fleet', '-c', 'user.email=fleet@example.invalid', 'commit', '-m', `[Fleet ${item.caseId}] evaluation delivery`], workspace.repositoryPath, environment)
  }
  const revision = await command('git', ['rev-parse', 'HEAD'], workspace.repositoryPath, environment)
  if (revision === workspace.baseRevision) throw new Error('Execution produced no commit to publish')
  await command('git', [
    'push', '--porcelain', `--force-with-lease=refs/heads/${branch}:`, mirrorPath, `HEAD:refs/heads/${branch}`,
  ], workspace.repositoryPath, environment)
  const repo = repositoryIdentity(spec)
  const ssh = `git@${repo.host}:${repo.owner}/${repo.name}.git`
  await command('git', [
    'push', '--porcelain', `--force-with-lease=refs/heads/${branch}:`, ssh, `HEAD:refs/heads/${branch}`,
  ], workspace.repositoryPath, environment, 180_000)
  const base = `fleet-eval/l2-v2/base/${item.caseId.toLowerCase()}`
  const title = `[Fleet ${item.caseId}] Codex evaluation delivery`
  const body = `Isolated Pactline Fleet M4 delivery for ${item.caseId}.\n\nRun: ${runId}\n\nThis Draft Pull Request must not be merged.`
  const url = (await command('gh', [
    'pr', 'create', '--repo', `${repo.owner}/${repo.name}`, '--base', base, '--head', branch,
    '--draft', '--title', title, '--body', body,
  ], workspace.repositoryPath, environment, 120_000)).split('\n').find(line => /^https:\/\/github\.com\//.test(line))
  if (url === undefined) throw new Error('GitHub did not return the delivery Draft Pull Request URL')
  const viewed = record(JSON.parse(await command('gh', [
    'pr', 'view', url, '--repo', `${repo.owner}/${repo.name}`,
    '--json', 'url,isDraft,state,baseRefName,headRefName,headRefOid',
  ], workspace.repositoryPath, environment)) as unknown, 'Delivery Draft Pull Request')
  if (viewed.url !== url || viewed.isDraft !== true || viewed.state !== 'OPEN' || viewed.baseRefName !== base
    || viewed.headRefName !== branch || viewed.headRefOid !== revision) throw new Error('Delivery Draft Pull Request evidence drifted')
  return { repository: repo, codeChangeUrl: url, revision, branch }
}

async function runStage(options: {
  readonly client: PactlineCLI
  readonly router: StaticRuntimeRouter
  readonly spec: L2V2Spec
  readonly item: L2V2CaseSpec
  readonly definition: FleetWorkDefinition
  readonly stage: ClaimWorkflowStage
  readonly taskVersion: number
  readonly input: RepositoryRevision
  readonly workspaceSource: string
  readonly runId: string
  readonly clientSessionId: string
  readonly evidenceDirectory: string
  readonly environment: NodeJS.ProcessEnv
  readonly resolutionAuthority?: ResolvedIssueAuthority
  readonly existingClaimId?: string
  readonly resumeRuntimeSessionId?: string
  readonly retainedWorkspace?: FleetWorkspace
  readonly log: (message: string) => void
}): Promise<StageRun> {
  await mkdir(options.evidenceDirectory, { recursive: true, mode: 0o700 })
  const mode = options.stage === 'review' ? 'review' : 'execution'
  const workspace = options.retainedWorkspace ?? await prepareWorkspace({
    input: { ...options.input, source: options.workspaceSource }, mode,
    runId: `${String(options.definition.taskNumber)}-${options.runId}`,
    branchPrefix: `${options.spec.repository.branchPrefix}run/`, environment: options.environment,
  })
  if (options.retainedWorkspace !== undefined) await verifyWorkspace(workspace, options.environment)
  if (mode === 'review') {
    await command('git', ['fetch', '--quiet', '--no-tags', 'origin', options.definition.base.ref], workspace.repositoryPath, options.environment)
  }
  const events: HarnessRunEvent[] = []
  let hidden: HiddenVerificationEvidence | undefined
  let delivery: RepositoryDelivery | undefined
  let completed = false
  await writePrivateJSON(join(options.evidenceDirectory, 'intent.json'), {
    schemaVersion: 1, caseId: options.item.caseId, taskNumber: options.definition.taskNumber,
    runId: options.runId, stage: options.stage, workspace: workspace.repositoryPath, input: options.input,
    startedAt: new Date().toISOString(),
  })
  try {
    const result = await runClaimStage({
      client: options.client, router: options.router, definition: options.definition,
      stage: options.stage, taskVersion: options.taskVersion, runId: options.runId,
      clientSessionId: options.clientSessionId, idempotencyKey: options.runId, workspace,
      deadline: new Date(Date.now() + 20 * 60_000).toISOString(),
      ...(options.resolutionAuthority === undefined ? {} : { resolutionAuthority: options.resolutionAuthority }),
      ...(options.existingClaimId === undefined ? {} : { existingClaimId: options.existingClaimId }),
      ...(options.resumeRuntimeSessionId === undefined ? {} : { resumeRuntimeSessionId: options.resumeRuntimeSessionId }),
      onRuntimeSession: async (runtimeSessionId, dispatch) => {
        await writePrivateJSON(join(options.evidenceDirectory, 'session.json'), {
          schemaVersion: 1, runId: options.runId, runtimeSessionId, claimId: dispatch.claimId, claimStage: options.stage,
        })
        options.log(`${options.item.caseId} ${options.stage} Session ${runtimeSessionId}`)
      },
      onEvent: event => { events.push(event) },
      onHarnessResult: async result => { await writePrivateJSON(join(options.evidenceDirectory, 'raw-result.json'), result) },
      validateObservation: async (_dispatch, proposal) => {
        if (proposal.recommendation === 'request_resolution') {
          if (options.item.expectedPath !== 'resolution_accept') throw new Error('Unexpected resolution request blocks settlement for this case')
          return
        }
        if (proposal.recommendation === 'unable_to_complete') throw new Error('Harness was unable to complete the admitted stage')
        const expected = proposal.kind === 'review' && proposal.recommendation === 'request_changes'
          ? options.item.expectedPath === 'changes_correction_accept' ? 'failed' : 'observed'
          : 'passed'
        hidden = await runL2V2HiddenVerification(workspace.repositoryPath, options.item.hiddenProfile, options.environment, expected)
      },
      publishDelivery: async () => {
        delivery = await publishDelivery(
          workspace, options.spec, options.item, options.runId, options.workspaceSource, options.environment,
        )
        return delivery
      },
    })
    await writePrivateJSON(join(options.evidenceDirectory, 'result.json'), result.harnessResult)
    await writePrivateJSON(join(options.evidenceDirectory, 'events.json'), events)
    await writePrivateJSON(join(options.evidenceDirectory, 'observation.json'), result.observation)
    if (hidden !== undefined) await writePrivateJSON(join(options.evidenceDirectory, 'hidden.json'), hidden)
    if (delivery !== undefined) await writePrivateJSON(join(options.evidenceDirectory, 'delivery.json'), delivery)
    completed = true
    return {
      result, ...(delivery === undefined ? {} : { delivery }),
      ...(workspace.branch === undefined ? {} : { workspaceBranch: workspace.branch }),
      evidenceDirectory: options.evidenceDirectory, ...(hidden === undefined ? {} : { hidden }),
    }
  } catch (error: unknown) {
    await writePrivateJSON(join(options.evidenceDirectory, 'failure.json'), {
      schemaVersion: 1, failedAt: new Date().toISOString(), error: safeError(error),
      workspace: workspace.repositoryPath, events,
    })
    throw error
  } finally {
    if (completed) await removeWorkspace(workspace)
  }
}

async function candidateImport(options: {
  readonly client: PactlineCLI
  readonly spec: L2V2Spec
  readonly item: L2V2CaseSpec
  readonly definition: FleetWorkDefinition
  readonly taskVersion: number
  readonly workspaceSource: string
  readonly clientSessionId: string
  readonly evidenceDirectory: string
  readonly environment: NodeJS.ProcessEnv
}): Promise<{ readonly taskVersion: number; readonly delivery: RepositoryDelivery }> {
  const candidate = options.definition.candidate
  if (candidate === undefined) throw new Error('Candidate import requires a frozen candidate')
  const workspace = await prepareWorkspace({
    input: { source: options.workspaceSource, ref: candidate.ref, revision: candidate.revision },
    mode: 'review', runId: `import-${options.item.caseId.toLowerCase()}`, environment: options.environment,
  })
  try {
    await command('git', ['fetch', '--quiet', '--no-tags', 'origin', options.definition.base.ref], workspace.repositoryPath, options.environment)
    const git = await observeGit(workspace.repositoryPath, options.definition.base.revision, options.environment)
    assertAllowedPaths(git.changedPaths, options.definition.allowedPaths)
    if (git.changedPaths.length === 0) throw new Error('Frozen candidate diff is empty')
    const visible = await runFixedVerification(workspace.repositoryPath, options.definition.verificationCommands, { environment: options.environment, timeoutMs: 600_000 })
    await writePrivateJSON(join(options.evidenceDirectory, 'candidate-visible.json'), { git, visible, candidate })
    if (visible.some(value => value.outcome !== 'passed')) {
      throw new Error(`Frozen candidate visible verification failed: ${visible.map(value => value.summary).join('; ')}`)
    }
    const hiddenExpected = options.item.expectedPath === 'changes_correction_accept' ? 'failed' : 'passed'
    const hidden = await runL2V2HiddenVerification(workspace.repositoryPath, options.item.hiddenProfile, options.environment, hiddenExpected)
    await writePrivateJSON(join(options.evidenceDirectory, 'candidate-import.json'), { git, visible, hidden, candidate })
    const criteria = options.definition.criteria.map((criterion, index) => ({
      criterionId: criterion.id, criterionRevision: criterion.revision,
      outcome: options.item.expectedPath === 'changes_correction_accept' && index === 0 ? 'failed' as const : 'passed' as const,
      evidence: options.item.expectedPath === 'changes_correction_accept' && index === 0
        ? 'Coordinator hidden matrix reproduced the seeded stage/outcome defect.'
        : 'Coordinator visible and hidden candidate checks passed for this criterion.',
    }))
    const settlement = await runCandidateImport({
      client: options.client, definition: options.definition, taskVersion: options.taskVersion,
      clientSessionId: options.clientSessionId,
      idempotencyKey: `m4-task-${String(options.definition.taskNumber)}-${options.item.caseId.toLowerCase()}-import`,
      summary: `Coordinator imported frozen candidate ${candidate.revision} for independent review.`,
      criteria, delivery: candidate,
    })
    if (settlement.task.phase !== 'in_review' || settlement.task.activity !== 'available') throw new Error('Candidate import did not reach Review')
    return { taskVersion: settlement.task.version, delivery: candidate }
  } finally {
    await removeWorkspace(workspace)
  }
}

async function assertNoRemoteDelivery(spec: L2V2Spec, branch: string, environment: NodeJS.ProcessEnv): Promise<void> {
  const repo = repositoryIdentity(spec)
  const matches = JSON.parse(await command('gh', [
    'api', `repos/${repo.owner}/${repo.name}/git/matching-refs/heads/${branch}`,
  ], fleetRoot, environment)) as unknown
  if (!Array.isArray(matches) || matches.some(value => record(value, 'GitHub ref').ref === `refs/heads/${branch}`)) {
    throw new Error('Typed-resolution case created a remote branch before resolution')
  }
}

function reviewCycle(settlement: ClaimStageResult['settlement']): number {
  return settlement.task.phase === 'done' ? 1 : 0
}

/** Run one frozen L2 v2 case through the real Codex Adapter and authoritative Pactline lifecycle. */
export async function runCodexL2V2Case(options: L2V2LiveOptions): Promise<L2V2CodexCaseResult> {
  const environment = options.environment ?? process.env
  const log = options.log ?? (message => { process.stdout.write(`${message}\n`) })
  const spec = await loadL2V2Spec(join(fleetRoot, 'evaluation/cases/l2-v2.json'))
  const manifestPath = resolve(options.manifestPath ?? join(fleetRoot, '.fleet/l2-v2/corpus-manifest.json'))
  const manifest = await loadProvisionManifest(manifestPath)
  const mirrorPath = resolve(options.mirrorPath ?? join(dirname(manifestPath), 'repository-mirror.git'))
  await validateMirror(mirrorPath, manifest)
  const item = spec.cases.find(value => value.caseId === options.caseId)
  const provisioned = options.provisionedCase ?? manifest.cases.find(value => value.caseId === options.caseId)
  if (item === undefined || provisioned === undefined) throw new Error(`Unknown L2 v2 case: ${options.caseId}`)
  const work = definition(spec, item, provisioned)
  const server = (options.server ?? environment.PACTLINE_LOCAL_SERVER ?? manifest.server).replace(/\/$/, '')
  if (server !== manifest.server) throw new Error('L2 v2 server does not match the frozen manifest')
  const resultRoot = resolve(options.resultRoot ?? join(fleetRoot, '.fleet/l2-v2/runs'))
  const runPrefix = options.runPrefix ?? `m4-${item.caseId.toLowerCase()}`
  if (!/^[a-z0-9][a-z0-9-]{0,20}$/.test(runPrefix)) throw new Error('L2 v2 run prefix is unsafe or too long')
  const overallRunId = `${runPrefix}-${randomUUID()}`
  const evidenceDirectory = join(resultRoot, overallRunId)
  await mkdir(evidenceDirectory, { recursive: true, mode: 0o700 })
  const api = new LocalDevelopmentAPI(server)
  let failure: unknown
  try {
    await api.login()
    const authority = await ensureAuthority(api, join(dirname(manifestPath), AUTHORITY_FILE), manifest.cohortId, server)
    const pactlineExecutable = resolve(options.pactlineExecutable ?? join(repositoryRoot, 'bin/pactline'))
    const clientSessionId = overallRunId
    const client = new PactlineCLI({ executable: pactlineExecutable, server, clientKind: 'pactline-fleet-m4-l2-v2' }, {
      environment: { ...environment, PACTLINE_TOKEN: authority.token },
    })
    await client.preflight({ sessionId: clientSessionId })
    const routeSelection = options.routeSelection ?? { execution: 'codex', review: 'codex' }
    const router = runtimeRouter(environment, routeSelection)
    let taskVersion = await admitReady(api, provisioned)
    log(`${item.caseId} admitted as Task #${String(provisioned.taskNumber)} version ${String(taskVersion)}`)
    let delivery: RepositoryDelivery | undefined
    let terminal: ClaimStageResult

    if (item.expectedPath === 'clean_review_accept' || item.expectedPath === 'changes_correction_accept') {
      const imported = await candidateImport({
        client, spec, item, definition: work, taskVersion, clientSessionId,
        evidenceDirectory, environment, workspaceSource: mirrorPath,
      })
      taskVersion = imported.taskVersion; delivery = imported.delivery
      const firstReview = await runStage({
        client, router, spec, item, definition: work, stage: 'review', taskVersion,
        input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
        workspaceSource: mirrorPath,
        runId: `${overallRunId}-r1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'review-1'), environment, log,
      })
      if (item.expectedPath === 'clean_review_accept') {
        if (firstReview.result.proposal.recommendation !== 'accept' || firstReview.result.settlement.task.phase !== 'done') {
          throw new Error('Clean review control was not accepted')
        }
        terminal = firstReview.result
      } else {
        if (firstReview.result.proposal.recommendation !== 'request_changes'
          || firstReview.result.settlement.task.phase !== 'in_progress' || firstReview.result.settlement.task.activity !== 'available') {
          throw new Error('Defective seeded candidate did not produce request_changes')
        }
        const correction = await runStage({
          client, router, spec, item, definition: work, stage: 'correction', taskVersion: firstReview.result.settlement.task.version,
          input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
          workspaceSource: mirrorPath,
          runId: `${overallRunId}-c1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'correction-1'), environment, log,
        })
        if (correction.result.proposal.recommendation !== 'complete' || correction.delivery === undefined
          || correction.result.settlement.task.phase !== 'in_review') throw new Error('Correction did not produce a reviewable delivery')
        delivery = correction.delivery
        const finalReview = await runStage({
          client, router, spec, item, definition: work, stage: 'review', taskVersion: correction.result.settlement.task.version,
          input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
          workspaceSource: mirrorPath,
          runId: `${overallRunId}-r2`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'review-2'), environment, log,
        })
        if (finalReview.result.proposal.recommendation !== 'accept' || finalReview.result.settlement.task.phase !== 'done') {
          throw new Error('Corrected delivery did not reach accepted Review')
        }
        terminal = finalReview.result
      }
    } else if (item.expectedPath === 'resolution_accept') {
      const request = await runStage({
        client, router, spec, item, definition: work, stage: 'execution', taskVersion,
        input: work.base, runId: `${overallRunId}-q1`, clientSessionId,
        workspaceSource: mirrorPath,
        evidenceDirectory: join(evidenceDirectory, 'resolution-request'), environment, log,
      })
      if (request.result.proposal.recommendation !== 'request_resolution'
        || request.result.settlement.task.activity !== 'needs_resolution' || request.delivery !== undefined
        || request.workspaceBranch === undefined) throw new Error('Typed-resolution control did not open a mutation-free Issue')
      await assertNoRemoteDelivery(spec, request.workspaceBranch, environment)
      const packet = await client.showTask(work.taskNumber, 40, { sessionId: clientSessionId })
      const blockedTask = record(packet.data.task, 'Blocked Task')
      const activeIssue = record(packet.data.active_issue_thread, 'Active Issue Thread')
      const issueThread = record(activeIssue.thread, 'Issue Thread')
      if (blockedTask.activity !== 'needs_resolution' || issueThread.id === undefined || issueThread.version === undefined) {
        throw new Error('Typed-resolution Issue packet is invalid')
      }
      const resolution = item.resolution
      if (resolution === undefined) throw new Error('Typed-resolution case has no frozen conclusion')
      const waived = work.criteria[resolution.supersededCriterionPosition]
      if (waived === undefined) throw new Error('Superseded criterion no longer exists')
      const resolutionAuthority = await resolveTypedIssue(client, {
        taskNumber: work.taskNumber, taskVersion: integer(blockedTask.version, 'Blocked Task version'),
        issueThreadId: text(issueThread.id, 'Issue Thread ID'), threadVersion: integer(issueThread.version, 'Issue Thread version'),
        conclusion: resolution.conclusion, waivedCriterionIds: [waived.id], sessionId: clientSessionId,
        idempotencyKey: `${overallRunId}-resolve`,
      })
      taskVersion = resolutionAuthority.resolvedAtTaskVersion
      await writePrivateJSON(join(evidenceDirectory, 'resolution.json'), {
        issueThreadId: resolutionAuthority.issueThreadId, conclusion: resolution.conclusion,
        resolvedAtTaskVersion: taskVersion, waivedCriterionIds: resolutionAuthority.waivedCriterionIds,
        preResolutionRemoteDelivery: false,
      })
      const execution = await runStage({
        client, router, spec, item, definition: work, stage: 'correction', taskVersion, input: work.base,
        workspaceSource: mirrorPath,
        runId: `${overallRunId}-e1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'execution'),
        environment, resolutionAuthority, log,
      })
      if (execution.result.proposal.recommendation !== 'complete' || execution.delivery === undefined
        || execution.result.proposal.criteria[resolution.supersededCriterionPosition]?.outcome !== 'waived') {
        throw new Error('Post-resolution execution did not honor the frozen waiver')
      }
      delivery = execution.delivery
      const review = await runStage({
        client, router, spec, item, definition: work, stage: 'review', taskVersion: execution.result.settlement.task.version,
        input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
        workspaceSource: mirrorPath,
        runId: `${overallRunId}-r1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'review'),
        environment, resolutionAuthority, log,
      })
      if (review.result.proposal.recommendation !== 'accept' || review.result.settlement.task.phase !== 'done'
        || review.result.proposal.criteria[resolution.supersededCriterionPosition]?.outcome !== 'waived') {
        throw new Error('Post-resolution Review did not honor the frozen waiver')
      }
      terminal = review.result
    } else {
      const execution = await runStage({
        client, router, spec, item, definition: work, stage: 'execution', taskVersion, input: work.base,
        workspaceSource: mirrorPath,
        runId: `${overallRunId}-e1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'execution'), environment, log,
      })
      if (execution.result.proposal.recommendation !== 'complete' || execution.delivery === undefined
        || execution.result.settlement.task.phase !== 'in_review') throw new Error('Execution did not produce a reviewable delivery')
      delivery = execution.delivery
      let review = await runStage({
        client, router, spec, item, definition: work, stage: 'review', taskVersion: execution.result.settlement.task.version,
        input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
        workspaceSource: mirrorPath,
        runId: `${overallRunId}-r1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'review-1'), environment, log,
      })
      if (review.result.proposal.recommendation === 'request_changes' && item.caseId === 'L2V2-03') {
        const correction = await runStage({
          client, router, spec, item, definition: work, stage: 'correction', taskVersion: review.result.settlement.task.version,
          input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
          workspaceSource: mirrorPath,
          runId: `${overallRunId}-c1`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'correction-1'), environment, log,
        })
        if (correction.delivery === undefined || correction.result.proposal.recommendation !== 'complete') throw new Error('Bounded correction failed')
        delivery = correction.delivery
        review = await runStage({
          client, router, spec, item, definition: work, stage: 'review', taskVersion: correction.result.settlement.task.version,
          input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
          workspaceSource: mirrorPath,
          runId: `${overallRunId}-r2`, clientSessionId, evidenceDirectory: join(evidenceDirectory, 'review-2'), environment, log,
        })
      }
      if (review.result.proposal.recommendation !== 'accept' || review.result.settlement.task.phase !== 'done') {
        throw new Error('Independent Review did not accept the delivery')
      }
      terminal = review.result
    }

    const finalPacket = await client.showTask(work.taskNumber, 40, { sessionId: clientSessionId })
    const finalTask = record(finalPacket.data.task, 'Final Task')
    if (finalTask.phase !== 'done' || finalTask.activity != null) throw new Error('Final authoritative Task is not done')
    const cycle = integer(finalTask.review_cycle, 'Final review cycle')
    const summary: L2V2CodexCaseResult = {
      caseId: item.caseId, taskNumber: work.taskNumber, outcome: text(terminal.settlement.claim.outcome, 'Terminal Claim outcome'),
      reviewCycle: cycle, ...(delivery === undefined ? {} : { delivery }), evidenceDirectory,
    }
    await writePrivateJSON(join(evidenceDirectory, 'coordinator.json'), {
      schemaVersion: 1, runId: overallRunId, completedAt: new Date().toISOString(), ...summary,
      terminalTask: finalTask, terminalClaim: terminal.settlement.claim,
    })
    log(`${item.caseId} PASS at review cycle ${String(cycle)}; evidence ${evidenceDirectory}`)
    return summary
  } catch (error: unknown) {
    failure = error
    await writePrivateJSON(join(evidenceDirectory, 'coordinator-failure.json'), {
      schemaVersion: 1, runId: overallRunId, caseId: item.caseId, failedAt: new Date().toISOString(), error: safeError(error),
      recoveryAuthority: 'retained',
    }).catch(() => undefined)
    throw error
  } finally {
    try { await api.logout() } catch (error: unknown) {
      if (failure === undefined) throw error
    }
  }
}

/** Resume an active execution Claim in the exact retained Codex Session and workspace after a rejected proposal. */
export async function resumeCodexL2V2Execution(options: L2V2ReviewResumeOptions): Promise<L2V2CodexCaseResult> {
  const environment = options.environment ?? process.env
  const log = options.log ?? (message => { process.stdout.write(`${message}\n`) })
  const spec = await loadL2V2Spec(join(fleetRoot, 'evaluation/cases/l2-v2.json'))
  const manifestPath = resolve(options.manifestPath ?? join(fleetRoot, '.fleet/l2-v2/corpus-manifest.json'))
  const manifest = await loadProvisionManifest(manifestPath)
  const mirrorPath = resolve(options.mirrorPath ?? join(dirname(manifestPath), 'repository-mirror.git'))
  await validateMirror(mirrorPath, manifest)
  const item = spec.cases.find(value => value.caseId === options.caseId)
  const provisioned = manifest.cases.find(value => value.caseId === options.caseId)
  if (item === undefined || provisioned === undefined) throw new Error(`Unknown L2 v2 case: ${options.caseId}`)
  if (!['direct_accept', 'bounded_correction_accept'].includes(item.expectedPath)) {
    throw new Error('Execution resume is only valid for direct execution cases')
  }
  const resultRoot = resolve(options.resultRoot ?? join(fleetRoot, '.fleet/l2-v2/runs'))
  const runDirectory = resolve(options.runDirectory)
  if (!runDirectory.startsWith(`${resultRoot}/`)
    || !/^m4-l2v2-[0-9]{2}-[a-f0-9-]{36}$/.test(runDirectory.split('/').at(-1) ?? '')) {
    throw new Error('Execution resume directory is outside the private L2 v2 result root')
  }
  const intent = record(JSON.parse(await readFile(join(runDirectory, 'execution/intent.json'), 'utf8')) as unknown, 'Execution intent')
  const failure = record(JSON.parse(await readFile(join(runDirectory, 'execution/failure.json'), 'utf8')) as unknown, 'Execution failure')
  const session = record(JSON.parse(await readFile(join(runDirectory, 'execution/session.json'), 'utf8')) as unknown, 'Execution Session')
  const retainedRepository = resolve(text(failure.workspace, 'Retained workspace'))
  const retainedRoot = dirname(retainedRepository)
  const branch = await command('git', ['symbolic-ref', '--quiet', '--short', 'HEAD'], retainedRepository, environment)
  const retainedWorkspace: FleetWorkspace = {
    mode: 'execution', root: retainedRoot, temporaryParent: dirname(retainedRoot), repositoryPath: retainedRepository,
    source: mirrorPath, baseRevision: provisioned.baseRevision, branch,
  }
  await verifyWorkspace(retainedWorkspace, environment)
  if (!branch.startsWith(`${spec.repository.branchPrefix}run/${String(provisioned.taskNumber)}-`)
    || intent.stage !== 'execution' || intent.caseId !== item.caseId) throw new Error('Retained execution workspace identity drifted')
  const claimId = text(session.claimId, 'Execution Claim ID')
  if (options.existingClaimId !== undefined && options.existingClaimId !== claimId) throw new Error('Explicit Claim does not match retained Session')
  const runtimeSessionId = text(session.runtimeSessionId, 'Codex runtime Session ID')
  const runId = text(session.runId, 'Execution run ID')
  const server = (options.server ?? environment.PACTLINE_LOCAL_SERVER ?? manifest.server).replace(/\/$/, '')
  if (server !== manifest.server) throw new Error('L2 v2 server does not match the frozen manifest')
  const api = new LocalDevelopmentAPI(server)
  let caught: unknown
  try {
    await api.login()
    const authority = await ensureAuthority(api, join(dirname(manifestPath), AUTHORITY_FILE), manifest.cohortId, server)
    const clientSessionId = `${runDirectory.split('/').at(-1)!}-execution-resume`
    const client = new PactlineCLI({
      executable: resolve(options.pactlineExecutable ?? join(repositoryRoot, 'bin/pactline')),
      server, clientKind: 'pactline-fleet-m4-l2-v2-execution-resume',
    }, { environment: { ...environment, PACTLINE_TOKEN: authority.token } })
    await client.preflight({ sessionId: clientSessionId })
    const packet = await client.showTask(provisioned.taskNumber, 40, { sessionId: clientSessionId })
    const task = record(packet.data.task, 'Execution-resume Task')
    if (task.phase !== 'in_progress' || task.activity !== 'working') {
      throw new Error('Execution resume requires in_progress.working')
    }
    const work = definition(spec, item, provisioned)
    const router = new StaticRuntimeRouter([
      new CodexHarnessAdapter({ environment, workspaceSandbox: 'danger-full-access' }),
    ], routes())
    const execution = await runStage({
      client, router, spec, item, definition: work, stage: 'execution',
      taskVersion: integer(task.version, 'Execution-resume Task version'), input: work.base,
      workspaceSource: mirrorPath, runId, clientSessionId,
      evidenceDirectory: join(runDirectory, 'execution-resume-1'), environment, log,
      existingClaimId: claimId, resumeRuntimeSessionId: runtimeSessionId, retainedWorkspace,
    })
    if (execution.result.proposal.recommendation !== 'complete' || execution.delivery === undefined
      || execution.result.settlement.task.phase !== 'in_review') throw new Error('Resumed execution did not produce a reviewable delivery')
    const review = await runStage({
      client, router, spec, item, definition: work, stage: 'review',
      taskVersion: execution.result.settlement.task.version,
      input: { source: spec.repository.url, ref: `refs/heads/${execution.delivery.branch}`, revision: execution.delivery.revision },
      workspaceSource: mirrorPath, runId: `${runDirectory.split('/').at(-1)!}-r1`, clientSessionId,
      evidenceDirectory: join(runDirectory, 'review-1'), environment, log,
    })
    if (review.result.proposal.recommendation !== 'accept' || review.result.settlement.task.phase !== 'done') {
      throw new Error('Independent Review did not accept resumed execution')
    }
    const finalPacket = await client.showTask(provisioned.taskNumber, 40, { sessionId: clientSessionId })
    const finalTask = record(finalPacket.data.task, 'Final Task')
    const result: L2V2CodexCaseResult = {
      caseId: item.caseId, taskNumber: provisioned.taskNumber,
      outcome: text(review.result.settlement.claim.outcome, 'Terminal Claim outcome'),
      reviewCycle: integer(finalTask.review_cycle, 'Final review cycle'),
      delivery: execution.delivery, evidenceDirectory: runDirectory,
    }
    await writePrivateJSON(join(runDirectory, 'coordinator.json'), {
      schemaVersion: 1, resumedExecution: true, completedAt: new Date().toISOString(), ...result,
      terminalTask: finalTask, terminalClaim: review.result.settlement.claim,
    })
    log(`${item.caseId} resumed Execution PASS; evidence ${runDirectory}`)
    return result
  } catch (error: unknown) {
    caught = error
    await writePrivateJSON(join(runDirectory, 'coordinator-execution-resume-failure.json'), {
      schemaVersion: 1, failedAt: new Date().toISOString(), error: safeError(error), recoveryAuthority: 'retained',
    }).catch(() => undefined)
    throw error
  } finally {
    try { await api.logout() } catch (error: unknown) { if (caught === undefined) throw error }
  }
}

/** Resume only the independent Review half of an already-settled execution delivery. */
export async function resumeCodexL2V2Review(options: L2V2ReviewResumeOptions): Promise<L2V2CodexCaseResult> {
  const environment = options.environment ?? process.env
  const log = options.log ?? (message => { process.stdout.write(`${message}\n`) })
  const spec = await loadL2V2Spec(join(fleetRoot, 'evaluation/cases/l2-v2.json'))
  const manifestPath = resolve(options.manifestPath ?? join(fleetRoot, '.fleet/l2-v2/corpus-manifest.json'))
  const manifest = await loadProvisionManifest(manifestPath)
  const mirrorPath = resolve(options.mirrorPath ?? join(dirname(manifestPath), 'repository-mirror.git'))
  await validateMirror(mirrorPath, manifest)
  const item = spec.cases.find(value => value.caseId === options.caseId)
  const provisioned = manifest.cases.find(value => value.caseId === options.caseId)
  if (item === undefined || provisioned === undefined) throw new Error(`Unknown L2 v2 case: ${options.caseId}`)
  const resultRoot = resolve(options.resultRoot ?? join(fleetRoot, '.fleet/l2-v2/runs'))
  const runDirectory = resolve(options.runDirectory)
  const relation = runDirectory.slice(resultRoot.length)
  if (!runDirectory.startsWith(`${resultRoot}/`) || relation.includes('/../') || !/^m4-l2v2-[0-9]{2}-[a-f0-9-]{36}$/.test(runDirectory.split('/').at(-1) ?? '')) {
    throw new Error('Review resume directory is outside the private L2 v2 result root')
  }
  let delivery: RepositoryDelivery
  try {
    delivery = JSON.parse(await readFile(join(runDirectory, 'execution/delivery.json'), 'utf8')) as RepositoryDelivery
  } catch (error: unknown) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error
    delivery = JSON.parse(await readFile(join(runDirectory, 'execution-resume-1/delivery.json'), 'utf8')) as RepositoryDelivery
  }
  if (delivery.repository.host !== 'github.com' || !delivery.branch.startsWith(`${spec.repository.branchPrefix}run/${String(provisioned.taskNumber)}-`)
    || !/^[a-f0-9]{40}$/.test(delivery.revision)) throw new Error('Retained execution delivery does not match the frozen case')
  const server = (options.server ?? environment.PACTLINE_LOCAL_SERVER ?? manifest.server).replace(/\/$/, '')
  if (server !== manifest.server) throw new Error('L2 v2 server does not match the frozen manifest')
  const api = new LocalDevelopmentAPI(server)
  let failure: unknown
  try {
    await api.login()
    const authority = await ensureAuthority(api, join(dirname(manifestPath), AUTHORITY_FILE), manifest.cohortId, server)
    const clientSessionId = `${runDirectory.split('/').at(-1)!}-review-resume`
    const client = new PactlineCLI({
      executable: resolve(options.pactlineExecutable ?? join(repositoryRoot, 'bin/pactline')),
      server, clientKind: 'pactline-fleet-m4-l2-v2-review-resume',
    }, { environment: { ...environment, PACTLINE_TOKEN: authority.token } })
    await client.preflight({ sessionId: clientSessionId })
    const packet = await client.showTask(provisioned.taskNumber, 40, { sessionId: clientSessionId })
    const task = record(packet.data.task, 'Review-resume Task')
    const expectedActivity = options.existingClaimId === undefined ? 'available' : 'working'
    if (task.phase !== 'in_review' || task.activity !== expectedActivity) throw new Error(`Review resume requires in_review.${expectedActivity}`)
    const work = definition(spec, item, provisioned)
    const router = new StaticRuntimeRouter([
      new CodexHarnessAdapter({ environment, workspaceSandbox: 'danger-full-access' }),
    ], routes())
    let retryId = `${runDirectory.split('/').at(-1)!}-rr1`
    let retainedWorkspace: FleetWorkspace | undefined
    let resumeRuntimeSessionId: string | undefined
    if (options.existingClaimId !== undefined) {
      const priorFailure = record(
        JSON.parse(await readFile(join(runDirectory, 'review-1/failure.json'), 'utf8')) as unknown,
        'Prior Review failure',
      )
      const priorSession = record(
        JSON.parse(await readFile(join(runDirectory, 'review-1/session.json'), 'utf8')) as unknown,
        'Prior Review Session',
      )
      if (text(priorSession.claimId, 'Prior Review Claim ID') !== options.existingClaimId) {
        throw new Error('Retained Review Session does not match the explicit Claim')
      }
      const retainedRepository = resolve(text(priorFailure.workspace, 'Retained Review workspace'))
      const retainedRoot = dirname(retainedRepository)
      retainedWorkspace = {
        mode: 'review', root: retainedRoot, temporaryParent: dirname(retainedRoot), repositoryPath: retainedRepository,
        source: mirrorPath, baseRevision: delivery.revision,
      }
      await verifyWorkspace(retainedWorkspace, environment)
      retryId = text(priorSession.runId, 'Prior Review run ID')
      resumeRuntimeSessionId = text(priorSession.runtimeSessionId, 'Prior Review runtime Session ID')
    }
    let review = await runStage({
      client, router,
      spec, item, definition: work, stage: 'review', taskVersion: integer(task.version, 'Review Task version'),
      input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
      workspaceSource: mirrorPath,
      runId: retryId, clientSessionId, evidenceDirectory: join(runDirectory, 'review-retry-1'), environment, log,
      ...(options.existingClaimId === undefined ? {} : { existingClaimId: options.existingClaimId }),
      ...(resumeRuntimeSessionId === undefined ? {} : { resumeRuntimeSessionId }),
      ...(retainedWorkspace === undefined ? {} : { retainedWorkspace }),
    })
    if (review.result.proposal.recommendation === 'request_changes' && item.caseId === 'L2V2-03') {
      const correction = await runStage({
        client, router, spec, item, definition: work, stage: 'correction',
        taskVersion: review.result.settlement.task.version,
        input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
        workspaceSource: mirrorPath, runId: `${runDirectory.split('/').at(-1)!}-c1`, clientSessionId,
        evidenceDirectory: join(runDirectory, 'correction-1'), environment, log,
      })
      if (correction.result.proposal.recommendation !== 'complete' || correction.delivery === undefined
        || correction.result.settlement.task.phase !== 'in_review') throw new Error('Resumed Review correction did not produce a delivery')
      delivery = correction.delivery
      review = await runStage({
        client, router, spec, item, definition: work, stage: 'review',
        taskVersion: correction.result.settlement.task.version,
        input: { source: spec.repository.url, ref: `refs/heads/${delivery.branch}`, revision: delivery.revision },
        workspaceSource: mirrorPath, runId: `${runDirectory.split('/').at(-1)!}-r2`, clientSessionId,
        evidenceDirectory: join(runDirectory, 'review-2'), environment, log,
      })
    }
    if (review.result.proposal.recommendation !== 'accept' || review.result.settlement.task.phase !== 'done') {
      throw new Error('Resumed independent Review did not accept the delivery')
    }
    const finalPacket = await client.showTask(provisioned.taskNumber, 40, { sessionId: clientSessionId })
    const finalTask = record(finalPacket.data.task, 'Final Task')
    if (finalTask.phase !== 'done' || finalTask.activity != null) throw new Error('Final authoritative Task is not done')
    const result: L2V2CodexCaseResult = {
      caseId: item.caseId, taskNumber: provisioned.taskNumber,
      outcome: text(review.result.settlement.claim.outcome, 'Terminal Claim outcome'),
      reviewCycle: integer(finalTask.review_cycle, 'Final review cycle'), delivery, evidenceDirectory: runDirectory,
    }
    await writePrivateJSON(join(runDirectory, 'coordinator.json'), {
      schemaVersion: 1, resumed: true, completedAt: new Date().toISOString(), ...result,
      terminalTask: finalTask, terminalClaim: review.result.settlement.claim,
    })
    log(`${item.caseId} resumed Review PASS; evidence ${runDirectory}`)
    return result
  } catch (error: unknown) {
    failure = error
    await writePrivateJSON(join(runDirectory, 'coordinator-resume-failure.json'), {
      schemaVersion: 1, failedAt: new Date().toISOString(), error: safeError(error), recoveryAuthority: 'retained',
    }).catch(() => undefined)
    throw error
  } finally {
    try { await api.logout() } catch (error: unknown) { if (failure === undefined) throw error }
  }
}
