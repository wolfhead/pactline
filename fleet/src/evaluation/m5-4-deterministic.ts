import { execFile } from 'node:child_process'
import { chmod, mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { ReplayHarnessAdapter } from '../adapters/replay/replay-adapter.js'
import { CodexHarnessAdapter } from '../adapters/codex/codex-adapter.js'
import { DeepSeekHarnessAdapter } from '../adapters/deepseek/deepseek-adapter.js'
import type { ExecutionProposal, HarnessRunResult, ReviewProposal } from '../core/harness-result.js'
import { FleetService } from '../service/fleet-service.js'
import { JSONFleetLogger } from '../service/logger.js'
import { FleetInjectedCrash } from '../scheduler/run-coordinator.js'
import { LocalDevelopmentAPI } from './l2-v2-provision.js'
import { preflightM54Usability, type M54UsabilityPreflightOptions } from './m5-4-usability.js'

const exec = promisify(execFile)
const DEVELOPMENT_USER_ID = '00000000-0000-0000-0000-000000000001'
const REPOSITORY_URL = 'https://github.com/wolfhead/pactline'
const CHANGED_PATH = 'M5_4_USABILITY.md'
const CHANGED_CONTENT = 'M5.4 usability passed\n'
const DRAFT_CONTENT = 'M5.4 usability draft\n'
const CORRECTED_CONTENT = 'M5.4 usability corrected\n'

export interface M54DeterministicResult {
  readonly status: 'passed'
  readonly runId: string
  readonly pactline: {
    readonly projectNumber: number
    readonly taskNumber: number
    readonly phase: string
    readonly activity: string
    readonly claimCount: number
    readonly claimStatus: string
  }
  readonly fleet: {
    readonly runId: string
    readonly runState: string
    readonly checkpoint: string
    readonly nonTerminalRuns: number
  }
  readonly repository: {
    readonly changedPath: typeof CHANGED_PATH
    readonly content: typeof CHANGED_CONTENT
    readonly deliveryRevision: string
    readonly branch: string
  }
  readonly evidencePath: string
}

export interface M54CorrectionResult {
  readonly status: 'passed'
  readonly runId: string
  readonly pactline: {
    readonly projectNumber: number
    readonly taskNumber: number
    readonly phase: string
    readonly claimCount: number
    readonly claimStatuses: readonly string[]
  }
  readonly fleet: { readonly runCount: number; readonly nonTerminalRuns: number }
  readonly repository: {
    readonly changedPath: typeof CHANGED_PATH
    readonly content: typeof CORRECTED_CONTENT
    readonly deliveryRevision: string
    readonly branch: string
  }
  readonly evidencePath: string
}

export interface M54RestartResult {
  readonly status: 'passed'
  readonly runId: string
  readonly pactline: {
    readonly projectNumber: number
    readonly taskNumber: number
    readonly phase: string
    readonly claimCount: number
    readonly claimStatuses: readonly string[]
  }
  readonly fleet: {
    readonly runCount: number
    readonly runStates: readonly string[]
    readonly nonTerminalRuns: number
  }
  readonly effects: { readonly preCrashAgentEffects: number; readonly deliveryCount: number }
  readonly evidencePath: string
}

export interface M54LiveResult {
  readonly status: 'passed'
  readonly runId: string
  readonly path: 'deepseek-codex' | 'codex-codex'
  readonly pactline: { readonly projectNumber: number; readonly taskNumber: number; readonly phase: string; readonly claimCount: number }
  readonly fleet: {
    readonly runCount: number
    readonly adapters: readonly string[]
    readonly runtimeSessionCount: number
    readonly nonTerminalRuns: number
  }
  readonly repository: { readonly changedPath: typeof CHANGED_PATH; readonly content: typeof CHANGED_CONTENT; readonly deliveryRevision: string }
  readonly evidencePath: string
}

interface ProvisionedAuthority {
  readonly projectNumber: number
  readonly taskNumber: number
  readonly criterionId: string
  readonly criterionRevision: number
  readonly tokenId: string
  readonly token: string
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function text(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`${name} must be non-empty text`)
  return value
}

function integer(value: unknown, name: string): number {
  if (!Number.isSafeInteger(value) || Number(value) < 1) throw new Error(`${name} must be a positive integer`)
  return Number(value)
}

function quote(value: string): string { return JSON.stringify(value) }

async function availablePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolvePromise, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolvePromise)
  })
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('M5.4 HTTP port was not allocated')
  await new Promise<void>((resolvePromise, reject) => server.close(error => error === undefined ? resolvePromise() : reject(error)))
  return address.port
}

