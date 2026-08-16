import { spawn } from 'node:child_process'
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { randomUUID } from 'node:crypto'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { HarnessRunEvent, HarnessRunRequest } from '../core/harness-adapter.js'
import { proposalResultSchema, validateHarnessProposal, type CriterionIdentity, type ReviewProposal } from '../core/harness-result.js'
import { assertProposalMatchesObservation, assertReviewFindingsExist, observeGit, runFixedVerification } from '../core/verification.js'
import { promptPolicy } from '../core/prompt-policy.js'
import { DeepSeekHarnessAdapter, deepSeekAdapterPolicy } from '../adapters/deepseek/deepseek-adapter.js'
import { resolveDeepSeekCredential } from '../adapters/deepseek/credential.js'

const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')
export const l1FixtureRoot = join(fleetRoot, 'evaluation/fixtures/deepseek-l1')
export const l1FixedVerification = 'npm test'

export const l1Criteria: readonly (CriterionIdentity & { description: string })[] = [
  { id: 'criterion-origin-validation', revision: 1, description: 'Assess redirect origin validation for prefix or hostname confusion.' },
  { id: 'criterion-average-empty', revision: 1, description: 'Assess average behavior for an empty numeric sample.' },
  { id: 'criterion-port-validation', revision: 1, description: 'Assess full-string HTTP port validation and the valid numeric range.' },
]

export const l1Issues = [
  { id: 'origin-prefix-confusion', path: 'src/url-utils.js', line: 2, keywords: ['origin', 'prefix', 'hostname'] },
  { id: 'empty-average', path: 'src/url-utils.js', line: 6, keywords: ['empty', 'zero', 'nan'] },
  { id: 'partial-or-out-of-range-port', path: 'src/url-utils.js', line: 10, keywords: ['parseint', 'suffix', '65535', 'range'] },
] as const

export interface DeepSeekL1Result {
  readonly runId: string
  readonly evidenceDirectory: string
  readonly issueRecall: number
  readonly matchedIssueIds: readonly string[]
  readonly falsePositiveFindings: number
  readonly eventCount: number
  readonly toolEventCounts: Readonly<Record<string, number>>
}

export interface DeepSeekL1Options {
  readonly environment?: NodeJS.ProcessEnv
  readonly resultRoot?: string
  readonly maxTokens?: number
  readonly timeoutMs?: number
  readonly log?: (message: string) => void
}

export function safeL1GitEnvironment(source: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const result: NodeJS.ProcessEnv = {
    LANG: 'C.UTF-8', LC_ALL: 'C.UTF-8', TZ: 'UTC',
    GIT_AUTHOR_DATE: '2000-01-01T00:00:00Z', GIT_COMMITTER_DATE: '2000-01-01T00:00:00Z',
  }
  if (source.PATH !== undefined) result.PATH = source.PATH
  return result
}

export function runL1Command(executable: string, args: readonly string[], cwd: string, env: NodeJS.ProcessEnv): Promise<string> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(executable, [...args], { cwd, env, stdio: ['ignore', 'pipe', 'pipe'], shell: false })
    let stdout = ''; let stderr = ''
    child.stdout.on('data', (chunk: Buffer) => { stdout = (stdout + chunk.toString('utf8')).slice(-65_536) })
    child.stderr.on('data', (chunk: Buffer) => { stderr = (stderr + chunk.toString('utf8')).slice(-65_536) })
    child.once('error', reject)
    child.once('close', code => {
      if (code === 0) resolvePromise(stdout)
      else reject(new Error(`${executable} exited ${String(code)}: ${redact(stderr.trim())}`))
    })
  })
}

function redact(value: string): string {
  return value
    .replace(/\b(?:sk[-_]|bb_pat_)[A-Za-z0-9._-]+\b/g, '[REDACTED]')
    .replace(/\bBearer\s+\S+/gi, 'Bearer [REDACTED]')
    .replace(/\b(?:KEY|SECRET|TOKEN|PASSWORD|AUTHORIZATION)\s*[:=]\s*\S+/gi, '[REDACTED]')
    .slice(0, 4_096)
}

