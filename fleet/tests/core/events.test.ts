import { describe, expect, it } from 'vitest'
import { HarnessEventCollector } from '../../src/core/events.js'

describe('HarnessEventCollector', () => {
  it('normalizes bounded event and tool summaries', () => {
    const collector = new HarnessEventCollector(3)
    collector.accept({ at: '2026-08-15T00:00:00Z', type: 'tool.completed', tool: 'shell', outcome: 'ok' })
    collector.accept({ at: '2026-08-15T00:00:01Z', type: 'tool.completed', tool: 'shell', outcome: 'error' })
    expect(collector.summary()).toEqual({
      total: 2, byType: { 'tool.completed': 2 }, toolCalls: { shell: 2 }, toolErrors: { shell: 1 },
    })
  })

  it('rejects invalid and unbounded events', () => {
    const collector = new HarnessEventCollector(1)
    expect(() => collector.accept({ at: 'invalid', type: 'event' })).toThrow('invalid')
    collector.accept({ at: '2026-08-15T00:00:00Z', type: 'event' })
    expect(() => collector.accept({ at: '2026-08-15T00:00:01Z', type: 'event' })).toThrow('limit')
  })
})