async function provision(api: LocalDevelopmentAPI, runId: string, correction = false): Promise<ProvisionedAuthority> {
  const projectResponse = await api.request('/api/v1/projects', {
    method: 'POST', body: JSON.stringify({
      name: `Fleet M5.4 ${runId}`.slice(0, 100),
      description: 'Isolated Pactline Fleet M5.4 deterministic usability acceptance.',
    }),
  }, `${runId}-project`)
  const project = record(projectResponse.value, 'M5.4 Project')
  const projectNumber = integer(project.number, 'M5.4 Project number')
  const projectVersion = integer(project.version, 'M5.4 Project version')
  await api.request(`/api/v1/projects/${String(projectNumber)}/repositories`, {
    method: 'POST', headers: { 'If-Match': `"${String(projectVersion)}"` },
    body: JSON.stringify({ repository_url: REPOSITORY_URL, provider: 'github' }),
  }, `${runId}-repository`)

  const taskResponse = await api.request('/api/v1/tasks', {
    method: 'POST', body: JSON.stringify({
      title: `[M5.4 ${runId}] Deterministic delivery`,
      context: correction
        ? `Create ${CHANGED_PATH}; review must reject the seeded draft and correction must produce the approved marker.`
        : `Create ${CHANGED_PATH} with one exact line: M5.4 usability passed`,
      expected_result: correction
        ? `${CHANGED_PATH} contains the corrected usability marker after a request-changes cycle.`
        : `${CHANGED_PATH} exists and fixed verification passes.`,
      description: correction
        ? 'Bounded Replay-backed correction usability acceptance; local delivery only.'
        : 'Bounded Replay-backed usability acceptance; local delivery only.',
      priority: 'medium', project_number: projectNumber, assignee_id: DEVELOPMENT_USER_ID,
    }),
  }, `${runId}-task`)
  const task = record(taskResponse.value, 'M5.4 Task')
  const taskNumber = integer(task.number, 'M5.4 Task number')
  const taskVersion = integer(task.version, 'M5.4 Task version')
  const criterionResponse = await api.request(`/api/v1/tasks/${String(taskNumber)}/criteria`, {
    method: 'POST', headers: { 'If-Match': `"${String(taskVersion)}"` },
    body: JSON.stringify({
      criterion: correction
        ? `${CHANGED_PATH} contains exactly the corrected usability marker after review feedback.`
        : `${CHANGED_PATH} contains exactly the approved usability marker.`,
      verification_instructions: correction
        ? `Run: test "$(cat ${CHANGED_PATH})" = "M5.4 usability corrected"`
        : `Run: test "$(cat ${CHANGED_PATH})" = "M5.4 usability passed"`,
      position: 0,
    }),
  }, `${runId}-criterion`)
  const criterion = record(criterionResponse.value, 'M5.4 Criterion')
  const currentTask = record((await api.request(`/api/v1/tasks/${String(taskNumber)}`)).value, 'M5.4 Task after criterion')
  await api.request(`/api/v1/tasks/${String(taskNumber)}/commands/mark-ready`, {
    method: 'POST', headers: { 'If-Match': `"${String(integer(currentTask.version, 'M5.4 current Task version'))}"` },
  }, `${runId}-ready`)
  const issued = record((await api.request('/api/account/tokens', {
    method: 'POST', body: JSON.stringify({ name: `pactline-fleet-m5-4:${runId}`, scopes: ['work:execute'], expires_in_days: 30 }),
  })).value, 'M5.4 Token')
  return {
    projectNumber, taskNumber,
    criterionId: text(criterion.id, 'M5.4 Criterion ID'),
    criterionRevision: integer(criterion.revision, 'M5.4 Criterion revision'),
    tokenId: text(issued.id, 'M5.4 Token ID'), token: text(issued.token, 'M5.4 Token value'),
  }
}

async function git(executable: string, args: readonly string[]): Promise<string> {
  return (await exec(executable, [...args], { maxBuffer: 2 * 1024 * 1024 })).stdout.trim()
}

async function createRepositoryFixture(repository: string, baseRevision: string, directory: string): Promise<{ origin: string; ref: string }> {
  const origin = join(directory, 'origin.git')
  await git('git', ['clone', '--quiet', '--bare', repository, origin])
  const ref = 'refs/heads/m5-4/base'
  await git('git', ['--git-dir', origin, 'update-ref', ref, baseRevision])
  return { origin, ref }
}

async function createWorkPlugin(
  path: string,
  fixture: { readonly origin: string; readonly ref: string },
  baseRevision: string,
  criterionId: string,
  criterionRevision: number,
): Promise<void> {
  await writeFile(path, `#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
let input = ''; for await (const chunk of process.stdin) input += chunk
const request = JSON.parse(input)
const operation = process.argv.at(-1)
const output = data => process.stdout.write(JSON.stringify({ ok: true, data }))
if (operation === 'resolve') {
  output({
    caseId: 'm5-4-u1', taskNumber: request.candidate.task.number, taskVersion: request.candidate.task.version,
    base: { source: ${JSON.stringify(fixture.origin)}, ref: ${JSON.stringify(fixture.ref)}, revision: ${JSON.stringify(baseRevision)} },
    repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
    allowedPaths: [${JSON.stringify(CHANGED_PATH)}],
    verificationCommands: [${JSON.stringify(`test "$(cat ${CHANGED_PATH})" = "M5.4 usability passed"`)}],
    criteria: [{ id: ${JSON.stringify(criterionId)}, revision: ${String(criterionRevision)} }]
  })
} else if (operation === 'commit') {
  execFileSync('git', ['add', '--', ${JSON.stringify(CHANGED_PATH)}], { cwd: request.workspace })
  execFileSync('git', ['-c', 'user.name=Pactline Fleet', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'test: add M5.4 usability marker'], { cwd: request.workspace })
  output({
    revision: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: request.workspace, encoding: 'utf8' }).trim(),
    branch: execFileSync('git', ['branch', '--show-current'], { cwd: request.workspace, encoding: 'utf8' }).trim()
  })
} else if (operation === 'push') {
  execFileSync('git', ['push', '--quiet', 'origin', 'HEAD:refs/heads/' + request.commit.branch], { cwd: request.workspace })
  output(request.commit)
} else if (operation === 'open-code-change') {
  output({
    repository: request.definition.repository,
    codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/5401',
    revision: request.push.revision,
    branch: request.push.branch
  })
} else process.exit(2)
`)
  await chmod(path, 0o700)
}

