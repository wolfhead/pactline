import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'
import type { FleetServiceHealth } from '../health/model.js'

const LOOPBACK_HOSTS = new Set(['127.0.0.1', 'localhost', '[::1]', '::1'])

export interface FleetHealthServerAddress {
  readonly address: string
  readonly port: number
  readonly url: string
}

function hostName(host: string): string | undefined {
  try {
    return new URL(`http://${host}`).hostname
  } catch {
    return undefined
  }
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
  } catch {
    return false
  }
}

function responseHeaders(response: ServerResponse, status: number): void {
  response.writeHead(status, {
    'Cache-Control': 'no-store',
    'Content-Security-Policy': "default-src 'none'; frame-ancestors 'none'; base-uri 'none'",
    'Content-Type': 'application/json; charset=utf-8',
    'Referrer-Policy': 'no-referrer',
    'X-Content-Type-Options': 'nosniff',
    'X-Frame-Options': 'DENY',
  })
}

function json(response: ServerResponse, status: number, value: unknown): void {
  responseHeaders(response, status)
  response.end(`${JSON.stringify(value)}\n`)
}

export class FleetHealthServer {
  private server: Server | undefined
  private addressValue: FleetHealthServerAddress | undefined

  constructor(private readonly health: () => FleetServiceHealth) {}

  get address(): FleetHealthServerAddress {
    if (this.addressValue === undefined) throw new Error('Fleet health server is not listening')
    return this.addressValue
  }

  async start(address: '127.0.0.1' | '::1' | 'localhost', port: number): Promise<FleetHealthServerAddress> {
    if (this.server !== undefined) throw new Error('Fleet health server is already started')
    const server = createServer((request, response) => { this.handle(request, response) })
    this.server = server
    await new Promise<void>((resolvePromise, reject) => {
      const onError = (error: Error): void => {
        server.off('listening', onListening)
        reject(error)
      }
      const onListening = (): void => {
        server.off('error', onError)
        resolvePromise()
      }
      server.once('error', onError)
      server.once('listening', onListening)
      server.listen(port, address)
    })
    const bound = server.address() as AddressInfo
    const displayHost = bound.family === 'IPv6' ? `[${bound.address}]` : bound.address
    this.addressValue = {
      address: bound.address,
      port: bound.port,
      url: `http://${displayHost}:${String(bound.port)}`,
    }
    return this.addressValue
  }

  async close(): Promise<void> {
    const server = this.server
    this.server = undefined
    this.addressValue = undefined
    if (server === undefined || !server.listening) return
    await new Promise<void>((resolvePromise, reject) => {
      server.close(error => {
        if (error !== undefined) reject(error)
        else resolvePromise()
      })
    })
  }

  private handle(request: IncomingMessage, response: ServerResponse): void {
    if (!requestAllowed(request)) {
      json(response, 403, { ok: false, error: { code: 'LOCAL_REQUEST_REQUIRED', message: 'Loopback Host and Origin are required' } })
      return
    }
    if (request.method !== 'GET') {
      response.setHeader('Allow', 'GET')
      json(response, 405, { ok: false, error: { code: 'METHOD_NOT_ALLOWED', message: 'The Fleet observation API is read-only' } })
      return
    }
    const path = new URL(request.url ?? '/', 'http://localhost').pathname
    const snapshot = this.health()
    if (path === '/livez') {
      json(response, snapshot.live ? 200 : 503, {
        ok: snapshot.live,
        data: { service_id: snapshot.serviceId, mode: snapshot.mode },
      })
      return
    }
    if (path === '/readyz') {
      json(response, snapshot.ready ? 200 : 503, {
        ok: snapshot.ready,
        data: { service_id: snapshot.serviceId, mode: snapshot.mode },
      })
      return
    }
    if (path === '/healthz' || path === '/api/v1/service') {
      json(response, 200, { ok: true, data: snapshot })
      return
    }
    json(response, 404, { ok: false, error: { code: 'NOT_FOUND', message: 'Fleet observation route not found' } })
  }
}
