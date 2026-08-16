import { createReadStream } from 'node:fs'
import { stat } from 'node:fs/promises'
import { extname, relative, resolve } from 'node:path'
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'
import type { FleetServiceHealth } from '../health/model.js'
import type { FleetObservationSource } from '../observation/projection.js'
import type { FleetRunListOptions, FleetRunStage, FleetRunState } from '../registry/fleet-registry.js'

const LOOPBACK_HOSTS = new Set(['127.0.0.1', 'localhost', '[::1]', '::1'])
const RUN_STATES = new Set<FleetRunState>([
  'admitted', 'claiming', 'claimed', 'preparing_workspace', 'starting_harness',
  'running_harness', 'validating', 'delivering', 'settling', 'releasing',
  'completed', 'released', 'quarantined', 'failed',
])
const RUN_STAGES = new Set<FleetRunStage>(['execution', 'review', 'correction'])
const MIME_TYPES: Readonly<Record<string, string>> = {
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.ico': 'image/x-icon',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
}

export interface FleetHealthServerAddress {
  readonly address: string
  readonly port: number
  readonly url: string
}

export interface FleetHealthServerOptions {
  readonly observation?: FleetObservationSource
  readonly staticDirectory?: string
  readonly eventPollIntervalMs?: number
}

function hostName(host: string): string | undefined {
  try { return new URL(`http://${host}`).hostname } catch { return undefined }
}

function requestAllowed(request: IncomingMessage): boolean {
  const host = request.headers.host
  if (host === undefined) return false
  const hostname = hostName(host)
  if (hostname === undefined || !LOOPBACK_HOSTS.has(hostname)) return false
  const origin = request.headers.origin
  if (origin === undefined) return true
  try {
    const parsed = new URL(origin)
    return parsed.protocol === 'http:' && parsed.host === host && LOOPBACK_HOSTS.has(parsed.hostname)
  } catch { return false }
}

function baseHeaders(contentType: string, ui = false): Record<string, string> {
  return {
    'Cache-Control': 'no-store',
    'Content-Security-Policy': ui
      ? "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
      : "default-src 'none'; frame-ancestors 'none'; base-uri 'none'",
    'Content-Type': contentType,
    'Referrer-Policy': 'no-referrer',
    'X-Content-Type-Options': 'nosniff',
    'X-Frame-Options': 'DENY',
  }
}

function json(response: ServerResponse, status: number, value: unknown): void {
  response.writeHead(status, baseHeaders('application/json; charset=utf-8'))
  response.end(`${JSON.stringify(value)}\n`)
}

function problem(response: ServerResponse, status: number, code: string, message: string): void {
  json(response, status, { ok: false, error: { code, message } })
}

function positiveInteger(value: string | null, fallback: number): number | undefined {
  if (value === null) return fallback
  if (!/^\d+$/.test(value)) return undefined
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined
}

function pathSegment(value: string): string | undefined {
  try {
    const decoded = decodeURIComponent(value)
    return decoded.includes('/') ? undefined : decoded
  } catch { return undefined }
}

export class FleetHealthServer {
  private server: Server | undefined
  private addressValue: FleetHealthServerAddress | undefined
  private readonly eventClients = new Set<ServerResponse>()

  constructor(
    private readonly health: () => FleetServiceHealth,
    private readonly options: FleetHealthServerOptions = {},
  ) {}

  get address(): FleetHealthServerAddress {
    if (this.addressValue === undefined) throw new Error('Fleet health server is not listening')
    return this.addressValue
  }

  async start(address: '127.0.0.1' | '::1' | 'localhost', port: number): Promise<FleetHealthServerAddress> {
    if (this.server !== undefined) throw new Error('Fleet health server is already started')
    const server = createServer((request, response) => { void this.handle(request, response) })
    this.server = server
    await new Promise<void>((resolvePromise, reject) => {
      const onError = (error: Error): void => { server.off('listening', onListening); reject(error) }
      const onListening = (): void => { server.off('error', onError); resolvePromise() }
      server.once('error', onError)
      server.once('listening', onListening)
      server.listen(port, address)
    })
    const bound = server.address() as AddressInfo
    const displayHost = bound.family === 'IPv6' ? `[${bound.address}]` : bound.address
    this.addressValue = { address: bound.address, port: bound.port, url: `http://${displayHost}:${String(bound.port)}` }
    return this.addressValue
  }

  async close(): Promise<void> {
    const server = this.server
    this.server = undefined
    this.addressValue = undefined
    for (const client of this.eventClients) client.end()
    this.eventClients.clear()
    if (server === undefined || !server.listening) return
    await new Promise<void>((resolvePromise, reject) => {
      server.close(error => { if (error !== undefined) reject(error); else resolvePromise() })
    })
  }

