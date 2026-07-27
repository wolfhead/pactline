import { apiDelete, apiGet, apiPatch, apiPost } from './client'
import type {
  Activity,
  Comment,
  CreateTaskBody,
  Label,
  Task,
  TaskListResponse,
  TaskPatchBody,
  TaskPriority,
  TaskStatus,
} from '../task-types'

export interface TaskListParams {
  status?: TaskStatus[]
  priority?: TaskPriority[]
  // A user UUID, or the literal "none" for "unassigned only" — mirrors
  // store.TaskListFilter.Unassigned overriding AssigneeID.
  assignee?: string
  label?: string[]
  q?: string
  sort?: string
  order?: string
  cursor?: string
  limit?: number
  archived?: string
}

function buildTaskQuery(params: TaskListParams): string {
  const sp = new URLSearchParams()
  for (const s of params.status ?? []) sp.append('status', s)
  for (const p of params.priority ?? []) sp.append('priority', p)
  for (const l of params.label ?? []) sp.append('label', l)
  if (params.assignee) sp.set('assignee', params.assignee)
  if (params.q) sp.set('q', params.q)
  if (params.sort) sp.set('sort', params.sort)
  if (params.order) sp.set('order', params.order)
  if (params.cursor) sp.set('cursor', params.cursor)
  if (params.limit) sp.set('limit', String(params.limit))
  if (params.archived) sp.set('archived', params.archived)
  return sp.toString()
}

export function listTasks(params: TaskListParams = {}): Promise<TaskListResponse> {
  const qs = buildTaskQuery(params)
  return apiGet<TaskListResponse>(`/api/tasks${qs ? `?${qs}` : ''}`)
}

export function getTask(number: number): Promise<Task> {
  return apiGet<Task>(`/api/tasks/${number}`)
}

export function createTask(body: CreateTaskBody): Promise<Task> {
  return apiPost<Task>('/api/tasks', body)
}

export function updateTask(number: number, patch: TaskPatchBody): Promise<Task> {
  return apiPatch<Task>(`/api/tasks/${number}`, patch)
}

export function archiveTask(number: number): Promise<Task> {
  return apiPost<Task>(`/api/tasks/${number}/archive`)
}

export function restoreTask(number: number): Promise<Task> {
  return apiPost<Task>(`/api/tasks/${number}/restore`)
}

export function listComments(number: number): Promise<Comment[]> {
  return apiGet<Comment[]>(`/api/tasks/${number}/comments`)
}

export function createComment(number: number, body: string): Promise<Comment> {
  return apiPost<Comment>(`/api/tasks/${number}/comments`, { body })
}

export function updateComment(number: number, id: string, body: string): Promise<Comment> {
  return apiPatch<Comment>(`/api/tasks/${number}/comments/${id}`, { body })
}

export function deleteComment(number: number, id: string): Promise<void> {
  return apiDelete<void>(`/api/tasks/${number}/comments/${id}`)
}

export function listActivity(number: number): Promise<Activity[]> {
  return apiGet<Activity[]>(`/api/tasks/${number}/activity`)
}

export function listLabels(): Promise<Label[]> {
  return apiGet<Label[]>('/api/labels')
}

export function createLabel(name: string): Promise<Label> {
  return apiPost<Label>('/api/labels', { name })
}

export function renameLabel(id: string, name: string): Promise<Label> {
  return apiPatch<Label>(`/api/labels/${id}`, { name })
}

export function deleteLabel(id: string): Promise<void> {
  return apiDelete<void>(`/api/labels/${id}`)
}
