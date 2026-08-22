import { describe, expect, it } from 'vitest'
import { aggregateMilestoneProgress } from './MilestoneProgress'
import type { TaskPhase } from '@/task-types'

function task(phase: TaskPhase, archived = false) {
  return { phase, archived_at: archived ? '2026-08-20T00:00:00Z' : null }
}

describe('aggregateMilestoneProgress', () => {
  it.each([
    {
      name: 'an empty milestone',
      tasks: [],
      expected: { eligible: 0, done: 0, inProgress: 0, inReview: 0, waiting: 0, completionPercentage: 0 },
    },
    {
      name: 'a single phase',
      tasks: [task('done'), task('done'), task('done')],
      expected: { eligible: 3, done: 3, inProgress: 0, inReview: 0, waiting: 0, completionPercentage: 100 },
    },
    {
      name: 'mixed phases',
      tasks: [task('done'), task('in_progress'), task('in_review'), task('ready'), task('backlog')],
      expected: { eligible: 5, done: 1, inProgress: 1, inReview: 1, waiting: 2, completionPercentage: 20 },
    },
    {
      name: 'cancelled tasks',
      tasks: [task('done'), task('cancelled'), task('cancelled')],
      expected: { eligible: 1, done: 1, inProgress: 0, inReview: 0, waiting: 0, completionPercentage: 100 },
    },
    {
      name: 'archived tasks',
      tasks: [task('done'), task('in_progress', true), task('cancelled', true)],
      expected: { eligible: 1, done: 1, inProgress: 0, inReview: 0, waiting: 0, completionPercentage: 100 },
    },
    {
      name: 'a rounded integer percentage',
      tasks: [task('done'), task('ready'), task('backlog')],
      expected: { eligible: 3, done: 1, inProgress: 0, inReview: 0, waiting: 2, completionPercentage: 33 },
    },
  ])('aggregates $name', ({ tasks, expected }) => {
    expect(aggregateMilestoneProgress(tasks)).toEqual(expected)
  })
})