async function createCorrectionWorkPlugin(
  path: string,
  fixture: { readonly origin: string; readonly ref: string },
  baseRevision: string,
  criterionId: string,
  criterionRevision: number,
  deliveryStatePath: string,
  verificationCommand = `test -s ${CHANGED_PATH}`,
): Promise<void> {
  await writeFile(path, `#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
let input = ''; for await (const chunk of process.stdin) input += chunk
const request = JSON.parse(input)
const operation = process.argv.at(-1)
const output = data => process.stdout.write(JSON.stringify({ ok: true, data }))
const statePath = ${JSON.stringify(deliveryStatePath)}
const state = () => existsSync(statePath) ? JSON.parse(readFileSync(statePath, 'utf8')) : undefined
if (operation === 'resolve') {
  const prior = state()
  const stage = request.candidate.stage
  const base = stage === 'execution' || prior === undefined
    ? { source: ${JSON.stringify(fixture.origin)}, ref: ${JSON.stringify(fixture.ref)}, revision: ${JSON.stringify(baseRevision)} }
    : { source: ${JSON.stringify(fixture.origin)}, ref: 'refs/heads/' + prior.branch, revision: prior.revision }
  output({
    caseId: 'm5-4-u3', taskNumber: request.candidate.task.number, taskVersion: request.candidate.task.version,
    base,
    ...(stage === 'review' ? { candidate: {
      source: ${JSON.stringify(fixture.origin)}, ref: 'refs/heads/' + prior.branch, revision: prior.revision,
      codeChangeUrl: prior.codeChangeUrl, branch: prior.branch
    } } : {}),
    repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
    allowedPaths: [${JSON.stringify(CHANGED_PATH)}],
    verificationCommands: [${JSON.stringify(verificationCommand)}],
    criteria: [{ id: ${JSON.stringify(criterionId)}, revision: ${String(criterionRevision)} }]
  })
} else if (operation === 'commit') {
  execFileSync('git', ['add', '--', ${JSON.stringify(CHANGED_PATH)}], { cwd: request.workspace })
  execFileSync('git', ['-c', 'user.name=Pactline Fleet', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'test: advance M5.4 correction marker'], { cwd: request.workspace })
  output({
    revision: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: request.workspace, encoding: 'utf8' }).trim(),
    branch: execFileSync('git', ['branch', '--show-current'], { cwd: request.workspace, encoding: 'utf8' }).trim()
  })
} else if (operation === 'push') {
  execFileSync('git', ['push', '--quiet', 'origin', 'HEAD:refs/heads/' + request.commit.branch], { cwd: request.workspace })
  output(request.commit)
} else if (operation === 'open-code-change') {
  const previous = state()
  const codeChangeUrl = 'https://github.com/wolfhead/pactline/pull/' + String(previous === undefined ? 5410 : 5411)
  const next = { revision: request.push.revision, branch: request.push.branch, codeChangeUrl }
  writeFileSync(statePath, JSON.stringify(next))
  output({ repository: request.definition.repository, ...next })
} else process.exit(2)
`)
  await chmod(path, 0o700)
}

function serviceConfig(options: {
  readonly server: string
  readonly pactlineExecutable: string
  readonly stateDirectory: string
  readonly workspaceRoot: string
  readonly projectNumber: number
  readonly plugin: string
  readonly port: number
  readonly routes?: {
    readonly execution: string
    readonly review: string
    readonly correction: string
    readonly resolutionAnalysis: string
  }
}): string {
  const route = '{ adapter: replay, model: replay-quality, reasoning: max }'
  const routes = options.routes ?? {
    execution: route, review: route, correction: route, resolutionAnalysis: route,
  }
  return `version: 1
service:
  pactline:
    server: ${quote(options.server)}
    tokenEnv: M54_PACTLINE_TOKEN
    executable: ${quote(options.pactlineExecutable)}
  stateDirectory: ${quote(options.stateDirectory)}
  pollInterval: 30s
  maxConcurrentRuns: 1
  shutdownDeadline: 15s
  http:
    address: 127.0.0.1
    port: ${String(options.port)}
fleets:
  usability:
    project: ${String(options.projectNumber)}
    maxConcurrentRuns: 1
    workspaceRoot: ${quote(options.workspaceRoot)}
    workPlugin:
      executable: ${quote(options.plugin)}
      timeout: 30s
    routing:
      execution: ${routes.execution}
      review: ${routes.review}
      correction: ${routes.correction}
      resolutionAnalysis: ${routes.resolutionAnalysis}
`
}

function executionResult(request: import('../core/harness-adapter.js').HarnessRunRequest, sessionId: string, taskNumber: number, criterionId: string, criterionRevision: number): HarnessRunResult {
  const command = `test "$(cat ${CHANGED_PATH})" = "M5.4 usability passed"`
  const proposal: ExecutionProposal = {
    schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId,
    taskNumber, recommendation: 'complete', summary: 'Created the bounded M5.4 usability marker.',
    changedPaths: [CHANGED_PATH], verification: [{ command, outcome: 'passed', summary: 'Marker matched.' }],
    criteria: [{ criterionId, criterionRevision, outcome: 'passed', evidence: 'Fleet fixed verification observed the exact marker.' }],
    limitations: [],
  }
  return {
    adapterId: 'replay', adapterVersion: '1.0.0', runtimeSessionId: sessionId,
    model: { provider: 'replay', model: 'replay-quality', reasoning: 'max' }, terminalState: 'completed', proposal,
    usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
  }
}

function replayResult(proposal: ExecutionProposal | ReviewProposal, sessionId: string): HarnessRunResult {
  return {
    adapterId: 'replay', adapterVersion: '1.0.0', runtimeSessionId: sessionId,
    model: { provider: 'replay', model: 'replay-quality', reasoning: 'max' }, terminalState: 'completed', proposal,
    usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
  }
}