  private async handle(request: IncomingMessage, response: ServerResponse): Promise<void> {
    if (!requestAllowed(request)) {
      problem(response, 403, 'LOCAL_REQUEST_REQUIRED', 'Loopback Host and Origin are required')
      return
    }
    if (request.method !== 'GET') {
      response.setHeader('Allow', 'GET')
      problem(response, 405, 'METHOD_NOT_ALLOWED', 'The Fleet observation API is read-only')
      return
    }
    const url = new URL(request.url ?? '/', 'http://localhost')
    const path = url.pathname
    const snapshot = this.health()
    if (path === '/livez') {
      json(response, snapshot.live ? 200 : 503, { ok: snapshot.live, data: { service_id: snapshot.serviceId, mode: snapshot.mode } })
      return
    }
    if (path === '/readyz') {
      json(response, snapshot.ready ? 200 : 503, { ok: snapshot.ready, data: { service_id: snapshot.serviceId, mode: snapshot.mode } })
      return
    }
    if (path === '/healthz') { json(response, 200, { ok: true, data: snapshot }); return }

    const observation = this.options.observation
    if (path === '/api/v1/service') {
      json(response, 200, observation?.service() ?? { ok: true, data: snapshot })
      return
    }
    if (path === '/api/v1/overview' && observation !== undefined) { json(response, 200, observation.overview()); return }
    if (path === '/api/v1/fleets' && observation !== undefined) { json(response, 200, observation.fleets()); return }
    if (path.startsWith('/api/v1/fleets/') && observation !== undefined) {
      const id = pathSegment(path.slice('/api/v1/fleets/'.length))
      const value = id === undefined ? undefined : observation.fleet(id)
      if (value === undefined) problem(response, 404, 'FLEET_NOT_FOUND', 'Fleet was not found')
      else json(response, 200, value)
      return
    }
    if (path === '/api/v1/runs' && observation !== undefined) {
      const limit = positiveInteger(url.searchParams.get('limit'), 50)
      const stateValue = url.searchParams.get('state')
      const stageValue = url.searchParams.get('stage')
      if (limit === undefined || limit > 200 || (stateValue !== null && !RUN_STATES.has(stateValue as FleetRunState))
        || (stageValue !== null && !RUN_STAGES.has(stageValue as FleetRunStage))) {
        problem(response, 400, 'INVALID_RUN_FILTER', 'Run filters are invalid')
        return
      }
      const query: FleetRunListOptions = {
        limit,
        ...(url.searchParams.get('fleet') === null ? {} : { fleetId: url.searchParams.get('fleet')! }),
        ...(stateValue === null ? {} : { state: stateValue as FleetRunState }),
        ...(stageValue === null ? {} : { stage: stageValue as FleetRunStage }),
        ...(url.searchParams.get('before') === null ? {} : { before: url.searchParams.get('before')! }),
      }
      json(response, 200, observation.runs(query))
      return
    }
    if (path.startsWith('/api/v1/runs/') && observation !== undefined) {
      const id = pathSegment(path.slice('/api/v1/runs/'.length))
      const value = id === undefined ? undefined : observation.run(id)
      if (value === undefined) problem(response, 404, 'RUN_NOT_FOUND', 'Run was not found')
      else json(response, 200, value)
      return
    }
    if (path === '/api/v1/adapters' && observation !== undefined) { json(response, 200, observation.adapters()); return }
    if (path === '/api/v1/events' && observation !== undefined) { this.events(request, response, observation); return }
    if (path === '/metrics' && observation !== undefined) {
      response.writeHead(200, baseHeaders('text/plain; version=0.0.4; charset=utf-8'))
      response.end(observation.metrics())
      return
    }
    if (!path.startsWith('/api/') && this.options.staticDirectory !== undefined && await this.staticAsset(path, response)) return
    problem(response, 404, 'NOT_FOUND', 'Fleet observation route not found')
  }

  private events(request: IncomingMessage, response: ServerResponse, observation: FleetObservationSource): void {
    response.writeHead(200, {
      ...baseHeaders('text/event-stream; charset=utf-8'),
      Connection: 'keep-alive',
      'X-Accel-Buffering': 'no',
    })
    this.eventClients.add(response)
    let revision = observation.revision()
    response.write(`event: snapshot\nid: ${revision}\ndata: ${JSON.stringify({ revision })}\n\n`)
    let ticks = 0
    const timer = setInterval(() => {
      ticks += 1
      const next = observation.revision()
      if (next !== revision) {
        revision = next
        response.write(`event: snapshot\nid: ${revision}\ndata: ${JSON.stringify({ revision })}\n\n`)
      } else if (ticks % 15 === 0) response.write(': heartbeat\n\n')
    }, this.options.eventPollIntervalMs ?? 1_000)
    timer.unref()
    request.once('close', () => { clearInterval(timer); this.eventClients.delete(response) })
  }

  private async staticAsset(requestPath: string, response: ServerResponse): Promise<boolean> {
    const root = resolve(this.options.staticDirectory!)
    const requested = requestPath === '/' ? 'index.html' : requestPath.replace(/^\/+/, '')
    let candidate = resolve(root, requested)
    if (relative(root, candidate).startsWith('..')) return false
    try {
      if (!(await stat(candidate)).isFile()) return false
    } catch {
      if (extname(requested) !== '') return false
      candidate = resolve(root, 'index.html')
      try { if (!(await stat(candidate)).isFile()) return false } catch { return false }
    }
    const extension = extname(candidate)
    const immutable = candidate.includes(`${resolve(root, 'assets')}/`)
    response.writeHead(200, {
      ...baseHeaders(MIME_TYPES[extension] ?? 'application/octet-stream', true),
      'Cache-Control': immutable ? 'public, max-age=31536000, immutable' : 'no-store',
    })
    createReadStream(candidate).pipe(response)
    return true
  }
}
