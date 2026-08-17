#!/usr/bin/env node

import { chmod, mkdir } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { preflightM54Usability } from './m5-4-usability.js'
import {
  runM54DeterministicCorrection,
  runM54DeterministicUsability,
  runM54LiveWorkflow,
  runM54RestartRecovery,
} from './m5-4-deterministic.js'

const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const repository = resolve(process.env.PACTLINE_FLEET_M54_REPOSITORY ?? resolve(fleetRoot, '..'))
const evidenceRoot = resolve(process.env.PACTLINE_FLEET_M54_EVIDENCE_ROOT ?? resolve(fleetRoot, '.fleet/m5-4-usability'))
const mode = process.argv[2] ?? 'preflight'
const runId = process.env.PACTLINE_FLEET_M54_RUN_ID ?? `m5-4-${new Date().toISOString().replace(/[:.]/g, '-')}`

await mkdir(evidenceRoot, { recursive: true, mode: 0o700 })
await chmod(evidenceRoot, 0o700)

const options = {
  server: process.env.PACTLINE_LOCAL_SERVER ?? 'http://localhost:5173',
  pactlineExecutable: resolve(process.env.PACTLINE_FLEET_PACTLINE_BIN ?? resolve(fleetRoot, '../bin/pactline')),
  fleetBinPath: resolve(fleetRoot, 'lib/bin.js'),
  repository,
  evidenceRoot,
  runId,
}

let result: unknown
if (mode === 'preflight') result = await preflightM54Usability(options)
else if (mode === 'deterministic') result = await runM54DeterministicUsability(options)
else if (mode === 'correction') result = await runM54DeterministicCorrection(options)
else if (mode === 'restart') result = await runM54RestartRecovery(options)
else if (mode === 'live') {
  result = [
    await runM54LiveWorkflow({ ...options, runId: `${runId}-deepseek-codex` }, 'deepseek-codex'),
    await runM54LiveWorkflow({ ...options, runId: `${runId}-codex-codex` }, 'codex-codex'),
  ]
} else if (mode === 'acceptance') {
  result = {
    deterministic: await runM54DeterministicUsability({ ...options, runId: `${runId}-u1` }),
    correction: await runM54DeterministicCorrection({ ...options, runId: `${runId}-u3` }),
    restart: await runM54RestartRecovery({ ...options, runId: `${runId}-u4` }),
    live: [
      await runM54LiveWorkflow({ ...options, runId: `${runId}-u2-deepseek-codex` }, 'deepseek-codex'),
      await runM54LiveWorkflow({ ...options, runId: `${runId}-u2-codex-codex` }, 'codex-codex'),
    ],
  }
} else throw new Error(`Unknown M5.4 mode: ${mode}`)

process.stdout.write(`${JSON.stringify({ ok: true, data: result })}\n`)
