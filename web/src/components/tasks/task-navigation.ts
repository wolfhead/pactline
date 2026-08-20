import type { Location } from 'react-router-dom'

export const DEFAULT_TASK_SOURCE = '/tasks'

export interface TaskNavigationState {
  taskSource: string
}

const KNOWN_COLLECTION_PATHS = [
  /^\/tasks$/,
  /^\/projects\/[1-9]\d*\/backlog$/,
  /^\/projects\/[1-9]\d*\/milestones\/[A-Za-z0-9-]+$/,
]

export function taskSourceFromLocation(location: Pick<Location, 'pathname' | 'search'>): string {
  const candidate = `${location.pathname}${location.search}`
  return safeTaskSource(candidate) ?? DEFAULT_TASK_SOURCE
}

export function taskSourceFromState(state: unknown): string {
  if (!state || typeof state !== 'object') return DEFAULT_TASK_SOURCE
  const taskSource = (state as { taskSource?: unknown }).taskSource
  return typeof taskSource === 'string'
    ? (safeTaskSource(taskSource) ?? DEFAULT_TASK_SOURCE)
    : DEFAULT_TASK_SOURCE
}

function safeTaskSource(candidate: string): string | null {
  if (!candidate.startsWith('/') || candidate.startsWith('//') || candidate.includes('#')) {
    return null
  }
  const parsed = new URL(candidate, 'https://pactline.invalid')
  if (parsed.origin !== 'https://pactline.invalid') return null
  if (!KNOWN_COLLECTION_PATHS.some((pattern) => pattern.test(parsed.pathname))) return null
  return `${parsed.pathname}${parsed.search}`
}
