import { randomUUID } from 'node:crypto'
import { PactlineCLI } from '../pactline/client.js'

const DEVELOPMENT_USER_ID = '00000000-0000-0000-0000-000000000001'
const TOKEN_PREFIX = 'pactline-fleet-m1-integration:'

interface BrowserSession {
  readonly cookie: string
  readonly csrf: string
}

interface IssuedToken {
  readonly id: string
  readonly token: string
}

export interface LocalPactlineIntegrationOptions {
  readonly server?: string
  readonly pactlineExecutable?: string
  readonly environment?: NodeJS.ProcessEnv
  readonly log?: (message: string) => void
}

function record(value: unknown, name = 'response'): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${name} must be an object`)
  return value as Record<string, unknown>
}

function sessionFrom(headers: Headers): BrowserSession {
  const values = headers.getSetCookie()
  const cookies = new Map<string, string>()
  for (const value of values) {
    const pair = value.slice(0, value.indexOf(';'))
    const separator = pair.indexOf('=')
    if (separator > 0) cookies.set(pair.slice(0, separator), pair.slice(separator + 1))
  }
  const session = cookies.get('bb_session')
  const csrf = cookies.get('bb_csrf')
  if (session === undefined || csrf === undefined) throw new Error('Development authentication did not return Fleet session cookies')
  return { cookie: `bb_session=${session}; bb_csrf=${csrf}`, csrf }
}

async function browserRequest(server: string, path: string, init: RequestInit, session?: BrowserSession): Promise<unknown> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body !== undefined) headers.set('Content-Type', 'application/json')
  if (session !== undefined) {
    headers.set('Cookie', session.cookie)
    headers.set('Origin', new URL(server).origin)
    headers.set('Sec-Fetch-Site', 'same-origin')
    headers.set('X-CSRF-Token', session.csrf)
  }
  const response = await fetch(new URL(path, server), { ...init, headers, redirect: 'manual', signal: AbortSignal.timeout(30_000) })
  const body = response.status === 204 ? undefined : await response.json().catch(() => undefined)
  if (!response.ok) throw new Error(`${init.method ?? 'GET'} ${path} failed with status ${String(response.status)}`)
  return body
}

async function createSession(server: string): Promise<BrowserSession> {
  const response = await fetch(new URL('/api/auth/dev/session', server), {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ user_id: DEVELOPMENT_USER_ID }),
    redirect: 'manual',
    signal: AbortSignal.timeout(30_000),
  })
  if (!response.ok) throw new Error(`Development authentication failed with status ${String(response.status)}`)
  return sessionFrom(response.headers)
}

async function issueToken(server: string, session: BrowserSession, runId: string): Promise<IssuedToken> {
  const value = record(await browserRequest(server, '/api/account/tokens', {
    method: 'POST',
    body: JSON.stringify({ name: TOKEN_PREFIX + runId, scopes: ['work:execute'], expires_in_days: 30 }),
  }, session), 'Token response')
  if (typeof value.id !== 'string' || typeof value.token !== 'string') throw new Error('Token response is invalid')
  return { id: value.id, token: value.token }
}

/** Exercise the installed CLI against local Docker without mutating Pactline work resources. */
export async function runLocalPactlineIntegration(options: LocalPactlineIntegrationOptions = {}): Promise<void> {
  const environment = options.environment ?? process.env
  const server = (options.server ?? environment.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173').replace(/\/$/, '')
  const executable = options.pactlineExecutable ?? environment.PACTLINE_FLEET_PACTLINE_BIN ?? 'pactline'
  const log = options.log ?? (message => process.stdout.write(`${message}\n`))
  const runId = randomUUID()
  let session: BrowserSession | undefined
  let token: IssuedToken | undefined
  let failure: unknown
  try {
    session = await createSession(server)
    token = await issueToken(server, session, runId)
    const client = new PactlineCLI(
      { executable, server, clientKind: 'pactline-fleet-m1-integration' },
      { environment: { ...environment, PACTLINE_TOKEN: token.token } },
    )
    const preflight = await client.preflight({ sessionId: runId })
    if (preflight.doctor?.token !== 'configured') throw new Error('Pactline doctor did not confirm configured authentication')
    log(`Pactline CLI authenticated: protocol=${String(preflight.capabilities.protocol)}, features=${String(preflight.capabilities.features.length)}`)

    const projects = record(await browserRequest(server, '/api/v1/projects?limit=2', { method: 'GET' }, session), 'Project list')
    const items = Array.isArray(projects.items) ? projects.items : []
    if (items.length > 0) {
      for (const value of items) {
        const project = record(value, 'Project')
        if (typeof project.number !== 'number') throw new Error('Project list returned an invalid number')
        const [execution, review] = await Promise.all([
          client.listTasks('execution', project.number, 1, { sessionId: runId }),
          client.listTasks('review', project.number, 1, { sessionId: runId }),
        ])
        log(`Pactline CLI discovery passed: project=${String(project.number)}, execution=${String(execution.data.length)}, review=${String(review.data.length)}`)
      }
      log(`Pactline multi-Project discovery passed: projects=${String(items.length)}`)
    } else {
      log('Pactline CLI discovery skipped: local server has no visible Project')
    }
  } catch (error: unknown) {
    failure = error
  } finally {
    const cleanupErrors: unknown[] = []
    if (session !== undefined && token !== undefined) {
      try { await browserRequest(server, `/api/account/tokens/${encodeURIComponent(token.id)}`, { method: 'DELETE' }, session) } catch (error) { cleanupErrors.push(error) }
    }
    if (session !== undefined) {
      try { await browserRequest(server, '/api/auth/logout', { method: 'POST' }, session) } catch (error) { cleanupErrors.push(error) }
    }
    if (cleanupErrors.length > 0) failure = new AggregateError([...(failure === undefined ? [] : [failure]), ...cleanupErrors], 'Local Pactline integration cleanup failed')
  }
  if (failure !== undefined) throw failure
  log('Fleet local Docker integration PASS: ephemeral Token revoked; no work resource mutated.')
}
