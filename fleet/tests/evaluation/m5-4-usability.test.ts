import { createServer } from 'node:http'
import { execFile } from 'node:child_process'
import { chmod, mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import {
  runM54DeterministicCorrection,
  runM54DeterministicUsability,
  runM54LiveWorkflow,
  runM54RestartRecovery,
} from '../../src/evaluation/m5-4-deterministic.js'
import { preflightM54Usability } from '../../src/evaluation/m5-4-usability.js'

const exec = promisify(execFile)
const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

describe('M5.4 usability preflight', () => {
  it('records one exact committed source and verifies the public local boundaries', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'fleet-m5-4-preflight-'))
    directories.push(directory)
    const repository = join(directory, 'repository')
    const evidenceRoot = join(directory, 'evidence')
    const pactline = join(directory, 'pactline.mjs')
    await mkdir(repository)
    await mkdir(evidenceRoot)
    await exec('git', ['init', '--quiet', repository])
    await writeFile(join(repository, 'README.md'), 'committed\n')
    await exec('git', ['-C', repository, 'add', 'README.md'])
    await exec('git', ['-C', repository, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'base'])
    const baseRevision = (await exec('git', ['-C', repository, 'rev-parse', 'HEAD'])).stdout.trim()
    await writeFile(join(repository, 'untracked.txt'), 'shared checkout dirt is not part of the base commit\n')
    await writeFile(pactline, `#!/usr/bin/env node
const args = process.argv.slice(2)
if (args.includes('capabilities')) process.stdout.write(JSON.stringify({ ok: true, data: { cli_version: '0.1.0-test', protocol: 2, features: ['bounded_work_packets'] } }))
else process.exit(2)
`)
    await chmod(pactline, 0o700)
    const server = createServer((request, response) => {
      if (request.url === '/readyz') {
        response.writeHead(200, { 'Content-Type': 'application/json' })
        response.end('{"ok":true}')
      } else { response.writeHead(404); response.end() }
    })
    await new Promise<void>((resolvePromise, reject) => {
      server.once('error', reject)
      server.listen(0, '127.0.0.1', resolvePromise)
    })
    const address = server.address()
    if (address === null || typeof address === 'string') throw new Error('test server did not bind')
    try {
      await expect(preflightM54Usability({
        server: `http://127.0.0.1:${String(address.port)}`,
        pactlineExecutable: pactline,
        fleetBinPath: resolve(fleetRoot, 'lib/bin.js'),
        repository,
        evidenceRoot,
        runId: 'preflight-test',
      })).resolves.toEqual({
        ready: true,
        server: `http://127.0.0.1:${String(address.port)}`,
        pactline: { cliVersion: '0.1.0-test', protocol: 2, featureCount: 1 },
        fleet: { version: '0.0.0-development' },
        repository: { path: repository, baseRevision, workingTreeDirty: true },
        evidence: { path: join(evidenceRoot, 'preflight-test'), available: true },
      })
    } finally {
      await new Promise<void>((resolvePromise, reject) => server.close(error => error === undefined ? resolvePromise() : reject(error)))
    }
  })

  it.runIf(process.env.PACTLINE_FLEET_M54_LOCAL === '1')('completes one deterministic resident Fleet delivery through public boundaries', async () => {
    const result = await runM54DeterministicUsability({
      server: process.env.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173',
      pactlineExecutable: process.env.PACTLINE_FLEET_PACTLINE_BIN ?? resolve(fleetRoot, '../bin/pactline'),
      fleetBinPath: resolve(fleetRoot, 'lib/bin.js'),
      repository: resolve(fleetRoot, '..'),
      evidenceRoot: resolve(fleetRoot, '.fleet/m5-4-usability'),
      runId: process.env.PACTLINE_FLEET_M54_RUN_ID ?? 'u1-local',
    })
    expect(result).toMatchObject({
      status: 'passed',
      pactline: { phase: 'in_review', activity: 'available', claimCount: 1, claimStatus: 'completed' },
      fleet: { runState: 'completed', checkpoint: 'settlement_observed', nonTerminalRuns: 0 },
      repository: { changedPath: 'M5_4_USABILITY.md', content: 'M5.4 usability passed\n' },
    })
    expect(result.repository.deliveryRevision).toMatch(/^[a-f0-9]{40}$/)
  }, 120_000)

  it.runIf(process.env.PACTLINE_FLEET_M54_LOCAL === '1')('completes request changes, correction, and final review without manual repair', async () => {
    const result = await runM54DeterministicCorrection({
      server: process.env.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173',
      pactlineExecutable: process.env.PACTLINE_FLEET_PACTLINE_BIN ?? resolve(fleetRoot, '../bin/pactline'),
      fleetBinPath: resolve(fleetRoot, 'lib/bin.js'),
      repository: resolve(fleetRoot, '..'),
      evidenceRoot: resolve(fleetRoot, '.fleet/m5-4-usability'),
      runId: process.env.PACTLINE_FLEET_M54_CORRECTION_RUN_ID ?? 'u3-local',
    })
    expect(result).toMatchObject({
      status: 'passed',
      pactline: { phase: 'done', claimCount: 4, claimStatuses: ['completed', 'completed', 'completed', 'completed'] },
      fleet: { runCount: 4, nonTerminalRuns: 0 },
      repository: { changedPath: 'M5_4_USABILITY.md', content: 'M5.4 usability corrected\n' },
    })
    expect(result.repository.deliveryRevision).toMatch(/^[a-f0-9]{40}$/)
  }, 120_000)

  it.runIf(process.env.PACTLINE_FLEET_M54_LOCAL === '1')('restarts after Session persistence and safely completes on a new Run', async () => {
    const result = await runM54RestartRecovery({
      server: process.env.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173',
      pactlineExecutable: process.env.PACTLINE_FLEET_PACTLINE_BIN ?? resolve(fleetRoot, '../bin/pactline'),
      fleetBinPath: resolve(fleetRoot, 'lib/bin.js'),
      repository: resolve(fleetRoot, '..'),
      evidenceRoot: resolve(fleetRoot, '.fleet/m5-4-usability'),
      runId: process.env.PACTLINE_FLEET_M54_RESTART_RUN_ID ?? 'u4-local',
    })
    expect(result).toMatchObject({
      status: 'passed',
      pactline: { phase: 'in_review', claimCount: 2, claimStatuses: ['released', 'completed'] },
      fleet: { runCount: 2, runStates: ['released', 'completed'], nonTerminalRuns: 0 },
      effects: { preCrashAgentEffects: 0, deliveryCount: 1 },
    })
  }, 120_000)

  it.runIf(process.env.PACTLINE_FLEET_M54_LIVE === '1')('completes the representative live DeepSeek/Codex and Codex/Codex paths', async () => {
    for (const path of ['deepseek-codex', 'codex-codex'] as const) {
      const result = await runM54LiveWorkflow({
        server: process.env.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173',
        pactlineExecutable: process.env.PACTLINE_FLEET_PACTLINE_BIN ?? resolve(fleetRoot, '../bin/pactline'),
        fleetBinPath: resolve(fleetRoot, 'lib/bin.js'),
        repository: resolve(fleetRoot, '..'),
        evidenceRoot: resolve(fleetRoot, '.fleet/m5-4-usability'),
        runId: `${process.env.PACTLINE_FLEET_M54_LIVE_RUN_ID ?? 'u2-local'}-${path}`,
      }, path)
      expect(result).toMatchObject({
        status: 'passed', path,
        pactline: { phase: 'done', claimCount: 2 },
        fleet: {
          runCount: 2,
          adapters: path === 'deepseek-codex' ? ['deepseek', 'codex'] : ['codex', 'codex'],
          runtimeSessionCount: 2,
          nonTerminalRuns: 0,
        },
        repository: { changedPath: 'M5_4_USABILITY.md', content: 'M5.4 usability passed\n' },
      })
    }
  }, 1_200_000)
})
