import { BACKEND_URL, WEB_URL } from './config'

/**
 * Thin Node-side REST client for the contract-first work API. It talks to the
 * Go backend directly and bypasses the browser so fixture setup does not hide
 * the UI behavior each scenario is intended to exercise.
 */

export type TaskStatus = 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
export type TaskPriority = 'none' | 'low' | 'medium' | 'high' | 'urgent'

export interface UserRef {
  id: string
  name: string
  email: string
}

export interface Label {
  id: string
  name: string
  version: number
  created_at: string
  updated_at: string
}

export interface Task {
  id: string
  number: number
  version: number
  title: string
  context: string
  expected_result: string
  description: string
  status: TaskStatus
  priority: TaskPriority
  assignee: UserRef | null
  creator: UserRef
  due_date: string | null
  project: { id: string; number: number; name: string }
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

async function call<T>(
  userId: string,
  method: string,
  path: string,
  body?: unknown,
  extraHeaders: Record<string, string> = {},
): Promise<T> {
  void userId
  const session = await developmentSession()
  const headers: Record<string, string> = {
    Cookie: session.cookie,
    Origin: WEB_URL,
    'X-CSRF-Token': session.csrf,
    ...extraHeaders,
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
    const problem = parsed && typeof parsed === 'object'
      ? parsed as { error?: unknown; detail?: unknown; title?: unknown }
      : {}
    const message = [problem.detail, problem.error, problem.title]
      .find((value): value is string => typeof value === 'string') ?? res.statusText
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
  context?: string
  expected_result?: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  assignee_id?: string | null
  due_date?: string | null
  label_ids?: string[]
  project_number?: number
  milestone_id?: string | null
}

export function createTask(userId: string, input: CreateTaskInput): Promise<Task> {
  return call<Task>(userId, 'POST', '/api/v1/tasks', {
    ...input,
    context: input.context ?? 'E2E fixture context',
    expected_result: input.expected_result ?? 'E2E fixture expected result',
  })
}

export function getTask(userId: string, number: number): Promise<Task> {
  return call<Task>(userId, 'GET', `/api/v1/tasks/${number}`)
}

export interface TaskPatchInput {
  title?: string
  context?: string
  expected_result?: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  assignee_id?: string | null
  due_date?: string | null
  label_ids?: string[]
  project_number?: number
  milestone_id?: string | null
}

export async function updateTask(userId: string, number: number, patch: TaskPatchInput): Promise<Task> {
  const current = await getTask(userId, number)
  return call<Task>(userId, 'PATCH', `/api/v1/tasks/${number}`, patch, {
    'If-Match': `"${current.version}"`,
  })
}

export async function archiveTask(userId: string, number: number): Promise<Task> {
  const current = await getTask(userId, number)
  return call<Task>(userId, 'POST', `/api/v1/tasks/${number}/archive`, undefined, {
    'If-Match': `"${current.version}"`,
  })
}

export async function restoreTask(userId: string, number: number): Promise<Task> {
  const current = await getTask(userId, number)
  return call<Task>(userId, 'POST', `/api/v1/tasks/${number}/restore`, undefined, {
    'If-Match': `"${current.version}"`,
  })
}

export async function createTaskCriterion(
  userId: string,
  number: number,
  criterion: string,
  verificationInstructions: string,
  position = 0,
): Promise<AcceptanceCriterion> {
  const current = await getTask(userId, number)
  return call<AcceptanceCriterion>(
    userId,
    'POST',
    `/api/v1/tasks/${number}/criteria`,
    {
      criterion,
      verification_instructions: verificationInstructions,
      position,
    },
    { 'If-Match': `"${current.version}"` },
  )
}

export async function listComments(userId: string, number: number): Promise<Comment[]> {
  const response = await call<{ items: Comment[] }>(userId, 'GET', `/api/v1/tasks/${number}/comments`)
  return response.items
}

export async function createComment(userId: string, number: number, body: string): Promise<Comment> {
  const current = await getTask(userId, number)
  return call<Comment>(userId, 'POST', `/api/v1/tasks/${number}/comments`, { body }, {
    'If-Match': `"${current.version}"`,
  })
}

export async function listActivity(userId: string, number: number): Promise<Activity[]> {
  const response = await call<{ items: Activity[] }>(userId, 'GET', `/api/v1/tasks/${number}/activity`)
  return response.items
}

export async function listLabels(userId: string): Promise<Label[]> {
  const response = await call<{ items: Label[] }>(userId, 'GET', '/api/v1/labels')
  return response.items
}

export function createLabel(userId: string, name: string): Promise<Label> {
  return call<Label>(userId, 'POST', '/api/v1/labels', { name })
}
