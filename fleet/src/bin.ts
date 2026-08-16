#!/usr/bin/env node

import { runFleetCLI } from './cli.js'

void runFleetCLI(process.argv.slice(2)).then(code => {
  process.exitCode = code
}).catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error)
  process.stderr.write(`${JSON.stringify({ ok: false, error: { code: 'FLEET_COMMAND_FAILED', message } })}\n`)
  process.exitCode = 1
})
