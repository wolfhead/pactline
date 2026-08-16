import { describe, expect, it } from 'vitest'
import { aggregateStageMetrics } from '../../src/evaluation/l2-v2-report.js'

describe('L2 v2 report metrics', () => {
  it('aggregates latency, usage, and tool errors without mixing cached and uncached input', () => {
    expect(aggregateStageMetrics([
      { stage: 'execution', adapterId: 'codex', durationMs: 10, inputTokens: 100, cachedInputTokens: 80, outputTokens: 20, reasoningTokens: 5, toolErrors: 1 },
      { stage: 'review', adapterId: 'deepseek', durationMs: 20, inputTokens: 30, cachedInputTokens: 10, outputTokens: 8, reasoningTokens: 3, toolErrors: 2 },
    ])).toEqual({ sessions: 2, durationMs: 30, inputTokens: 130, cachedInputTokens: 90, outputTokens: 28, reasoningTokens: 8, toolErrors: 3 })
  })

  it('treats unavailable duration and usage as zero while retaining the Session count', () => {
    expect(aggregateStageMetrics([
      { stage: 'execution', adapterId: 'codex', inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningTokens: 0, toolErrors: 0 },
    ])).toMatchObject({ sessions: 1, durationMs: 0, inputTokens: 0 })
  })
})
