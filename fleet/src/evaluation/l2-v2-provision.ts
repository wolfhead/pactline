import { createHash, randomUUID } from 'node:crypto'
import { chmod, lstat, mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { L2V2CaseSpec, L2V2Spec } from './l2-v2-spec.js'
import {
  defaultL2V2CommandRunner, l2V2EffectInventory, preflightL2V2Repository, type L2V2CommandRunner,
} from './l2-v2-preflight.js'

const DEVELOPMENT_USER_ID = '00000000-0000-0000-0000-000000000001'
const moduleDirectory = dirname(fileURLToPath(import.meta.url))
const fleetRoot = resolve(moduleDirectory, '../..')

interface BrowserSession { readonly cookie: string; readonly csrf: string }

export interface ProvisionedCriterion {
  readonly id: string
  readonly version: number
  readonly revision: number
  readonly position: number
}

export interface ProvisionedCase {
  readonly caseId: string
  readonly taskId: string
  readonly taskNumber: number
  readonly taskVersion: number
  readonly phase: 'backlog'
  readonly criteria: readonly ProvisionedCriterion[]
  readonly baseRef: string
  readonly baseRevision: string
  readonly candidateRef?: string
  readonly candidateRevision?: string
  readonly seededDraftPullRequest?: string
}

export interface L2V2ProvisionManifest {
  readonly schemaVersion: 1
  readonly cohortId: string
  readonly status: 'provisioning' | 'provisioned'
  readonly createdAt: string
  readonly updatedAt: string
  readonly specSha256: string
  readonly server: string
  readonly repository: {
    readonly url: string
    readonly sourceRef: string
    readonly sourceRevision: string
    readonly createdRefs: Readonly<Record<string, string>>
    readonly seededDraftPullRequests: Readonly<Record<string, string>>
  }
  readonly pactline?: {
    readonly projectId: string
    readonly projectNumber: number
    readonly projectVersion: number
    readonly projectName: string
    readonly repositoryId: string
  }
  readonly cases: readonly ProvisionedCase[]
}

export interface L2V2ProvisionOptions {
  readonly server?: string
  readonly manifestPath?: string
  readonly run?: L2V2CommandRunner
  readonly environment?: NodeJS.ProcessEnv
  readonly now?: () => Date
  readonly log?: (message: string) => void
}

interface APIResponse { readonly value: unknown; readonly etag?: string }

function record(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function positiveInteger(value: unknown, name: string): number {
  if (!Number.isSafeInteger(value) || Number(value) < 1) throw new Error(`${name} must be a positive integer`)
  return Number(value)
}

function nonEmpty(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`${name} must be non-empty text`)
  return value
}

function uuid(value: unknown, name: string): string {
  const parsed = nonEmpty(value, name)
  if (!/^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/i.test(parsed)) throw new Error(`${name} must be a UUID`)
  return parsed
}

function etagVersion(value: string | undefined, name: string): number {
  // The local Caddy stack appends its content-encoding suffix to upstream
  // version ETags for compressed responses. Preserve Pactline's numeric
  // concurrency token and never send the proxy suffix back in If-Match.
  const match = /^"([1-9][0-9]*)(?:-gzip)?"$/.exec(value ?? '')
  if (match === null) throw new Error(`${name} did not return a version ETag`)
  return Number(match[1])
}

function responseVersion(response: APIResponse, body: Record<string, unknown>, name: string): number {
  return response.etag === undefined ? positiveInteger(body.version, `${name} body version`) : etagVersion(response.etag, name)
}

function sessionFrom(headers: Headers): BrowserSession {
  const cookies = new Map<string, string>()
  for (const value of headers.getSetCookie()) {
    const pair = value.slice(0, value.indexOf(';'))
    const separator = pair.indexOf('=')
    if (separator > 0) cookies.set(pair.slice(0, separator), pair.slice(separator + 1))
  }
  const session = cookies.get('bb_session'); const csrf = cookies.get('bb_csrf')
  if (session === undefined || csrf === undefined) throw new Error('Development authentication did not return session cookies')
  return { cookie: `bb_session=${session}; bb_csrf=${csrf}`, csrf }
}

function safeProblem(value: unknown): string {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return 'request rejected'
  const item = value as Record<string, unknown>
  const code = typeof item.code === 'string' ? item.code : 'UNKNOWN_ERROR'
  const detail = typeof item.detail === 'string' ? item.detail : ''
  return `${code}${detail === '' ? '' : `: ${detail}`}`.slice(0, 2_048)
}

export class LocalDevelopmentAPI {
  private session: BrowserSession | undefined

  constructor(readonly server: string) {}

  async login(): Promise<void> {
    const response = await fetch(new URL('/api/auth/dev/session', this.server), {
      method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: DEVELOPMENT_USER_ID }), redirect: 'manual', signal: AbortSignal.timeout(30_000),
    })
    if (!response.ok) throw new Error(`Development authentication failed (${String(response.status)})`)
    this.session = sessionFrom(response.headers)
  }

  async logout(): Promise<void> {
    if (this.session === undefined) return
    await this.request('/api/auth/logout', { method: 'POST' })
    this.session = undefined
  }

  async request(path: string, init: RequestInit = {}, idempotencyKey?: string): Promise<APIResponse> {
    if (this.session === undefined) throw new Error('Local development session is not authenticated')
    const headers = new Headers(init.headers)
    headers.set('Accept', 'application/json')
    headers.set('Cookie', this.session.cookie)
    headers.set('Origin', new URL(this.server).origin)
    headers.set('Sec-Fetch-Site', 'same-origin')
    headers.set('X-CSRF-Token', this.session.csrf)
    if (init.body !== undefined) headers.set('Content-Type', 'application/json')
    if (idempotencyKey !== undefined) headers.set('Idempotency-Key', idempotencyKey)
    const response = await fetch(new URL(path, this.server), {
      ...init, headers, redirect: 'manual', signal: AbortSignal.timeout(30_000),
    })
    const text = await response.text()
    let value: unknown
    if (text !== '') {
      try { value = JSON.parse(text) as unknown } catch { throw new Error(`${init.method ?? 'GET'} ${path} returned invalid JSON`) }
    }
    if (!response.ok) throw new Error(`${init.method ?? 'GET'} ${path} failed (${String(response.status)}): ${safeProblem(value)}`)
    const etag = response.headers.get('ETag') ?? undefined
    return { value, ...(etag === undefined ? {} : { etag }) }
  }
}

