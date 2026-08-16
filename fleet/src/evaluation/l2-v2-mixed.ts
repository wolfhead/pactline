import { chmod, lstat, mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadL2V2Spec, type L2V2CaseSpec } from './l2-v2-spec.js'
import {
  LocalDevelopmentAPI,
  type L2V2ProvisionManifest,
  type ProvisionedCase,
  type ProvisionedCriterion,
} from './l2-v2-provision.js'
import { runCodexL2V2Case, type L2V2CodexCaseResult, type L2V2RouteSelection } from './l2-v2-live.js'

const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')

export interface MixedComparison {
  readonly comparisonId: 'deepseek-codex' | 'codex-deepseek'
  readonly caseId: 'L2V2-03' | 'L2V2-04'
  readonly route: L2V2RouteSelection
  readonly provisionedCase: ProvisionedCase
}

export interface MixedComparisonManifest {
  readonly schemaVersion: 1
  readonly status: 'provisioned'
  readonly server: string
  readonly projectNumber: number
  readonly createdAt: string
  readonly comparisons: readonly MixedComparison[]
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

async function writePrivate(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 })
  await chmod(dirname(path), 0o700)
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
  await chmod(path, 0o600)
}

function comparisonTitle(comparisonId: MixedComparison['comparisonId'], item: L2V2CaseSpec): string {
  const route = comparisonId === 'deepseek-codex' ? 'DS-CX' : 'CX-DS'
  return `[M4 MIXED ${route} ${item.caseId}] ${item.title.replace(/^\[[^\]]+\]\s*/, '')}`
}

async function provisionTask(
  api: LocalDevelopmentAPI,
  primary: L2V2ProvisionManifest,
  item: L2V2CaseSpec,
  comparisonId: MixedComparison['comparisonId'],
): Promise<ProvisionedCase> {
  const projectNumber = integer(primary.pactline?.projectNumber, 'Primary Project number')
  const title = comparisonTitle(comparisonId, item)
  const listed = record((await api.request(`/api/v1/tasks?project_number=${String(projectNumber)}&archived=all&limit=200`)).value, 'Task list')
  if (!Array.isArray(listed.items)) throw new Error('Task list items are invalid')
  const matches = listed.items.map(value => record(value, 'Task')).filter(value => value.title === title && value.archived_at == null)
  if (matches.length > 1) throw new Error(`Multiple mixed comparison Tasks match ${title}`)
  let task = matches[0]
  if (task === undefined) {
    const source = primary.cases.find(value => value.caseId === item.caseId)
    if (source === undefined) throw new Error(`Primary provision record is missing ${item.caseId}`)
    task = record((await api.request('/api/v1/tasks', {
      method: 'POST', body: JSON.stringify({
        title,
        context: [
          `Bounded M4 mixed-Adapter comparison ${comparisonId}.`, item.description,
          `Start from ${source.baseRef} at ${source.baseRevision}.`,
          `Changes are restricted to: ${item.allowedPaths.join(', ')}.`,
          `Required visible verification: ${item.verificationCommands.join('; ')}.`,
        ].join('\n'),
        expected_result: item.criteria.map((criterion, index) => `${String(index + 1)}. ${criterion.criterion}`).join('\n'),
        description: `Isolated M4 comparison; route=${comparisonId}; expected path=${item.expectedPath}.`,
        priority: 'medium', project_number: projectNumber,
      }),
    }, `fleet-m4-mixed-${comparisonId}-${item.caseId.toLowerCase()}-task`)).value, 'Created mixed Task')
  }
  const taskNumber = integer(task.number, 'Mixed Task number')
  let taskVersion = integer(task.version, 'Mixed Task version')
  const listedCriteria = record((await api.request(`/api/v1/tasks/${String(taskNumber)}/criteria?limit=200`)).value, 'Criterion list')
  if (!Array.isArray(listedCriteria.items)) throw new Error('Criterion list items are invalid')
  const criteria: ProvisionedCriterion[] = listedCriteria.items.map(value => {
    const criterion = record(value, 'Mixed Criterion')
    const position = Number(criterion.position)
    const expected = item.criteria[position]
    if (expected === undefined || criterion.criterion !== expected.criterion
      || criterion.verification_instructions !== expected.verificationInstructions) {
      throw new Error(`Mixed comparison Criterion drifted for ${comparisonId}`)
    }
    return {
      id: text(criterion.id, 'Criterion ID'), version: integer(criterion.version, 'Criterion version'),
      revision: integer(criterion.revision, 'Criterion revision'), position,
    }
  }).sort((left, right) => left.position - right.position)
  for (let position = criteria.length; position < item.criteria.length; position++) {
    const expected = item.criteria[position]!
    const criterion = record((await api.request(`/api/v1/tasks/${String(taskNumber)}/criteria`, {
      method: 'POST', headers: { 'If-Match': `"${String(taskVersion)}"` },
      body: JSON.stringify({
        criterion: expected.criterion, verification_instructions: expected.verificationInstructions, position,
      }),
    }, `fleet-m4-mixed-${comparisonId}-${item.caseId.toLowerCase()}-criterion-${String(position)}`)).value, 'Created mixed Criterion')
    criteria.push({
      id: text(criterion.id, 'Criterion ID'), version: integer(criterion.version, 'Criterion version'),
      revision: integer(criterion.revision, 'Criterion revision'), position,
    })
    taskVersion += 1
  }
  const authoritative = record((await api.request(`/api/v1/tasks/${String(taskNumber)}`)).value, 'Mixed Task')
  if (authoritative.phase !== 'backlog' || authoritative.activity != null) throw new Error('Mixed comparison Task is not in backlog')
  taskVersion = integer(authoritative.version, 'Mixed Task version')
  const source = primary.cases.find(value => value.caseId === item.caseId)!
  return {
    ...source, taskId: text(authoritative.id, 'Mixed Task ID'), taskNumber, taskVersion, phase: 'backlog', criteria,
  }
}

