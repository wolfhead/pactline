import { execFile } from 'node:child_process'
import { chmod, mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createServer } from 'node:net'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import { ReplayHarnessAdapter } from '../../src/adapters/replay/replay-adapter.js'
import { DeepSeekHarnessAdapter } from '../../src/adapters/deepseek/deepseek-adapter.js'
import type { ExecutionProposal, HarnessRunResult } from '../../src/core/harness-result.js'
import { REQUIRED_PACTLINE_FEATURES } from '../../src/pactline/client.js'
import { FleetService } from '../../src/service/fleet-service.js'
import { NullFleetLogger } from '../../src/service/logger.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'
import type { FleetCrashCheckpoint } from '../../src/scheduler/run-coordinator.js'

const exec = promisify(execFile)
const directories: string[] = []
const liveDeepSeek = process.env.PACTLINE_FLEET_LIVE_DEEPSEEK === '1'

async function availablePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolvePromise, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolvePromise)
  })
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('Test port was not allocated')
  await new Promise<void>((resolvePromise, reject) => server.close(error => error === undefined ? resolvePromise() : reject(error)))
  return address.port
}

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

const FAKE_PACTLINE = `#!/usr/bin/env node
import { readFileSync, writeFileSync } from 'node:fs'
const statePath = process.env.FAKE_PACTLINE_STATE
const state = JSON.parse(readFileSync(statePath, 'utf8'))
const args = process.argv.slice(2)
const commandIndex = args.findIndex(value => ['capabilities', 'doctor', 'task', 'claim'].includes(value))
const command = args.slice(commandIndex)
const ok = data => process.stdout.write(JSON.stringify({ ok: true, data }))
const task = () => ({
  id: 'task-21', number: 21, title: 'M5.2 plugin service', version: state.version,
  phase: state.phase, activity: state.activity, project: { number: 5 },
  context: 'Change README.md from baseline to implemented.', expected_result: 'README.md contains exactly implemented followed by a newline.',
  description: 'A bounded M5.2 scheduler demonstration.'
})
const workflow = () => ({ task_number: 21, version: state.version, phase: state.phase, activity: state.activity })
const claim = () => ({ id: '11111111-1111-4111-8111-111111111111', task_number: 21, stage: 'execution', status: state.claimStatus, version: 1 })
const packet = () => ({ task: task(), claim: claim(), criteria: [{ id: 'criterion-1', revision: 1 }], delivery: {}, main_thread: { items: [] } })
if (command[0] === 'capabilities') ok({ cli_version: 'test', protocol: 2, features: ${JSON.stringify(REQUIRED_PACTLINE_FEATURES)} })
else if (command[0] === 'doctor') ok({ server: process.env.PACTLINE_SERVER, client_kind: process.env.PACTLINE_CLIENT_KIND, session_id: process.env.PACTLINE_SESSION_ID, token: 'present', principal: { id: 'test' } })
else if (command[0] === 'task' && command[1] === 'list') {
  const stage = command[command.indexOf('--stage') + 1]
  ok({ items: stage === 'execution' && state.phase === 'ready' ? [task()] : [] })
} else if (command[0] === 'task' && command[1] === 'show') ok({ ...packet(), claim: undefined })
else if (command[0] === 'task' && command[1] === 'claim') {
  state.phase = 'in_progress'; state.activity = 'working'; state.claimStatus = 'active'; state.version += 1
  writeFileSync(statePath, JSON.stringify(state)); ok({ task: workflow(), claim: claim() })
} else if (command[0] === 'claim' && command[1] === 'list') ok({ items: state.claimStatus === 'active' ? [claim()] : [] })
else if (command[0] === 'claim' && command[1] === 'show') ok(packet())
else if (command[0] === 'claim' && command[1] === 'verify') ok({ recorded: true })
else if (command[0] === 'claim' && command[1] === 'change') {
  state.version += 1; writeFileSync(statePath, JSON.stringify(state)); ok({ task: workflow(), code_change: { id: 'change-1' } })
} else if (command[0] === 'claim' && command[1] === 'submit') ok({ task: workflow(), claim: claim() })
else if (command[0] === 'claim' && command[1] === 'complete') {
  state.phase = 'in_review'; state.activity = 'available'; state.claimStatus = 'completed'
  writeFileSync(statePath, JSON.stringify(state)); ok({ task: workflow(), claim: claim() })
} else if (command[0] === 'claim' && command[1] === 'release') {
  state.activity = 'available'; state.claimStatus = 'released'
  writeFileSync(statePath, JSON.stringify(state)); ok({ task: workflow(), claim: claim() })
} else { process.stderr.write('unsupported ' + command.join(' ')); process.exit(2) }
`

