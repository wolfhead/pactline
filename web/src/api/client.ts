/**
 * Phase 1 has no authentication. The current identity is carried in an
 * X-User-Id header set by the user switcher. Both disappear when Feishu OAuth
 * lands in Phase 6.
 */
let currentUserId = ''

export function setCurrentUserId(id: string): void {
  currentUserId = id
}

export function getCurrentUserId(): string {
  return currentUserId
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      'X-User-Id': currentUserId,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  const text = await res.text()
  const parsed = text ? JSON.parse(text) : null

  if (!res.ok) {
    const message = parsed && typeof parsed.error === 'string' ? parsed.error : res.statusText
    throw new ApiError(message, res.status)
  }
  return parsed as T
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>('GET', path)
}

export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body)
}
