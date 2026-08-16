import { sanitizeHealthDiagnostic } from '../health/store.js'

export type FleetLogLevel = 'debug' | 'info' | 'warn' | 'error'

export interface FleetLogger {
  log(level: FleetLogLevel, event: string, fields?: Readonly<Record<string, unknown>>): void
}

export interface FleetLogTarget {
  write(value: string): unknown
}

function safeValue(value: unknown, depth = 0): unknown {
  if (depth > 4) return '[TRUNCATED]'
  if (typeof value === 'string') return sanitizeHealthDiagnostic(value)
  if (Array.isArray(value)) return value.slice(0, 100).map(item => safeValue(item, depth + 1))
  if (typeof value !== 'object' || value === null) return value
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .filter(([key]) => !/(token|secret|password|credential|authorization|cookie)/i.test(key))
      .slice(0, 100)
      .map(([key, child]) => [key, safeValue(child, depth + 1)]),
  )
}

export class JSONFleetLogger implements FleetLogger {
  constructor(
    private readonly target: FleetLogTarget = process.stderr,
    private readonly now: () => Date = () => new Date(),
  ) {}

  log(level: FleetLogLevel, event: string, fields: Readonly<Record<string, unknown>> = {}): void {
    this.target.write(`${JSON.stringify({
      at: this.now().toISOString(),
      level,
      event,
      ...safeValue(fields) as Record<string, unknown>,
    })}\n`)
  }
}

export class NullFleetLogger implements FleetLogger {
  log(_level: FleetLogLevel, _event: string, _fields?: Readonly<Record<string, unknown>>): void {}
}
