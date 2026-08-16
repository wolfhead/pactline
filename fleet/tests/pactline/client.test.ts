import { chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { PactlineCLI, PactlineClientError, REQUIRED_PACTLINE_FEATURES } from '../../src/pactline/client.js'

const FAKE_CLI = `#!/usr/bin/env node
import { appendFileSync } from 'node:fs'
let input = ''; for await (const chunk of process.stdin) input += chunk
const args = process.argv.slice(2)
if (process.env.FAKE_CAPTURE) appendFileSync(process.env.FAKE_CAPTURE, JSON.stringify({
  args, input, clientKind: process.env.PACTLINE_CLIENT_KIND, server: process.env.PACTLINE_SERVER,
  sessionId: process.env.PACTLINE_SESSION_ID, token: process.env.PACTLINE_TOKEN,
}) + '\\n')
const command = args.at(-1)
const output = command === 'capabilities' ? process.env.FAKE_CAPABILITIES : process.env.FAKE_OUTPUT
const finish = () => {
  process.stdout.write(output ?? '')
  process.stderr.write(process.env.FAKE_STDERR ?? '')
  process.exit(Number(process.env.FAKE_EXIT ?? 0))
}
const delay = Number(process.env.FAKE_DELAY_MS ?? 0)
if (delay > 0) setTimeout(finish, delay); else finish()
`

function success(data: unknown, meta?: unknown): string {
  return JSON.stringify({ ok: true, data, ...(meta === undefined ? {} : { meta }) })
}

function capabilities(protocol = 2, features: readonly string[] = REQUIRED_PACTLINE_FEATURES): string {
  return success({ cli_version: '0.1.0-test', protocol, features })
}

describe('PactlineCLI', () => {
  let directory: string
  let executable: string
  let capture: string

  beforeEach(async () => {
    directory = await mkdtemp(join(tmpdir(), 'pactline-fleet-client-'))
    executable = join(directory, 'fake-pactline.mjs')
    capture = join(directory, 'capture.jsonl')
    await writeFile(executable, FAKE_CLI)
    await chmod(executable, 0o700)
  })

  afterEach(async () => { await rm(directory, { recursive: true, force: true }) })

  it('uses the JSON machine contract without placing credentials in argv', async () => {
    const client = new PactlineCLI(
      { executable, server: 'https://pactline.example' },
      { environment: { ...process.env, FAKE_CAPABILITIES: capabilities(), FAKE_CAPTURE: capture, PACTLINE_TOKEN: 'secret' } },
    )
    await expect(client.capabilities()).resolves.toMatchObject({ protocol: 2 })
    const invocation = JSON.parse((await readFile(capture, 'utf8')).trim()) as Record<string, unknown>
    expect(invocation).toMatchObject({ args: ['--json', 'capabilities'], clientKind: 'pactline-fleet', token: 'secret' })
    expect(invocation.args).not.toContain('secret')
  })

  it('enforces protocol and every required feature before authentication', async () => {
    const protocol = new PactlineCLI({ executable }, { environment: { ...process.env, FAKE_CAPABILITIES: capabilities(3) } })
    await expect(protocol.preflight()).rejects.toMatchObject({ code: 'PROTOCOL_MISMATCH' })
    const feature = new PactlineCLI({ executable }, { environment: { ...process.env, FAKE_CAPABILITIES: capabilities(2, ['execution_claims']) } })
    await expect(feature.preflight({ verifyAuthentication: false })).rejects.toMatchObject({ code: 'MISSING_FEATURE' })
  })

  it('returns typed list data and success correlation metadata', async () => {
    const task = { id: 'task-id', number: 7, title: 'Task', version: 3, phase: 'ready', activity: '' }
    const client = new PactlineCLI({ executable }, { environment: {
      ...process.env, FAKE_OUTPUT: success({ items: [task] }, { request_id: 'request-1', etag: '"3"' }), FAKE_CAPTURE: capture,
    } })
    await expect(client.listTasks('execution', 4, 10, { sessionId: 'run-1' })).resolves.toEqual({
      data: [task], meta: { request_id: 'request-1', etag: '"3"' },
    })

    const claim = { id: 'claim-7', task_number: 7, stage: 'execution', status: 'active', version: 1 }
    const claims = new PactlineCLI({ executable }, { environment: {
      ...process.env, FAKE_OUTPUT: success({ items: [claim] }), FAKE_CAPTURE: capture,
    } })
    await expect(claims.listActiveClaims({ sessionId: 'run-1' })).resolves.toEqual({ data: [claim] })
  })

  it('sends mutation bodies on stdin and preserves idempotency', async () => {
    const response = {
      task: { task_number: 7, version: 4, phase: 'in_review', activity: 'available' },
      claim: { id: 'claim-1', task_number: 7, stage: 'execution', status: 'completed', version: 2 },
    }
    const client = new PactlineCLI({ executable }, { environment: { ...process.env, FAKE_OUTPUT: success(response), FAKE_CAPTURE: capture } })
    await client.completeClaim('claim-1', 3, 'Ready.', { sessionId: 'run-1', idempotencyKey: 'key-1' })
    const invocation = JSON.parse((await readFile(capture, 'utf8')).trim()) as Record<string, unknown>
    expect(invocation).toMatchObject({
      args: ['--json', '--idempotency-key', 'key-1', 'claim', 'complete', 'claim-1', '--task-version', '3', '--file', '-'],
      input: 'Ready.', sessionId: 'run-1',
    })
  })

  it('bounds output, duration, and caller cancellation', async () => {
    const oversized = new PactlineCLI({ executable, maxOutputBytes: 256 }, { environment: { ...process.env, FAKE_CAPABILITIES: 'x'.repeat(512) } })
    await expect(oversized.capabilities()).rejects.toMatchObject({ code: 'OUTPUT_LIMIT' })
    const slow = new PactlineCLI({ executable, timeoutMs: 10 }, { environment: { ...process.env, FAKE_DELAY_MS: '1000', FAKE_CAPABILITIES: capabilities() } })
    await expect(slow.capabilities()).rejects.toMatchObject({ code: 'TIMEOUT' })
    const controller = new AbortController()
    const cancelled = new PactlineCLI({ executable }, { environment: { ...process.env, FAKE_DELAY_MS: '1000', FAKE_CAPABILITIES: capabilities() } })
    const pending = cancelled.capabilities(controller.signal)
    controller.abort()
    await expect(pending).rejects.toMatchObject({ code: 'ABORTED' })
  })

  it('redacts environment and Bearer credentials from diagnostics', async () => {
    const client = new PactlineCLI({ executable }, { environment: {
      ...process.env,
      PACTLINE_TOKEN: 'secret-token',
      FAKE_CAPABILITIES: success({}),
      FAKE_STDERR: 'PACTLINE_TOKEN=secret-token Bearer other-secret',
      FAKE_EXIT: '2',
    } })
    let error: unknown
    try { await client.capabilities() } catch (caught: unknown) { error = caught }
    expect(error).toBeInstanceOf(PactlineClientError)
    expect(String(error)).toContain('[REDACTED]')
    expect(String(error)).not.toContain('secret-token')
    expect(String(error)).not.toContain('other-secret')
  })
})
