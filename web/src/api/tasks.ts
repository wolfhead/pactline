import {
  etagForVersion,
  requireVersioned,
  v1Delete,
  v1Get,
  v1Patch,
  v1Post,
} from './v1/client'
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
  return v1Get<Omit<TaskListResponse, 'has_more'>>(
    `/api/v1/tasks${qs ? `?${qs}` : ''}`,
  ).then(({ value }) => ({ ...value, has_more: Boolean(value.next_cursor) }))
}

export function getTask(number: number): Promise<Task> {
  return v1Get<Task>(`/api/v1/tasks/${number}`)
    .then((response) => requireVersioned(response).value)
}

export function createTask(body: CreateTaskBody): Promise<Task> {
  return v1Post<Task>('/api/v1/tasks', { body })
    .then((response) => requireVersioned(response).value)
}

export function updateTask(
  number: number,
  version: number,
  patch: TaskPatchBody,
): Promise<Task> {
  return v1Patch<Task>(`/api/v1/tasks/${number}`, {
    ifMatch: etagForVersion(version), body: patch,
  }).then((response) => requireVersioned(response).value)
}

export function archiveTask(number: number, version: number): Promise<Task> {
  return v1Post<Task>(`/api/v1/tasks/${number}/archive`, {
    ifMatch: etagForVersion(version),
  }).then((response) => requireVersioned(response).value)
}

export function restoreTask(number: number, version: number): Promise<Task> {
  return v1Post<Task>(`/api/v1/tasks/${number}/restore`, {
    ifMatch: etagForVersion(version),
  }).then((response) => requireVersioned(response).value)
}

export function listComments(number: number): Promise<Comment[]> {
  return v1Get<{ items: Comment[] }>(`/api/v1/tasks/${number}/comments`)
    .then(({ value }) => value.items)
}

export function createComment(
  number: number,
  taskVersion: number,
  body: string,
): Promise<Comment> {
  return v1Post<Comment>(`/api/v1/tasks/${number}/comments`, {
    ifMatch: etagForVersion(taskVersion), body: { body },
  }).then((response) => requireVersioned(response).value)
}

export function updateComment(
  number: number,
  id: string,
  version: number,
  body: string,
): Promise<Comment> {
  return v1Patch<Comment>(`/api/v1/tasks/${number}/comments/${id}`, {
    ifMatch: etagForVersion(version), body: { body },
  }).then((response) => requireVersioned(response).value)
}

export function deleteComment(number: number, id: string, version: number): Promise<void> {
  return v1Delete(`/api/v1/tasks/${number}/comments/${id}`, {
    ifMatch: etagForVersion(version),
  }).then(() => undefined)
}

export function listActivity(number: number): Promise<Activity[]> {
  return v1Get<{ items: Activity[] }>(`/api/v1/tasks/${number}/activity`)
    .then(({ value }) => value.items)
}

export function listLabels(): Promise<Label[]> {
  return v1Get<{ items: Label[] }>('/api/v1/labels')
    .then(({ value }) => value.items)
}

export function createLabel(name: string): Promise<Label> {
  return v1Post<Label>('/api/v1/labels', { body: { name } })
    .then((response) => requireVersioned(response).value)
}

export function renameLabel(id: string, version: number, name: string): Promise<Label> {
  return v1Patch<Label>(`/api/v1/labels/${id}`, {
    ifMatch: etagForVersion(version), body: { name },
  }).then((response) => requireVersioned(response).value)
}

export function deleteLabel(id: string, version: number): Promise<void> {
  return v1Delete(`/api/v1/labels/${id}`, {
    ifMatch: etagForVersion(version),
  }).then(() => undefined)
}