describe('M5.2 plugin-backed resident scheduling', () => {
  it(`runs one complete service cycle through discovery, Claim, ${liveDeepSeek ? 'DeepSeek' : 'Replay'}, delivery, and settlement`, async () => {
    const directory = await mkdtemp(join(tmpdir(), 'fleet-m5-2-service-'))
    directories.push(directory)
    const pactline = join(directory, 'pactline.mjs')
    const plugin = join(directory, 'work-plugin.mjs')
    const statePath = join(directory, 'pactline-state.json')
    const origin = join(directory, 'origin.git')
    const seed = join(directory, 'seed')
    await writeFile(pactline, FAKE_PACTLINE)
    await chmod(pactline, 0o700)
    await writeFile(statePath, JSON.stringify({ phase: 'ready', activity: 'available', version: 1, claimStatus: 'none' }))
    await mkdir(seed)
    await exec('git', ['init', '--quiet', '--bare', origin])
    await exec('git', ['init', '--quiet', seed])
    await writeFile(join(seed, 'README.md'), 'baseline\n')
    await exec('git', ['-C', seed, 'add', 'README.md'])
    await exec('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'baseline'])
    const revision = (await exec('git', ['-C', seed, 'rev-parse', 'HEAD'])).stdout.trim()
    await exec('git', ['-C', seed, 'branch', '-M', 'main'])
    await exec('git', ['-C', seed, 'remote', 'add', 'origin', origin])
    await exec('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])
    await writeFile(plugin, `#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
let input = ''; for await (const chunk of process.stdin) input += chunk
const request = JSON.parse(input)
const operation = process.argv.at(-1)
if (operation === 'resolve') {
  process.stdout.write(JSON.stringify({ ok: true, data: {
    caseId: 'm5-2-service', taskNumber: 21, taskVersion: 1,
    base: { source: '${origin}', ref: 'refs/heads/main', revision: '${revision}' },
    repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
    allowedPaths: ['README.md'], verificationCommands: ['true'], criteria: [{ id: 'criterion-1', revision: 1 }]
  } }))
} else if (operation === 'commit') {
  execFileSync('git', ['add', 'README.md'], { cwd: request.workspace })
  execFileSync('git', ['-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '-m', 'delivery'], { cwd: request.workspace })
  const revision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: request.workspace, encoding: 'utf8' }).trim()
  const branch = execFileSync('git', ['branch', '--show-current'], { cwd: request.workspace, encoding: 'utf8' }).trim()
  process.stdout.write(JSON.stringify({ ok: true, data: { revision, branch } }))
} else if (operation === 'push') {
  process.stdout.write(JSON.stringify({ ok: true, data: request.commit }))
} else {
  process.stdout.write(JSON.stringify({ ok: true, data: {
    repository: request.definition.repository, codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/52',
    revision: request.push.revision, branch: request.push.branch
  } }))
}
`)
    await chmod(plugin, 0o700)
    const route = liveDeepSeek
      ? 'adapter: deepseek, model: deepseek-v4-pro, reasoning: max'
      : 'adapter: replay, model: replay-quality, reasoning: max'
    const config = serviceConfigYAML({
      stateDirectory: join(directory, 'state'), firstWorkspace: join(directory, 'work'),
      server: 'http://127.0.0.1:8080', httpPort: await availablePort(),
    })
      .replace('/test/bin/pactline', pactline)
      .replaceAll('adapter: codex, model: gpt-5.6-sol', route)
      .replace('    credentials:\n', `    workPlugin:\n      executable: ${plugin}\n      timeout: 30s\n    credentials:\n`)
    const configPath = join(directory, 'fleet.yml')
    await writeFile(configPath, config)
    const replay = new ReplayHarnessAdapter([{
      sessionId: 'm5-2-replay-session',
      effect: async request => { await writeFile(join(request.workspace, 'README.md'), 'implemented\n') },
      result: request => {
        const proposal: ExecutionProposal = {
          schemaVersion: 1, kind: 'execution', runId: request.runId, claimId: request.claimId, taskNumber: 21,
          recommendation: 'complete', summary: 'Implemented.', changedPaths: ['README.md'],
          verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }],
          criteria: [{ criterionId: 'criterion-1', criterionRevision: 1, outcome: 'passed', evidence: 'verified' }], limitations: [],
        }
        const result: HarnessRunResult = {
          adapterId: 'replay', adapterVersion: '1.0.0', runtimeSessionId: 'm5-2-replay-session',
          model: { provider: 'replay', model: 'replay-quality', reasoning: 'max' }, terminalState: 'completed', proposal,
          usage: {}, eventSummary: { total: 0, byType: {}, toolCalls: {}, toolErrors: {} },
        }
        return result
      },
    }])
    const adapter = liveDeepSeek ? new DeepSeekHarnessAdapter({ maxTokens: 16_384 }) : replay
    const checkpoints: FleetCrashCheckpoint[] = []
    const service = new FleetService(configPath, {
      adapters: [adapter], logger: new NullFleetLogger(),
      environment: { ...process.env, TEST_PACTLINE_TOKEN: 'not-real', FAKE_PACTLINE_STATE: statePath },
      faultInjector: checkpoint => { checkpoints.push(checkpoint) },
    })
    await service.start()
    try {
      await expect(service.runOnce()).resolves.toMatchObject({ discovered: 1, admitted: 1, contentions: 0 })
      const finalState = JSON.parse(await readFile(statePath, 'utf8')) as Record<string, unknown>
      expect(finalState).toMatchObject({
        phase: 'in_review', activity: 'available', claimStatus: 'completed', version: 3,
      })
      expect(service.health.registry.nonTerminalRuns).toBe(0)
      expect(checkpoints).toEqual([
        'before_claim_creation',
        'after_claim_creation_before_persistence',
        'after_claim_persistence_before_session',
        'after_workspace_effect_before_persistence',
        'after_session_persistence_before_agent',
        'after_harness_result_before_persistence',
        'after_harness_result_persistence_before_delivery',
        'after_commit_before_persistence',
        'after_commit_persistence_before_push',
        'after_push_before_persistence',
        'after_push_persistence_before_code_change',
        'after_code_change_before_persistence',
        'after_code_change_persistence_before_link',
        'after_settlement_before_terminal_persistence',
      ])
      if (liveDeepSeek) {
        const evidenceDirectory = join(process.cwd(), '.fleet', 'm5-2-deepseek-demo')
        await mkdir(evidenceDirectory, { recursive: true, mode: 0o700 })
        await chmod(evidenceDirectory, 0o700)
        const evidencePath = join(evidenceDirectory, 'latest.json')
        await writeFile(evidencePath, `${JSON.stringify({
          schemaVersion: 1,
          status: 'passed',
          completedAt: new Date().toISOString(),
          route: { adapter: 'deepseek', model: 'deepseek-v4-pro', reasoning: 'max' },
          workflow: finalState,
          scheduler: { nonTerminalRuns: service.health.registry.nonTerminalRuns },
          isolation: { pactline: 'fake CLI', repository: 'temporary local Git origin', remoteEffects: false },
        }, null, 2)}\n`, { mode: 0o600 })
        await chmod(evidencePath, 0o600)
      }
    } finally {
      await service.stop('test complete')
    }
  }, liveDeepSeek ? 600_000 : 15_000)
})
