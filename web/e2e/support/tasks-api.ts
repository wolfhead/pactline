import { BACKEND_URL, WEB_URL } from './config'

/**
 * Thin Node-side REST client for the task-management API (internal/api/
 * task_handler.go, task_comment_handler.go, task_activity_handler.go,
 * label_handler.go), talking to the Go backend directly on :8080 and
 * bypassing the browser: setup that doesn't need to be driven through the
 * UI goes through here, so each test's actual browser interaction stays
 * focused on the one behaviour under test.
 */

export type TaskStatus = 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
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
  project: { id: string; number: number; name: string } | null
  milestone: { id: string; name: string } | null
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

export interface AcceptanceCriterion {
  id: string
  criterion: string
  verification_instructions: string
  revision: number
  position: number
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
  void userId
  const session = await developmentSession()
  const headers: Record<string, string> = {
    Cookie: session.cookie,
    Origin: WEB_URL,
    'X-CSRF-Token': session.csrf,
  }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(`${BACKEND_URL}${path}`, {
    method,
    headers,
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

export interface DevelopmentSession {
  cookie: string
  session: string
  csrf: string
}

let sessionPromise: Promise<DevelopmentSession> | null = null

export function developmentSession(): Promise<DevelopmentSession> {
  if (sessionPromise) return sessionPromise
  sessionPromise = fetch(`${BACKEND_URL}/api/auth/dev/session`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user_id: '00000000-0000-0000-0000-000000000001' }),
  }).then(async (response) => {
    if (!response.ok) throw new ApiRequestError(await response.text(), response.status)
    const setCookies = response.headers.getSetCookie()
    const sessionCookie = setCookies.find((value) => value.startsWith('bb_session='))
    const csrfCookie = setCookies.find((value) => value.startsWith('bb_csrf='))
    if (!sessionCookie || !csrfCookie) throw new Error('development session cookies are missing')
    const csrf = csrfCookie.split(';', 1)[0].slice('bb_csrf='.length)
    return {
      cookie: `${sessionCookie.split(';', 1)[0]}; ${csrfCookie.split(';', 1)[0]}`,
      session: sessionCookie.split(';', 1)[0].slice('bb_session='.length),
      csrf,
    }
  }).catch((error) => {
    sessionPromise = null
    throw error
  })
  return sessionPromise
}

export interface CreateTaskInput {
  title: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  assignee_id?: string | null
  due_date?: string | null
  label_ids?: string[]
  project_number?: number | null
  milestone_id?: string | null
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
  project_number?: number | null
  milestone_id?: string | null
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

export function createTaskCriterion(
  userId: string,
  number: number,
  criterion: string,
  verificationInstructions: string,
  position = 0,
): Promise<AcceptanceCriterion> {
  return call<AcceptanceCriterion>(
    userId,
    'POST',
    `/api/tasks/${number}/acceptance-criteria`,
    {
      criterion,
      verification_instructions: verificationInstructions,
      position,
    },
  )
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
