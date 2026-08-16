import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { CodexExecRuntime } from '../../src/adapters/codex/wire.js'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

describe('Codex JSONL process boundary', () => {
  it('performs finite input, event parsing, exit, and process reap', async () => {
    const runtime = new CodexExecRuntime({
      command: process.execPath, args: ['-e', [
        "process.stdin.resume(); let body='';",
        "process.stdin.on('data', c => body += c);",
        "process.stdin.on('end', () => {",
        " process.stdout.write(JSON.stringify({type:'thread.started',thread_id:'fixture'})+'\\n');",
        " process.stdout.write(JSON.stringify({type:'turn.completed',usage:{input_tokens:1}})+'\\n');",
        "});",
      ].join('')],
      cwd: root, env: { PATH: process.env.PATH }, input: 'fixture prompt',
      maxLineBytes: 4_096, maxOutputBytes: 16_384, maxStderrBytes: 4_096,
      exitGraceMs: 1_000, terminateGraceMs: 1_000,
    })
    await expect(runtime.nextEvent()).resolves.toMatchObject({ type: 'thread.started', thread_id: 'fixture' })
    await expect(runtime.nextEvent()).resolves.toMatchObject({ type: 'turn.completed' })
    await expect(runtime.waitForSuccessfulExit()).resolves.toBeUndefined()
    await expect(runtime.close()).resolves.toBeUndefined()
  })

  it('rejects invalid JSONL and terminates the owned process', async () => {
    const runtime = new CodexExecRuntime({
      command: process.execPath, args: ['-e', "process.stdout.write('not-json\\n'); setInterval(()=>{},1000)"],
      cwd: root, env: { PATH: process.env.PATH }, input: '',
      maxLineBytes: 4_096, maxOutputBytes: 16_384, maxStderrBytes: 4_096,
      exitGraceMs: 100, terminateGraceMs: 1_000,
    })
    await expect(runtime.nextEvent()).rejects.toThrow('invalid JSONL')
    await expect(runtime.close()).resolves.toBeUndefined()
  })

  it('enforces the total JSONL output bound', async () => {
    const runtime = new CodexExecRuntime({
      command: process.execPath, args: ['-e', "process.stdout.write('x'.repeat(8192)); setInterval(()=>{},1000)"],
      cwd: root, env: { PATH: process.env.PATH }, input: '',
      maxLineBytes: 16_384, maxOutputBytes: 1_024, maxStderrBytes: 4_096,
      exitGraceMs: 100, terminateGraceMs: 1_000,
    })
    await expect(runtime.nextEvent()).rejects.toThrow('output exceeded 1024 bytes')
    await expect(runtime.close()).resolves.toBeUndefined()
  })
})
