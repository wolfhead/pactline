import { runCodexL1 } from './codex-l1-live.js'

void runCodexL1().then(result => {
  process.stdout.write(`${JSON.stringify({ ok: true, data: result })}\n`)
}).catch((error: unknown) => {
  process.stderr.write(`${JSON.stringify({ ok: false, error: { code: 'CODEX_L1_FAILED', message: error instanceof Error ? error.message : String(error) } })}\n`)
  process.exitCode = 1
})
