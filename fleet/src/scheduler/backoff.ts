export interface FleetBackoffOptions {
  readonly baseMs?: number
  readonly maximumMs?: number
  readonly jitterRatio?: number
  readonly random?: () => number
}

/** Per-Fleet bounded exponential backoff with symmetric jitter. */
export class FleetBackoff {
  private failures = 0
  private readonly baseMs: number
  private readonly maximumMs: number
  private readonly jitterRatio: number
  private readonly random: () => number

  constructor(options: FleetBackoffOptions = {}) {
    this.baseMs = options.baseMs ?? 1_000
    this.maximumMs = options.maximumMs ?? 60_000
    this.jitterRatio = options.jitterRatio ?? 0.2
    this.random = options.random ?? Math.random
    if (this.baseMs < 1 || this.maximumMs < this.baseMs) throw new Error('Backoff bounds are invalid')
    if (this.jitterRatio < 0 || this.jitterRatio > 1) throw new Error('Backoff jitter ratio must be between zero and one')
  }

  success(): void { this.failures = 0 }

  fail(nowMs: number): number {
    const exponent = Math.min(this.failures, 30)
    this.failures += 1
    const bounded = Math.min(this.maximumMs, this.baseMs * (2 ** exponent))
    const jitter = bounded * this.jitterRatio * ((this.random() * 2) - 1)
    return nowMs + Math.max(1, Math.round(bounded + jitter))
  }
}
