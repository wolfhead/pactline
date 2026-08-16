import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { DeepSeekJsonRpcRuntime } from '../../src/adapters/deepseek/wire.js'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

describe('DeepSeek JSON-RPC process boundary', () => {
  it('performs finite request, notification, shutdown, and process reap', async () => {
    const runtime = new DeepSeekJsonRpcRuntime({
      command: process.execPath,
      args: [resolve(root, 'tests/fixtures/fake-deepseek-runtime.mjs')],
      cwd: root,
      env: { PATH: process.env.PATH },
      requestTimeoutMs: 1_000,
      shutdownTimeoutMs: 1_000,
      terminateTimeoutMs: 1_000,
      maxStderrBytes: 4_096,
    })
    await expect(runtime.request('initialize', { cwd: root, provider: 'deepseek-official', model: 'deepseek-v4-pro' }))
      .resolves.toMatchObject({ serverInfo: { name: 'deepseek-harness-sdk-runtime' } })
    await expect(runtime.request('session/prompt', { sessionId: 'fixture', contentBlocks: [] }))
      .resolves.toEqual({ messageId: 'fixture-message' })
    await expect(runtime.nextNotification()).resolves.toEqual({ method: 'fixture.notice', params: { value: 1 } })
    await expect(runtime.close()).resolves.toBeUndefined()
  })
})
