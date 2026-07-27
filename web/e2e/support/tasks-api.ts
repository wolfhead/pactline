import { BACKEND_URL } from './config'

/**
 * Thin Node-side REST client for the task-management API (internal/api/
 * task_handler.go, task_comment_handler.go, task_activity_handler.go,
 * label_handler.go), talking to the Go backend directly on :8080 and
 * bypassing the browser. Mirrors e2e/support/api.ts (the bounty-domain
 * client already used by the legacy specs): setup that doesn't need to be
 * driven through the UI goes through here, so each test's actual browser
 * interaction stays focused on the one behaviour under test.
 */

export type TaskStatus = 'backlog' | 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
export type TaskPriority = 'none' | 'low' | 'medium' | 'high' | 'urgent'

export interface UserRef {
  id: string
  name: string
  email: string
}

export interface Label {
  ID: string
  Name: string
  CreatedAt: string
}

export interface Task {
  id: string
  number: number
  title: string
  description: string
  status: TaskStatus
  priority: TaskPriority
  assignee: UserRef | null
  creator: UserRef
  due_date: string | null
  labels: Label[]
  created_at: string
  updated_at: string
  completed_at: string | null
  archived_at: string | null
}

export interface Comment {
  id: string
  task_id: string
  author_id: string
  body: string
  created_at: string
  updated_at: string
}

export interface Activity {
  id: string
  actor_id: string
  field: string
  old_value: string | null
  new_value: string | null
  created_at: string
}

export class ApiRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiRequestError'
  }
}

async function call<T>(userId: string, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BACKEND_URL}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-User-Id': userId },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const parsed: unknown = text ? JSON.parse(text) : null
  if (!res.ok) {
    const errorField = parsed && typeof parsed === 'object' ? (parsed as { error?: unknown }).error : undefined
    const message = typeof errorField === 'string' ? errorField : res.statusText
    throw new ApiRequestError(message, res.status)
  }
  return parsed as T
}

export interface CreateTaskInput {
  title: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  assignee_id?: string | null
  due_date?: string | null
  label_ids?: string[]
}

export function createTask(userId: string, input: CreateTaskInput): Promise<Task> {
  return call<Task>(userId, 'POST', '/api/tasks', input)
}

export function getTask(userId: string, number: number): Promise<Task> {
  return call<Task>(userId, 'GET', `/api/tasks/${number}`)
}

export interface TaskPatchInput {
  title?: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  assignee_id?: string | null
  due_date?: string | null
  label_ids?: string[]
}

export function updateTask(userId: string, number: number, patch: TaskPatchInput): Promise<Task> {
  return call<Task>(userId, 'PATCH', `/api/tasks/${number}`, patch)
}

export function archiveTask(userId: string, number: number): Promise<Task> {
  return call<Task>(userId, 'POST', `/api/tasks/${number}/archive`)
}

export function restoreTask(userId: string, number: number): Promise<Task> {
  return call<Task>(userId, 'POST', `/api/tasks/${number}/restore`)
}

export function listComments(userId: string, number: number): Promise<Comment[]> {
  return call<Comment[]>(userId, 'GET', `/api/tasks/${number}/comments`)
}

export function createComment(userId: string, number: number, body: string): Promise<Comment> {
  return call<Comment>(userId, 'POST', `/api/tasks/${number}/comments`, { body })
}

export function listActivity(userId: string, number: number): Promise<Activity[]> {
  return call<Activity[]>(userId, 'GET', `/api/tasks/${number}/activity`)
}

export function listLabels(userId: string): Promise<Label[]> {
  return call<Label[]>(userId, 'GET', '/api/labels')
}

export function createLabel(userId: string, name: string): Promise<Label> {
  return call<Label>(userId, 'POST', '/api/labels', { name })
}