function correctionExecutionResult(
  request: import('../core/harness-adapter.js').HarnessRunRequest,
  sessionId: string,
  taskNumber: number,
  criterionId: string,
  criterionRevision: number,
  summary: string,
): HarnessRunResult {
  const proposal: ExecutionProposal = {
    schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId, taskNumber,
    recommendation: 'complete', summary, changedPaths: [CHANGED_PATH],
    verification: [{ command: `test -s ${CHANGED_PATH}`, outcome: 'passed', summary: 'Marker exists.' }],
    criteria: [{ criterionId, criterionRevision, outcome: 'passed', evidence: summary }], limitations: [],
  }
  return replayResult(proposal, sessionId)
}

function correctionReviewResult(
  request: import('../core/harness-adapter.js').HarnessRunRequest,
  sessionId: string,
  taskNumber: number,
  criterionId: string,
  criterionRevision: number,
  recommendation: ReviewProposal['recommendation'],
): HarnessRunResult {
  const requestsChanges = recommendation === 'request_changes'
  const proposal: ReviewProposal = {
    schemaVersion: 1, kind: 'review', runId: request.runId, claimId: request.claimId, taskNumber,
    recommendation,
    summary: requestsChanges ? 'The seeded draft marker must be corrected.' : 'The corrected marker is accepted.',
    findings: requestsChanges ? [{
      path: CHANGED_PATH, line: 1, severity: 'high', category: 'correctness',
      evidence: 'M5.4 usability draft', explanation: 'The approved value is M5.4 usability corrected.',
    }] : [],
    verification: [{ command: `test -s ${CHANGED_PATH}`, outcome: 'passed', summary: 'Marker exists.' }],
    criteria: [{
      criterionId, criterionRevision, outcome: requestsChanges ? 'failed' : 'passed',
      evidence: requestsChanges ? 'The draft value is not acceptable.' : 'The corrected value matches the requested outcome.',
    }],
    limitations: [],
  }
  return replayResult(proposal, sessionId)
}

function envelopeData(value: unknown, name: string): Record<string, unknown> {
  const envelope = record(value, name)
  if (envelope.ok !== true) throw new Error(`${name} is not successful`)
  return record(envelope.data, `${name} data`)
}

export async function runM54DeterministicUsability(options: M54UsabilityPreflightOptions): Promise<M54DeterministicResult> {
  const preflight = await preflightM54Usability(options)
  await mkdir(preflight.evidence.path, { mode: 0o700 })
  await chmod(preflight.evidence.path, 0o700)
  const api = new LocalDevelopmentAPI(preflight.server)
  let authority: ProvisionedAuthority | undefined
  let service: FleetService | undefined
  let failure: unknown
  let result: M54DeterministicResult | undefined
  try {
    await api.login()
    authority = await provision(api, options.runId)
    const fixture = await createRepositoryFixture(options.repository, preflight.repository.baseRevision, preflight.evidence.path)
    const plugin = join(preflight.evidence.path, 'work-plugin.mjs')
    await createWorkPlugin(plugin, fixture, preflight.repository.baseRevision, authority.criterionId, authority.criterionRevision)
    const stateDirectory = join(preflight.evidence.path, 'state')
    const workspaceRoot = join(preflight.evidence.path, 'work')
    await mkdir(workspaceRoot, { mode: 0o700 })
    const configPath = join(preflight.evidence.path, 'fleet.yml')
    await writeFile(configPath, serviceConfig({
      server: preflight.server, pactlineExecutable: options.pactlineExecutable,
      stateDirectory, workspaceRoot, projectNumber: authority.projectNumber,
      plugin, port: await availablePort(),
    }), { mode: 0o600 })
    const replay = new ReplayHarnessAdapter([{
      sessionId: `${options.runId}-execution`,
      effect: async request => { await writeFile(join(request.workspace, CHANGED_PATH), CHANGED_CONTENT) },
      result: (request, sessionId) => executionResult(request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision),
    }])
    service = new FleetService(configPath, {
      adapters: [replay], logger: new JSONFleetLogger(process.stderr),
      environment: { ...process.env, M54_PACTLINE_TOKEN: authority.token },
    })
    await service.start()
    const cycle = await service.runOnce()
    if (cycle.admitted !== 1 || cycle.contentions !== 0) throw new Error('M5.4 deterministic cycle did not admit exactly one Task')

    const task = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}`)).value, 'M5.4 completed Task')
    const claims = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}/claims`)).value, 'M5.4 Claim list')
    if (!Array.isArray(claims.items) || claims.items.length !== 1) throw new Error('M5.4 Task did not retain exactly one Claim')
    const claim = record(claims.items[0], 'M5.4 Claim')
    const runsResponse = await fetch(new URL('/api/v1/runs?limit=20', service.address.url))
    const runs = envelopeData(await runsResponse.json() as unknown, 'M5.4 Fleet Runs')
    if (!Array.isArray(runs.items)) throw new Error('M5.4 Fleet Run list is invalid')
    const run = runs.items.map(value => record(value, 'M5.4 Fleet Run')).find(value => value.taskNumber === authority?.taskNumber)
    if (run === undefined) throw new Error('M5.4 Fleet Run was not observable')
    const detailResponse = await fetch(new URL(`/api/v1/runs/${encodeURIComponent(text(run.runId, 'M5.4 Run ID'))}`, service.address.url))
    const detail = envelopeData(await detailResponse.json() as unknown, 'M5.4 Fleet Run detail')
    if (!Array.isArray(detail.effects)) throw new Error('M5.4 Fleet Run effects are invalid')
    const delivery = detail.effects.map(value => record(value, 'M5.4 Run effect')).find(value => value.kind === 'code_change_creation')
    const deliveryDetail = record(delivery?.detail, 'M5.4 delivery detail')
    const branch = text(deliveryDetail.branch, 'M5.4 delivery branch')
    const deliveryRevision = text(deliveryDetail.revision, 'M5.4 delivery revision')
    const observedRevision = await git('git', ['--git-dir', fixture.origin, 'rev-parse', `refs/heads/${branch}`])
    if (observedRevision !== deliveryRevision) throw new Error('M5.4 local Git delivery revision does not match Fleet observation')
    const content = await git('git', ['--git-dir', fixture.origin, 'show', `${deliveryRevision}:${CHANGED_PATH}`])
    if (`${content}\n` !== CHANGED_CONTENT) throw new Error('M5.4 delivered file content does not match the approved marker')

    result = {
      status: 'passed', runId: options.runId,
      pactline: {
        projectNumber: authority.projectNumber, taskNumber: authority.taskNumber,
        phase: text(task.phase, 'M5.4 Task phase'), activity: text(task.activity, 'M5.4 Task activity'),
        claimCount: claims.items.length, claimStatus: text(claim.status, 'M5.4 Claim status'),
      },
      fleet: {
        runId: text(run.runId, 'M5.4 Run ID'), runState: text(run.state, 'M5.4 Run state'),
        checkpoint: text(run.checkpoint, 'M5.4 Run checkpoint'), nonTerminalRuns: service.health.registry.nonTerminalRuns,
      },
      repository: { changedPath: CHANGED_PATH, content: CHANGED_CONTENT, deliveryRevision, branch },
      evidencePath: preflight.evidence.path,
    }
    await writeFile(join(preflight.evidence.path, 'result.json'), `${JSON.stringify(result, null, 2)}\n`, { mode: 0o600 })
  } catch (error) { failure = error }
  try { await service?.stop('M5.4 deterministic acceptance complete') } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 service cleanup failed') }
  if (authority !== undefined) {
    try { await api.request(`/api/account/tokens/${encodeURIComponent(authority.tokenId)}`, { method: 'DELETE' }) } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 Token cleanup failed') }
  }
  try { await api.logout() } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 logout failed') }
  if (failure !== undefined) throw failure
  return result!
}

