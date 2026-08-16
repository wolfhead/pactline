import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { loadL2V2Spec } from '../../src/evaluation/l2-v2-spec.js'
import { provisionL2V2 } from '../../src/evaluation/l2-v2-provision.js'
import type { L2V2CommandRunner } from '../../src/evaluation/l2-v2-preflight.js'

const roots: string[] = []
afterEach(async () => { await Promise.all(roots.splice(0).map(path => rm(path, { recursive: true, force: true }))) })

function json(response: ServerResponse, status: number, value: unknown, headers: Record<string, string | string[]> = {}): void {
  response.writeHead(status, { 'Content-Type': 'application/json', ...headers })
  response.end(JSON.stringify(value))
}

function uuid(index: number): string { return `00000000-0000-4000-8000-${String(index).padStart(12, '0')}` }

describe('M4 L2 v2 provisioner', () => {
  it('creates only the frozen isolated corpus and leaves all six tasks in backlog', async () => {
    const spec = await loadL2V2Spec(resolve('evaluation/cases/l2-v2.json'))
    const taskVersions = new Map<number, number>()
    const requests: { method: string; url: string; ifMatch?: string }[] = []
    let nextTask = 101; let nextCriterion = 1
    const server = createServer((request: IncomingMessage, response: ServerResponse) => {
      const method = request.method ?? 'GET'; const url = request.url ?? '/'
      requests.push({ method, url, ...(request.headers['if-match'] === undefined ? {} : { ifMatch: request.headers['if-match'] }) })
      if (method === 'POST' && url === '/api/auth/dev/session') {
        json(response, 200, {}, { 'Set-Cookie': ['bb_session=session; Path=/', 'bb_csrf=csrf; Path=/'] }); return
      }
      if (method === 'POST' && url === '/api/auth/logout') { response.writeHead(204); response.end(); return }
      if (method === 'GET' && url.startsWith('/api/v1/projects?')) { json(response, 200, { items: [] }); return }
      if (method === 'POST' && url === '/api/v1/projects') {
        json(response, 201, { id: uuid(1), number: 9, version: 1 }, { ETag: '"1"' }); return
      }
      if (method === 'POST' && url === '/api/v1/projects/9/repositories') {
        json(response, 201, { project_version: 2, repository: { id: uuid(2) } }, { ETag: '"2"' }); return
      }
      if (method === 'GET' && url.startsWith('/api/v1/tasks?')) { json(response, 200, { items: [] }); return }
      if (method === 'POST' && url === '/api/v1/tasks') {
        const number = nextTask++
        taskVersions.set(number, 1)
        json(response, 201, { id: uuid(number), number, version: 1 }, { ETag: '"1-gzip"' }); return
      }
      const criterion = /^\/api\/v1\/tasks\/(\d+)\/criteria$/.exec(url)
      if (method === 'POST' && criterion !== null) {
        const taskNumber = Number(criterion[1]); const version = taskVersions.get(taskNumber)!
        expect(request.headers['if-match']).toBe(`"${String(version)}"`)
        taskVersions.set(taskNumber, version + 1)
        const position = (version - 1) % 2
        json(response, 201, {
          id: uuid(1_000 + nextCriterion++), version: 1, revision: 1, position,
        }, { ETag: '"1"' }); return
      }
      if (method === 'GET' && /^\/api\/v1\/tasks\/\d+\/criteria\?limit=200$/.test(url)) { json(response, 200, { items: [] }); return }
      json(response, 404, { code: 'NOT_FOUND' })
    })
    await new Promise<void>(resolvePromise => server.listen(0, '127.0.0.1', resolvePromise))
    const address = server.address()
    if (address === null || typeof address === 'string') throw new Error('test server did not bind')

    const commandCalls: { executable: string; args: readonly string[] }[] = []
    const run: L2V2CommandRunner = async (executable, args) => {
      commandCalls.push({ executable, args })
      if (args[0] === 'repo') return { stdout: JSON.stringify({ nameWithOwner: 'wolfhead/pactline', viewerPermission: 'ADMIN' }), stderr: '' }
      if (args[0] === 'api' && args[1]?.includes('matching-refs')) return { stdout: '[]', stderr: '' }
      if (args[0] === 'api' && args[1]?.includes('/git/ref/')) {
        const refs = new Map<string, string>([[spec.repository.baseRef, spec.repository.baseRevision]])
        for (const item of spec.cases) {
          refs.set(item.seedRef, item.baseRevision)
          if (item.candidate !== undefined) refs.set(item.candidate.seedRef, item.candidate.revision)
        }
        const requested = `refs/${args[1].split('/git/ref/')[1] ?? ''}`
        return { stdout: JSON.stringify({ ref: requested, object: { sha: refs.get(requested) } }), stderr: '' }
      }
      if (args[0] === 'pr') {
        const head = args[args.indexOf('--head') + 1]
        if (head === undefined) throw new Error('missing test head')
        return { stdout: `https://github.com/wolfhead/pactline/pull/${head.endsWith('04') ? '401' : '501'}\n`, stderr: '' }
      }
      return { stdout: '{}', stderr: '' }
    }

    const root = await mkdtemp(join(tmpdir(), 'fleet-l2-v2-provision-test-')); roots.push(root)
    const manifestPath = join(root, 'corpus-manifest.json')
    try {
      const manifest = await provisionL2V2(spec, {
        server: `http://127.0.0.1:${String(address.port)}`, manifestPath, run, log: () => undefined,
        now: () => new Date('2026-08-15T00:00:00.000Z'),
      })
      expect(manifest.status).toBe('provisioned')
      expect(manifest.pactline).toMatchObject({ projectNumber: 9, projectVersion: 2 })
      expect(manifest.cases).toHaveLength(6)
      expect(manifest.cases.every(item => item.phase === 'backlog' && item.criteria.length === 2 && item.taskVersion === 3)).toBe(true)
      expect(commandCalls.filter(call => call.executable === 'gh' && call.args[0] === 'api' && call.args.includes('POST'))).toHaveLength(9)
      expect(commandCalls.filter(call => call.executable === 'gh' && call.args[0] === 'pr')).toHaveLength(2)
      expect(requests.some(item => item.url.includes('mark-ready'))).toBe(false)
      expect((await stat(manifestPath)).mode & 0o077).toBe(0)
      expect(JSON.parse(await readFile(manifestPath, 'utf8'))).toMatchObject({ status: 'provisioned' })
    } finally {
      await new Promise<void>(resolvePromise => server.close(() => resolvePromise()))
    }
  })
})
