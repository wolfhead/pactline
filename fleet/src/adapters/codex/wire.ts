import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { createInterface, type Interface as ReadLineInterface } from 'node:readline'

export interface CodexWireEvent extends Record<string, unknown> {
  readonly type: string
}

export interface CodexRuntimeLaunch {
  readonly command: string
  readonly args: readonly string[]
  readonly cwd: string
  readonly env: NodeJS.ProcessEnv
  readonly input: string
  readonly maxLineBytes: number
  readonly maxOutputBytes: number
  readonly maxStderrBytes: number
  readonly exitGraceMs: number
  readonly terminateGraceMs: number
}

interface EventWaiter {
  readonly resolve: (event: CodexWireEvent) => void
  readonly reject: (error: Error) => void
}

interface ExitResult {
  readonly code: number | null
  readonly signal: NodeJS.Signals | null
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function sanitizeDiagnostic(value: string): string {
  return value
    .replace(/\bBearer\s+\S+/gi, 'Bearer [REDACTED]')
    .replace(/\b(?:sk|bb_pat)[-_][A-Za-z0-9._-]{8,}\b/g, '[REDACTED]')
    .replace(/\b(?:token|api[_-]?key|secret|password|authorization)(\s*[=:]\s*)\S+/gi, 'credential$1[REDACTED]')
    .slice(-4_096)
}

function exitError(result: ExitResult, stderr: string, eventDiagnostic = ''): Error {
  const detail = sanitizeDiagnostic(`${stderr.trim()}\n${eventDiagnostic.trim()}`.trim())
  return new Error(
    `Codex runtime exited before successful completion (code=${String(result.code)}, signal=${String(result.signal)})`
    + (detail === '' ? '' : `: ${detail}`),
  )
}

/** Bounded JSONL transport with caller-owned full process-tree cancellation. */
export class CodexExecRuntime {
  private readonly child: ChildProcessWithoutNullStreams
  private readonly lines: ReadLineInterface
  private readonly events: CodexWireEvent[] = []
  private readonly waiters: EventWaiter[] = []
  private readonly exitedTask: Promise<ExitResult>
  private exitResult: ExitResult | undefined
  private terminalError: Error | undefined
  private stderrTail = ''
  private eventDiagnostic = ''
  private stdoutBytes = 0
  private terminateTask: Promise<void> | undefined

  constructor(private readonly launch: CodexRuntimeLaunch) {
    this.child = spawn(launch.command, [...launch.args], {
      cwd: launch.cwd,
      env: launch.env,
      detached: process.platform !== 'win32',
      windowsHide: true,
      stdio: ['pipe', 'pipe', 'pipe'],
    })
    this.lines = createInterface({ input: this.child.stdout, crlfDelay: Number.POSITIVE_INFINITY })
    this.exitedTask = new Promise(resolve => {
      this.child.once('exit', (code, signal) => {
        const result = { code, signal }
        this.exitResult = result
        if ((code !== 0 || signal !== null) && this.terminalError === undefined) {
          this.fail(exitError(result, this.stderrTail, this.eventDiagnostic))
        } else if (this.events.length === 0 && this.waiters.length > 0) {
          this.fail(new Error('Codex runtime exited before another JSONL event'))
        }
        resolve(result)
      })
    })
    this.child.stdout.on('data', (chunk: Buffer | string) => {
      this.stdoutBytes += Buffer.byteLength(chunk)
      if (this.stdoutBytes > this.launch.maxOutputBytes) {
        this.failAndTerminate(new Error(`Codex JSONL output exceeded ${String(this.launch.maxOutputBytes)} bytes`))
      }
    })
    this.lines.on('line', line => { this.acceptLine(line) })
    this.child.stderr.on('data', (chunk: Buffer | string) => {
      const combined = this.stderrTail + String(chunk)
      this.stderrTail = combined.slice(-this.launch.maxStderrBytes)
    })
    this.child.once('error', error => {
      this.fail(new Error('Codex runtime failed to start', { cause: error }))
    })
    this.child.stdin.end(launch.input)
  }

  get processId(): number | undefined {
    return this.child.pid
  }

