import { parseArgs } from 'node:util'
import { runFleetDoctor } from './commands/doctor.js'
import { runDeepSeekDoctor } from './commands/deepseek-doctor.js'
import { runCodexDoctor } from './commands/codex-doctor.js'
import { fleetVersion } from './commands/version.js'
import { runFleetServe } from './commands/serve.js'

export interface FleetCLIIO {
  readonly stdout: { write(value: string): unknown }
  readonly stderr: { write(value: string): unknown }
}

function usage(): string {
  return [
    'Usage:',
    '  pactline-fleet version [--json]',
    '  pactline-fleet doctor [--json] [--pactline <executable>]',
    '  pactline-fleet deepseek-doctor [--json] [--runtime-command <path>] [--runtime-config <path>]',
    '  pactline-fleet codex-doctor [--json] [--runtime-command <path>]',
    '  pactline-fleet serve --config <path> [--once] [--json]',
  ].join('\n')
}

function writeJSON(target: FleetCLIIO['stdout'], value: unknown): void {
  target.write(`${JSON.stringify(value)}\n`)
}

export async function runFleetCLI(args: readonly string[], io: FleetCLIIO = process): Promise<number> {
  const command = args[0]
  if (command === undefined || command === 'help' || command === '--help' || command === '-h') {
    io.stdout.write(`${usage()}\n`); return 0
  }
  if (command === 'version') {
    const parsed = parseArgs({ args: args.slice(1), options: { json: { type: 'boolean', default: false } }, strict: true })
    const result = fleetVersion()
    if (parsed.values.json) writeJSON(io.stdout, { ok: true, data: result })
    else io.stdout.write(`${result.executable} ${result.version}\n`)
    return 0
  }
  if (command === 'doctor') {
    const parsed = parseArgs({
      args: args.slice(1),
      options: {
        json: { type: 'boolean', default: false },
        pactline: { type: 'string', default: process.env.PACTLINE_CLI ?? 'pactline' },
      },
      strict: true,
    })
    const result = await runFleetDoctor({ pactlineExecutable: parsed.values.pactline })
    if (parsed.values.json) writeJSON(io.stdout, { ok: true, data: result })
    else io.stdout.write(`Pactline Fleet doctor: ok (CLI ${result.pactline.cliVersion}, protocol ${String(result.pactline.protocol)}, ${String(result.pactline.featureCount)} features)\n`)
    return 0
  }
  if (command === 'deepseek-doctor') {
    const parsed = parseArgs({
      args: args.slice(1),
      options: {
        json: { type: 'boolean', default: false },
        'runtime-command': { type: 'string' },
        'runtime-config': { type: 'string' },
      },
      strict: true,
    })
    const result = await runDeepSeekDoctor({
      ...(parsed.values['runtime-command'] === undefined ? {} : { runtimeCommand: parsed.values['runtime-command'] }),
      ...(parsed.values['runtime-config'] === undefined ? {} : { runtimeConfig: parsed.values['runtime-config'] }),
    })
    if (parsed.values.json) writeJSON(io.stdout, { ok: true, data: result })
    else io.stdout.write(`DeepSeek Adapter doctor: ok (${result.route.provider}/${result.route.model}, reasoning ${result.route.reasoning})\n`)
    return 0
  }
  if (command === 'codex-doctor') {
    const parsed = parseArgs({
      args: args.slice(1),
      options: { json: { type: 'boolean', default: false }, 'runtime-command': { type: 'string' } },
      strict: true,
    })
    const result = await runCodexDoctor({
      ...(parsed.values['runtime-command'] === undefined ? {} : { runtimeCommand: parsed.values['runtime-command'] }),
    })
    if (parsed.values.json) writeJSON(io.stdout, { ok: true, data: result })
    else io.stdout.write(`Codex Adapter doctor: ok (${result.route.provider}/${result.route.model}, reasoning ${result.route.reasoning})\n`)
    return 0
  }
  if (command === 'serve') {
    const parsed = parseArgs({
      args: args.slice(1),
      options: {
        config: { type: 'string', default: process.env.PACTLINE_FLEET_CONFIG },
        json: { type: 'boolean', default: false },
        once: { type: 'boolean', default: false },
      },
      strict: true,
    })
    if (parsed.values.config === undefined || parsed.values.config.trim() === '') {
      throw new Error('Fleet Service configuration is required; use --config or PACTLINE_FLEET_CONFIG')
    }
    await runFleetServe({
      configPath: parsed.values.config,
      once: parsed.values.once,
      onStarted: result => {
        if (parsed.values.json) writeJSON(io.stdout, { ok: true, data: result })
        else io.stdout.write(`Pactline Fleet Service ${result.ready ? 'ready' : 'degraded'} at ${result.url}\n`)
      },
    })
    return 0
  }
  io.stderr.write(`Unknown command: ${command}\n${usage()}\n`)
  return 2
}
