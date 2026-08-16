import { request } from 'node:http'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { FleetHealthServer } from '../../src/http/health-server.js'
import type { FleetServiceHealth } from '../../src/health/model.js'
import type { FleetObservationSource } from '../../src/observation/projection.js'

const directories: string[] = []
afterEach(async () => { await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true }))) })

function projection(ready: boolean): FleetServiceHealth {
  return {
    serviceId: 'service-test',
    version: 'test',
    mode: ready ? 'ready' : 'degraded',
    live: true,
    ready,
    startedAt: '2026-08-15T10:00:00.000Z',
    updatedAt: '2026-08-15T10:00:01.000Z',
    config: {
      revision: 'a'.repeat(64),
      loadedAt: '2026-08-15T10:00:00.000Z',
    },
    registry: {
      status: 'ok',
      path: '/tmp/fleet.sqlite3',
      schemaVersion: 1,
      nonTerminalRuns: 0,
    },
    pactline: {
      status: 'ok',
      server: 'http://localhost:8080',
    },
    adapters: [],
    fleets: [],
  }
}

function call(options: {
  readonly port: number
  readonly path: string
  readonly method?: string
  readonly host?: string
  readonly origin?: string
}): Promise<{ status: number; headers: Record<string, string | string[] | undefined>; body: string }> {
  return new Promise((resolvePromise, reject) => {
    const req = request({
      hostname: '127.0.0.1',
      port: options.port,
      path: options.path,
      method: options.method ?? 'GET',
      headers: {
        Host: options.host ?? `127.0.0.1:${String(options.port)}`,
        ...(options.origin === undefined ? {} : { Origin: options.origin }),
      },
    }, response => {
      const chunks: Buffer[] = []
      response.on('data', (chunk: Buffer) => chunks.push(chunk))
      response.on('end', () => {
        resolvePromise({
          status: response.statusCode ?? 0,
          headers: response.headers,
          body: Buffer.concat(chunks).toString('utf8'),
        })
      })
    })
    req.on('error', reject)
    req.end()
  })
}

function firstEvent(port: number): Promise<{ status: number; contentType?: string; body: string }> {
  return new Promise((resolvePromise, reject) => {
    const req = request({ hostname: '127.0.0.1', port, path: '/api/v1/events', headers: { Host: `127.0.0.1:${String(port)}` } }, response => {
      response.once('data', (chunk: Buffer) => {
        const contentType = response.headers['content-type']
        resolvePromise({ status: response.statusCode ?? 0, ...(contentType === undefined ? {} : { contentType }), body: chunk.toString('utf8') })
        response.destroy()
      })
    })
    req.on('error', reject)
    req.end()
  })
}

