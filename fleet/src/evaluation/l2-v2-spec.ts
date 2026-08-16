import { readFile } from 'node:fs/promises'

const SHA = /^[a-f0-9]{40}$/
const CASE_ID = /^L2V2-[0-9]{2}$/
const BASELINE_ID = /^L2-[0-9]{2}$/

export type L2V2ExpectedPath = 'direct_accept' | 'changes_correction_accept' | 'clean_review_accept' | 'resolution_accept'

export interface L2V2CriterionSpec {
  readonly criterion: string
  readonly verificationInstructions: string
}

export interface L2V2CaseSpec {
  readonly caseId: string
  readonly baselineCaseId: string
  readonly title: string
  readonly description: string
  readonly seedRef: string
  readonly baseRevision: string
  readonly candidate?: { readonly seedRef: string; readonly revision: string }
  readonly allowedPaths: readonly string[]
  readonly verificationCommands: readonly string[]
  readonly expectedPath: L2V2ExpectedPath
  readonly hiddenProfile: string
  readonly resolution?: {
    readonly issueType: 'decision_required'
    readonly supersededCriterionPosition: number
    readonly conclusion: string
  }
  readonly criteria: readonly L2V2CriterionSpec[]
}

export interface L2V2Spec {
  readonly schemaVersion: 1
  readonly projectName: string
  readonly repository: {
    readonly url: string
    readonly baseRef: string
    readonly baseRevision: string
    readonly branchPrefix: string
  }
  readonly cases: readonly L2V2CaseSpec[]
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function text(value: unknown, name: string, max = 8_192): string {
  if (typeof value !== 'string' || value.trim() === '' || Buffer.byteLength(value) > max) throw new Error(`${name} must be bounded non-empty text`)
  return value
}

function sha(value: unknown, name: string): string {
  const parsed = text(value, name, 40)
  if (!SHA.test(parsed)) throw new Error(`${name} must be a lowercase Git SHA`)
  return parsed
}

function ref(value: unknown, name: string): string {
  const parsed = text(value, name, 256)
  if (!parsed.startsWith('refs/heads/') || parsed.includes('..') || /[~^:?*[\\\s]/.test(parsed)) throw new Error(`${name} must be a safe absolute branch ref`)
  return parsed
}

function list(value: unknown, name: string, maxItems: number): string[] {
  if (!Array.isArray(value) || value.length === 0 || value.length > maxItems) throw new Error(`${name} must be a bounded non-empty array`)
  const parsed = value.map((item, index) => text(item, `${name}[${String(index)}]`, 2_048))
  if (new Set(parsed).size !== parsed.length) throw new Error(`${name} must not contain duplicates`)
  return parsed
}

function repositoryPathList(value: unknown, name: string): string[] {
  const parsed = list(value, name, 64)
  if (parsed.some(path => path.startsWith('/') || path.includes('\\') || path.split('/').some(part => part === '.' || part === '..') || path === '.git' || path.startsWith('.git/'))) {
    throw new Error(`${name} contains an unsafe path`)
  }
  return parsed
}

function criterion(value: unknown, name: string): L2V2CriterionSpec {
  const item = record(value, name)
  return {
    criterion: text(item.criterion, `${name}.criterion`),
    verificationInstructions: text(item.verificationInstructions, `${name}.verificationInstructions`),
  }
}

export function parseL2V2Spec(value: unknown): L2V2Spec {
  const root = record(value, 'spec')
  if (root.schemaVersion !== 1) throw new Error('spec.schemaVersion must be 1')
  const repository = record(root.repository, 'spec.repository')
  const repositoryURL = text(repository.url, 'spec.repository.url', 512)
  const parsedURL = new URL(repositoryURL)
  if (parsedURL.protocol !== 'https:' || parsedURL.username !== '' || parsedURL.password !== '' || parsedURL.search !== '' || parsedURL.hash !== '') {
    throw new Error('spec.repository.url must be a credential-free HTTPS URL')
  }
  const branchPrefix = text(repository.branchPrefix, 'spec.repository.branchPrefix', 128)
  if (!branchPrefix.endsWith('/') || !branchPrefix.startsWith('fleet-eval/') || branchPrefix.includes('..') || /[~^:?*[\\\s]/.test(branchPrefix)) {
    throw new Error('spec.repository.branchPrefix is unsafe')
  }
  if (!Array.isArray(root.cases) || root.cases.length !== 6) throw new Error('M4 requires exactly six cases')
  const caseIds = new Set<string>(); const baselineIds = new Set<string>(); const hiddenProfiles = new Set<string>()
  const cases = root.cases.map((raw, index): L2V2CaseSpec => {
    const item = record(raw, `cases[${String(index)}]`)
    const caseId = text(item.caseId, `cases[${String(index)}].caseId`, 16)
    const baselineCaseId = text(item.baselineCaseId, `cases[${String(index)}].baselineCaseId`, 16)
    const hiddenProfile = text(item.hiddenProfile, `cases[${String(index)}].hiddenProfile`, 128)
    if (!CASE_ID.test(caseId) || !BASELINE_ID.test(baselineCaseId)) throw new Error('case IDs are invalid')
    if (caseIds.has(caseId) || baselineIds.has(baselineCaseId) || hiddenProfiles.has(hiddenProfile)) throw new Error('case identities must be unique')
    caseIds.add(caseId); baselineIds.add(baselineCaseId); hiddenProfiles.add(hiddenProfile)
    const expectedPath = text(item.expectedPath, `cases[${String(index)}].expectedPath`, 64)
    if (!['direct_accept', 'changes_correction_accept', 'clean_review_accept', 'resolution_accept'].includes(expectedPath)) throw new Error('case expectedPath is invalid')
    let candidate: L2V2CaseSpec['candidate']
    if (item.candidate !== undefined) {
      const value = record(item.candidate, `cases[${String(index)}].candidate`)
      candidate = { seedRef: ref(value.seedRef, 'candidate.seedRef'), revision: sha(value.revision, 'candidate.revision') }
    }
    let resolution: L2V2CaseSpec['resolution']
    if (item.resolution !== undefined) {
      const value = record(item.resolution, `cases[${String(index)}].resolution`)
      if (value.issueType !== 'decision_required' || !Number.isSafeInteger(value.supersededCriterionPosition) || Number(value.supersededCriterionPosition) < 0) {
        throw new Error('resolution policy is invalid')
      }
      resolution = {
        issueType: 'decision_required', supersededCriterionPosition: Number(value.supersededCriterionPosition),
        conclusion: text(value.conclusion, 'resolution.conclusion'),
      }
    }
    const criteria = Array.isArray(item.criteria) ? item.criteria.map((value, criterionIndex) => criterion(value, `criteria[${String(criterionIndex)}]`)) : []
    if (criteria.length !== 2) throw new Error('each M4 case requires exactly two criteria')
    if ((expectedPath === 'changes_correction_accept' || expectedPath === 'clean_review_accept') !== (candidate !== undefined)) {
      throw new Error('candidate presence does not match the expected path')
    }
    if ((expectedPath === 'resolution_accept') !== (resolution !== undefined)) throw new Error('resolution policy does not match the expected path')
    if (resolution !== undefined && resolution.supersededCriterionPosition >= criteria.length) throw new Error('superseded criterion position is out of range')
    return {
      caseId, baselineCaseId,
      title: text(item.title, `cases[${String(index)}].title`, 512),
      description: text(item.description, `cases[${String(index)}].description`),
      seedRef: ref(item.seedRef, `cases[${String(index)}].seedRef`),
      baseRevision: sha(item.baseRevision, `cases[${String(index)}].baseRevision`),
      ...(candidate === undefined ? {} : { candidate }),
      allowedPaths: repositoryPathList(item.allowedPaths, `cases[${String(index)}].allowedPaths`),
      verificationCommands: list(item.verificationCommands, `cases[${String(index)}].verificationCommands`, 16),
      expectedPath: expectedPath as L2V2ExpectedPath, hiddenProfile,
      ...(resolution === undefined ? {} : { resolution }), criteria,
    }
  })
  return {
    schemaVersion: 1, projectName: text(root.projectName, 'spec.projectName', 256),
    repository: {
      url: repositoryURL, baseRef: ref(repository.baseRef, 'spec.repository.baseRef'),
      baseRevision: sha(repository.baseRevision, 'spec.repository.baseRevision'), branchPrefix,
    },
    cases,
  }
}

export async function loadL2V2Spec(path: string): Promise<L2V2Spec> {
  return parseL2V2Spec(JSON.parse(await readFile(path, 'utf8')) as unknown)
}