/** Provision exactly two bounded mixed-route Tasks in the existing isolated M4 Project. */
export async function provisionMixedComparisons(options: {
  readonly server?: string
  readonly primaryManifestPath?: string
  readonly manifestPath?: string
} = {}): Promise<MixedComparisonManifest> {
  const primaryPath = resolve(options.primaryManifestPath ?? join(fleetRoot, '.fleet/l2-v2/corpus-manifest.json'))
  const manifestPath = resolve(options.manifestPath ?? join(fleetRoot, '.fleet/l2-v2/mixed-manifest.json'))
  try {
    const info = await lstat(manifestPath)
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) throw new Error('Mixed manifest must be private')
    const current = JSON.parse(await readFile(manifestPath, 'utf8')) as MixedComparisonManifest
    if (current.schemaVersion !== 1 || current.status !== 'provisioned' || current.comparisons.length !== 2) throw new Error('Mixed manifest is invalid')
    return current
  } catch (error: unknown) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error
  }
  const primary = JSON.parse(await readFile(primaryPath, 'utf8')) as L2V2ProvisionManifest
  if (primary.schemaVersion !== 1 || primary.status !== 'provisioned' || primary.pactline === undefined) throw new Error('Primary M4 manifest is invalid')
  const server = (options.server ?? primary.server).replace(/\/$/, '')
  if (server !== primary.server) throw new Error('Mixed comparison server differs from the primary cohort')
  const spec = await loadL2V2Spec(join(fleetRoot, 'evaluation/cases/l2-v2.json'))
  const definitions = [
    { comparisonId: 'deepseek-codex' as const, caseId: 'L2V2-03' as const, route: { execution: 'deepseek' as const, review: 'codex' as const } },
    { comparisonId: 'codex-deepseek' as const, caseId: 'L2V2-04' as const, route: { execution: 'codex' as const, review: 'deepseek' as const } },
  ]
  const api = new LocalDevelopmentAPI(server)
  const comparisons: MixedComparison[] = []
  let failure: unknown
  try {
    await api.login()
    for (const definition of definitions) {
      const item = spec.cases.find(value => value.caseId === definition.caseId)
      if (item === undefined) throw new Error(`Mixed comparison case is missing: ${definition.caseId}`)
      comparisons.push({ ...definition, provisionedCase: await provisionTask(api, primary, item, definition.comparisonId) })
    }
  } catch (error: unknown) { failure = error }
  try { await api.logout() } catch (error: unknown) { if (failure === undefined) failure = error }
  if (failure !== undefined) throw failure
  const manifest: MixedComparisonManifest = {
    schemaVersion: 1, status: 'provisioned', server,
    projectNumber: primary.pactline.projectNumber, createdAt: new Date().toISOString(), comparisons,
  }
  await writePrivate(manifestPath, manifest)
  return manifest
}

/** Run one provisioned mixed route through the same authoritative M4 lifecycle. */
export async function runMixedComparison(comparisonId: MixedComparison['comparisonId']): Promise<L2V2CodexCaseResult> {
  const primaryPath = join(fleetRoot, '.fleet/l2-v2/corpus-manifest.json')
  const mixed = await provisionMixedComparisons({ primaryManifestPath: primaryPath })
  const comparison = mixed.comparisons.find(value => value.comparisonId === comparisonId)
  if (comparison === undefined) throw new Error(`Unknown mixed comparison: ${comparisonId}`)
  return runCodexL2V2Case({
    caseId: comparison.caseId, manifestPath: primaryPath,
    provisionedCase: comparison.provisionedCase, routeSelection: comparison.route,
    runPrefix: comparisonId === 'deepseek-codex' ? 'm4mix-ds-cx' : 'm4mix-cx-ds',
  })
}
