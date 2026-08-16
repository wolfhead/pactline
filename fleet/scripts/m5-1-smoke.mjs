import { chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { fileURLToPath } from 'node:url'

const requiredFeatures = [
  'bounded_work_packets',
  'claim_progress',
  'claim_release',
  'execution_claims',
  'execution_completion',
  'execution_verification',
  'issue_resolution',
  'repository_code_change_links',
  'repeatable_submission',
  'resolution_request',
  'review_acceptance',
  'review_claims',
  'review_request_changes',
  'success_metadata',
  'task_acceptance',
  'thread_collaboration',
]

function timeout(milliseconds, message) {
  return new Promise((_, reject) => {
    const timer = setTimeout(() => reject(new Error(message)), milliseconds)
    timer.unref()
  })
}

async function waitForLine(stream, child, milliseconds) {
  return await Promise.race([
    new Promise((resolvePromise, reject) => {
      let buffered = ''
      stream.setEncoding('utf8')
      stream.on('data', chunk => {
        buffered += chunk
        const index = buffered.indexOf('\n')
        if (index < 0) return
        try {
          resolvePromise(JSON.parse(buffered.slice(0, index)))
        } catch (error) {
          reject(error)
        }
      })
      stream.on('error', reject)
      child.once('exit', code => reject(new Error(`Fleet Service exited before startup with code ${String(code)}`)))
    }),
    timeout(milliseconds, 'Fleet Service did not report startup'),
  ])
}

async function availablePort() {
  const server = createServer()
  await new Promise((resolvePromise, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolvePromise)
  })
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('Smoke port was not allocated')
  await new Promise((resolvePromise, reject) => {
    server.close(error => error === undefined ? resolvePromise() : reject(error))
  })
  return address.port
}

async function waitForExit(child, milliseconds) {
  if (child.exitCode !== null) return child.exitCode
  return await Promise.race([
    new Promise((resolvePromise, reject) => {
      child.once('error', reject)
      child.once('exit', code => resolvePromise(code))
    }),
    timeout(milliseconds, 'Fleet Service process did not exit'),
  ])
}

const fleetRoot = dirname(dirname(fileURLToPath(import.meta.url)))
const root = await mkdtemp(join(tmpdir(), 'pactline-fleet-m5-1-smoke-'))
const fakeCLI = join(root, 'pactline')
const configPath = join(root, 'fleet.yml')
const stateDirectory = join(root, 'state')
const port = await availablePort()
const featureJSON = JSON.stringify(requiredFeatures)
const fakeSource = `#!/usr/bin/env node
const args = process.argv.slice(2)
if (args.includes('capabilities')) {
  process.stdout.write(JSON.stringify({ok:true,data:{cli_version:'0.1.0-smoke',protocol:2,features:${featureJSON}}}) + '\\n')
  process.exit(0)
}
if (args.includes('doctor')) {
  process.stdout.write(JSON.stringify({ok:true,data:{server:'http://127.0.0.1:8080',client_kind:'pactline-fleet-service',session_id:'smoke',token:'configured',principal:{type:'agent'}}}) + '\\n')
  process.exit(0)
}
process.stderr.write('unsupported fake Pactline command\\n')
process.exit(2)
`
const config = `version: 1
service:
  pactline:
    server: http://127.0.0.1:8080
    tokenEnv: PACTLINE_TOKEN
    executable: ${JSON.stringify(fakeCLI)}
  stateDirectory: ${JSON.stringify(stateDirectory)}
  maxConcurrentRuns: 1
  http:
    address: 127.0.0.1
    port: ${String(port)}
fleets: {}
`

let child
try {
  await writeFile(fakeCLI, fakeSource, { mode: 0o700 })
  await chmod(fakeCLI, 0o700)
  await writeFile(configPath, config, { mode: 0o600 })
  child = spawn(process.execPath, [join(fleetRoot, 'lib/bin.js'), 'serve', '--config', configPath, '--json'], {
    cwd: fleetRoot,
    env: { ...process.env, PACTLINE_TOKEN: 'not-a-real-smoke-token' },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const stderr = []
  child.stderr.on('data', chunk => stderr.push(Buffer.from(chunk)))
  const started = await waitForLine(child.stdout, child, 10_000)
  if (started?.ok !== true || typeof started.data?.url !== 'string') {
    throw new Error('Fleet Service startup response is invalid')
  }
  const live = await fetch(`${started.data.url}/livez`)
  const ready = await fetch(`${started.data.url}/readyz`)
  const health = await fetch(`${started.data.url}/healthz`)
  const healthBody = await health.json()
  if (live.status !== 200 || ready.status !== 503 || health.status !== 200) {
    throw new Error('Fleet Service health status codes are invalid')
  }
  if (healthBody?.data?.pactline?.status !== 'ok' || healthBody?.data?.registry?.status !== 'ok') {
    throw new Error('Fleet Service smoke dependencies are not healthy')
  }

  const contender = spawn(process.execPath, [join(fleetRoot, 'lib/bin.js'), 'serve', '--config', configPath, '--json'], {
    cwd: fleetRoot,
    env: { ...process.env, PACTLINE_TOKEN: 'not-a-real-smoke-token' },
    stdio: ['ignore', 'ignore', 'pipe'],
  })
  const contenderStderr = []
  contender.stderr.on('data', chunk => contenderStderr.push(Buffer.from(chunk)))
  const contenderExit = await waitForExit(contender, 10_000)
  if (contenderExit !== 1 || !Buffer.concat(contenderStderr).toString('utf8').includes('already locked')) {
    throw new Error('A second Fleet Service did not fail on the local state lock')
  }
  if ((await fetch(`${started.data.url}/livez`)).status !== 200) {
    throw new Error('The owning Fleet Service stopped after a lock contender')
  }

  child.kill('SIGTERM')
  const exitCode = await waitForExit(child, 10_000)
  if (exitCode !== 0) {
    throw new Error(`Fleet Service exited with ${String(exitCode)}: ${Buffer.concat(stderr).toString('utf8').slice(-2_048)}`)
  }
  const database = await readFile(join(stateDirectory, 'fleet.sqlite3'))
  if (database.includes(Buffer.from('not-a-real-smoke-token'))) {
    throw new Error('Fleet registry contains the Pactline smoke credential')
  }
  process.stdout.write(`${JSON.stringify({
    ok: true,
    data: {
      startup: started.data,
      live: live.status,
      ready: ready.status,
      health: healthBody.data.mode,
      secondProcessRejected: true,
      cleanExit: true,
      credentialAbsentFromRegistry: true,
    },
  })}\n`)
} finally {
  if (child !== undefined && child.exitCode === null) child.kill('SIGKILL')
  await rm(root, { recursive: true, force: true })
}
