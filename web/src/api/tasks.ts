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
  CreateTaskBody,
  Label,
  Task,
  TaskAttachment,
  TaskAttachmentUpload,
  TaskListResponse,
  TaskPatchBody,
  TaskPriority,
  TaskActivityState,
  TaskPhase,
} from '../task-types'
import { v1Upload } from './v1/client'

export interface TaskListParams {
  phase?: TaskPhase[]
  activity?: TaskActivityState[]
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
  project_number?: number
  milestone_id?: string
  creator_id?: string
  backlog_only?: boolean
}

function buildTaskQuery(params: TaskListParams): string {
  const sp = new URLSearchParams()
  for (const phase of params.phase ?? []) sp.append('phase', phase)
  for (const activity of params.activity ?? []) sp.append('activity', activity)
  for (const p of params.priority ?? []) sp.append('priority', p)
  for (const l of params.label ?? []) sp.append('label', l)
  if (params.assignee) sp.set('assignee', params.assignee)
  if (params.q) sp.set('q', params.q)
  if (params.sort) sp.set('sort', params.sort)
  if (params.order) sp.set('order', params.order)
  if (params.cursor) sp.set('cursor', params.cursor)
  if (params.limit) sp.set('limit', String(params.limit))
  if (params.archived) sp.set('archived', params.archived)
  if (params.project_number) sp.set('project_number', String(params.project_number))
  if (params.milestone_id) sp.set('milestone_id', params.milestone_id)
  if (params.creator_id) sp.set('creator_id', params.creator_id)
  if (params.backlog_only) sp.set('backlog_only', 'true')
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

export function listTaskAttachments(number: number): Promise<TaskAttachment[]> {
  return v1Get<{ items: TaskAttachment[] }>(`/api/v1/tasks/${number}/attachments`)
    .then(({ value }) => value.items)
}

export function createTaskAttachmentUpload(
  number: number,
  file: File,
): Promise<TaskAttachmentUpload> {
  return v1Post<TaskAttachmentUpload>(`/api/v1/tasks/${number}/attachments/uploads`, {
    body: {
      filename: file.name,
      media_type: file.type || 'application/octet-stream',
      size_bytes: file.size,
    },
  }).then(({ value }) => value)
}

export function uploadTaskAttachment(upload: TaskAttachmentUpload, file: File): Promise<void> {
  return v1Upload(upload.upload_url, file, upload.headers)
}

export function completeTaskAttachmentUpload(
  number: number,
  uploadID: string,
  taskVersion: number,
): Promise<TaskAttachment> {
  return completeTaskAttachmentUploadVersioned(number, uploadID, taskVersion)
    .then(({ attachment }) => attachment)
}

export interface TaskAttachmentUploadCompletion {
  attachment: TaskAttachment
  taskVersion: number
}

export function completeTaskAttachmentUploadVersioned(
  number: number,
  uploadID: string,
  taskVersion: number,
): Promise<TaskAttachmentUploadCompletion> {
  return v1Post<TaskAttachment>(
    `/api/v1/tasks/${number}/attachments/uploads/${uploadID}/complete`,
    { ifMatch: etagForVersion(taskVersion) },
  ).then((response) => {
    const versioned = requireVersioned(response)
    const matched = /^"([1-9][0-9]*)"$/.exec(versioned.etag)
    const nextTaskVersion = matched ? Number(matched[1]) : NaN
    if (!Number.isSafeInteger(nextTaskVersion)) {
      throw new Error('Attachment completion returned an invalid Task version')
    }
    return { attachment: versioned.value, taskVersion: nextTaskVersion }
  })
}

export function deleteTaskAttachment(
  number: number,
  attachmentID: string,
  version: number,
): Promise<void> {
  return v1Delete(`/api/v1/tasks/${number}/attachments/${attachmentID}`, {
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
