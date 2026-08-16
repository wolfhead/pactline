#!/usr/bin/env node

import { runDeepSeekL1 } from './deepseek-l1-live.js'

void runDeepSeekL1().then((result) => {
  process.stdout.write(`${JSON.stringify({ ok: true, data: result })}\n`)
}).catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error)
  process.stderr.write(`${JSON.stringify({ ok: false, error: { code: 'DEEPSEEK_L1_FAILED', message } })}\n`)
  process.exitCode = 1
})
