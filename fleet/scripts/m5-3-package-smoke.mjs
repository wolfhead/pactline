import { execFileSync } from 'node:child_process'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { FleetHealthServer } from '../lib/http/health-server.js'
import { fleetWebAssetsPath } from '../lib/http/static-assets.js'

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

const health = {
  serviceId: 'package-smoke', version: 'test', mode: 'ready', live: true, ready: true,
  startedAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
  config: { revision: 'a'.repeat(64), loadedAt: new Date().toISOString() },
  registry: { status: 'ok', path: '/redacted', schemaVersion: 3, nonTerminalRuns: 0 },
  pactline: { status: 'ok', server: 'http://localhost:8080' }, adapters: [], fleets: [],
}

const server = new FleetHealthServer(() => health, { staticDirectory: fleetWebAssetsPath() })
const address = await server.start('127.0.0.1', 0)
try {
  const response = await fetch(address.url)
  const body = await response.text()
  if (!response.ok || !body.includes('<div id="root"></div>')) throw new Error('Built Fleet UI was not served')
  if (!response.headers.get('content-security-policy')?.includes("script-src 'self'")) throw new Error('Built Fleet UI CSP is missing')
} finally {
  await server.close()
}

const packed = JSON.parse(execFileSync('npm', ['pack', '--json', '--dry-run'], { cwd: packageRoot, encoding: 'utf8' }))
const files = packed[0]?.files?.map(file => file.path) ?? []
if (!files.includes('web/dist/index.html') || !files.some(path => path.startsWith('web/dist/assets/'))) {
  throw new Error('Packed Fleet artifact does not contain the built UI')
}
process.stdout.write(`${JSON.stringify({ ok: true, served: true, packedAssets: files.filter(path => path.startsWith('web/dist/')).length })}\n`)