export async function runM54DeterministicCorrection(options: M54UsabilityPreflightOptions): Promise<M54CorrectionResult> {
  const preflight = await preflightM54Usability(options)
  await mkdir(preflight.evidence.path, { mode: 0o700 })
  await chmod(preflight.evidence.path, 0o700)
  const api = new LocalDevelopmentAPI(preflight.server)
  let authority: ProvisionedAuthority | undefined
  let service: FleetService | undefined
  let failure: unknown
  let result: M54CorrectionResult | undefined
  try {
    await api.login()
    authority = await provision(api, options.runId, true)
    const fixture = await createRepositoryFixture(options.repository, preflight.repository.baseRevision, preflight.evidence.path)
    const plugin = join(preflight.evidence.path, 'work-plugin.mjs')
    const deliveryStatePath = join(preflight.evidence.path, 'delivery-state.json')
    await createCorrectionWorkPlugin(
      plugin, fixture, preflight.repository.baseRevision,
      authority.criterionId, authority.criterionRevision, deliveryStatePath,
    )
    const stateDirectory = join(preflight.evidence.path, 'state')
    const workspaceRoot = join(preflight.evidence.path, 'work')
    await mkdir(workspaceRoot, { mode: 0o700 })
    const configPath = join(preflight.evidence.path, 'fleet.yml')
    await writeFile(configPath, serviceConfig({
      server: preflight.server, pactlineExecutable: options.pactlineExecutable,
      stateDirectory, workspaceRoot, projectNumber: authority.projectNumber,
      plugin, port: await availablePort(),
    }), { mode: 0o600 })
    const replay = new ReplayHarnessAdapter([
      {
        sessionId: `${options.runId}-execution`,
        effect: async request => { await writeFile(join(request.workspace, CHANGED_PATH), DRAFT_CONTENT) },
        result: (request, sessionId) => correctionExecutionResult(
          request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision,
          'Created the intentionally reviewable draft marker.',
        ),
      },
      {
        sessionId: `${options.runId}-review-changes`,
        result: (request, sessionId) => correctionReviewResult(
          request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision, 'request_changes',
        ),
      },
      {
        sessionId: `${options.runId}-correction`,
        effect: async request => { await writeFile(join(request.workspace, CHANGED_PATH), CORRECTED_CONTENT) },
        result: (request, sessionId) => correctionExecutionResult(
          request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision,
          'Applied the requested correction to the marker.',
        ),
      },
      {
        sessionId: `${options.runId}-review-accept`,
        result: (request, sessionId) => correctionReviewResult(
          request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision, 'accept',
        ),
      },
    ])
    service = new FleetService(configPath, {
      adapters: [replay], logger: new JSONFleetLogger(process.stderr),
      environment: { ...process.env, M54_PACTLINE_TOKEN: authority.token },
    })
    await service.start()
    for (const expectedStage of ['execution', 'review', 'correction', 'review'] as const) {
      const cycle = await service.runOnce()
      if (cycle.admitted !== 1 || cycle.contentions !== 0 || cycle.outcomes[0]?.kind !== 'completed') {
        throw new Error(`M5.4 correction cycle did not complete the expected ${expectedStage} stage`)
      }
    }

    const task = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}`)).value, 'M5.4 corrected Task')
    const claims = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}/claims`)).value, 'M5.4 correction Claim list')
    if (!Array.isArray(claims.items) || claims.items.length !== 4) throw new Error('M5.4 correction Task did not retain four Claims')
    const claimStatuses = claims.items.map((value, index) => text(record(value, `M5.4 correction Claim ${String(index)}`).status, 'M5.4 correction Claim status'))
    const runsResponse = await fetch(new URL('/api/v1/runs?limit=20', service.address.url))
    const runs = envelopeData(await runsResponse.json() as unknown, 'M5.4 correction Fleet Runs')
    if (!Array.isArray(runs.items)) throw new Error('M5.4 correction Fleet Run list is invalid')
    const taskRuns = runs.items.map(value => record(value, 'M5.4 correction Fleet Run'))
      .filter(value => value.taskNumber === authority?.taskNumber)
    if (taskRuns.length !== 4) throw new Error('M5.4 correction workflow did not expose four Fleet Runs')
    const correctionRun = taskRuns.find(value => value.stage === 'correction')
    if (correctionRun === undefined) throw new Error('M5.4 correction Run was not observable')
    const detailResponse = await fetch(new URL(`/api/v1/runs/${encodeURIComponent(text(correctionRun.runId, 'M5.4 correction Run ID'))}`, service.address.url))
    const detail = envelopeData(await detailResponse.json() as unknown, 'M5.4 correction Run detail')
    if (!Array.isArray(detail.effects)) throw new Error('M5.4 correction Run effects are invalid')
    const delivery = detail.effects.map(value => record(value, 'M5.4 correction Run effect')).find(value => value.kind === 'code_change_creation')
    const deliveryDetail = record(delivery?.detail, 'M5.4 correction delivery detail')
    const branch = text(deliveryDetail.branch, 'M5.4 correction delivery branch')
    const deliveryRevision = text(deliveryDetail.revision, 'M5.4 correction delivery revision')
    const observedRevision = await git('git', ['--git-dir', fixture.origin, 'rev-parse', `refs/heads/${branch}`])
    if (observedRevision !== deliveryRevision) throw new Error('M5.4 corrected Git revision does not match Fleet observation')
    const content = await git('git', ['--git-dir', fixture.origin, 'show', `${deliveryRevision}:${CHANGED_PATH}`])
    if (`${content}\n` !== CORRECTED_CONTENT) throw new Error('M5.4 corrected file does not match the accepted marker')

    result = {
      status: 'passed', runId: options.runId,
      pactline: {
        projectNumber: authority.projectNumber, taskNumber: authority.taskNumber,
        phase: text(task.phase, 'M5.4 corrected Task phase'), claimCount: claims.items.length, claimStatuses,
      },
      fleet: { runCount: taskRuns.length, nonTerminalRuns: service.health.registry.nonTerminalRuns },
      repository: { changedPath: CHANGED_PATH, content: CORRECTED_CONTENT, deliveryRevision, branch },
      evidencePath: preflight.evidence.path,
    }
    await writeFile(join(preflight.evidence.path, 'result.json'), `${JSON.stringify(result, null, 2)}\n`, { mode: 0o600 })
  } catch (error) { failure = error }
  try { await service?.stop('M5.4 correction acceptance complete') } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 correction service cleanup failed') }
  if (authority !== undefined) {
    try { await api.request(`/api/account/tokens/${encodeURIComponent(authority.tokenId)}`, { method: 'DELETE' }) } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 correction Token cleanup failed') }
  }
  try { await api.logout() } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 correction logout failed') }
  if (failure !== undefined) throw failure
  return result!
}