  nextEvent(): Promise<CodexWireEvent> {
    const event = this.events.shift()
    if (event !== undefined) return Promise.resolve(event)
    if (this.terminalError !== undefined) return Promise.reject(this.terminalError)
    if (this.exitResult !== undefined) return Promise.reject(new Error('Codex runtime exited before another JSONL event'))
    return new Promise<CodexWireEvent>((resolve, reject) => { this.waiters.push({ resolve, reject }) })
  }

  async waitForSuccessfulExit(): Promise<void> {
    const result = await this.exitedTask
    if (this.terminalError !== undefined) throw this.terminalError
    if (result.code !== 0 || result.signal !== null) throw exitError(result, this.stderrTail, this.eventDiagnostic)
  }

  close(): Promise<void> {
    this.terminateTask ??= this.closeProcess()
    return this.terminateTask
  }

  terminate(reason: string): Promise<void> {
    this.fail(new Error(reason))
    this.terminateTask ??= this.terminateProcessTree()
    return this.terminateTask
  }

  private acceptLine(line: string): void {
    if (this.terminalError !== undefined || line.trim() === '') return
    if (Buffer.byteLength(line) > this.launch.maxLineBytes) {
      this.failAndTerminate(new Error(`Codex JSONL line exceeded ${String(this.launch.maxLineBytes)} bytes`))
      return
    }
    let value: unknown
    try {
      value = JSON.parse(line) as unknown
    } catch {
      this.failAndTerminate(new Error('Codex runtime emitted invalid JSONL'))
      return
    }
    const item = record(value)
    if (item === undefined || typeof item.type !== 'string' || item.type.trim() === '') {
      this.failAndTerminate(new Error('Codex runtime emitted an invalid event'))
      return
    }
    const event = item as CodexWireEvent
    if (event.type === 'error') {
      const message = typeof event.message === 'string' ? event.message : JSON.stringify(event)
      this.eventDiagnostic = sanitizeDiagnostic(`${this.eventDiagnostic}\n${message}`).slice(-4_096)
    }
    const waiter = this.waiters.shift()
    if (waiter === undefined) this.events.push(event)
    else waiter.resolve(event)
  }

  private failAndTerminate(error: Error): void {
    this.fail(error)
    this.terminateTask ??= this.terminateProcessTree()
    void this.terminateTask.catch(() => undefined)
  }

  private fail(error: Error): void {
    if (this.terminalError !== undefined) return
    this.terminalError = error
    for (const waiter of this.waiters.splice(0)) waiter.reject(error)
  }

  private async closeProcess(): Promise<void> {
    if (this.exited()) {
      await this.waitForSuccessfulExit()
      return
    }
    if (await this.waitForExit(this.launch.exitGraceMs)) {
      await this.waitForSuccessfulExit()
      return
    }
    await this.terminateProcessTree()
  }

  private async terminateProcessTree(): Promise<void> {
    this.lines.close()
    this.child.stdin.end()
    if (this.exited()) return
    await this.signalTree('SIGTERM')
    if (await this.waitForExit(this.launch.terminateGraceMs)) return
    await this.signalTree('SIGKILL')
    if (!await this.waitForExit(this.launch.terminateGraceMs)) {
      throw new Error('Codex runtime process tree did not exit after SIGKILL')
    }
  }

  private async signalTree(signal: NodeJS.Signals): Promise<void> {
    const pid = this.child.pid
    if (pid === undefined || this.exited()) return
    if (process.platform === 'win32') {
      await new Promise<void>((resolve) => {
        const killer = spawn('taskkill', ['/PID', String(pid), '/T', '/F'], {
          stdio: 'ignore', windowsHide: true,
        })
        killer.once('error', () => { resolve() })
        killer.once('exit', () => { resolve() })
      })
      return
    }
    try {
      process.kill(-pid, signal)
    } catch (error: unknown) {
      if ((error as NodeJS.ErrnoException).code !== 'ESRCH') this.child.kill(signal)
    }
  }

  private exited(): boolean {
    return this.exitResult !== undefined || this.child.exitCode !== null || this.child.signalCode !== null
  }

  private waitForExit(timeoutMs: number): Promise<boolean> {
    if (this.exited()) return Promise.resolve(true)
    return new Promise<boolean>((resolve) => {
      const onExit = (): void => {
        clearTimeout(timer)
        resolve(true)
      }
      const timer = setTimeout(() => {
        this.child.off('exit', onExit)
        resolve(false)
      }, timeoutMs)
      timer.unref()
      this.child.once('exit', onExit)
    })
  }
}
