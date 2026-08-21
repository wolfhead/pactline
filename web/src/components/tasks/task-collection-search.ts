import {
  TASK_PHASES,
  TASK_PRIORITIES,
  type TaskPhase,
  type TaskPriority,
} from '@/task-types'
import { DEFAULT_FILTERS, type TaskFilters } from './FilterBar'

const PHASES = new Set<string>(TASK_PHASES)
const PRIORITIES = new Set<string>(TASK_PRIORITIES)

export function taskFiltersFromSearchParams(searchParams: URLSearchParams): TaskFilters {
  return {
    phases: searchParams.getAll('phase').filter((value): value is TaskPhase => PHASES.has(value)),
    priorities: searchParams.getAll('priority').filter((value): value is TaskPriority => PRIORITIES.has(value)),
    assignee: searchParams.get('assignee') ?? DEFAULT_FILTERS.assignee,
    labelId: searchParams.get('label') ?? DEFAULT_FILTERS.labelId,
    search: searchParams.get('q') ?? DEFAULT_FILTERS.search,
    sort: searchParams.get('sort') ?? DEFAULT_FILTERS.sort,
    order: searchParams.get('order') === 'asc' ? 'asc' : DEFAULT_FILTERS.order,
  }
}

export function searchParamsWithTaskFilters(
  current: URLSearchParams,
  filters: TaskFilters,
): URLSearchParams {
  const next = new URLSearchParams(current)
  for (const key of ['phase', 'priority', 'assignee', 'label', 'q', 'sort', 'order', 'pages']) {
    next.delete(key)
  }
  for (const phase of filters.phases) next.append('phase', phase)
  for (const priority of filters.priorities) next.append('priority', priority)
  if (filters.assignee) next.set('assignee', filters.assignee)
  if (filters.labelId) next.set('label', filters.labelId)
  if (filters.search) next.set('q', filters.search)
  if (filters.sort !== DEFAULT_FILTERS.sort) next.set('sort', filters.sort)
  if (filters.order !== DEFAULT_FILTERS.order) next.set('order', filters.order)
  return next
}

export function taskPageCountFromSearchParams(searchParams: URLSearchParams): number {
  const value = Number(searchParams.get('pages'))
  return Number.isSafeInteger(value) && value > 1 ? Math.min(value, 20) : 1
}

export function searchParamsWithTaskPageCount(
  current: URLSearchParams,
  pageCount: number,
): URLSearchParams {
  const next = new URLSearchParams(current)
  if (pageCount > 1) next.set('pages', String(pageCount))
  else next.delete('pages')
  return next
}
