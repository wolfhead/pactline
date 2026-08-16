import { lstat, readFile } from 'node:fs/promises'
import type { FleetWorkDefinition } from '../core/work-definition.js'
import type { RepositoryIdentity } from '../repository/delivery.js'
import { validateRepositoryDelivery } from '../repository/delivery.js'

export type { FleetWorkDefinition } from '../core/work-definition.js'

export interface FleetEvaluationCorpus {
  readonly schemaVersion: 1
  readonly projectNumber: number
  readonly cases: readonly FleetWorkDefinition[]
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function text(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`${name} must be non-empty`)
  return value
}

function integer(value: unknown, name: string): number {
  if (!Number.isSafeInteger(value) || Number(value) < 1) throw new Error(`${name} must be a positive integer`)
  return Number(value)
}

function revision(value: unknown, name: string): string {
  const parsed = text(value, name)
  if (!/^[a-f0-9]{40}$/.test(parsed)) throw new Error(`${name} must be a lowercase 40-character Git SHA`)
  return parsed
}

function stringList(value: unknown, name: string, maximum: number): string[] {
  if (!Array.isArray(value) || value.length === 0 || value.length > maximum) throw new Error(`${name} must be a non-empty bounded array`)
  const values = value.map((item, index) => text(item, `${name}[${String(index)}]`))
  if (new Set(values).size !== values.length) throw new Error(`${name} must not contain duplicates`)
  return values
}

function repositoryPathList(value: unknown, name: string): string[] {
  const values = stringList(value, name, 64)
  if (values.some(path => path.startsWith('/') || path.includes('\\') || path.split('/').includes('..') || path === '.git' || path.startsWith('.git/'))) {
    throw new Error(`${name} contains an unsafe path`)
  }
  return values
}

function repository(value: unknown, name: string): RepositoryIdentity {
  const item = record(value, name)
  if (!['github', 'gitlab'].includes(String(item.provider))) throw new Error(`${name}.provider is invalid`)
  return {
    provider: item.provider as RepositoryIdentity['provider'],
    host: text(item.host, `${name}.host`), owner: text(item.owner, `${name}.owner`), name: text(item.name, `${name}.name`),
  }
}

export function parseFleetEvaluationCorpus(value: unknown): FleetEvaluationCorpus {
  const root = record(value, 'corpus')
  if (root.schemaVersion !== 1) throw new Error('corpus.schemaVersion must be 1')
  if (!Array.isArray(root.cases) || root.cases.length === 0 || root.cases.length > 64) throw new Error('corpus.cases must be bounded and non-empty')
  const ids = new Set<string>()
  const tasks = new Set<number>()
  const cases = root.cases.map((raw, index): FleetWorkDefinition => {
    const item = record(raw, `cases[${String(index)}]`)
    const caseId = text(item.caseId, `cases[${String(index)}].caseId`)
    const taskNumber = integer(item.taskNumber, `cases[${String(index)}].taskNumber`)
    if (ids.has(caseId) || tasks.has(taskNumber)) throw new Error('Corpus case IDs and Task numbers must be unique')
    ids.add(caseId); tasks.add(taskNumber)
    const base = record(item.base, `cases[${String(index)}].base`)
    if (!Array.isArray(item.criteria) || item.criteria.length === 0 || item.criteria.length > 32) throw new Error('Case criteria must be bounded and non-empty')
    const criteria = item.criteria.map((rawCriterion, criterionIndex) => {
      const criterion = record(rawCriterion, `criteria[${String(criterionIndex)}]`)
      return { id: text(criterion.id, 'criterion.id'), revision: integer(criterion.revision, 'criterion.revision') }
    })
    const repositoryIdentity = repository(item.repository, 'repository')
    let candidate: FleetWorkDefinition['candidate']
    if (item.candidate !== undefined) {
      const rawCandidate = record(item.candidate, 'candidate')
      candidate = {
        repository: repositoryIdentity,
        ref: text(rawCandidate.ref, 'candidate.ref'),
        codeChangeUrl: text(rawCandidate.codeChangeUrl, 'candidate.codeChangeUrl'),
        revision: revision(rawCandidate.revision, 'candidate.revision'),
        branch: text(rawCandidate.branch, 'candidate.branch'),
      }
      validateRepositoryDelivery(candidate)
    }
    return {
      caseId, taskNumber, taskVersion: integer(item.taskVersion, `cases[${String(index)}].taskVersion`),
      base: { source: text(base.source, 'base.source'), ref: text(base.ref, 'base.ref'), revision: revision(base.revision, 'base.revision') },
      repository: repositoryIdentity,
      allowedPaths: repositoryPathList(item.allowedPaths, 'allowedPaths'),
      verificationCommands: stringList(item.verificationCommands, 'verificationCommands', 32),
      criteria,
      ...(candidate === undefined ? {} : { candidate }),
    }
  })
  return { schemaVersion: 1, projectNumber: integer(root.projectNumber, 'projectNumber'), cases }
}

export async function loadFleetEvaluationCorpus(path: string): Promise<FleetEvaluationCorpus> {
  const info = await lstat(path)
  if (!info.isFile() || info.isSymbolicLink()) throw new Error('Fleet corpus must be a regular non-symlink file')
  if ((info.mode & 0o077) !== 0) throw new Error('Fleet corpus must be owner-only')
  let value: unknown
  try { value = JSON.parse(await readFile(path, 'utf8')) as unknown } catch { throw new Error('Fleet corpus is invalid JSON') }
  return parseFleetEvaluationCorpus(value)
}
