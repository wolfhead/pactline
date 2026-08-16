import { request } from 'node:http'
import { describe, expect, it } from 'vitest'
import { FleetHealthServer } from '../../src/http/health-server.js'
import type { FleetServiceHealth } from '../../src/health/model.js'

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
})