export async function runM54RestartRecovery(options: M54UsabilityPreflightOptions): Promise<M54RestartResult> {
  const preflight = await preflightM54Usability(options)
  await mkdir(preflight.evidence.path, { mode: 0o700 })
  await chmod(preflight.evidence.path, 0o700)
  const api = new LocalDevelopmentAPI(preflight.server)
  let authority: ProvisionedAuthority | undefined
  let service: FleetService | undefined
  let failure: unknown
  let result: M54RestartResult | undefined
  let preCrashAgentEffects = 0
  try {
    await api.login()
    authority = await provision(api, options.runId)
    const fixture = await createRepositoryFixture(options.repository, preflight.repository.baseRevision, preflight.evidence.path)
    const plugin = join(preflight.evidence.path, 'work-plugin.mjs')
    await createWorkPlugin(plugin, fixture, preflight.repository.baseRevision, authority.criterionId, authority.criterionRevision)
    const stateDirectory = join(preflight.evidence.path, 'state')
    const workspaceRoot = join(preflight.evidence.path, 'work')
    await mkdir(workspaceRoot, { mode: 0o700 })
    const configPath = join(preflight.evidence.path, 'fleet.yml')
    await writeFile(configPath, serviceConfig({
      server: preflight.server, pactlineExecutable: options.pactlineExecutable,
      stateDirectory, workspaceRoot, projectNumber: authority.projectNumber,
      plugin, port: await availablePort(),
    }), { mode: 0o600 })

    const interrupted = new ReplayHarnessAdapter([{
      sessionId: `${options.runId}-interrupted`,
      effect: () => { preCrashAgentEffects += 1 },
      result: (request, sessionId) => executionResult(
        request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision,
      ),
    }])
    service = new FleetService(configPath, {
      adapters: [interrupted], logger: new JSONFleetLogger(process.stderr),
      environment: { ...process.env, M54_PACTLINE_TOKEN: authority.token },
      faultInjector: checkpoint => {
        if (checkpoint === 'after_session_persistence_before_agent') throw new FleetInjectedCrash(checkpoint)
      },
    })
    await service.start()
    let interruptedAsPlanned = false
    try { await service.runOnce() } catch (error) {
      interruptedAsPlanned = error instanceof FleetInjectedCrash
        && error.checkpoint === 'after_session_persistence_before_agent'
      if (!interruptedAsPlanned) throw error
    }
    if (!interruptedAsPlanned || preCrashAgentEffects !== 0) throw new Error('M5.4 restart did not stop before the first Agent effect')
    await service.stop('M5.4 injected restart boundary')

    const resumed = new ReplayHarnessAdapter([{
      sessionId: `${options.runId}-replacement`,
      effect: async request => { await writeFile(join(request.workspace, CHANGED_PATH), CHANGED_CONTENT) },
      result: (request, sessionId) => executionResult(
        request, sessionId, authority!.taskNumber, authority!.criterionId, authority!.criterionRevision,
      ),
    }])
    service = new FleetService(configPath, {
      adapters: [resumed], logger: new JSONFleetLogger(process.stderr),
      environment: { ...process.env, M54_PACTLINE_TOKEN: authority.token },
    })
    await service.start()
    const cycle = await service.runOnce()
    if (cycle.admitted !== 1 || cycle.contentions !== 0 || cycle.outcomes[0]?.kind !== 'completed') {
      throw new Error('M5.4 restarted Fleet did not complete one replacement Run')
    }

    const task = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}`)).value, 'M5.4 restarted Task')
    const claims = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}/claims`)).value, 'M5.4 restart Claim list')
    if (!Array.isArray(claims.items) || claims.items.length !== 2) throw new Error('M5.4 restart did not retain exactly two Claims')
    const claimStatuses = claims.items.map((value, index) => text(record(value, `M5.4 restart Claim ${String(index)}`).status, 'M5.4 restart Claim status'))
      .sort((first, second) => (first === 'released' ? -1 : second === 'released' ? 1 : first.localeCompare(second)))
    const runsResponse = await fetch(new URL('/api/v1/runs?limit=20', service.address.url))
    const runs = envelopeData(await runsResponse.json() as unknown, 'M5.4 restart Fleet Runs')
    if (!Array.isArray(runs.items)) throw new Error('M5.4 restart Fleet Run list is invalid')
    const taskRuns = runs.items.map(value => record(value, 'M5.4 restart Fleet Run'))
      .filter(value => value.taskNumber === authority?.taskNumber)
      .sort((first, second) => text(first.createdAt, 'M5.4 Run createdAt').localeCompare(text(second.createdAt, 'M5.4 Run createdAt')))
    if (taskRuns.length !== 2) throw new Error('M5.4 restart workflow did not expose two Fleet Runs')
    const runStates = taskRuns.map(value => text(value.state, 'M5.4 restart Run state'))
    let deliveryCount = 0
    for (const run of taskRuns) {
      const detailResponse = await fetch(new URL(`/api/v1/runs/${encodeURIComponent(text(run.runId, 'M5.4 restart Run ID'))}`, service.address.url))
      const detail = envelopeData(await detailResponse.json() as unknown, 'M5.4 restart Run detail')
      if (!Array.isArray(detail.effects)) throw new Error('M5.4 restart Run effects are invalid')
      deliveryCount += detail.effects.map(value => record(value, 'M5.4 restart Run effect'))
        .filter(value => value.kind === 'code_change_creation' && value.status === 'observed').length
    }
    if (runStates[0] !== 'released' || runStates[1] !== 'completed' || deliveryCount !== 1) {
      throw new Error('M5.4 restart produced a duplicate or incorrect terminal effect')
    }
    result = {
      status: 'passed', runId: options.runId,
      pactline: {
        projectNumber: authority.projectNumber, taskNumber: authority.taskNumber,
        phase: text(task.phase, 'M5.4 restarted Task phase'), claimCount: claims.items.length, claimStatuses,
      },
      fleet: { runCount: taskRuns.length, runStates, nonTerminalRuns: service.health.registry.nonTerminalRuns },
      effects: { preCrashAgentEffects, deliveryCount }, evidencePath: preflight.evidence.path,
    }
    await writeFile(join(preflight.evidence.path, 'result.json'), `${JSON.stringify(result, null, 2)}\n`, { mode: 0o600 })
  } catch (error) { failure = error }
  try { await service?.stop('M5.4 restart acceptance complete') } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 restart service cleanup failed') }
  if (authority !== undefined) {
    try { await api.request(`/api/account/tokens/${encodeURIComponent(authority.tokenId)}`, { method: 'DELETE' }) } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 restart Token cleanup failed') }
  }
  try { await api.logout() } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 restart logout failed') }
  if (failure !== undefined) throw failure
  return result!
}

