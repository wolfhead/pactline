import type { HarnessRunEvent } from './harness-adapter.js'
import type { HarnessEventSummary } from './harness-result.js'

/** Bounded aggregation of runtime-neutral operational events. */
export class HarnessEventCollector {
  private total = 0
  private readonly byType = new Map<string, number>()
  private readonly toolCalls = new Map<string, number>()
  private readonly toolErrors = new Map<string, number>()

  constructor(private readonly maxEvents = 10_000) {}

  accept(event: HarnessRunEvent): void {
    if (this.total >= this.maxEvents) throw new Error('Harness event limit exceeded')
    if (!Number.isFinite(Date.parse(event.at)) || event.type.trim() === '') throw new Error('Harness event is invalid')
    this.total += 1
    this.increment(this.byType, event.type)
    if (event.tool !== undefined) {
      this.increment(this.toolCalls, event.tool)
      if (event.outcome === 'error') this.increment(this.toolErrors, event.tool)
    }
  }

  summary(): HarnessEventSummary {
    return {
      total: this.total,
      byType: Object.fromEntries([...this.byType].sort()),
      toolCalls: Object.fromEntries([...this.toolCalls].sort()),
      toolErrors: Object.fromEntries([...this.toolErrors].sort()),
    }
  }

  private increment(target: Map<string, number>, key: string): void {
    target.set(key, (target.get(key) ?? 0) + 1)
  }
}
