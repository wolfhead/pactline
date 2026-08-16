import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { createInterface, type Interface as ReadLineInterface } from 'node:readline'

export interface DeepSeekWireNotification {
  readonly method: string
  readonly params: Record<string, unknown>
}

export interface DeepSeekRuntimeLaunch {
  readonly command: string
  readonly args: readonly string[]
  readonly cwd: string
  readonly env: NodeJS.ProcessEnv
  readonly requestTimeoutMs: number
  readonly shutdownTimeoutMs: number
  readonly terminateTimeoutMs: number
  readonly maxStderrBytes: number
}

interface PendingRequest {
  readonly resolve: (value: unknown) => void
  readonly reject: (error: Error) => void
  readonly timer: NodeJS.Timeout
}

interface NotificationWaiter {
  readonly resolve: (notification: DeepSeekWireNotification) => void
  readonly reject: (error: Error) => void
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function errorMessage(value: unknown): string {
  const item = record(value)
  return typeof item?.message === 'string' && item.message.trim() !== ''
    ? item.message
    : 'DeepSeek Harness returned an unknown JSON-RPC error'
}

/** Minimal caller-owned JSON-RPC transport for the DSH stdio SDK protocol. */
export class DeepSeekJsonRpcRuntime {
  private readonly child: ChildProcessWithoutNullStreams
  private readonly lines: ReadLineInterface
  private readonly pending = new Map<number, PendingRequest>()
  private readonly notifications: DeepSeekWireNotification[] = []
  private readonly waiters: NotificationWaiter[] = []
  private nextRequestId = 1
  private stderrTail = ''
  private terminalError: Error | undefined
  private closeTask: Promise<void> | undefined

  constructor(private readonly launch: DeepSeekRuntimeLaunch) {
    this.child = spawn(launch.command, [...launch.args], {
      cwd: launch.cwd,
      env: launch.env,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    })
    this.lines = createInterface({ input: this.child.stdout, crlfDelay: Number.POSITIVE_INFINITY })
    this.lines.on('line', line => { this.acceptLine(line) })
    this.child.stderr.on('data', (chunk: Buffer | string) => {
      const combined = this.stderrTail + String(chunk)
      this.stderrTail = combined.slice(-this.launch.maxStderrBytes)
    })
    this.child.once('error', error => { this.fail(new Error('DeepSeek Harness runtime failed to start', { cause: error })) })
    this.child.once('exit', (code, signal) => {
      if (this.terminalError !== undefined) return
      const detail = this.stderrTail.trim()
      this.fail(new Error(
        `DeepSeek Harness runtime exited before protocol completion (code=${String(code)}, signal=${String(signal)})`
        + (detail === '' ? '' : `: ${detail}`),
      ))
    })
  }

  get processId(): number | undefined {
    return this.child.pid
  }

  request(method: string, params?: Record<string, unknown>, timeoutMs = this.launch.requestTimeoutMs): Promise<unknown> {
    if (this.terminalError !== undefined) return Promise.reject(this.terminalError)
    const id = this.nextRequestId++
    return new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`DeepSeek Harness JSON-RPC request timed out: ${method}`))
      }, timeoutMs)
      timer.unref()
      this.pending.set(id, { resolve, reject, timer })
      const frame = { jsonrpc: '2.0', id, method, ...(params === undefined ? {} : { params }) }
      this.child.stdin.write(`${JSON.stringify(frame)}\n`, (error) => {
        if (error === null || error === undefined) return
        const entry = this.pending.get(id)
        if (entry === undefined) return
        this.pending.delete(id)
        clearTimeout(entry.timer)
        entry.reject(new Error(`DeepSeek Harness JSON-RPC write failed: ${method}`, { cause: error }))
      })
    })
  }

  nextNotification(): Promise<DeepSeekWireNotification> {
    const available = this.notifications.shift()
    if (available !== undefined) return Promise.resolve(available)
    if (this.terminalError !== undefined) return Promise.reject(this.terminalError)
    return new Promise<DeepSeekWireNotification>((resolve, reject) => { this.waiters.push({ resolve, reject }) })
  }

  close(): Promise<void> {
    this.closeTask ??= this.closeGracefully()
    return this.closeTask
  }

  terminate(reason: string): Promise<void> {
    this.fail(new Error(reason))
    this.closeTask ??= this.terminateProcess()
    return this.closeTask
  }

  private acceptLine(line: string): void {
    let value: unknown
    try {
      value = JSON.parse(line) as unknown
    } catch {
      return
    }
    const frame = record(value)
    if (frame === undefined) return
    if (typeof frame.id === 'number') {
      const pending = this.pending.get(frame.id)
      if (pending === undefined) return
      this.pending.delete(frame.id)
      clearTimeout(pending.timer)
      if (frame.error !== undefined) pending.reject(new Error(errorMessage(frame.error)))
      else pending.resolve(frame.result)
      return
    }
    if (typeof frame.method !== 'string') return
    const notification = { method: frame.method, params: record(frame.params) ?? {} }
    const waiter = this.waiters.shift()
    if (waiter === undefined) this.notifications.push(notification)
    else waiter.resolve(notification)
  }

  private fail(error: Error): void {
    if (this.terminalError !== undefined) return
    this.terminalError = error
    for (const entry of this.pending.values()) {
      clearTimeout(entry.timer)
      entry.reject(error)
    }
    this.pending.clear()
    for (const waiter of this.waiters.splice(0)) waiter.reject(error)
  }

  private async closeGracefully(): Promise<void> {
    if (!this.exited()) {
      try {
        await this.request('shutdown', undefined, this.launch.shutdownTimeoutMs)
      } catch {
        // The bounded termination ladder below remains authoritative.
      }
    }
    await this.terminateProcess()
  }

  private async terminateProcess(): Promise<void> {
    this.lines.close()
    if (this.exited()) return
    this.child.stdin.end()
    if (await this.waitForExit(this.launch.terminateTimeoutMs)) return
    this.child.kill('SIGTERM')
    if (await this.waitForExit(this.launch.terminateTimeoutMs)) return
    this.child.kill('SIGKILL')
    if (!await this.waitForExit(this.launch.terminateTimeoutMs)) {
      throw new Error('DeepSeek Harness runtime did not exit after SIGKILL')
    }
  }

  private exited(): boolean {
    return this.child.exitCode !== null || this.child.signalCode !== null
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