function targetRef(spec: L2V2Spec, suffix: string): string {
  return `refs/heads/${spec.repository.branchPrefix}${suffix}`
}

function caseSuffix(item: L2V2CaseSpec): string { return item.caseId.toLowerCase() }

function sha256(spec: L2V2Spec): string {
  return createHash('sha256').update(JSON.stringify(spec)).digest('hex')
}

async function writeManifest(path: string, manifest: L2V2ProvisionManifest): Promise<void> {
  const directory = dirname(path)
  await mkdir(directory, { recursive: true, mode: 0o700 })
  await chmod(directory, 0o700)
  const temporary = join(directory, `.${randomUUID()}.tmp`)
  await writeFile(temporary, `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600, flag: 'wx' })
  await rename(temporary, path)
  await chmod(path, 0o600)
}

function newManifest(spec: L2V2Spec, server: string, now: Date): L2V2ProvisionManifest {
  return {
    schemaVersion: 1, cohortId: `m4-${randomUUID()}`, status: 'provisioning',
    createdAt: now.toISOString(), updatedAt: now.toISOString(), specSha256: sha256(spec), server,
    repository: {
      url: spec.repository.url, sourceRef: spec.repository.baseRef, sourceRevision: spec.repository.baseRevision,
      createdRefs: {}, seededDraftPullRequests: {},
    },
    cases: [],
  }
}

function updated(manifest: L2V2ProvisionManifest, patch: Partial<L2V2ProvisionManifest>, now: Date): L2V2ProvisionManifest {
  return { ...manifest, ...patch, updatedAt: now.toISOString() }
}

function remoteRefPlan(spec: L2V2Spec): readonly { readonly ref: string; readonly revision: string }[] {
  return [
    { ref: targetRef(spec, 'source'), revision: spec.repository.baseRevision },
    ...spec.cases.map(item => ({ ref: targetRef(spec, `base/${caseSuffix(item)}`), revision: item.baseRevision })),
    ...spec.cases.flatMap(item => item.candidate === undefined ? [] : [{
      ref: targetRef(spec, `candidate/${caseSuffix(item)}`), revision: item.candidate.revision,
    }]),
  ]
}

function repositoryPath(spec: L2V2Spec): string {
  return new URL(spec.repository.url).pathname.replace(/^\//, '').replace(/\/$/, '')
}

async function createRemoteArtifacts(
  spec: L2V2Spec,
  manifest: L2V2ProvisionManifest,
  manifestPath: string,
  run: L2V2CommandRunner,
  now: () => Date,
  log: (message: string) => void,
): Promise<L2V2ProvisionManifest> {
  let current = manifest
  const repo = repositoryPath(spec)
  for (const item of remoteRefPlan(spec)) {
    const recorded = current.repository.createdRefs[item.ref]
    if (recorded !== undefined) {
      if (recorded !== item.revision) throw new Error(`Recorded frozen ref revision drifted: ${item.ref}`)
      const observed = record(JSON.parse((await run('gh', ['api', `repos/${repo}/git/ref/${item.ref.replace('refs/', '')}`])).stdout) as unknown, 'Recorded GitHub ref')
      const object = record(observed.object, 'Recorded GitHub ref object')
      if (observed.ref !== item.ref || object.sha !== item.revision) throw new Error(`Recorded frozen ref no longer matches GitHub: ${item.ref}`)
      continue
    }
    await run('gh', ['api', '--method', 'POST', `repos/${repo}/git/refs`, '-f', `ref=${item.ref}`, '-f', `sha=${item.revision}`])
    current = updated(current, { repository: {
      ...current.repository, createdRefs: { ...current.repository.createdRefs, [item.ref]: item.revision },
    } }, now())
    await writeManifest(manifestPath, current)
    log(`Created frozen ref ${item.ref}`)
  }
  for (const item of spec.cases.filter(value => value.candidate !== undefined)) {
    const base = targetRef(spec, `base/${caseSuffix(item)}`).replace('refs/heads/', '')
    const head = targetRef(spec, `candidate/${caseSuffix(item)}`).replace('refs/heads/', '')
    const title = `[Fleet ${item.caseId}] Frozen seeded candidate`
    const body = `Coordinator-owned frozen candidate for ${item.caseId}.\n\nThis Draft Pull Request is an isolated Pactline Fleet M4 evaluation artifact. It must not be merged.`
    const recorded = current.repository.seededDraftPullRequests[item.caseId]
    if (recorded !== undefined) {
      const viewed = record(JSON.parse((await run('gh', [
        'pr', 'view', recorded, '--repo', repo, '--json', 'url,isDraft,state,baseRefName,headRefName,headRefOid',
      ])).stdout) as unknown, 'Recorded seeded Draft Pull Request')
      if (viewed.url !== recorded || viewed.isDraft !== true || viewed.state !== 'OPEN'
        || viewed.baseRefName !== base || viewed.headRefName !== head || viewed.headRefOid !== item.candidate?.revision) {
        throw new Error(`Recorded seeded Draft Pull Request drifted: ${item.caseId}`)
      }
      continue
    }
    const result = await run('gh', ['pr', 'create', '--repo', repo, '--base', base, '--head', head, '--draft', '--title', title, '--body', body])
    const url = result.stdout.trim().split('\n').find(line => /^https:\/\/github\.com\//.test(line))
    if (url === undefined) throw new Error(`GitHub did not return the seeded Draft Pull Request URL for ${item.caseId}`)
    current = updated(current, { repository: {
      ...current.repository,
      seededDraftPullRequests: { ...current.repository.seededDraftPullRequests, [item.caseId]: url },
    } }, now())
    await writeManifest(manifestPath, current)
    log(`Created seeded Draft Pull Request for ${item.caseId}`)
  }
  return current
}

function taskBody(spec: L2V2Spec, item: L2V2CaseSpec, projectNumber: number): Record<string, unknown> {
  const baseRef = targetRef(spec, `base/${caseSuffix(item)}`)
  return {
    title: item.title,
    context: [
      item.description,
      `Start from the frozen base ${baseRef} at ${item.baseRevision}.`,
      `Changes are restricted to: ${item.allowedPaths.join(', ')}.`,
      `Required visible verification: ${item.verificationCommands.join('; ')}.`,
    ].join('\n'),
    expected_result: item.criteria.map((criterion, index) => `${String(index + 1)}. ${criterion.criterion}`).join('\n'),
    description: `Isolated Pactline Fleet M4 evaluation case ${item.caseId}; expected lifecycle path: ${item.expectedPath}.`,
    priority: 'medium', project_number: projectNumber,
  }
}

async function recoverOrCreateTask(
  api: LocalDevelopmentAPI,
  spec: L2V2Spec,
  item: L2V2CaseSpec,
  projectNumber: number,
  idempotencyKey: string,
): Promise<{ readonly task: Record<string, unknown>; readonly response?: APIResponse }> {
  const listed = record((await api.request(`/api/v1/tasks?project_number=${String(projectNumber)}&archived=all&limit=200`)).value, 'Task list')
  if (!Array.isArray(listed.items)) throw new Error('Task list items are invalid')
  const matches = listed.items.map(value => record(value, 'Task')).filter(value => value.title === item.title && value.archived_at == null)
  if (matches.length > 1) throw new Error(`Multiple active Pactline Tasks match ${item.caseId}; manual reconciliation is required`)
  const expected = taskBody(spec, item, projectNumber)
  if (matches.length === 1) {
    const task = matches[0]!
    if (task.context !== expected.context || task.expected_result !== expected.expected_result || task.phase !== 'backlog'
      || record(task.project, 'Task Project').number !== projectNumber) {
      throw new Error(`Existing Pactline Task does not match the frozen ${item.caseId} definition`)
    }
    return { task }
  }
  const response = await api.request('/api/v1/tasks', {
    method: 'POST', body: JSON.stringify(expected),
  }, idempotencyKey)
  return { task: record(response.value, `Created Task ${item.caseId}`), response }
}

async function assertProjectNameAvailable(api: LocalDevelopmentAPI, name: string): Promise<void> {
  const response = record((await api.request('/api/v1/projects?archived=all&limit=200')).value, 'Project list')
  if (!Array.isArray(response.items)) throw new Error('Project list items are invalid')
  if (response.items.some(value => record(value, 'Project').name === name)) {
    throw new Error(`Pactline Project already exists without an active M4 provision journal: ${name}`)
  }
}

async function createPactlineArtifacts(
  spec: L2V2Spec,
  manifest: L2V2ProvisionManifest,
  manifestPath: string,
  api: LocalDevelopmentAPI,
  now: () => Date,
  log: (message: string) => void,
): Promise<L2V2ProvisionManifest> {
  const cohortKey = manifest.cohortId
  let current = manifest
  let projectNumber: number
  if (manifest.pactline === undefined) {
    await assertProjectNameAvailable(api, spec.projectName)
    const projectResponse = await api.request('/api/v1/projects', {
      method: 'POST', body: JSON.stringify({ name: spec.projectName, description: 'Isolated harness-neutral Pactline Fleet M4 L2 v2 evaluation cohort.' }),
    }, `${cohortKey}-project`)
    const project = record(projectResponse.value, 'Created Project')
    const projectId = uuid(project.id, 'Project ID'); projectNumber = positiveInteger(project.number, 'Project number')
    let projectVersion = responseVersion(projectResponse, project, 'Project creation')
    const repositoryResponse = await api.request(`/api/v1/projects/${String(projectNumber)}/repositories`, {
      method: 'POST', headers: { 'If-Match': `"${String(projectVersion)}"` },
      body: JSON.stringify({ repository_url: spec.repository.url, provider: 'github' }),
    }, `${cohortKey}-repository`)
    const repositoryMutation = record(repositoryResponse.value, 'Repository binding')
    projectVersion = positiveInteger(repositoryMutation.project_version, 'Bound Project version')
    const projectRepository = record(repositoryMutation.repository, 'Bound Project repository')
    const repositoryId = uuid(projectRepository.id, 'Project repository ID')
    current = updated(manifest, { pactline: {
      projectId, projectNumber, projectVersion, projectName: spec.projectName, repositoryId,
    } }, now())
    await writeManifest(manifestPath, current)
    log(`Created Pactline Project #${String(projectNumber)} and repository binding`)
  } else {
    projectNumber = manifest.pactline.projectNumber
    const projects = record((await api.request('/api/v1/projects?archived=all&limit=200')).value, 'Project list')
    if (!Array.isArray(projects.items) || !projects.items.some(value => {
      const project = record(value, 'Project')
      return project.id === manifest.pactline?.projectId && project.number === projectNumber && project.name === spec.projectName
    })) throw new Error('Recorded M4 Pactline Project no longer matches the local server')
    const repositories = record((await api.request(`/api/v1/projects/${String(projectNumber)}/repositories`)).value, 'Project repository list')
    if (!Array.isArray(repositories.items) || !repositories.items.some(value => {
      const repository = record(value, 'Project repository')
      return repository.id === manifest.pactline?.repositoryId && repository.canonical_web_url === spec.repository.url
    })) throw new Error('Recorded M4 Project repository binding no longer matches the local server')
    log(`Resuming recorded Pactline Project #${String(projectNumber)}`)
  }

  const cases: ProvisionedCase[] = [...current.cases]
  for (const item of spec.cases) {
    if (cases.some(value => value.caseId === item.caseId)) continue
    const recovered = await recoverOrCreateTask(
      api, spec, item, projectNumber, `${cohortKey}-${item.caseId.toLowerCase()}-task`,
    )
    const task = recovered.task
    const taskId = uuid(task.id, `${item.caseId} Task ID`); const taskNumber = positiveInteger(task.number, `${item.caseId} Task number`)
    let taskVersion = recovered.response === undefined
      ? positiveInteger(task.version, `${item.caseId} Task version`)
      : responseVersion(recovered.response, task, `${item.caseId} Task creation`)
    const listedCriteria = record((await api.request(`/api/v1/tasks/${String(taskNumber)}/criteria?limit=200`)).value, 'Criterion list')
    if (!Array.isArray(listedCriteria.items)) throw new Error('Criterion list items are invalid')
    const criteria = listedCriteria.items.map((value, index): ProvisionedCriterion => {
      const criterion = record(value, 'Existing Criterion')
      const position = Number(criterion.position)
      const expected = item.criteria[position]
      if (expected === undefined || criterion.criterion !== expected.criterion
        || criterion.verification_instructions !== expected.verificationInstructions) {
        throw new Error(`Existing Pactline Criterion does not match the frozen ${item.caseId} definition`)
      }
      return {
        id: uuid(criterion.id, 'Criterion ID'), version: positiveInteger(criterion.version, 'Criterion version'),
        revision: positiveInteger(criterion.revision, 'Criterion revision'), position: Number(criterion.position),
      }
    }).sort((left, right) => left.position - right.position)
    if (criteria.some((criterion, index) => criterion.position !== index) || criteria.length > item.criteria.length) {
      throw new Error(`Existing Pactline Criteria are incomplete or duplicated for ${item.caseId}`)
    }
    for (let position = criteria.length; position < item.criteria.length; position++) {
      const criterion = item.criteria[position]!
      const criterionResponse = await api.request(`/api/v1/tasks/${String(taskNumber)}/criteria`, {
        method: 'POST', headers: { 'If-Match': `"${String(taskVersion)}"` }, body: JSON.stringify({
          criterion: criterion.criterion, verification_instructions: criterion.verificationInstructions, position,
        }),
      }, `${cohortKey}-${item.caseId.toLowerCase()}-criterion-${String(position)}`)
      const value = record(criterionResponse.value, `${item.caseId} Criterion ${String(position)}`)
      criteria.push({
        id: uuid(value.id, 'Criterion ID'), version: positiveInteger(value.version, 'Criterion version'),
        revision: positiveInteger(value.revision, 'Criterion revision'), position: Number(value.position),
      })
      taskVersion += 1
    }
    const provisioned: ProvisionedCase = {
      caseId: item.caseId, taskId, taskNumber, taskVersion, phase: 'backlog', criteria,
      baseRef: targetRef(spec, `base/${caseSuffix(item)}`), baseRevision: item.baseRevision,
      ...(item.candidate === undefined ? {} : {
        candidateRef: targetRef(spec, `candidate/${caseSuffix(item)}`), candidateRevision: item.candidate.revision,
        seededDraftPullRequest: current.repository.seededDraftPullRequests[item.caseId],
      }),
    }
    cases.push(provisioned)
    current = updated(current, { cases: [...cases] }, now())
    await writeManifest(manifestPath, current)
    log(`Created ${item.caseId} as Pactline Task #${String(taskNumber)} in backlog`)
  }
  return updated(current, { status: 'provisioned' }, now())
}

