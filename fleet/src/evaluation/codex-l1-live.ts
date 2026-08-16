import { spawn } from 'node:child_process'
import { cp, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { randomUUID } from 'node:crypto'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { HarnessRunEvent, HarnessRunRequest } from '../core/harness-adapter.js'
import { proposalResultSchema, validateHarnessProposal, type ReviewProposal } from '../core/harness-result.js'
import { assertProposalMatchesObservation, assertReviewFindingsExist, observeGit, runFixedVerification } from '../core/verification.js'
import { promptPolicy } from '../core/prompt-policy.js'
import { CodexHarnessAdapter, codexAdapterPolicy } from '../adapters/codex/codex-adapter.js'
import {
  assertL1Sanitized, l1Criteria, l1FixedVerification, l1FixtureRoot, l1Issues,
  runL1Command, safeL1GitEnvironment, scoreL1, writeL1Evidence,
} from './deepseek-l1-live.js'

const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')

export interface CodexL1Result {
  readonly runId: string
  readonly evidenceDirectory: string
  readonly issueRecall: number
  readonly matchedIssueIds: readonly string[]
  readonly falsePositiveFindings: number
  readonly eventCount: number
  readonly toolEventCounts: Readonly<Record<string, number>>
}

export interface CodexL1Options {
  readonly environment?: NodeJS.ProcessEnv
  readonly resultRoot?: string
  readonly timeoutMs?: number
  readonly log?: (message: string) => void
}

function redact(value: string): string {
  return value
    .replace(/\b(?:sk[-_]|bb_pat_)[A-Za-z0-9._-]+\b/g, '[REDACTED]')
    .replace(/\bBearer\s+\S+/gi, 'Bearer [REDACTED]')
    .replace(/\b(?:KEY|SECRET|TOKEN|PASSWORD|AUTHORIZATION)\s*[:=]\s*\S+/gi, '[REDACTED]')
    .slice(0, 4_096)
}

async function assertNoResidualCodexProcess(workspace: string): Promise<void> {
  if (process.platform === 'win32') return
  const output = await new Promise<string>((resolvePromise, reject) => {
    const child = spawn('ps', ['-axo', 'command='], { stdio: ['ignore', 'pipe', 'pipe'] })
    let stdout = ''; child.stdout.on('data', (chunk: Buffer) => { stdout += chunk.toString('utf8') })
    child.once('error', reject); child.once('close', code => code === 0 ? resolvePromise(stdout) : reject(new Error('ps failed')))
  })
  if (output.split('\n').some(line => line.includes('codex exec') && line.includes(workspace))) {
    throw new Error('Codex L1 left a residual runtime process')
  }
}

/** Run one finite, real, read-only Codex review against the shared deterministic L1 fixture. */
export async function runCodexL1(options: CodexL1Options = {}): Promise<CodexL1Result> {
  const environment = options.environment ?? process.env
  const log = options.log ?? (message => { process.stdout.write(`${message}\n`) })
  const runId = `m3-l1-${randomUUID()}`
  const claimId = randomUUID()
  const resultRoot = resolve(options.resultRoot ?? join(fleetRoot, '.fleet/codex-l1-results'))
  const evidenceDirectory = join(resultRoot, runId)
  await mkdir(evidenceDirectory, { recursive: true, mode: 0o700 })
  const controlRoot = await mkdtemp(join(tmpdir(), 'pactline-fleet-m3-l1-'))
  const workspace = join(controlRoot, 'workspace')
  const events: HarnessRunEvent[] = []
  const gitEnvironment = safeL1GitEnvironment(environment)
  try {
    await cp(l1FixtureRoot, workspace, { recursive: true, force: false })
    await runL1Command('git', ['init', '-q'], workspace, gitEnvironment)
    await runL1Command('git', ['-c', 'user.name=pactline-fleet', '-c', 'user.email=fleet@example.invalid', 'add', '.'], workspace, gitEnvironment)
    await runL1Command('git', ['-c', 'user.name=pactline-fleet', '-c', 'user.email=fleet@example.invalid', 'commit', '-qm', 'Shared L1 fixture'], workspace, gitEnvironment)
    const revision = (await runL1Command('git', ['rev-parse', 'HEAD'], workspace, gitEnvironment)).trim()
    const tree = (await runL1Command('git', ['rev-parse', 'HEAD^{tree}'], workspace, gitEnvironment)).trim()
    log(`[1/4] Shared read-only fixture materialized (${revision.slice(0, 12)})`)

    const policy = promptPolicy('review', 'fleet-m3-v1')
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
        claim: { id: claimId, stage: 'review', status: 'active' }, criteria: l1Criteria,
      },
      allowedPaths: ['README.md', 'package.json', 'src', 'test'], verificationCommands: [l1FixedVerification],
      resultSchema: proposalResultSchema(validationContext), sandbox: 'read_only',
      deadline: new Date(Date.now() + (options.timeoutMs ?? 300_000)).toISOString(),
      policy: {
        model: codexAdapterPolicy.model, reasoning: codexAdapterPolicy.reasoning,
        promptVersion: policy.version, systemInstructions: policy.system,
        stageInstructions: policy.stageInstructions, resultContractVersion: 1,
      },
    }
    const adapter = new CodexHarnessAdapter({ environment })
    await adapter.probe({
      requiredStages: ['review'], requiredSandbox: 'read_only', requireNativeTools: true,
      requireStructuredResult: true, requireEventStream: true, requireCancellation: true, requireSessionResume: true,
    })
    log('[2/4] Pinned Codex SOL/high/native preflight passed')
    const result = await adapter.run(request, {
      onSessionStarted: reference => { log(`[3/4] Codex Session started (${reference.runtimeSessionId})`) },
      onEvent: event => { events.push(event) },
    }, new AbortController().signal)
    assertL1Sanitized(result)
    await writeL1Evidence(evidenceDirectory, 'result.json', result)
    await writeL1Evidence(evidenceDirectory, 'events.json', events)
    const proposal = validateHarnessProposal(result.proposal, validationContext)
    if (proposal.kind !== 'review' || proposal.recommendation !== 'request_changes') {
      throw new Error('Codex L1 review did not return the expected request_changes proposal')
    }
    const commands = await runFixedVerification(workspace, [l1FixedVerification], { environment })
    const git = await observeGit(workspace, revision, environment)
    assertProposalMatchesObservation(proposal, { git, commands }, { baseHead: revision, allowedPaths: request.allowedPaths })
    await assertReviewFindingsExist(workspace, proposal)
    const afterTree = (await runL1Command('git', ['rev-parse', 'HEAD^{tree}'], workspace, gitEnvironment)).trim()
    if (afterTree !== tree) throw new Error('Codex L1 review changed the frozen Git tree')
    const evaluation = scoreL1(proposal as ReviewProposal)
    if (evaluation.recall !== 1) throw new Error(`Codex L1 issue recall was ${String(evaluation.recall)}; expected 1`)
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
    await assertNoResidualCodexProcess(workspace)
    log(`[4/4] Codex live read-only gate passed; private evidence retained at ${evidenceDirectory}`)
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
