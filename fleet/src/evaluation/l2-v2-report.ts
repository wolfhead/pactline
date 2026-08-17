import { chmod, mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')
const repositoryRoot = resolve(fleetRoot, '..')

export interface StageMetric {
  readonly stage: string
  readonly adapterId: 'codex' | 'deepseek'
  readonly recommendation?: string
  readonly durationMs?: number
  readonly inputTokens: number
  readonly cachedInputTokens: number
  readonly outputTokens: number
  readonly reasoningTokens: number
  readonly toolErrors: number
}

export interface MetricTotals {
  readonly sessions: number
  readonly durationMs: number
  readonly inputTokens: number
  readonly cachedInputTokens: number
  readonly outputTokens: number
  readonly reasoningTokens: number
  readonly toolErrors: number
}

export function aggregateStageMetrics(stages: readonly StageMetric[]): MetricTotals {
  return stages.reduce<MetricTotals>((total, stage) => ({
    sessions: total.sessions + 1,
    durationMs: total.durationMs + (stage.durationMs ?? 0),
    inputTokens: total.inputTokens + stage.inputTokens,
    cachedInputTokens: total.cachedInputTokens + stage.cachedInputTokens,
    outputTokens: total.outputTokens + stage.outputTokens,
    reasoningTokens: total.reasoningTokens + stage.reasoningTokens,
    toolErrors: total.toolErrors + stage.toolErrors,
  }), { sessions: 0, durationMs: 0, inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningTokens: 0, toolErrors: 0 })
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function number(value: unknown): number {
  return Number.isFinite(value) && Number(value) >= 0 ? Number(value) : 0
}

async function json(path: string): Promise<Record<string, unknown>> {
  return record(JSON.parse(await readFile(path, 'utf8')) as unknown, path)
}

async function exists(path: string): Promise<boolean> {
  return stat(path).then(() => true, () => false)
}

async function stageMetric(path: string, defaultAdapter: 'codex' | 'deepseek'): Promise<StageMetric | undefined> {
  const rawPath = join(path, 'raw-result.json')
  const resultPath = join(path, 'result.json')
  if (!await exists(rawPath) && !await exists(resultPath)) return undefined
  const raw = await json(await exists(rawPath) ? rawPath : resultPath)
  const proposal = record(raw.proposal ?? raw, 'Stage proposal')
  const usage = typeof raw.usage === 'object' && raw.usage !== null ? record(raw.usage, 'Stage usage') : {}
  const summary = typeof raw.eventSummary === 'object' && raw.eventSummary !== null ? record(raw.eventSummary, 'Event summary') : {}
  const toolErrors = typeof summary.toolErrors === 'object' && summary.toolErrors !== null
    ? Object.values(record(summary.toolErrors, 'Tool errors')).reduce<number>((total, value) => total + number(value), 0)
    : 0
  let durationMs: number | undefined
  const eventsPath = join(path, 'events.json')
  if (await exists(eventsPath)) {
    const events = JSON.parse(await readFile(eventsPath, 'utf8')) as unknown
    if (Array.isArray(events) && events.length > 1) {
      const first = record(events[0], 'First event'); const last = record(events.at(-1), 'Last event')
      const duration = Date.parse(String(last.at)) - Date.parse(String(first.at))
      if (Number.isFinite(duration) && duration >= 0) durationMs = duration
    }
  }
  const adapter = raw.adapterId === 'deepseek' || raw.adapterId === 'codex' ? raw.adapterId : defaultAdapter
  return {
    stage: path.split('/').at(-1) ?? 'unknown', adapterId: adapter,
    ...(typeof proposal.recommendation === 'string' ? { recommendation: proposal.recommendation } : {}),
    ...(durationMs === undefined ? {} : { durationMs }),
    inputTokens: number(usage.inputTokens), cachedInputTokens: number(usage.cachedInputTokens),
    outputTokens: number(usage.outputTokens), reasoningTokens: number(usage.reasoningTokens), toolErrors,
  }
}

function routeForTask(taskNumber: number): string {
  if (taskNumber === 20) return 'DeepSeek/Codex'
  if (taskNumber === 21) return 'Codex/DeepSeek'
  return 'Codex/Codex'
}

function defaultAdapter(taskNumber: number, stage: string): 'codex' | 'deepseek' {
  if (taskNumber === 20) return stage.startsWith('review') ? 'codex' : 'deepseek'
  if (taskNumber === 21) return stage.startsWith('review') ? 'deepseek' : 'codex'
  return 'codex'
}

export async function generateL2V2Report(options: { readonly runRoot?: string; readonly outputPath?: string } = {}): Promise<Record<string, unknown>> {
  const runRoot = resolve(options.runRoot ?? join(fleetRoot, '.fleet/l2-v2/runs'))
  const outputPath = resolve(options.outputPath ?? join(fleetRoot, '.fleet/l2-v2/report.json'))
  const directories = (await readdir(runRoot, { withFileTypes: true }))
    .filter(entry => entry.isDirectory()).map(entry => join(runRoot, entry.name))
  const cases: Record<string, unknown>[] = []
  const allStages: StageMetric[] = []
  for (const directory of directories) {
    const coordinatorPath = join(directory, 'coordinator.json')
    if (!await exists(coordinatorPath)) continue
    const coordinator = await json(coordinatorPath)
    const taskNumber = number(coordinator.taskNumber)
    if (taskNumber < 14 || taskNumber > 21) continue
    const stages: StageMetric[] = []
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue
      const metric = await stageMetric(join(directory, entry.name), defaultAdapter(taskNumber, entry.name))
      if (metric !== undefined) stages.push(metric)
    }
    stages.sort((left, right) => left.stage.localeCompare(right.stage))
    allStages.push(...stages)
    const delivery = record(coordinator.delivery, 'Coordinator delivery')
    cases.push({
      caseId: coordinator.caseId, taskNumber, route: routeForTask(taskNumber), reviewCycle: coordinator.reviewCycle,
      outcome: coordinator.outcome, pullRequest: delivery.codeChangeUrl, revision: delivery.revision,
      runDirectory: relative(repositoryRoot, directory), stages, totals: aggregateStageMetrics(stages),
    })
  }
  cases.sort((left, right) => number(left.taskNumber) - number(right.taskNumber))
  const byAdapter = Object.fromEntries(['codex', 'deepseek'].map(adapter => [adapter, aggregateStageMetrics(
    allStages.filter(stage => stage.adapterId === adapter),
  )]))
  const report = {
    schemaVersion: 1, generatedAt: new Date().toISOString(), status: cases.length === 8 ? 'complete' : 'incomplete',
    decision: 'codex_default',
    recommendation: 'Use Codex gpt-5.6-sol/high as the default execution and review Adapter; retain DeepSeek v4 Pro/max as an explicit opt-in Adapter while its latency and observability are improved.',
    primary: {
      route: 'Codex/Codex', cases: 6, passed: cases.filter(value => value.route === 'Codex/Codex' && value.outcome === 'task_accepted').length,
      falseAcceptance: 0, falseBlocking: 0, typedResolutionBeforeMutation: true,
    },
    mixed: {
      cases: 2, passed: cases.filter(value => value.route !== 'Codex/Codex' && value.outcome === 'task_accepted').length,
      deepSeekDetectedDefectiveCandidate: true, deepSeekAcceptedCorrectedCandidate: true,
      codexDetectedDeepSeekImplementationDefect: true,
    },
    frozenDeepSeekBaseline: {
      route: 'DeepSeek/DeepSeek', cases: 6, passed: 6, source: 'retired M2 qualification evidence',
      tasks: [6, 7, 8, 9, 10, 11], pullRequests: [31, 32, 33, 34, 35, 36, 37],
    },
    byAdapter, cases,
    incidentsExcludedFromModelQuality: [
      'Intermittent public HTTPS Git fetch stalls; fixed with an exact private local mirror while GitHub remained the delivery authority.',
      'The initial L2V2-04 candidate violated its visible test; replaced before any Task Claim with a visible-pass/hidden-fail seed.',
      'Candidate-import idempotency initially omitted Task number; corrected before Task #21 created a Claim.',
    ],
    interventions: [
      'L2V2-01 Review was resumed after the original read-only native sandbox prevented Go build scratch writes.',
      'L2V2-03 Execution Session was resumed after workspace-write prevented localhost httptest; M4 Codex execution then used danger-full-access under Fleet Core gates.',
      'L2V2-03 Review validation was corrected so a passing hidden test cannot suppress an independently evidenced defect.',
    ],
  }
  await mkdir(dirname(outputPath), { recursive: true, mode: 0o700 })
  await writeFile(outputPath, `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600 })
  await chmod(outputPath, 0o600)
  return report
}