/** Create the exact isolated M4 repository and Pactline corpus after a final no-write preflight. */
export async function provisionL2V2(spec: L2V2Spec, options: L2V2ProvisionOptions = {}): Promise<L2V2ProvisionManifest> {
  const server = (options.server ?? options.environment?.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173').replace(/\/$/, '')
  const manifestPath = resolve(options.manifestPath ?? join(fleetRoot, '.fleet/l2-v2/corpus-manifest.json'))
  const now = options.now ?? (() => new Date())
  const log = options.log ?? (message => { process.stdout.write(`${message}\n`) })
  const run = options.run
  const inventory = l2V2EffectInventory(spec)
  let manifest: L2V2ProvisionManifest
  try {
    const info = await lstat(manifestPath)
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) throw new Error('M4 provision manifest must be a private regular file')
    manifest = JSON.parse(await readFile(manifestPath, 'utf8')) as L2V2ProvisionManifest
    if (manifest.schemaVersion !== 1 || !['provisioning', 'provisioned'].includes(manifest.status)
      || manifest.specSha256 !== sha256(spec) || manifest.server !== server || manifest.repository.url !== spec.repository.url) {
      throw new Error('Existing M4 provision manifest does not match the frozen cohort')
    }
    log(`Resuming private M4 provision journal ${manifest.cohortId}`)
  } catch (error: unknown) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error
    const preflight = await preflightL2V2Repository(spec, run)
    if (preflight.targetNamespaceEmpty !== true || inventory.staticRefCreates.length !== 9) throw new Error('M4 effect inventory changed after approval')
    manifest = newManifest(spec, server, now())
    await writeManifest(manifestPath, manifest)
    log(`Private M4 provision journal created at ${manifestPath}`)
  }
  manifest = await createRemoteArtifacts(spec, manifest, manifestPath, run ?? defaultL2V2CommandRunner, now, log)
  const api = new LocalDevelopmentAPI(server)
  let failure: unknown
  try {
    await api.login()
    manifest = await createPactlineArtifacts(spec, manifest, manifestPath, api, now, log)
    await writeManifest(manifestPath, manifest)
  } catch (error) { failure = error }
  try { await api.logout() } catch (error) { failure = failure === undefined ? error : new AggregateError([failure, error], 'M4 provisioning and logout failed') }
  if (failure !== undefined) throw failure
  return manifest
}