export async function runM54LiveWorkflow(
  options: M54UsabilityPreflightOptions,
  path: M54LiveResult['path'],
): Promise<M54LiveResult> {
  const preflight = await preflightM54Usability(options)
  await mkdir(preflight.evidence.path, { mode: 0o700 })
  await chmod(preflight.evidence.path, 0o700)
  const api = new LocalDevelopmentAPI(preflight.server)
  let authority: ProvisionedAuthority | undefined
  let service: FleetService | undefined
  let failure: unknown
  let result: M54LiveResult | undefined
  try {
    await api.login()
    authority = await provision(api, options.runId)
    const fixture = await createRepositoryFixture(options.repository, preflight.repository.baseRevision, preflight.evidence.path)
    const plugin = join(preflight.evidence.path, 'work-plugin.mjs')
    const verificationCommand = `test "$(cat ${CHANGED_PATH})" = "M5.4 usability passed"`
    await createCorrectionWorkPlugin(
      plugin, fixture, preflight.repository.baseRevision,
      authority.criterionId, authority.criterionRevision,
      join(preflight.evidence.path, 'delivery-state.json'), verificationCommand,
    )
    const stateDirectory = join(preflight.evidence.path, 'state')
    const workspaceRoot = join(preflight.evidence.path, 'work')
    await mkdir(workspaceRoot, { mode: 0o700 })
    const deepSeekRoute = '{ adapter: deepseek, model: deepseek-v4-pro, reasoning: max }'
    const codexRoute = '{ adapter: codex, model: gpt-5.6-sol, reasoning: high }'
    const executionRoute = path === 'deepseek-codex' ? deepSeekRoute : codexRoute
    const configPath = join(preflight.evidence.path, 'fleet.yml')
    await writeFile(configPath, serviceConfig({
      server: preflight.server, pactlineExecutable: options.pactlineExecutable,
      stateDirectory, workspaceRoot, projectNumber: authority.projectNumber,
      plugin, port: await availablePort(),
      routes: {
        execution: executionRoute, review: codexRoute,
        correction: executionRoute, resolutionAnalysis: codexRoute,
      },
    }), { mode: 0o600 })
    service = new FleetService(configPath, {
      adapters: path === 'deepseek-codex'
        ? [new DeepSeekHarnessAdapter({ maxTokens: 32_768 }), new CodexHarnessAdapter()]
        : [new CodexHarnessAdapter()],
      logger: new JSONFleetLogger(process.stderr),
      environment: { ...process.env, M54_PACTLINE_TOKEN: authority.token },
    })
    await service.start()
    for (const expectedStage of ['execution', 'review'] as const) {
      const cycle = await service.runOnce()
      if (cycle.admitted !== 1 || cycle.contentions !== 0 || cycle.outcomes[0]?.kind !== 'completed') {
        throw new Error(`M5.4 live ${path} path did not complete ${expectedStage}`)
      }
    }

    const task = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}`)).value, 'M5.4 live Task')
    const claims = record((await api.request(`/api/v1/tasks/${String(authority.taskNumber)}/claims`)).value, 'M5.4 live Claim list')
    if (!Array.isArray(claims.items) || claims.items.length !== 2) throw new Error(`M5.4 live ${path} path did not retain two Claims`)
    const runsResponse = await fetch(new URL('/api/v1/runs?limit=20', service.address.url))
    const runs = envelopeData(await runsResponse.json() as unknown, 'M5.4 live Fleet Runs')
    if (!Array.isArray(runs.items)) throw new Error('M5.4 live Fleet Run list is invalid')
    const taskRuns = runs.items.map(value => record(value, 'M5.4 live Fleet Run'))
      .filter(value => value.taskNumber === authority?.taskNumber)
      .sort((first, second) => text(first.createdAt, 'M5.4 live Run createdAt').localeCompare(text(second.createdAt, 'M5.4 live Run createdAt')))
    if (taskRuns.length !== 2) throw new Error(`M5.4 live ${path} path did not expose two Runs`)
    const adapters = taskRuns.map(value => text(value.adapter, 'M5.4 live Run Adapter'))
    let runtimeSessionCount = 0
    let deliveryRevision: string | undefined
    for (const run of taskRuns) {
      const detailResponse = await fetch(new URL(`/api/v1/runs/${encodeURIComponent(text(run.runId, 'M5.4 live Run ID'))}`, service.address.url))
      const detail = envelopeData(await detailResponse.json() as unknown, 'M5.4 live Run detail')
      if (typeof detail.runtimeSessionId === 'string' && detail.runtimeSessionId.trim() !== '') runtimeSessionCount += 1
      if (!Array.isArray(detail.effects)) throw new Error('M5.4 live Run effects are invalid')
      const delivery = detail.effects.map(value => record(value, 'M5.4 live Run effect')).find(value => value.kind === 'code_change_creation')
      if (delivery !== undefined) deliveryRevision = text(record(delivery.detail, 'M5.4 live delivery detail').revision, 'M5.4 live delivery revision')
    }
    if (deliveryRevision === undefined) throw new Error(`M5.4 live ${path} path has no observable delivery`)
    const content = await git('git', ['--git-dir', fixture.origin, 'show', `${deliveryRevision}:${CHANGED_PATH}`])
    if (`${content}\n` !== CHANGED_CONTENT) throw new Error(`M5.4 live ${path} delivery does not match the approved marker`)
    result = {
      status: 'passed', runId: options.runId, path,
      pactline: {
        projectNumber: authority.projectNumber, taskNumber: authority.taskNumber,
        phase: text(task.phase, 'M5.4 live Task phase'), claimCount: claims.items.length,
      },
      fleet: { runCount: taskRuns.length, adapters, runtimeSessionCount, nonTerminalRuns: service.health.registry.nonTerminalRuns },
      repository: { changedPath: CHANGED_PATH, content: CHANGED_CONTENT, deliveryRevision },
      evidencePath: preflight.evidence.path,
    }
    await writeFile(join(preflight.evidence.path, 'result.json'), `${JSON.stringify(result, null, 2)}\n`, { mode: 0o600 })
  } catch (error) { failure = error }
  try { await service?.stop(`M5.4 live ${path} acceptance complete`) } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 live service cleanup failed') }
  if (authority !== undefined) {
    try { await api.request(`/api/account/tokens/${encodeURIComponent(authority.tokenId)}`, { method: 'DELETE' }) } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 live Token cleanup failed') }
  }
  try { await api.logout() } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M5.4 live logout failed') }
  if (failure !== undefined) throw failure
  return result!
}