export function assertL1Sanitized(value: unknown): void {
  const serialized = JSON.stringify(value)
  if (/\b(?:sk[-_]|bb_pat_)[A-Za-z0-9._-]{8,}\b/.test(serialized)
    || /\bBearer\s+\S+/i.test(serialized)
    || /\b(?:DEEPSEEK_API_KEY|PACTLINE_TOKEN|AUTHORIZATION)\s*[:=]\s*[^\s"']+/i.test(serialized)) {
    throw new Error('DeepSeek L1 result contains credential-shaped material')
  }
}

export function scoreL1(proposal: ReviewProposal): { matched: string[]; recall: number; falsePositives: number } {
  const matched = new Set<string>()
  const matchedFindings = new Set<number>()
  for (const issue of l1Issues) {
    proposal.findings.forEach((finding, index) => {
      const text = `${finding.explanation} ${finding.evidence}`.toLowerCase()
      if (finding.path === issue.path && Math.abs(finding.line - issue.line) <= 1
        && issue.keywords.some(keyword => text.includes(keyword))) {
        matched.add(issue.id)
        matchedFindings.add(index)
      }
    })
  }
  return {
    matched: [...matched].sort(),
    recall: matched.size / l1Issues.length,
    falsePositives: proposal.findings.length - matchedFindings.size,
  }
}

export async function writeL1Evidence(directory: string, name: string, value: unknown): Promise<void> {
  assertL1Sanitized(value)
  await writeFile(join(directory, name), `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
}

/** Run one live, finite Pro/max DSH review against a deterministic read-only fixture. */
export async function runDeepSeekL1(options: DeepSeekL1Options = {}): Promise<DeepSeekL1Result> {
  const environment = options.environment ?? process.env
  if (await resolveDeepSeekCredential(environment) === undefined) {
    throw new Error('DeepSeek credential is unavailable; export DEEPSEEK_API_KEY or configure a private DSH credential document')
  }
  const log = options.log ?? (message => { process.stdout.write(`${message}\n`) })
  const runId = `m2-l1-${randomUUID()}`
  const claimId = randomUUID()
  const resultRoot = resolve(options.resultRoot ?? join(fleetRoot, '.fleet/deepseek-l1-results'))
  const evidenceDirectory = join(resultRoot, runId)
  await mkdir(evidenceDirectory, { recursive: true, mode: 0o700 })
  const controlRoot = await mkdtemp(join(tmpdir(), 'pactline-fleet-m2-l1-'))
  const workspace = join(controlRoot, 'workspace')
  const events: HarnessRunEvent[] = []
  const gitEnvironment = safeL1GitEnvironment(environment)
  try {
    await cp(l1FixtureRoot, workspace, { recursive: true, force: false })
    await runL1Command('git', ['init', '-q'], workspace, gitEnvironment)
    await runL1Command('git', ['-c', 'user.name=pactline-fleet', '-c', 'user.email=fleet@example.invalid', 'add', '.'], workspace, gitEnvironment)
    await runL1Command('git', ['-c', 'user.name=pactline-fleet', '-c', 'user.email=fleet@example.invalid', 'commit', '-qm', 'DeepSeek L1 fixture'], workspace, gitEnvironment)
    const revision = (await runL1Command('git', ['rev-parse', 'HEAD'], workspace, gitEnvironment)).trim()
    const tree = (await runL1Command('git', ['rev-parse', 'HEAD^{tree}'], workspace, gitEnvironment)).trim()
    log(`[1/4] Deterministic read-only fixture materialized (${revision.slice(0, 12)})`)

    const policy = promptPolicy('review', 'fleet-m2-v1')
    const validationContext = {
      stage: 'review' as const, runId, claimId, taskNumber: 7001,
      criteria: l1Criteria.map(({ id, revision: criterionRevision }) => ({ id, revision: criterionRevision })),
      verificationCommands: [l1FixedVerification],
    }
    const request: HarnessRunRequest = {
      runId, claimId, stage: 'review', workspace, repositoryRevision: revision,
      taskPacket: {
        task: {
          number: 7001, version: 1, phase: 'in_review', title: 'Review URL utility correctness and security',
          description: 'Inspect all implementation and test files. Identify concrete defects without modifying the repository.',
        },
        claim: { id: claimId, stage: 'review', status: 'active' },
        criteria: l1Criteria,
      },
      allowedPaths: ['README.md', 'package.json', 'src', 'test'],
      verificationCommands: [l1FixedVerification], resultSchema: proposalResultSchema(validationContext),
      sandbox: 'read_only', deadline: new Date(Date.now() + (options.timeoutMs ?? 300_000)).toISOString(),
      policy: {
        model: deepSeekAdapterPolicy.model, reasoning: deepSeekAdapterPolicy.reasoning,
        promptVersion: policy.version, systemInstructions: policy.system,
        stageInstructions: policy.stageInstructions, resultContractVersion: 1,
      },
    }
    const adapter = new DeepSeekHarnessAdapter({ environment, maxTokens: options.maxTokens ?? 16_384 })
    await adapter.probe({
      requiredStages: ['review'], requiredSandbox: 'read_only', requireNativeTools: true,
      requireStructuredResult: true, requireEventStream: true, requireCancellation: true, requireSessionResume: false,
    })
    log('[2/4] Adapter profile passed keyless Pro/max/native preflight')
    const result = await adapter.run(request, {
      onSessionStarted: reference => { log(`[3/4] DSH Session started (${reference.runtimeSessionId})`) },
      onEvent: event => { events.push(event) },
    }, new AbortController().signal)
    assertL1Sanitized(result)
    // Retain bounded Adapter output before cross-checking it against Fleet's
    // own observations so a rejected live run remains diagnosable.
    await writeL1Evidence(evidenceDirectory, 'result.json', result)
    await writeL1Evidence(evidenceDirectory, 'events.json', events)
    const proposal = validateHarnessProposal(result.proposal, validationContext)
    if (proposal.kind !== 'review' || proposal.recommendation !== 'request_changes') {
      throw new Error('DeepSeek L1 review did not return the expected request_changes proposal')
    }
    const commands = await runFixedVerification(workspace, [l1FixedVerification], { environment })
    const git = await observeGit(workspace, revision, environment)
    assertProposalMatchesObservation(proposal, { git, commands }, { baseHead: revision, allowedPaths: request.allowedPaths })
    await assertReviewFindingsExist(workspace, proposal)
    const afterTree = (await runL1Command('git', ['rev-parse', 'HEAD^{tree}'], workspace, gitEnvironment)).trim()
    if (afterTree !== tree) throw new Error('DeepSeek L1 review changed the frozen Git tree')
    const evaluation = scoreL1(proposal)
    if (evaluation.recall !== 1) throw new Error(`DeepSeek L1 issue recall was ${String(evaluation.recall)}; expected 1`)

    await writeL1Evidence(evidenceDirectory, 'verification.json', commands)
    await writeL1Evidence(evidenceDirectory, 'workspace.json', { revision, tree, unchanged: true, git })
    await writeL1Evidence(evidenceDirectory, 'evaluation.json', {
      seededIssues: l1Issues.length, matchedIssueIds: evaluation.matched,
      issueRecall: evaluation.recall, falsePositiveFindings: evaluation.falsePositives,
    })
    await writeL1Evidence(evidenceDirectory, 'manifest.json', {
      schemaVersion: 1, runId, claimId, completedAt: new Date().toISOString(),
      adapter: { id: result.adapterId, version: result.adapterVersion }, model: result.model,
      policy: { promptVersion: request.policy.promptVersion, resultContractVersion: request.policy.resultContractVersion },
      runtimeSessionId: result.runtimeSessionId,
    })
    log(`[4/4] Live read-only parity passed; sanitized evidence retained at ${evidenceDirectory}`)
    return {
      runId, evidenceDirectory, issueRecall: evaluation.recall, matchedIssueIds: evaluation.matched,
      falsePositiveFindings: evaluation.falsePositives, eventCount: result.eventSummary.total,
      toolEventCounts: result.eventSummary.toolCalls,
    }
  } catch (error: unknown) {
    await writeFile(join(evidenceDirectory, 'failure.json'), `${JSON.stringify({
      schemaVersion: 1, runId, failedAt: new Date().toISOString(),
      error: redact(error instanceof Error ? error.message : String(error)),
    }, null, 2)}\n`, { mode: 0o600 })
    throw error
  } finally {
    await rm(controlRoot, { recursive: true, force: true })
  }
}
