import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadL2V2Spec } from './l2-v2-spec.js'
import { preflightL2V2Repository } from './l2-v2-preflight.js'

const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

try {
  const spec = await loadL2V2Spec(resolve(fleetRoot, 'evaluation/cases/l2-v2.json'))
  const data = await preflightL2V2Repository(spec)
  process.stdout.write(`${JSON.stringify({ ok: true, data })}\n`)
} catch (error: unknown) {
  process.stderr.write(`${JSON.stringify({ ok: false, error: { code: 'L2_V2_PREFLIGHT_FAILED', message: error instanceof Error ? error.message : String(error) } })}\n`)
  process.exitCode = 1
}