describe('FleetHealthServer', () => {
  it('serves liveness and readiness with bounded security headers', async () => {
    let ready = false
    const server = new FleetHealthServer(() => projection(ready))
    const address = await server.start('127.0.0.1', 0)
    try {
      const live = await call({ port: address.port, path: '/livez' })
      expect(live.status).toBe(200)
      expect(JSON.parse(live.body)).toMatchObject({ ok: true, data: { mode: 'degraded' } })
      expect(live.headers['access-control-allow-origin']).toBeUndefined()
      expect(live.headers['content-security-policy']).toContain("default-src 'none'")

      const unavailable = await call({ port: address.port, path: '/readyz' })
      expect(unavailable.status).toBe(503)
      ready = true
      const available = await call({ port: address.port, path: '/readyz' })
      expect(available.status).toBe(200)
    } finally {
      await server.close()
    }
  })

  it('rejects remote Host, cross-origin, and mutation requests', async () => {
    const server = new FleetHealthServer(() => projection(true))
    const address = await server.start('127.0.0.1', 0)
    try {
      expect((await call({ port: address.port, path: '/healthz', host: 'example.com' })).status).toBe(403)
      expect((await call({
        port: address.port,
        path: '/healthz',
        origin: 'http://example.com',
      })).status).toBe(403)
      const mutation = await call({ port: address.port, path: '/api/v1/service', method: 'POST' })
      expect(mutation.status).toBe(405)
      expect(mutation.headers.allow).toBe('GET')
    } finally {
      await server.close()
    }
  })

  it('serves versioned observation routes, bounded metrics, and validates filters', async () => {
    const source = {
      service: () => ({ ok: true, data: projection(true), meta: { generatedAt: 'now', revision: 'rev' } }),
      overview: () => ({ ok: true, data: { attention: [], fleets: [], activeRuns: [], recentRuns: [] }, meta: { generatedAt: 'now', revision: 'rev' } }),
      fleets: () => ({ ok: true, data: { items: [] }, meta: { generatedAt: 'now', revision: 'rev' } }),
      fleet: () => undefined,
      runs: () => ({ ok: true, data: { items: [] }, meta: { generatedAt: 'now', revision: 'rev' } }),
      run: () => undefined,
      adapters: () => ({ ok: true, data: { items: [] }, meta: { generatedAt: 'now', revision: 'rev' } }),
      revision: () => 'rev',
      metrics: () => 'pactline_fleet_ready 1\n',
    } as FleetObservationSource
    const server = new FleetHealthServer(() => projection(true), { observation: source })
    const address = await server.start('127.0.0.1', 0)
    try {
      expect(JSON.parse((await call({ port: address.port, path: '/api/v1/overview' })).body)).toMatchObject({ ok: true, meta: { revision: 'rev' } })
      expect((await call({ port: address.port, path: '/api/v1/runs?state=not-real' })).status).toBe(400)
      expect((await call({ port: address.port, path: '/api/v1/runs/%ZZ' })).status).toBe(404)
      const metrics = await call({ port: address.port, path: '/metrics' })
      expect(metrics.body).toBe('pactline_fleet_ready 1\n')
      expect(metrics.headers['content-type']).toContain('text/plain')
    } finally { await server.close() }
  })

  it('serves production UI assets with a restrictive UI CSP and SPA fallback', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'fleet-static-'))
    directories.push(directory)
    await mkdir(join(directory, 'assets'))
    await writeFile(join(directory, 'index.html'), '<!doctype html><title>Fleet UI</title>')
    await writeFile(join(directory, 'assets', 'app.js'), 'document.body.dataset.ready = "true"')
    const server = new FleetHealthServer(() => projection(true), { staticDirectory: directory })
    const address = await server.start('127.0.0.1', 0)
    try {
      const route = await call({ port: address.port, path: '/runs/example' })
      expect(route.status).toBe(200)
      expect(route.body).toContain('Fleet UI')
      expect(route.headers['content-security-policy']).toContain("script-src 'self'")
      const asset = await call({ port: address.port, path: '/assets/app.js' })
      expect(asset.headers['cache-control']).toContain('immutable')
      expect((await call({ port: address.port, path: '/../package.json' })).status).toBe(404)
    } finally { await server.close() }
  })

  it('streams bounded snapshot events and closes subscribers with the service', async () => {
    const source = {
      service: () => ({ ok: true, data: projection(true), meta: { generatedAt: 'now', revision: 'revision-1' } }),
      overview: () => ({ ok: true, data: { attention: [], fleets: [], activeRuns: [], recentRuns: [] }, meta: { generatedAt: 'now', revision: 'revision-1' } }),
      fleets: () => ({ ok: true, data: { items: [] }, meta: { generatedAt: 'now', revision: 'revision-1' } }), fleet: () => undefined,
      runs: () => ({ ok: true, data: { items: [] }, meta: { generatedAt: 'now', revision: 'revision-1' } }), run: () => undefined,
      adapters: () => ({ ok: true, data: { items: [] }, meta: { generatedAt: 'now', revision: 'revision-1' } }),
      revision: () => 'revision-1', metrics: () => '',
    } as FleetObservationSource
    const server = new FleetHealthServer(() => projection(true), { observation: source, eventPollIntervalMs: 10 })
    const address = await server.start('127.0.0.1', 0)
    const event = await firstEvent(address.port)
    expect(event.status).toBe(200)
    expect(event.contentType).toContain('text/event-stream')
    expect(event.body).toContain('event: snapshot')
    expect(event.body).toContain('revision-1')
    await server.close()
  })
})
